package auth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrUserNotFound dikembalikan jika user tidak ditemukan
var ErrUserNotFound = errors.New("user tidak ditemukan")

// Repository menangani akses data user untuk modul auth
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository auth
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// FindByEmail mencari user berdasarkan email (sudah dinormalisasi lowercase)
func (r *Repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("email = ?", email).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	return &user, err
}

// FindByID mencari user berdasarkan ID
func (r *Repository) FindByID(ctx context.Context, id string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	return &user, err
}

// EmailExists mengecek apakah email sudah terdaftar
func (r *Repository) EmailExists(ctx context.Context, email string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&User{}).Where("email = ?", email).Count(&count).Error
	return count > 0, err
}

// SchoolExists mengecek apakah sekolah dengan ID tertentu ada
func (r *Repository) SchoolExists(ctx context.Context, schoolID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("schools").Where("id = ?", schoolID).Count(&count).Error
	return count > 0, err
}

// DefaultSchoolID mengembalikan sekolah pertama (mode single-tenant MVP)
func (r *Repository) DefaultSchoolID(ctx context.Context) (string, error) {
	var id string
	err := r.db.WithContext(ctx).Raw("SELECT id FROM schools ORDER BY created_at ASC LIMIT 1").Scan(&id).Error
	return id, err
}

// CreateNasabahWithNotification menyimpan nasabah baru dan mengirim notifikasi
// NASABAH_REGISTERED ke seluruh admin aktif di sekolah yang sama (1 DB transaction).
func (r *Repository) CreateNasabahWithNotification(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return tx.Exec(`
			INSERT INTO notifications (user_id, title, body, type, reference_id)
			SELECT id, ?, ?, 'NASABAH_REGISTERED', ?
			FROM users
			WHERE role = 'admin' AND status = 'active' AND school_id IS NOT DISTINCT FROM ?`,
			"Pendaftaran Nasabah Baru",
			user.FullName+" mendaftar sebagai nasabah dan menunggu verifikasi Anda.",
			user.ID,
			user.SchoolID,
		).Error
	})
}

// HasRecentResetToken mengecek apakah sudah ada token reset yang dibuat setelah `since`
// (cooldown agar email reset tidak dikirim berulang-ulang/spam)
func (r *Repository) HasRecentResetToken(ctx context.Context, userID string, since time.Time) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("password_reset_tokens").
		Where("user_id = ? AND created_at > ?", userID, since).Count(&count).Error
	return count > 0, err
}

// CreateResetToken menyimpan hash token reset password
func (r *Repository) CreateResetToken(ctx context.Context, userID, tokenHash, reason, ip string, expiresAt time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO password_reset_tokens (user_id, token_hash, reason, expires_at, requested_ip)
		VALUES (?, ?, ?, ?, NULLIF(?, ''))`,
		userID, tokenHash, reason, expiresAt, ip,
	).Error
}

// FindUserByValidResetToken mencari pemilik token reset yang belum dipakai & belum kedaluwarsa
func (r *Repository) FindUserByValidResetToken(ctx context.Context, tokenHash string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Raw(`
		SELECT u.* FROM password_reset_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.used_at IS NULL AND t.expires_at > NOW()`,
		tokenHash,
	).Scan(&user).Error
	if err != nil {
		return nil, err
	}
	if user.ID == "" {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

// ResetPassword mengganti password dan menandai token sebagai terpakai (1 DB transaction).
// Seluruh token reset lain milik user ikut dibatalkan. Mengembalikan ErrUserNotFound jika
// token sudah tidak valid (misal dipakai bersamaan oleh dua request).
func (r *Repository) ResetPassword(ctx context.Context, tokenHash, newPasswordHash string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Klaim token secara atomik: hanya satu request yang berhasil meng-update
		var userID string
		if err := tx.Raw(`
			UPDATE password_reset_tokens SET used_at = NOW()
			WHERE token_hash = ? AND used_at IS NULL AND expires_at > NOW()
			RETURNING user_id`,
			tokenHash,
		).Scan(&userID).Error; err != nil {
			return err
		}
		if userID == "" {
			return ErrUserNotFound
		}

		if err := tx.Exec(`UPDATE password_reset_tokens SET used_at = NOW() WHERE user_id = ? AND used_at IS NULL`,
			userID).Error; err != nil {
			return err
		}
		// password_changed_at membuat semua token login lama otomatis tidak berlaku
		if err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
			"password_hash":       newPasswordHash,
			"password_changed_at": gorm.Expr("NOW()"),
		}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", userID).Take(&user).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateLastLogin memperbarui waktu login terakhir user
func (r *Repository) UpdateLastLogin(ctx context.Context, userID string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).
		Update("last_login_at", at).Error
}
