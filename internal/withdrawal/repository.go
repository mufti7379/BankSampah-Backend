package withdrawal

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/utils"
)

// Repository menangani akses data withdrawal_requests
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository penarikan
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB mengembalikan koneksi untuk memulai transaksi dari service
func (r *Repository) DB(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// =====================================================================
// Dipanggil di dalam DB transaction (mengunci baris)
// =====================================================================

// nasabahRow adalah data nasabah yang dikunci saat membuat pengajuan
type nasabahRow struct {
	Status   string
	Balance  int64
	SchoolID *string
	FullName string
}

// lockNasabahTx mengunci baris nasabah agar saldo & pengajuan aktif dicek secara aman
func lockNasabahTx(tx *gorm.DB, nasabahID string) (*nasabahRow, error) {
	var rows []nasabahRow
	err := tx.Raw(`SELECT status, balance, school_id, full_name FROM users
		WHERE id = ? AND role = 'nasabah' FOR UPDATE`, nasabahID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// activeRequestTx mengembalikan ID pengajuan yang masih diproses (pending/approved), jika ada
func activeRequestTx(tx *gorm.DB, nasabahID string) (string, error) {
	var ids []string
	err := tx.Raw(`SELECT id FROM withdrawal_requests
		WHERE nasabah_id = ? AND status IN ('pending', 'approved') LIMIT 1`, nasabahID).Scan(&ids).Error
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// insertTx menyimpan pengajuan baru berstatus pending; mengembalikan ID-nya
func insertTx(tx *gorm.DB, nasabahID string, amount int64) (string, error) {
	var created struct{ ID string }
	err := tx.Raw(`INSERT INTO withdrawal_requests (nasabah_id, amount, method, status)
		VALUES (?, ?, ?, 'pending') RETURNING id`, nasabahID, amount, MethodCash).Scan(&created).Error
	return created.ID, err
}

// lockedRequest adalah pengajuan yang sedang dikunci untuk diproses
type lockedRequest struct {
	ID          string
	NasabahID   string
	NasabahName string
	SchoolID    *string
	Amount      int64
	Status      string
}

// lockRequestTx mengunci satu pengajuan. Batasi dengan schoolID (admin) atau nasabahID (nasabah).
func lockRequestTx(tx *gorm.DB, id string, schoolID *string, nasabahID string) (*lockedRequest, error) {
	q := `SELECT w.id, w.nasabah_id, n.full_name AS nasabah_name, n.school_id, w.amount, w.status
		FROM withdrawal_requests w JOIN users n ON n.id = w.nasabah_id
		WHERE w.id = ? AND n.school_id IS NOT DISTINCT FROM ?`
	args := []any{id, schoolID}
	if nasabahID != "" {
		q += ` AND w.nasabah_id = ?`
		args = append(args, nasabahID)
	}
	q += ` FOR UPDATE OF w`

	var rows []lockedRequest
	if err := tx.Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// updateStatusTx mengubah status + mencatat siapa (verified_by), kapan, dan catatannya
func updateStatusTx(tx *gorm.DB, id, status string, verifiedBy *string, note string) error {
	return tx.Exec(`UPDATE withdrawal_requests
		SET status = ?, verified_by = ?, verified_at = NOW(), note = ?
		WHERE id = ?`, status, verifiedBy, note, id).Error
}

// =====================================================================
// Query baca
// =====================================================================

// filter adalah kriteria daftar pengajuan
type filter struct {
	SchoolID  *string
	NasabahID string
	Status    string
	Search    string
	From      *time.Time // inklusif
	To        *time.Time // eksklusif
}

func (r *Repository) base(ctx context.Context, f filter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("withdrawal_requests AS w").
		Joins("JOIN users n ON n.id = w.nasabah_id").
		Where("n.school_id IS NOT DISTINCT FROM ?", f.SchoolID)
	if f.NasabahID != "" {
		q = q.Where("w.nasabah_id = ?", f.NasabahID)
	}
	if f.Status != "" {
		q = q.Where("w.status = ?", f.Status)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + utils.EscapeLike(s) + "%"
		q = q.Where("(n.full_name ILIKE ? OR n.qr_code ILIKE ?)", like, like)
	}
	if f.From != nil {
		q = q.Where("w.requested_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("w.requested_at < ?", *f.To)
	}
	return q
}

// itemRow adalah hasil query daftar/detail (flat)
type itemRow struct {
	ID           string     `gorm:"column:id"`
	Amount       int64      `gorm:"column:amount"`
	Method       string     `gorm:"column:method"`
	Status       string     `gorm:"column:status"`
	Note         *string    `gorm:"column:note"`
	RequestedAt  time.Time  `gorm:"column:requested_at"`
	VerifiedAt   *time.Time `gorm:"column:verified_at"`
	VerifierID   *string    `gorm:"column:verifier_id"`
	VerifierName *string    `gorm:"column:verifier_name"`
	NasabahID    string     `gorm:"column:nasabah_id"`
	NasabahName  string     `gorm:"column:nasabah_name"`
	NasabahClass *string    `gorm:"column:nasabah_class"`
	NasabahQR    *string    `gorm:"column:nasabah_qr"`
	NasabahBal   int64      `gorm:"column:nasabah_balance"`
	MutationID   *string    `gorm:"column:mutation_id"`
	BalanceAfter *int64     `gorm:"column:balance_after"`
}

const itemColumns = `w.id, w.amount, w.method, w.status, w.note, w.requested_at, w.verified_at,
	v.id AS verifier_id, v.full_name AS verifier_name,
	n.id AS nasabah_id, n.full_name AS nasabah_name, n.class_name AS nasabah_class,
	n.qr_code AS nasabah_qr, n.balance AS nasabah_balance,
	m.id AS mutation_id, m.balance_after`

func withDetails(q *gorm.DB) *gorm.DB {
	return q.Select(itemColumns).
		Joins("LEFT JOIN users v ON v.id = w.verified_by").
		Joins("LEFT JOIN balance_mutations m ON m.reference_type = 'withdrawal' AND m.reference_id = w.id")
}

func (row itemRow) toItem(withNasabah bool) Item {
	it := Item{
		ID: row.ID, Amount: row.Amount, Method: row.Method,
		Status: row.Status, StatusLabel: statusLabels[row.Status], Note: row.Note,
		RequestedAt: row.RequestedAt, VerifiedAt: row.VerifiedAt,
		MutationID: row.MutationID, BalanceAfter: row.BalanceAfter,
		CanCancel: row.Status == StatusPending,
	}
	if row.VerifierID != nil && row.VerifierName != nil {
		it.VerifiedBy = &VerifierRef{ID: *row.VerifierID, FullName: *row.VerifierName}
	}
	if withNasabah {
		it.Nasabah = &NasabahRef{
			ID: row.NasabahID, FullName: row.NasabahName, ClassName: row.NasabahClass,
			QRCode: row.NasabahQR, Balance: row.NasabahBal,
		}
	}
	return it
}

// List mengambil daftar pengajuan. Antrean pending diurutkan dari yang TERLAMA
// (siapa cepat dia dilayani); status lain dari yang terbaru.
func (r *Repository) List(ctx context.Context, f filter, page utils.PageQuery, withNasabah bool) ([]Item, int64, error) {
	var total int64
	if err := r.base(ctx, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := "w.requested_at DESC, w.id DESC"
	if f.Status == StatusPending {
		order = "w.requested_at ASC, w.id ASC"
	}

	var rows []itemRow
	err := withDetails(r.base(ctx, f)).Order(order).
		Limit(page.Limit).Offset(page.Offset()).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]Item, len(rows))
	for i, row := range rows {
		items[i] = row.toItem(withNasabah)
	}
	return items, total, nil
}

// Find mengambil satu pengajuan sesuai filter (sekolah / nasabah)
func (r *Repository) Find(ctx context.Context, f filter, id string, withNasabah bool) (*Item, error) {
	var rows []itemRow
	if err := withDetails(r.base(ctx, f)).Where("w.id = ?", id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	it := rows[0].toItem(withNasabah)
	return &it, nil
}

// StatusCounts menghitung jumlah pengajuan per status di sekolah admin
func (r *Repository) StatusCounts(ctx context.Context, schoolID *string) (StatusCounts, error) {
	var c StatusCounts
	err := r.base(ctx, filter{SchoolID: schoolID}).Select(`
		COUNT(*) FILTER (WHERE w.status = 'pending')   AS pending,
		COUNT(*) FILTER (WHERE w.status = 'approved')  AS approved,
		COUNT(*) FILTER (WHERE w.status = 'rejected')  AS rejected,
		COUNT(*) FILTER (WHERE w.status = 'completed') AS completed,
		COUNT(*) FILTER (WHERE w.status = 'cancelled') AS cancelled`).Scan(&c).Error
	return c, err
}
