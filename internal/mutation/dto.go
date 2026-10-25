package mutation

import (
	"time"

	"github.com/banksampahku/backend/internal/utils"
)

// Label ramah pengguna (PRD Bab 9: hindari istilah teknis di UI nasabah)
var (
	typeLabels = map[string]string{
		TypeCredit: "Saldo Masuk",
		TypeDebit:  "Saldo Keluar",
	}
	sourceLabels = map[string]string{
		RefWeighing:   "Setoran Sampah",
		RefWithdrawal: "Penarikan Saldo",
		RefAdjustment: "Koreksi Saldo oleh Admin",
	}
)

// ---------- Request ----------

// FilterQuery adalah filter bersama untuk semua daftar mutasi.
// Periode: pakai month (YYYY-MM) ATAU date_from/date_to (YYYY-MM-DD).
type FilterQuery struct {
	utils.PageQuery
	Type          string `form:"type" binding:"omitempty,oneof=credit debit"`
	ReferenceType string `form:"reference_type" binding:"omitempty,oneof=weighing withdrawal adjustment"`
	Month         string `form:"month" binding:"omitempty,datetime=2006-01"`
	DateFrom      string `form:"date_from" binding:"omitempty,datetime=2006-01-02"`
	DateTo        string `form:"date_to" binding:"omitempty,datetime=2006-01-02"`
}

// AdminListQuery adalah query GET /api/v1/admin/mutations (global)
type AdminListQuery struct {
	FilterQuery
	NasabahID string `form:"nasabah_id" binding:"omitempty,uuid"`
	Search    string `form:"search" binding:"omitempty,max=100"` // nama / QR nasabah
}

// ---------- Response ----------

// NasabahRef adalah ringkasan nasabah pemilik mutasi (untuk tampilan admin)
type NasabahRef struct {
	ID        string  `json:"id"`
	FullName  string  `json:"full_name"`
	ClassName *string `json:"class_name"`
	QRCode    *string `json:"qr_code"`
}

// Item adalah satu baris mutasi saldo
type Item struct {
	ID            string      `json:"id"`
	Type          string      `json:"type"`   // credit | debit
	Label         string      `json:"label"`  // "Saldo Masuk" | "Saldo Keluar"
	Amount        int64       `json:"amount"` // selalu positif
	BalanceAfter  int64       `json:"balance_after"`
	ReferenceType string      `json:"reference_type"` // weighing | withdrawal | adjustment
	SourceLabel   string      `json:"source_label"`   // "Setoran Sampah", dst.
	ReferenceID   *string     `json:"reference_id"`   // ID penimbangan/penarikan (untuk tautan detail)
	Description   *string     `json:"description"`
	CreatedAt     time.Time   `json:"created_at"`
	Nasabah       *NasabahRef `json:"nasabah,omitempty"` // hanya pada daftar global admin
}

// Summary adalah ringkasan total mutasi sesuai filter (seluruh halaman, bukan hanya halaman ini)
type Summary struct {
	TotalCredit int64 `json:"total_credit"`
	TotalDebit  int64 `json:"total_debit"`
	Net         int64 `json:"net"` // total_credit - total_debit
	Count       int64 `json:"count"`
}

// ListResponse adalah respons daftar mutasi
type ListResponse struct {
	Items   []Item         `json:"items"`
	Meta    utils.PageMeta `json:"meta"`
	Summary Summary        `json:"summary"`
}

// NasabahMutationsResponse adalah respons GET /admin/nasabah/:id/mutations
type NasabahMutationsResponse struct {
	Nasabah   NasabahBalance `json:"nasabah"`
	Mutations ListResponse   `json:"mutations"`
}

// NasabahBalance adalah identitas + saldo nasabah
type NasabahBalance struct {
	NasabahRef
	Status  string `json:"status"`
	Balance int64  `json:"balance"`
}

// BalanceResponse adalah respons GET /api/v1/nasabah/me/balance (kartu saldo Home Flutter)
type BalanceResponse struct {
	Balance                 int64      `json:"balance"`
	TotalCredit             int64      `json:"total_credit"`              // total saldo masuk sepanjang waktu
	TotalDebit              int64      `json:"total_debit"`               // total saldo keluar sepanjang waktu
	TotalWeighings          int64      `json:"total_weighings"`           // jumlah setoran
	TotalWeightKg           float64    `json:"total_weight_kg"`           // total sampah disetor
	PendingWithdrawalAmount int64      `json:"pending_withdrawal_amount"` // penarikan yang sedang diproses
	AvailableBalance        int64      `json:"available_balance"`         // saldo - penarikan yang sedang diproses
	LastMutationAt          *time.Time `json:"last_mutation_at"`
}
