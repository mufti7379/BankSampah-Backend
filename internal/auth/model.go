package auth

import "time"

// Role & status user sesuai CHECK constraint tabel users
const (
	RoleAdmin   = "admin"
	RoleNasabah = "nasabah"

	StatusActive   = "active"
	StatusPending  = "pending"
	StatusInactive = "inactive"
)

// User memetakan tabel users (lihat migrations/0001_init.up.sql)
type User struct {
	ID           string     `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	SchoolID     *string    `gorm:"column:school_id;type:uuid"`
	Email        string     `gorm:"column:email"`
	PasswordHash string     `gorm:"column:password_hash"`
	FullName     string     `gorm:"column:full_name"`
	Phone        *string    `gorm:"column:phone"`
	ClassName    *string    `gorm:"column:class_name"`
	Role         string     `gorm:"column:role"`
	Status       string     `gorm:"column:status"`
	QRCode       *string    `gorm:"column:qr_code"`
	Balance      int64      `gorm:"column:balance"`
	AvatarURL    *string    `gorm:"column:avatar_url"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
	// PasswordChangedAt: token yang terbit sebelum waktu ini dianggap tidak berlaku
	PasswordChangedAt *time.Time `gorm:"column:password_changed_at"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
}

// TableName memastikan GORM memakai tabel "users"
func (User) TableName() string { return "users" }

// ---------- Request DTO ----------

// RegisterRequest adalah body POST /api/v1/auth/register (khusus nasabah)
type RegisterRequest struct {
	FullName  string  `json:"full_name" binding:"required,min=3,max=150"`
	Email     string  `json:"email" binding:"required,email,max=150"`
	Password  string  `json:"password" binding:"required,min=8,max=128"`
	Phone     *string `json:"phone" binding:"omitempty,phone_id"`
	ClassName *string `json:"class_name" binding:"omitempty,max=30"`
	SchoolID  *string `json:"school_id" binding:"omitempty,uuid"`
}

// LoginRequest adalah body POST /api/v1/auth/login
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email,max=150"`
	Password string `json:"password" binding:"required,max=128"`
}

// ForgotPasswordRequest adalah body POST /api/v1/auth/forgot-password
type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email,max=150"`
}

// ResetPasswordRequest adalah body POST /api/v1/auth/reset-password
type ResetPasswordRequest struct {
	Token           string `json:"token" binding:"required,max=128"`
	Password        string `json:"password" binding:"required,min=8,max=128"`
	PasswordConfirm string `json:"password_confirm" binding:"required,eqfield=Password"`
}

// ---------- Response DTO ----------

// UserResponse adalah representasi user yang aman dikirim ke client (tanpa password_hash)
type UserResponse struct {
	ID          string     `json:"id"`
	SchoolID    *string    `json:"school_id"`
	Email       string     `json:"email"`
	FullName    string     `json:"full_name"`
	Phone       *string    `json:"phone"`
	ClassName   *string    `json:"class_name"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	QRCode      *string    `json:"qr_code"`
	Balance     int64      `json:"balance"`
	AvatarURL   *string    `json:"avatar_url"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// LoginResponse berisi token JWT & profil user setelah login berhasil.
// access_token & token_type hanya dikirim ke client mobile; web memakai cookie httpOnly.
type LoginResponse struct {
	AccessToken string       `json:"access_token,omitempty"`
	TokenType   string       `json:"token_type,omitempty"`
	ExpiresAt   time.Time    `json:"expires_at"`
	User        UserResponse `json:"user"`
}

// ToResponse mengubah model User menjadi UserResponse
func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:          u.ID,
		SchoolID:    u.SchoolID,
		Email:       u.Email,
		FullName:    u.FullName,
		Phone:       u.Phone,
		ClassName:   u.ClassName,
		Role:        u.Role,
		Status:      u.Status,
		QRCode:      u.QRCode,
		Balance:     u.Balance,
		AvatarURL:   u.AvatarURL,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}
