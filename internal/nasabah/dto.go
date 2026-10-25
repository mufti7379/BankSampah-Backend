// Package nasabah menangani manajemen nasabah oleh admin: CRUD, status akun, dan QR Code.
package nasabah

import (
	"time"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// ---------- Request ----------

// CreateRequest adalah body POST /api/v1/admin/nasabah.
// Password opsional: jika kosong, sistem membuat password sementara.
type CreateRequest struct {
	FullName  string  `json:"full_name" binding:"required,min=3,max=150"`
	Email     string  `json:"email" binding:"required,email,max=150"`
	Phone     *string `json:"phone" binding:"omitempty,phone_id"`
	ClassName *string `json:"class_name" binding:"omitempty,max=30"`
	Password  *string `json:"password" binding:"omitempty,min=8,max=128"`
}

// UpdateRequest adalah body PATCH /api/v1/admin/nasabah/:id (semua field opsional).
// Kirim string kosong pada phone/class_name untuk mengosongkan nilainya.
type UpdateRequest struct {
	FullName  *string `json:"full_name" binding:"omitempty,min=3,max=150"`
	Email     *string `json:"email" binding:"omitempty,email,max=150"`
	Phone     *string `json:"phone" binding:"omitempty,max=20"`
	ClassName *string `json:"class_name" binding:"omitempty,max=30"`
}

// ListQuery adalah query string GET /api/v1/admin/nasabah
type ListQuery struct {
	utils.PageQuery
	Search    string `form:"search" binding:"omitempty,max=100"` // nama, email, atau qr_code
	Status    string `form:"status" binding:"omitempty,oneof=active pending inactive"`
	ClassName string `form:"class_name" binding:"omitempty,max=30"`
	Sort      string `form:"sort" binding:"omitempty,oneof=newest oldest name balance_desc balance_asc"`
}

// QRQuery adalah query string GET /api/v1/admin/nasabah/:id/qr.png
type QRQuery struct {
	Size     int  `form:"size" binding:"omitempty,min=128,max=1024"`
	Download bool `form:"download"`
}

// ---------- Response ----------

// Response adalah data nasabah untuk admin (tanpa password_hash)
type Response struct {
	ID          string     `json:"id"`
	FullName    string     `json:"full_name"`
	Email       string     `json:"email"`
	Phone       *string    `json:"phone"`
	ClassName   *string    `json:"class_name"`
	Status      string     `json:"status"`
	QRCode      *string    `json:"qr_code"`
	Balance     int64      `json:"balance"`
	AvatarURL   *string    `json:"avatar_url"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Stats adalah ringkasan aktivitas nasabah untuk halaman detail
type Stats struct {
	TotalWeighings     int64      `json:"total_weighings"`
	TotalWeightKg      float64    `json:"total_weight_kg"`
	LastWeighingDate   *time.Time `json:"last_weighing_date"`
	PendingWithdrawals int64      `json:"pending_withdrawals"`
}

// DetailResponse adalah respons GET /api/v1/admin/nasabah/:id
type DetailResponse struct {
	Response
	Stats Stats `json:"stats"`
}

// CreateResponse adalah respons POST /api/v1/admin/nasabah.
// InitialPassword hanya terisi (sekali ini saja) jika password dibuat otomatis.
type CreateResponse struct {
	Response
	InitialPassword *string `json:"initial_password,omitempty"`
}

// toResponse mengubah auth.User menjadi Response
func toResponse(u *auth.User) Response {
	return Response{
		ID:          u.ID,
		FullName:    u.FullName,
		Email:       u.Email,
		Phone:       u.Phone,
		ClassName:   u.ClassName,
		Status:      u.Status,
		QRCode:      u.QRCode,
		Balance:     u.Balance,
		AvatarURL:   u.AvatarURL,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
