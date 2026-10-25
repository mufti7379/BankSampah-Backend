// Package mutation mengelola saldo nasabah dan buku besar (ledger) mutasi saldo.
//
// ATURAN PENTING: saldo (users.balance) HANYA boleh diubah lewat fungsi di file ini.
// Setiap perubahan saldo selalu disertai satu baris balance_mutations dengan
// balance_after, sehingga saldo selalu bisa direkonsiliasi:
//
//	users.balance == SUM(credit) - SUM(debit) dari balance_mutations
//
// Mutasi bersifat immutable (PRD Bab 6E): tidak pernah di-update atau di-delete.
// Koreksi dilakukan dengan mutasi baru (reference_type "adjustment").
package mutation

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Tipe mutasi (kolom balance_mutations.type)
const (
	TypeCredit = "credit" // saldo masuk
	TypeDebit  = "debit"  // saldo keluar
)

// Sumber mutasi (kolom balance_mutations.reference_type)
const (
	RefWeighing   = "weighing"
	RefWithdrawal = "withdrawal"
	RefAdjustment = "adjustment"
)

var (
	// ErrInvalidAmount dikembalikan jika nominal mutasi <= 0
	ErrInvalidAmount = errors.New("nominal mutasi harus lebih dari 0")
	// ErrNasabahNotFound dikembalikan jika user bukan nasabah / tidak ada
	ErrNasabahNotFound = errors.New("nasabah tidak ditemukan")
	// ErrInsufficientBalance dikembalikan jika debit membuat saldo negatif
	ErrInsufficientBalance = errors.New("saldo tidak mencukupi")
)

// Entry adalah hasil pencatatan satu mutasi
type Entry struct {
	ID            string
	Type          string
	Amount        int64
	BalanceBefore int64
	BalanceAfter  int64
	CreatedAt     time.Time
}

// CreditTx menambah saldo nasabah dan mencatat mutasi "credit" di dalam transaksi tx.
//
// UPDATE ... RETURNING bersifat atomik dan mengunci baris nasabah sampai transaksi
// selesai, sehingga dua transaksi bersamaan untuk nasabah yang sama akan berjalan
// bergantian dan balance_after selalu benar.
func CreditTx(tx *gorm.DB, nasabahID string, amount int64, refType string, refID *string, description string) (*Entry, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	var balances []int64
	err := tx.Raw(`
		UPDATE users SET balance = balance + ?, updated_at = NOW()
		WHERE id = ? AND role = 'nasabah'
		RETURNING balance`,
		amount, nasabahID,
	).Scan(&balances).Error
	if err != nil {
		return nil, fmt.Errorf("gagal menambah saldo: %w", err)
	}
	if len(balances) == 0 {
		return nil, ErrNasabahNotFound
	}

	return insertMutation(tx, nasabahID, TypeCredit, amount, balances[0]-amount, balances[0], refType, refID, description)
}

// DebitTx mengurangi saldo nasabah dan mencatat mutasi "debit" di dalam transaksi tx.
// Syarat saldo cukup dicek di dalam UPDATE yang sama (atomik), sehingga saldo tidak
// pernah bisa menjadi negatif walaupun ada transaksi bersamaan (PRD Bab 6E).
func DebitTx(tx *gorm.DB, nasabahID string, amount int64, refType string, refID *string, description string) (*Entry, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	var balances []int64
	err := tx.Raw(`
		UPDATE users SET balance = balance - ?, updated_at = NOW()
		WHERE id = ? AND role = 'nasabah' AND balance >= ?
		RETURNING balance`,
		amount, nasabahID, amount,
	).Scan(&balances).Error
	if err != nil {
		return nil, fmt.Errorf("gagal mengurangi saldo: %w", err)
	}
	if len(balances) == 0 {
		// Bedakan "bukan nasabah" dengan "saldo kurang"
		var count int64
		if err := tx.Raw(`SELECT COUNT(*) FROM users WHERE id = ? AND role = 'nasabah'`, nasabahID).
			Scan(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrNasabahNotFound
		}
		return nil, ErrInsufficientBalance
	}

	return insertMutation(tx, nasabahID, TypeDebit, amount, balances[0]+amount, balances[0], refType, refID, description)
}

// insertMutation menyimpan satu baris ledger
func insertMutation(tx *gorm.DB, nasabahID, typ string, amount, before, after int64,
	refType string, refID *string, description string) (*Entry, error) {
	var row struct {
		ID        string
		CreatedAt time.Time
	}
	err := tx.Raw(`
		INSERT INTO balance_mutations (nasabah_id, type, amount, balance_after, reference_type, reference_id, description)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at`,
		nasabahID, typ, amount, after, refType, refID, description,
	).Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("gagal mencatat mutasi saldo: %w", err)
	}
	return &Entry{
		ID: row.ID, Type: typ, Amount: amount,
		BalanceBefore: before, BalanceAfter: after, CreatedAt: row.CreatedAt,
	}, nil
}
