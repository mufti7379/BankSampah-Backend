// Package weighing menangani transaksi penimbangan sampah: admin memindai QR nasabah,
// mencatat berat per jenis sampah, dan sistem menghitung nilai rupiahnya.
package weighing

import (
	"time"

	"github.com/banksampahku/backend/internal/utils"
)

// Batas transaksi penimbangan (PRD Bab 6D)
const (
	MinItems        = 1
	MaxItems        = 20
	MinWeight       = 0.01
	MaxWeight       = 9999.99
	MaxBackdateDays = 7 // boleh back-date maksimal 7 hari, dengan alasan wajib diisi
	DateLayout      = "2006-01-02"
	weightScale     = 100 // berat disimpan 2 desimal → dihitung dalam satuan 1/100 kg
)

// ---------- Request ----------

// ItemRequest adalah satu baris jenis sampah yang ditimbang
type ItemRequest struct {
	WasteTypeID string  `json:"waste_type_id" binding:"required,uuid"`
	Weight      float64 `json:"weight" binding:"gte=0.01,lte=9999.99"`
}

// CreateRequest adalah body POST /api/v1/admin/weighings.
// Nasabah diidentifikasi lewat qr_code (hasil scan) ATAU nasabah_id.
type CreateRequest struct {
	QRCode          string        `json:"qr_code" binding:"omitempty,max=64"`
	NasabahID       string        `json:"nasabah_id" binding:"omitempty,uuid"`
	TransactionDate string        `json:"transaction_date" binding:"omitempty,datetime=2006-01-02"` // default: hari ini
	Note            *string       `json:"note" binding:"omitempty,max=500"`                         // wajib jika back-date
	Items           []ItemRequest `json:"items" binding:"required,min=1,max=20,dive"`
}

// RequestMeta adalah info dari header HTTP (bukan body) yang dibutuhkan service saat mencatat penimbangan
type RequestMeta struct {
	IP             string
	Source         string // "web" | "mobile" (header X-Client-Platform), dicatat di audit log
	IdempotencyKey string // UUID dari header Idempotency-Key, kosong jika tidak dikirim
}

// ListQuery adalah query GET /api/v1/admin/weighings
type ListQuery struct {
	utils.PageQuery
	NasabahID string `form:"nasabah_id" binding:"omitempty,uuid"`
	Search    string `form:"search" binding:"omitempty,max=100"` // nama / QR nasabah
	DateFrom  string `form:"date_from" binding:"omitempty,datetime=2006-01-02"`
	DateTo    string `form:"date_to" binding:"omitempty,datetime=2006-01-02"`
}

// MyListQuery adalah query GET /api/v1/nasabah/me/weighings
type MyListQuery struct {
	utils.PageQuery
	DateFrom string `form:"date_from" binding:"omitempty,datetime=2006-01-02"`
	DateTo   string `form:"date_to" binding:"omitempty,datetime=2006-01-02"`
}

// LookupQuery adalah query GET /api/v1/admin/weighings/lookup
type LookupQuery struct {
	QRCode string `form:"qr_code" binding:"required,max=64"`
}

// ---------- Response ----------

// PersonRef adalah ringkasan nasabah/admin di respons penimbangan
type PersonRef struct {
	ID        string  `json:"id"`
	FullName  string  `json:"full_name"`
	ClassName *string `json:"class_name,omitempty"`
	QRCode    *string `json:"qr_code,omitempty"`
}

// Summary adalah satu baris di daftar penimbangan
type Summary struct {
	ID              string    `json:"id"`
	TransactionDate string    `json:"transaction_date"` // YYYY-MM-DD
	Nasabah         PersonRef `json:"nasabah"`
	Admin           PersonRef `json:"admin"`        // petugas yang menimbang
	TotalWeight     float64   `json:"total_weight"` // kg (2 desimal)
	TotalAmount     int64     `json:"total_amount"` // Rupiah
	ItemCount       int64     `json:"item_count"`
	Note            *string   `json:"note"`
	CreatedAt       time.Time `json:"created_at"`
}

// ItemResponse adalah rincian satu jenis sampah (harga = snapshot saat transaksi)
type ItemResponse struct {
	ID            string  `json:"id"`
	WasteTypeID   string  `json:"waste_type_id"`
	WasteTypeName string  `json:"waste_type_name"`
	CategoryName  string  `json:"category_name"`
	Unit          string  `json:"unit"`
	Weight        float64 `json:"weight"`
	PricePerUnit  int64   `json:"price_per_unit"` // snapshot harga saat penimbangan
	Subtotal      int64   `json:"subtotal"`
}

// Detail adalah respons detail penimbangan (untuk halaman detail & cetak struk)
type Detail struct {
	Summary
	Items []ItemResponse `json:"items"`
}

// BalanceChange adalah perubahan saldo nasabah akibat penimbangan
type BalanceChange struct {
	Before     int64  `json:"before"`      // saldo sebelum penimbangan
	Credit     int64  `json:"credit"`      // saldo masuk (= total_amount)
	After      int64  `json:"after"`       // saldo setelah penimbangan
	MutationID string `json:"mutation_id"` // ID baris balance_mutations
}

// CreateResult adalah respons POST /admin/weighings (detail + perubahan saldo untuk struk)
type CreateResult struct {
	Detail
	Balance BalanceChange `json:"balance"`
	// Replayed true jika request ini kiriman ulang (Idempotency-Key sama) dan
	// transaksi yang dikembalikan sudah tersimpan sebelumnya, tidak dicatat dua kali
	Replayed bool `json:"replayed"`
}

// LookupResponse adalah profil nasabah hasil scan QR (sebelum mengisi penimbangan)
type LookupResponse struct {
	ID          string  `json:"id"`
	FullName    string  `json:"full_name"`
	ClassName   *string `json:"class_name"`
	QRCode      string  `json:"qr_code"`
	Status      string  `json:"status"`
	Balance     int64   `json:"balance"`
	AvatarURL   *string `json:"avatar_url"`
	CanTransact bool    `json:"can_transact"` // false jika akun pending/nonaktif
}
