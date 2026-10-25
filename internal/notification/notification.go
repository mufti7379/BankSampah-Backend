// Package notification mengelola notifikasi in-app (PRD Bab 6G).
// Endpoint baca/tandai dibaca dibuat di Task 3.5; file ini menyediakan
// fungsi pembuatan notifikasi yang dipakai modul lain di dalam DB transaction.
package notification

import (
	"fmt"

	"gorm.io/gorm"
)

// Tipe notifikasi (kolom notifications.type) - PRD Bab 6G
const (
	// Untuk nasabah
	TypeWelcome             = "WELCOME"
	TypeWeighingCreated     = "WEIGHING_CREATED"
	TypeWithdrawalApproved  = "WITHDRAWAL_APPROVED"
	TypeWithdrawalRejected  = "WITHDRAWAL_REJECTED"
	TypeWithdrawalCompleted = "WITHDRAWAL_COMPLETED"

	// Untuk admin
	TypeWithdrawalRequested = "WITHDRAWAL_REQUESTED"
	TypeNasabahRegistered   = "NASABAH_REGISTERED"
)

// New adalah data notifikasi yang akan dibuat
type New struct {
	UserID      string
	Title       string
	Body        string
	Type        string
	ReferenceID *string // ID entitas terkait (penimbangan, penarikan, dll.)
}

// CreateTx menyimpan satu notifikasi memakai koneksi/transaksi tx, sehingga
// notifikasi ikut dibatalkan jika transaksi bisnisnya gagal.
func CreateTx(tx *gorm.DB, n New) error {
	err := tx.Exec(`
		INSERT INTO notifications (user_id, title, body, type, reference_id)
		VALUES (?, ?, ?, ?, ?)`,
		n.UserID, n.Title, n.Body, n.Type, n.ReferenceID,
	).Error
	if err != nil {
		return fmt.Errorf("gagal membuat notifikasi: %w", err)
	}
	return nil
}

// CreateForAdminsTx mengirim notifikasi yang sama ke seluruh admin aktif di sekolah
// (UserID pada n diabaikan). Dipakai misalnya untuk WITHDRAWAL_REQUESTED.
func CreateForAdminsTx(tx *gorm.DB, schoolID *string, n New) error {
	err := tx.Exec(`
		INSERT INTO notifications (user_id, title, body, type, reference_id)
		SELECT id, ?, ?, ?, ?
		FROM users
		WHERE role = 'admin' AND status = 'active' AND school_id IS NOT DISTINCT FROM ?`,
		n.Title, n.Body, n.Type, n.ReferenceID, schoolID,
	).Error
	if err != nil {
		return fmt.Errorf("gagal membuat notifikasi admin: %w", err)
	}
	return nil
}
