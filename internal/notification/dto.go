package notification

import (
	"time"

	"github.com/banksampahku/backend/internal/utils"
)

// targets memetakan tipe notifikasi ke halaman tujuan saat notifikasi diklik.
// Frontend membuka halaman detail berdasarkan target + reference_id.
var targets = map[string]string{
	TypeWelcome:             "home",
	TypeWeighingCreated:     "weighing",
	TypeWithdrawalApproved:  "withdrawal",
	TypeWithdrawalRejected:  "withdrawal",
	TypeWithdrawalCompleted: "withdrawal",
	TypeWithdrawalRequested: "withdrawal",
	TypeNasabahRegistered:   "nasabah",
}

// ListQuery adalah query GET /api/v1/notifications
type ListQuery struct {
	utils.PageQuery
	Status string `form:"status" binding:"omitempty,oneof=unread read all"` // default: all
	Type   string `form:"type" binding:"omitempty,max=30"`
}

// Item adalah satu notifikasi in-app
type Item struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Target      string    `json:"target"`       // halaman tujuan: home | weighing | withdrawal | nasabah
	ReferenceID *string   `json:"reference_id"` // ID entitas yang dibuka (penimbangan, penarikan, dll.)
	IsRead      bool      `json:"is_read"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListResponse adalah respons daftar notifikasi (+ jumlah belum dibaca untuk badge lonceng)
type ListResponse struct {
	Items       []Item         `json:"items"`
	Meta        utils.PageMeta `json:"meta"`
	UnreadCount int64          `json:"unread_count"`
}

// UnreadCountResponse adalah respons GET /api/v1/notifications/unread-count
type UnreadCountResponse struct {
	UnreadCount int64 `json:"unread_count"`
}

// ReadAllResponse adalah respons PATCH /api/v1/notifications/read-all
type ReadAllResponse struct {
	Updated int64 `json:"updated"` // jumlah notifikasi yang baru ditandai dibaca
}
