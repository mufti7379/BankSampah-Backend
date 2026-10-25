// Package withdrawal menangani pengajuan penarikan saldo oleh nasabah dan
// verifikasinya oleh admin (PRD Bab 6F). Pembayaran 100% tunai di sekolah.
//
// Alur status:
//
//	pending ──approve──► approved ──complete──► completed   (saldo didebit di sini)
//	   │                    │
//	   └──reject────────────┴──reject──► rejected            (wajib catatan)
//	   └──cancel (oleh nasabah)──► cancelled
//
// Setelah completed / rejected / cancelled, pengajuan tidak bisa diubah lagi.
package withdrawal

import (
	"time"

	"github.com/banksampahku/backend/internal/utils"
)

// Status pengajuan (kolom withdrawal_requests.status)
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// Aturan nominal (PRD Bab 6F)
const (
	MinAmount  = 10_000
	MethodCash = "cash"
)

// Catatan bawaan jika admin tidak mengisi (PRD: note wajib terisi saat ada aksi)
const (
	defaultApproveNote  = "Pengajuan disetujui. Uang tunai siap diambil di bank sampah sekolah."
	defaultCompleteNote = "Uang tunai telah diserahkan kepada nasabah."
	cancelNote          = "Dibatalkan oleh nasabah."
)

// statusLabels adalah label ramah pengguna untuk UI (PRD Bab 9)
var statusLabels = map[string]string{
	StatusPending:   "Menunggu Verifikasi",
	StatusApproved:  "Disetujui, Siap Diambil",
	StatusRejected:  "Ditolak",
	StatusCompleted: "Selesai",
	StatusCancelled: "Dibatalkan",
}

// ---------- Request ----------

// CreateRequest adalah body POST /api/v1/nasabah/me/withdrawals
type CreateRequest struct {
	Amount int64 `json:"amount" binding:"gte=10000,max=100000000"`
}

// ApproveRequest / CompleteRequest: catatan opsional (diisi otomatis jika kosong)
type ApproveRequest struct {
	Note *string `json:"note" binding:"omitempty,max=500"`
}

// RejectRequest: alasan penolakan WAJIB diisi (PRD Task 3.4)
type RejectRequest struct {
	Note string `json:"note" binding:"required,min=5,max=500"`
}

// MyListQuery adalah query GET /api/v1/nasabah/me/withdrawals
type MyListQuery struct {
	utils.PageQuery
	Status string `form:"status" binding:"omitempty,oneof=pending approved rejected completed cancelled"`
}

// AdminListQuery adalah query GET /api/v1/admin/withdrawals
type AdminListQuery struct {
	utils.PageQuery
	Status    string `form:"status" binding:"omitempty,oneof=pending approved rejected completed cancelled"`
	NasabahID string `form:"nasabah_id" binding:"omitempty,uuid"`
	Search    string `form:"search" binding:"omitempty,max=100"` // nama / QR nasabah
	DateFrom  string `form:"date_from" binding:"omitempty,datetime=2006-01-02"`
	DateTo    string `form:"date_to" binding:"omitempty,datetime=2006-01-02"`
}

// ---------- Response ----------

// NasabahRef adalah ringkasan nasabah pengaju
type NasabahRef struct {
	ID        string  `json:"id"`
	FullName  string  `json:"full_name"`
	ClassName *string `json:"class_name"`
	QRCode    *string `json:"qr_code"`
	Balance   int64   `json:"balance"` // saldo nasabah saat ini
}

// VerifierRef adalah admin yang melakukan aksi terakhir
type VerifierRef struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
}

// Item adalah satu pengajuan penarikan
type Item struct {
	ID           string       `json:"id"`
	Amount       int64        `json:"amount"`
	Method       string       `json:"method"`
	Status       string       `json:"status"`
	StatusLabel  string       `json:"status_label"`
	Note         *string      `json:"note"`
	RequestedAt  time.Time    `json:"requested_at"`
	VerifiedAt   *time.Time   `json:"verified_at"`
	VerifiedBy   *VerifierRef `json:"verified_by"`
	Nasabah      *NasabahRef  `json:"nasabah,omitempty"`       // hanya untuk tampilan admin
	MutationID   *string      `json:"mutation_id"`             // terisi setelah completed
	BalanceAfter *int64       `json:"balance_after,omitempty"` // saldo setelah penarikan selesai
	CanCancel    bool         `json:"can_cancel"`              // nasabah masih bisa membatalkan
}

// StatusCounts adalah jumlah pengajuan per status (untuk badge di dashboard admin)
type StatusCounts struct {
	Pending   int64 `json:"pending"`
	Approved  int64 `json:"approved"`
	Rejected  int64 `json:"rejected"`
	Completed int64 `json:"completed"`
	Cancelled int64 `json:"cancelled"`
}

// AdminListResponse adalah respons GET /api/v1/admin/withdrawals
type AdminListResponse struct {
	Items        []Item         `json:"items"`
	Meta         utils.PageMeta `json:"meta"`
	StatusCounts StatusCounts   `json:"status_counts"`
}
