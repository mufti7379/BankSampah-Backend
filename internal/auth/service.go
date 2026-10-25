package auth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/mailer"
	"github.com/banksampahku/backend/internal/utils"
)

// Error domain modul auth (dipetakan ke HTTP status oleh handler)
var (
	ErrEmailTaken         = errors.New("email sudah terdaftar")
	ErrSchoolNotFound     = errors.New("sekolah tidak ditemukan")
	ErrInvalidCredentials = errors.New("email atau password salah")
	ErrAccountPending     = errors.New("akun Anda sedang menunggu verifikasi admin")
	ErrAccountInactive    = errors.New("akun Anda telah dinonaktifkan, silakan hubungi admin bank sampah")
	ErrSessionExpired     = errors.New("password akun Anda telah diubah, silakan login kembali")
)

// LockedError dikembalikan saat akun terkunci karena terlalu banyak login gagal
type LockedError struct {
	RetryAfter time.Duration
}

func (e *LockedError) Error() string {
	minutes := int(math.Ceil(e.RetryAfter.Minutes()))
	return fmt.Sprintf("akun terkunci karena terlalu banyak percobaan login gagal. Coba lagi dalam %d menit, "+
		"atau atur ulang password melalui menu \"Lupa Password\"", minutes)
}

// Deps adalah dependensi yang dibutuhkan Service auth
type Deps struct {
	Repo     *Repository
	Hasher   *utils.PasswordHasher
	Tokens   *TokenManager
	Limiter  *LoginLimiter
	Revoked  *RevocationStore
	Mailer   mailer.Mailer
	QRPrefix string

	PasswordResetURL string        // URL halaman reset di frontend
	PasswordResetTTL time.Duration // Masa berlaku link reset
}

// Service berisi logika bisnis autentikasi
type Service struct {
	Deps

	dummyHashOnce sync.Once
	dummyHash     string
}

// NewService membuat service auth
func NewService(deps Deps) *Service {
	return &Service{Deps: deps}
}

// normalizeEmail menyeragamkan email agar pencarian & keunikan tidak case-sensitive
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Register mendaftarkan nasabah baru dengan status "pending" (menunggu verifikasi admin)
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*User, error) {
	email := normalizeEmail(req.Email)

	exists, err := s.Repo.EmailExists(ctx, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrEmailTaken
	}

	// Tentukan sekolah: dari request, atau sekolah default (single-tenant MVP)
	var schoolID *string
	if req.SchoolID != nil && *req.SchoolID != "" {
		ok, err := s.Repo.SchoolExists(ctx, *req.SchoolID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrSchoolNotFound
		}
		schoolID = req.SchoolID
	} else {
		id, err := s.Repo.DefaultSchoolID(ctx)
		if err != nil {
			return nil, err
		}
		if id != "" {
			schoolID = &id
		}
	}

	hash, err := s.Hasher.Hash(req.Password)
	if err != nil {
		return nil, err
	}

	// Coba beberapa kali jika kode QR acak kebetulan bentrok (sangat jarang)
	const maxQRAttempts = 3
	for attempt := 1; attempt <= maxQRAttempts; attempt++ {
		qr, err := utils.GenerateQRCode(s.QRPrefix)
		if err != nil {
			return nil, err
		}

		user := &User{
			SchoolID:     schoolID,
			Email:        email,
			PasswordHash: hash,
			FullName:     strings.TrimSpace(req.FullName),
			Phone:        trimPtr(req.Phone),
			ClassName:    trimPtr(req.ClassName),
			Role:         RoleNasabah,
			Status:       StatusPending,
			QRCode:       &qr,
		}

		err = s.Repo.CreateNasabahWithNotification(ctx, user)
		if err == nil {
			log.Info().Str("user_id", user.ID).Msg("Nasabah baru mendaftar (pending)")
			return user, nil
		}

		switch utils.UniqueViolation(err) {
		case "users_email_key":
			return nil, ErrEmailTaken // race condition: email didaftarkan bersamaan
		case "users_qr_code_key":
			log.Warn().Int("attempt", attempt).Msg("Kode QR bentrok, membangkitkan ulang")
			continue
		default:
			return nil, fmt.Errorf("gagal menyimpan nasabah: %w", err)
		}
	}
	return nil, errors.New("gagal membangkitkan kode QR unik, silakan coba lagi")
}

// Login memverifikasi kredensial dan menerbitkan JWT
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	email := normalizeEmail(req.Email)

	if remaining := s.Limiter.LockedFor(email); remaining > 0 {
		return nil, &LockedError{RetryAfter: remaining}
	}

	user, err := s.Repo.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}

	if user == nil {
		// Tetap jalankan bcrypt agar waktu respons sama dengan email terdaftar
		// (mencegah enumerasi email lewat timing attack)
		_ = s.Hasher.Compare(s.getDummyHash(), req.Password)
		return nil, s.recordFailure(email)
	}

	if err := s.Hasher.Compare(user.PasswordHash, req.Password); err != nil {
		if errors.Is(err, utils.ErrPasswordMismatch) {
			return nil, s.recordFailure(email)
		}
		return nil, err
	}

	// Password benar: reset limiter, lalu cek status akun
	s.Limiter.Reset(email)
	switch user.Status {
	case StatusPending:
		return nil, ErrAccountPending
	case StatusInactive:
		return nil, ErrAccountInactive
	}

	token, expiresAt, err := s.Tokens.Generate(user)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.Repo.UpdateLastLogin(ctx, user.ID, now); err != nil {
		log.Warn().Err(err).Str("user_id", user.ID).Msg("Gagal memperbarui last_login_at")
	} else {
		user.LastLoginAt = &now
	}

	log.Info().Str("user_id", user.ID).Str("role", user.Role).Msg("Login berhasil")
	return &LoginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        user.ToResponse(),
	}, nil
}

// ResolveSession memverifikasi token lalu memuat user terbaru dari database.
// Dipakai middleware auth di setiap request terproteksi, sehingga perubahan
// status akun / password langsung berlaku tanpa menunggu token kedaluwarsa.
//
// Error yang mungkin: ErrInvalidToken, ErrSessionExpired, ErrAccountInactive, ErrAccountPending.
func (s *Service) ResolveSession(ctx context.Context, tokenString string) (*Claims, *User, error) {
	claims, err := s.Tokens.Parse(tokenString)
	if err != nil {
		return nil, nil, err
	}
	if s.Revoked.IsRevoked(claims.ID) {
		return nil, nil, ErrInvalidToken
	}

	user, err := s.Repo.FindByID(ctx, claims.Subject)
	if errors.Is(err, ErrUserNotFound) {
		return nil, nil, ErrInvalidToken
	}
	if err != nil {
		return nil, nil, err
	}

	// Token terbit sebelum password terakhir diganti → sesi lama dicabut.
	// iat berpresisi detik, jadi waktu ganti password dibulatkan ke bawah per detik.
	if user.PasswordChangedAt != nil && claims.IssuedAt != nil &&
		claims.IssuedAt.Time.Before(user.PasswordChangedAt.Truncate(time.Second)) {
		return nil, nil, ErrSessionExpired
	}

	switch user.Status {
	case StatusInactive:
		return nil, nil, ErrAccountInactive
	case StatusPending:
		return nil, nil, ErrAccountPending
	}
	return claims, user, nil
}

// Logout mencabut token sampai masa berlakunya habis
func (s *Service) Logout(claims *Claims) {
	s.Revoked.Revoke(claims.ID, claims.ExpiresAt.Time)
	log.Info().Str("user_id", claims.Subject).Msg("Logout berhasil")
}

// recordFailure mencatat login gagal dan mengembalikan error yang sesuai.
// Saat batas tercapai akun dikunci; user dapat membuka kunci lewat menu "Lupa Password".
func (s *Service) recordFailure(email string) error {
	if lock := s.Limiter.RecordFailure(email); lock > 0 {
		log.Warn().Str("email", maskEmail(email)).Dur("lock", lock).Msg("Akun dikunci karena terlalu banyak login gagal")
		return &LockedError{RetryAfter: lock}
	}
	return ErrInvalidCredentials
}

// getDummyHash membuat hash palsu sekali saja untuk penyamaan waktu respons
func (s *Service) getDummyHash() string {
	s.dummyHashOnce.Do(func() {
		h, err := s.Hasher.Hash("dummy-password-untuk-timing-1234")
		if err != nil {
			log.Error().Err(err).Msg("Gagal membuat dummy hash")
		}
		s.dummyHash = h
	})
	return s.dummyHash
}

// trimPtr merapikan string opsional; string kosong dianggap nil
func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}
