package nasabah

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// Error domain modul nasabah
var (
	ErrEmailTaken      = errors.New("email sudah terdaftar")
	ErrInvalidPhone    = errors.New("format nomor HP tidak valid (contoh: 081234567890)")
	ErrAlreadyActive   = errors.New("nasabah sudah aktif")
	ErrAlreadyInactive = errors.New("nasabah sudah nonaktif")
	ErrNoChanges       = errors.New("tidak ada data yang diubah")
)

// tempPasswordLength adalah panjang password sementara untuk nasabah baru
const tempPasswordLength = 10

// Service berisi logika bisnis manajemen nasabah
type Service struct {
	repo     *Repository
	hasher   *utils.PasswordHasher
	qrPrefix string
}

// NewService membuat service nasabah
func NewService(repo *Repository, hasher *utils.PasswordHasher, qrPrefix string) *Service {
	return &Service{repo: repo, hasher: hasher, qrPrefix: qrPrefix}
}

// Create mendaftarkan nasabah baru (status aktif) dengan QR Code unik otomatis
func (s *Service) Create(ctx context.Context, admin *auth.User, req CreateRequest) (*auth.User, *string, error) {
	email := normalizeEmail(req.Email)
	taken, err := s.repo.EmailTaken(ctx, email, "")
	if err != nil {
		return nil, nil, err
	}
	if taken {
		return nil, nil, ErrEmailTaken
	}

	// Password dari admin, atau password sementara yang ditampilkan sekali ke admin
	var initialPassword *string
	password := ""
	if req.Password != nil && *req.Password != "" {
		password = *req.Password
	} else {
		password, err = utils.GenerateTempPassword(tempPasswordLength)
		if err != nil {
			return nil, nil, err
		}
		initialPassword = &password
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, nil, err
	}

	// Coba ulang jika kode QR acak kebetulan bentrok (sangat jarang)
	const maxQRAttempts = 3
	for attempt := 1; attempt <= maxQRAttempts; attempt++ {
		qr, err := utils.GenerateQRCode(s.qrPrefix)
		if err != nil {
			return nil, nil, err
		}

		user := &auth.User{
			SchoolID:     admin.SchoolID,
			Email:        email,
			PasswordHash: hash,
			FullName:     strings.TrimSpace(req.FullName),
			Phone:        trimPtr(req.Phone),
			ClassName:    trimPtr(req.ClassName),
			Role:         auth.RoleNasabah,
			Status:       auth.StatusActive,
			QRCode:       &qr,
		}

		err = s.repo.CreateWithWelcome(ctx, user)
		if err == nil {
			log.Info().Str("nasabah_id", user.ID).Str("qr_code", qr).Str("admin_id", admin.ID).
				Msg("Nasabah baru dibuat oleh admin")
			return user, initialPassword, nil
		}

		switch utils.UniqueViolation(err) {
		case "users_email_key":
			return nil, nil, ErrEmailTaken
		case "users_qr_code_key":
			log.Warn().Int("attempt", attempt).Msg("Kode QR bentrok, membangkitkan ulang")
			continue
		default:
			return nil, nil, fmt.Errorf("gagal menyimpan nasabah: %w", err)
		}
	}
	return nil, nil, errors.New("gagal membangkitkan kode QR unik, silakan coba lagi")
}

// List mengambil daftar nasabah di sekolah admin
func (s *Service) List(ctx context.Context, admin *auth.User, q ListQuery) ([]auth.User, int64, error) {
	return s.repo.List(ctx, admin.SchoolID, q)
}

// Get mengambil satu nasabah di sekolah admin
func (s *Service) Get(ctx context.Context, admin *auth.User, id string) (*auth.User, error) {
	return s.repo.FindByID(ctx, admin.SchoolID, id)
}

// Detail mengambil nasabah beserta ringkasan aktivitasnya
func (s *Service) Detail(ctx context.Context, admin *auth.User, id string) (*DetailResponse, error) {
	user, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, err
	}
	stats, err := s.repo.Stats(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &DetailResponse{Response: toResponse(user), Stats: stats}, nil
}

// Update memperbarui profil nasabah. Mengembalikan data terbaru dan daftar
// perubahan {field: {before, after}} untuk metadata audit log.
func (s *Service) Update(ctx context.Context, admin *auth.User, id string, req UpdateRequest) (*auth.User, map[string]any, error) {
	user, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, nil, err
	}

	fields := map[string]any{}
	changes := map[string]any{}
	track := func(column string, before, after any) {
		fields[column] = after
		changes[column] = map[string]any{"before": before, "after": after}
	}

	if req.FullName != nil {
		if name := strings.TrimSpace(*req.FullName); name != user.FullName {
			track("full_name", user.FullName, name)
		}
	}
	if req.Email != nil {
		email := normalizeEmail(*req.Email)
		if email != user.Email {
			taken, err := s.repo.EmailTaken(ctx, email, user.ID)
			if err != nil {
				return nil, nil, err
			}
			if taken {
				return nil, nil, ErrEmailTaken
			}
			track("email", user.Email, email)
		}
	}
	if req.Phone != nil {
		phone := trimPtr(req.Phone)
		if phone != nil && !utils.IsPhoneID(*phone) {
			return nil, nil, ErrInvalidPhone
		}
		if !equalPtr(phone, user.Phone) {
			track("phone", user.Phone, phone)
		}
	}
	if req.ClassName != nil {
		className := trimPtr(req.ClassName)
		if !equalPtr(className, user.ClassName) {
			track("class_name", user.ClassName, className)
		}
	}

	if len(fields) == 0 {
		return nil, nil, ErrNoChanges
	}
	if err := s.repo.Update(ctx, user.ID, fields); err != nil {
		if utils.UniqueViolation(err) == "users_email_key" {
			return nil, nil, ErrEmailTaken
		}
		return nil, nil, err
	}

	updated, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	return updated, changes, err
}

// Deactivate menonaktifkan nasabah (soft delete - data & riwayat tetap tersimpan).
// Nasabah nonaktif langsung tidak bisa memakai sesi login yang sedang berjalan.
func (s *Service) Deactivate(ctx context.Context, admin *auth.User, id string) (*auth.User, string, error) {
	user, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, "", err
	}
	if user.Status == auth.StatusInactive {
		return nil, "", ErrAlreadyInactive
	}
	before := user.Status
	if err := s.repo.Update(ctx, user.ID, map[string]any{"status": auth.StatusInactive}); err != nil {
		return nil, "", err
	}
	user.Status = auth.StatusInactive
	return user, before, nil
}

// Activate menyetujui nasabah pending (daftar mandiri) atau mengaktifkan kembali nasabah nonaktif
func (s *Service) Activate(ctx context.Context, admin *auth.User, id string) (*auth.User, string, error) {
	user, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, "", err
	}
	if user.Status == auth.StatusActive {
		return nil, "", ErrAlreadyActive
	}
	before := user.Status
	if err := s.repo.Activate(ctx, user); err != nil {
		return nil, "", err
	}
	user.Status = auth.StatusActive
	return user, before, nil
}

// ---------- helper ----------

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// trimPtr merapikan string opsional; string kosong dianggap nil (kolom dikosongkan)
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

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
