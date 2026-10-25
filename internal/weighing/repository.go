package weighing

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/mutation"
	"github.com/banksampahku/backend/internal/utils"
)

// ErrNotFound dikembalikan jika penimbangan tidak ditemukan (atau bukan milik sekolah/nasabah)
var ErrNotFound = errors.New("penimbangan tidak ditemukan")

// Repository menangani akses data weighings & weighing_items
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository penimbangan
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB mengembalikan koneksi untuk memulai transaksi dari service
func (r *Repository) DB(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// ---------- Nasabah & jenis sampah ----------

// FindNasabah mencari nasabah di sekolah admin berdasarkan ID atau kode QR
func (r *Repository) FindNasabah(ctx context.Context, schoolID *string, nasabahID, qrCode string) (*auth.User, error) {
	q := r.db.WithContext(ctx).Model(&auth.User{}).
		Where("role = ? AND school_id IS NOT DISTINCT FROM ?", auth.RoleNasabah, schoolID)
	if nasabahID != "" {
		q = q.Where("id = ?", nasabahID)
	} else {
		q = q.Where("qr_code = ?", qrCode)
	}

	var u auth.User
	err := q.Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNasabahNotFound
	}
	return &u, err
}

// lockNasabahTx mengunci baris nasabah (SELECT ... FOR UPDATE) sampai transaksi selesai
// dan mengembalikan status terbarunya
func lockNasabahTx(tx *gorm.DB, nasabahID string) (string, error) {
	var statuses []string
	if err := tx.Raw(`SELECT status FROM users WHERE id = ? FOR UPDATE`, nasabahID).Scan(&statuses).Error; err != nil {
		return "", err
	}
	if len(statuses) == 0 {
		return "", ErrNasabahNotFound
	}
	return statuses[0], nil
}

// wasteTypeInfo adalah data jenis sampah yang dibutuhkan saat penimbangan
type wasteTypeInfo struct {
	ID             string `gorm:"column:id"`
	Name           string `gorm:"column:name"`
	Unit           string `gorm:"column:unit"`
	IsActive       bool   `gorm:"column:is_active"`
	CategoryName   string `gorm:"column:category_name"`
	CategoryActive bool   `gorm:"column:category_active"`
}

// loadWasteTypes mengambil jenis sampah (milik sekolah admin) berdasarkan daftar ID
func loadWasteTypes(tx *gorm.DB, schoolID *string, ids []string) (map[string]wasteTypeInfo, error) {
	var rows []wasteTypeInfo
	err := tx.Raw(`
		SELECT t.id, t.name, t.unit, t.is_active, c.name AS category_name, c.is_active AS category_active
		FROM waste_types t JOIN waste_categories c ON c.id = t.category_id
		WHERE t.id IN ? AND c.school_id IS NOT DISTINCT FROM ?`, ids, schoolID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]wasteTypeInfo, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// ---------- Insert (dipanggil di dalam DB transaction) ----------

// header adalah data baris weighings yang akan disimpan
type header struct {
	NasabahID       string
	AdminID         string
	SchoolID        *string
	TransactionDate string // YYYY-MM-DD
	TotalAmount     int64
	TotalWeight     string // NUMERIC(10,2) dalam bentuk teks, misal "3.50"
	Note            *string
	IdempotencyKey  *string // UUID dari header Idempotency-Key (nil jika tidak dikirim)
}

// item adalah data baris weighing_items yang akan disimpan
type item struct {
	WasteTypeID  string
	Weight       string // NUMERIC(10,2) dalam bentuk teks
	PricePerUnit int64  // snapshot harga
	Subtotal     int64
}

// insertWeighing menyimpan header + seluruh item; mengembalikan ID penimbangan
func insertWeighing(tx *gorm.DB, h header, items []item) (string, error) {
	var created struct{ ID string }
	err := tx.Raw(`
		INSERT INTO weighings (nasabah_id, admin_id, school_id, transaction_date, total_amount, total_weight, note, idempotency_key)
		VALUES (?, ?, ?, ?::date, ?, ?::numeric, ?, ?::uuid)
		RETURNING id`,
		h.NasabahID, h.AdminID, h.SchoolID, h.TransactionDate, h.TotalAmount, h.TotalWeight, h.Note, h.IdempotencyKey,
	).Scan(&created).Error
	if err != nil {
		return "", err
	}

	for _, it := range items {
		if err := tx.Exec(`
			INSERT INTO weighing_items (weighing_id, waste_type_id, weight, price_per_unit, subtotal)
			VALUES (?, ?, ?::numeric, ?, ?)`,
			created.ID, it.WasteTypeID, it.Weight, it.PricePerUnit, it.Subtotal,
		).Error; err != nil {
			return "", err
		}
	}
	return created.ID, nil
}

// previousWeighing adalah penimbangan yang sudah tercatat dengan Idempotency-Key yang sama
type previousWeighing struct {
	ID        string `gorm:"column:id"`
	NasabahID string `gorm:"column:nasabah_id"`
}

// findByIdempotencyKeyTx mencari penimbangan admin dengan Idempotency-Key tertentu (nil jika belum ada)
func findByIdempotencyKeyTx(tx *gorm.DB, adminID, key string) (*previousWeighing, error) {
	var rows []previousWeighing
	err := tx.Raw(`SELECT id, nasabah_id FROM weighings WHERE admin_id = ? AND idempotency_key = ?::uuid`,
		adminID, key).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// creditRow adalah mutasi saldo "credit" hasil satu penimbangan
type creditRow struct {
	ID           string `gorm:"column:id"`
	Amount       int64  `gorm:"column:amount"`
	BalanceAfter int64  `gorm:"column:balance_after"`
}

// FindCredit mengambil perubahan saldo akibat penimbangan (untuk respons request ulang)
func (r *Repository) FindCredit(ctx context.Context, weighingID string) (*BalanceChange, error) {
	var rows []creditRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, amount, balance_after FROM balance_mutations
		WHERE reference_type = ? AND reference_id = ?`, mutation.RefWeighing, weighingID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	c := rows[0]
	return &BalanceChange{Before: c.BalanceAfter - c.Amount, Credit: c.Amount, After: c.BalanceAfter, MutationID: c.ID}, nil
}

// ---------- Query daftar & detail ----------

// summaryRow adalah hasil query daftar penimbangan (flat)
type summaryRow struct {
	ID              string    `gorm:"column:id"`
	TransactionDate time.Time `gorm:"column:transaction_date"`
	TotalAmount     int64     `gorm:"column:total_amount"`
	TotalWeight     float64   `gorm:"column:total_weight"`
	Note            *string   `gorm:"column:note"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	NasabahID       string    `gorm:"column:nasabah_id"`
	NasabahName     string    `gorm:"column:nasabah_name"`
	NasabahClass    *string   `gorm:"column:nasabah_class"`
	NasabahQR       *string   `gorm:"column:nasabah_qr"`
	AdminID         string    `gorm:"column:admin_id"`
	AdminName       string    `gorm:"column:admin_name"`
	ItemCount       int64     `gorm:"column:item_count"`
}

func (r summaryRow) toSummary() Summary {
	return Summary{
		ID:              r.ID,
		TransactionDate: r.TransactionDate.Format(DateLayout),
		Nasabah:         PersonRef{ID: r.NasabahID, FullName: r.NasabahName, ClassName: r.NasabahClass, QRCode: r.NasabahQR},
		Admin:           PersonRef{ID: r.AdminID, FullName: r.AdminName},
		TotalWeight:     r.TotalWeight,
		TotalAmount:     r.TotalAmount,
		ItemCount:       r.ItemCount,
		Note:            r.Note,
		CreatedAt:       r.CreatedAt,
	}
}

// filter adalah kriteria pencarian daftar penimbangan
type filter struct {
	SchoolID  *string
	NasabahID string // jika diisi, hanya penimbangan nasabah ini
	Search    string
	DateFrom  string
	DateTo    string
}

func (r *Repository) base(ctx context.Context, f filter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("weighings AS w").
		Joins("JOIN users n ON n.id = w.nasabah_id").
		Joins("JOIN users a ON a.id = w.admin_id").
		Where("w.school_id IS NOT DISTINCT FROM ?", f.SchoolID)
	if f.NasabahID != "" {
		q = q.Where("w.nasabah_id = ?", f.NasabahID)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + utils.EscapeLike(s) + "%"
		q = q.Where("(n.full_name ILIKE ? OR n.qr_code ILIKE ?)", like, like)
	}
	if f.DateFrom != "" {
		q = q.Where("w.transaction_date >= ?::date", f.DateFrom)
	}
	if f.DateTo != "" {
		q = q.Where("w.transaction_date <= ?::date", f.DateTo)
	}
	return q
}

const summaryColumns = `w.id, w.transaction_date, w.total_amount, w.total_weight, w.note, w.created_at,
	n.id AS nasabah_id, n.full_name AS nasabah_name, n.class_name AS nasabah_class, n.qr_code AS nasabah_qr,
	a.id AS admin_id, a.full_name AS admin_name,
	(SELECT COUNT(*) FROM weighing_items wi WHERE wi.weighing_id = w.id) AS item_count`

// List mengambil daftar penimbangan (terbaru di atas) dengan pagination
func (r *Repository) List(ctx context.Context, f filter, page utils.PageQuery) ([]Summary, int64, error) {
	var total int64
	if err := r.base(ctx, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []summaryRow
	err := r.base(ctx, f).Select(summaryColumns).
		Order("w.transaction_date DESC, w.created_at DESC").
		Limit(page.Limit).Offset(page.Offset()).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	out := make([]Summary, len(rows))
	for i, row := range rows {
		out[i] = row.toSummary()
	}
	return out, total, nil
}

// FindDetail mengambil satu penimbangan beserta seluruh itemnya
func (r *Repository) FindDetail(ctx context.Context, f filter, id string) (*Detail, error) {
	var rows []summaryRow
	if err := r.base(ctx, f).Select(summaryColumns).Where("w.id = ?", id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}

	var items []ItemResponse
	err := r.db.WithContext(ctx).Raw(`
		SELECT wi.id, wi.waste_type_id, t.name AS waste_type_name, c.name AS category_name, t.unit,
			wi.weight, wi.price_per_unit, wi.subtotal
		FROM weighing_items wi
		JOIN waste_types t ON t.id = wi.waste_type_id
		JOIN waste_categories c ON c.id = t.category_id
		WHERE wi.weighing_id = ?
		ORDER BY c.name, t.name`, id,
	).Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return &Detail{Summary: rows[0].toSummary(), Items: items}, nil
}
