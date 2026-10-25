package nasabah

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/notification"
	"github.com/banksampahku/backend/internal/utils"
)

// ErrNotFound dikembalikan jika nasabah tidak ditemukan (atau beda sekolah)
var ErrNotFound = errors.New("nasabah tidak ditemukan")

// sortColumns adalah whitelist urutan list (mencegah SQL injection lewat ORDER BY)
var sortColumns = map[string]string{
	"":             "created_at DESC",
	"newest":       "created_at DESC",
	"oldest":       "created_at ASC",
	"name":         "full_name ASC",
	"balance_desc": "balance DESC",
	"balance_asc":  "balance ASC",
}

// Repository menangani akses data nasabah
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository nasabah
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// scoped membatasi query hanya ke nasabah di sekolah milik admin
func (r *Repository) scoped(ctx context.Context, schoolID *string) *gorm.DB {
	return r.db.WithContext(ctx).Model(&auth.User{}).
		Where("role = ?", auth.RoleNasabah).
		Where("school_id IS NOT DISTINCT FROM ?", schoolID)
}

// List mengambil daftar nasabah dengan filter, pencarian, dan pagination
func (r *Repository) List(ctx context.Context, schoolID *string, q ListQuery) ([]auth.User, int64, error) {
	query := r.scoped(ctx, schoolID)

	if s := strings.TrimSpace(q.Search); s != "" {
		like := "%" + utils.EscapeLike(s) + "%"
		query = query.Where("(full_name ILIKE ? OR email ILIKE ? OR qr_code ILIKE ?)", like, like, like)
	}
	if q.Status != "" {
		query = query.Where("status = ?", q.Status)
	}
	if c := strings.TrimSpace(q.ClassName); c != "" {
		query = query.Where("class_name ILIKE ?", utils.EscapeLike(c))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var users []auth.User
	err := query.Order(sortColumns[q.Sort]).Order("id").
		Limit(q.Limit).Offset(q.Offset()).Find(&users).Error
	return users, total, err
}

// FindByID mengambil satu nasabah di sekolah admin
func (r *Repository) FindByID(ctx context.Context, schoolID *string, id string) (*auth.User, error) {
	var user auth.User
	err := r.scoped(ctx, schoolID).Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

// Stats menghitung ringkasan penimbangan & penarikan pending milik nasabah
func (r *Repository) Stats(ctx context.Context, id string) (Stats, error) {
	var s Stats
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			(SELECT COUNT(*) FROM weighings WHERE nasabah_id = @id)                      AS total_weighings,
			(SELECT COALESCE(SUM(total_weight), 0) FROM weighings WHERE nasabah_id = @id)  AS total_weight_kg,
			(SELECT MAX(transaction_date) FROM weighings WHERE nasabah_id = @id)           AS last_weighing_date,
			(SELECT COUNT(*) FROM withdrawal_requests
			  WHERE nasabah_id = @id AND status IN ('pending', 'approved'))                AS pending_withdrawals`,
		map[string]any{"id": id},
	).Scan(&s).Error
	return s, err
}

// EmailTaken mengecek apakah email sudah dipakai user lain (semua role)
func (r *Repository) EmailTaken(ctx context.Context, email, excludeID string) (bool, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&auth.User{}).Where("email = ?", email)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&count).Error
	return count > 0, err
}

// CreateWithWelcome menyimpan nasabah baru + notifikasi WELCOME (1 DB transaction)
func (r *Repository) CreateWithWelcome(ctx context.Context, user *auth.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return insertWelcome(tx, user)
	})
}

// Update memperbarui kolom tertentu milik nasabah
func (r *Repository) Update(ctx context.Context, id string, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&auth.User{}).Where("id = ?", id).Updates(fields).Error
}

// Activate mengaktifkan nasabah; jika sebelumnya pending (daftar mandiri),
// sekalian kirim notifikasi WELCOME (1 DB transaction)
func (r *Repository) Activate(ctx context.Context, user *auth.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&auth.User{}).Where("id = ?", user.ID).
			Update("status", auth.StatusActive).Error; err != nil {
			return err
		}
		if user.Status == auth.StatusPending {
			return insertWelcome(tx, user)
		}
		return nil
	})
}

// insertWelcome mengirim notifikasi in-app WELCOME ke nasabah
func insertWelcome(tx *gorm.DB, user *auth.User) error {
	return notification.CreateTx(tx, notification.New{
		UserID: user.ID,
		Type:   notification.TypeWelcome,
		Title:  "Selamat Datang di BankSampahKu",
		Body: "Halo " + user.FullName + ", akun Anda sudah aktif. Tunjukkan QR Code Anda kepada petugas " +
			"bank sampah setiap kali menyetor sampah agar saldo Anda bertambah otomatis.",
	})
}
