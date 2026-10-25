// Package report menyediakan laporan & agregasi untuk admin (PRD Bab 6I):
// ringkasan periode, rekap per kategori, rekap per nasabah, dan ekspor CSV.
package report

import (
	"time"

	"github.com/banksampahku/backend/internal/utils"
)

// MaxRangeDays membatasi rentang laporan agar query tetap ringan
const MaxRangeDays = 366

// ---------- Request ----------

// PeriodQuery adalah periode laporan. Kosong = bulan berjalan (PRD Bab 6I).
type PeriodQuery struct {
	From string `form:"from" binding:"omitempty,datetime=2006-01-02"`
	To   string `form:"to" binding:"omitempty,datetime=2006-01-02"`
}

// ByNasabahQuery adalah query GET /admin/reports/by-nasabah
type ByNasabahQuery struct {
	PeriodQuery
	utils.PageQuery
	Search string `form:"search" binding:"omitempty,max=100"` // nama / QR / kelas
	Sort   string `form:"sort" binding:"omitempty,oneof=amount_desc weight_desc withdrawn_desc balance_desc name"`
	// all=true menampilkan juga nasabah tanpa aktivitas pada periode ini
	All bool `form:"all"`
}

// ExportQuery adalah query GET /admin/reports/export.csv
type ExportQuery struct {
	PeriodQuery
	Type      string `form:"type" binding:"omitempty,oneof=by-nasabah by-category weighings mutations"` // default by-nasabah
	Delimiter string `form:"delimiter" binding:"omitempty,oneof=semicolon comma"`                       // default semicolon
}

// ---------- Response ----------

// Period adalah periode yang dipakai laporan (sudah dinormalisasi)
type Period struct {
	From string `json:"from"` // YYYY-MM-DD
	To   string `json:"to"`   // YYYY-MM-DD
	Days int    `json:"days"`
}

// WeighingStats adalah statistik penimbangan pada periode
type WeighingStats struct {
	Count         int64   `json:"count"`           // jumlah transaksi
	TotalWeightKg float64 `json:"total_weight_kg"` // total berat (semua satuan dijumlahkan)
	TotalAmount   int64   `json:"total_amount"`    // total nilai rupiah
	ActiveNasabah int64   `json:"active_nasabah"`  // nasabah yang menyetor
	AverageAmount int64   `json:"average_amount"`  // rata-rata per transaksi
}

// MutationStats adalah arus saldo pada periode
type MutationStats struct {
	TotalCredit int64 `json:"total_credit"` // saldo masuk
	TotalDebit  int64 `json:"total_debit"`  // saldo keluar
	Net         int64 `json:"net"`
}

// WithdrawalStats adalah statistik penarikan
type WithdrawalStats struct {
	CompletedCount  int64 `json:"completed_count"`  // penarikan selesai pada periode
	CompletedAmount int64 `json:"completed_amount"` // uang tunai yang dibayarkan pada periode
	InProcessCount  int64 `json:"in_process_count"` // pending + approved saat ini
	InProcessAmount int64 `json:"in_process_amount"`
}

// Snapshot adalah kondisi saat ini (tidak tergantung periode)
type Snapshot struct {
	ActiveNasabah    int64 `json:"active_nasabah"`
	PendingNasabah   int64 `json:"pending_nasabah"`     // menunggu persetujuan
	OutstandingSaldo int64 `json:"outstanding_balance"` // total saldo beredar = kewajiban kas
}

// DailyPoint adalah satu titik tren harian (untuk grafik)
type DailyPoint struct {
	Date          string  `json:"date"`
	Count         int64   `json:"count"`
	TotalWeightKg float64 `json:"total_weight_kg"`
	TotalAmount   int64   `json:"total_amount"`
}

// SummaryResponse adalah respons GET /admin/reports/summary
type SummaryResponse struct {
	Period      Period          `json:"period"`
	Weighings   WeighingStats   `json:"weighings"`
	Mutations   MutationStats   `json:"mutations"`
	Withdrawals WithdrawalStats `json:"withdrawals"`
	Snapshot    Snapshot        `json:"snapshot"`
	Daily       []DailyPoint    `json:"daily"`
	GeneratedAt time.Time       `json:"generated_at"`
}

// TypeRow adalah rekap satu jenis sampah
type TypeRow struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Unit          string  `json:"unit"`
	Quantity      float64 `json:"quantity"` // berat/jumlah sesuai satuan
	TotalAmount   int64   `json:"total_amount"`
	WeighingCount int64   `json:"weighing_count"`
	SharePercent  float64 `json:"share_percent"` // kontribusi terhadap total rupiah periode
}

// CategoryRow adalah rekap satu kategori beserta jenis-jenisnya
type CategoryRow struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	TotalWeightKg float64   `json:"total_weight_kg"` // hanya jenis bersatuan kg
	TotalAmount   int64     `json:"total_amount"`
	WeighingCount int64     `json:"weighing_count"`
	SharePercent  float64   `json:"share_percent"`
	Types         []TypeRow `json:"types"`
}

// ByCategoryResponse adalah respons GET /admin/reports/by-category
type ByCategoryResponse struct {
	Period        Period        `json:"period"`
	TotalAmount   int64         `json:"total_amount"`
	TotalWeightKg float64       `json:"total_weight_kg"`
	Categories    []CategoryRow `json:"categories"`
}

// NasabahRow adalah rekap satu nasabah pada periode
type NasabahRow struct {
	ID             string  `json:"id"`
	FullName       string  `json:"full_name"`
	ClassName      *string `json:"class_name"`
	QRCode         *string `json:"qr_code"`
	Status         string  `json:"status"`
	WeighingCount  int64   `json:"weighing_count"`
	TotalWeightKg  float64 `json:"total_weight_kg"`
	TotalAmount    int64   `json:"total_amount"`    // saldo masuk dari setoran
	TotalWithdrawn int64   `json:"total_withdrawn"` // penarikan selesai
	Balance        int64   `json:"balance"`         // saldo saat ini
}

// ByNasabahResponse adalah respons GET /admin/reports/by-nasabah
type ByNasabahResponse struct {
	Period Period         `json:"period"`
	Items  []NasabahRow   `json:"items"`
	Meta   utils.PageMeta `json:"meta"`
}
