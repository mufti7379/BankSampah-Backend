package withdrawal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/mutation"
	"github.com/banksampahku/backend/internal/notification"
	"github.com/banksampahku/backend/internal/utils"
)

// Error domain penarikan
var (
	ErrNotFound            = errors.New("pengajuan penarikan tidak ditemukan")
	ErrNasabahNotActive    = errors.New("akun nasabah tidak aktif")
	ErrActiveRequestExists = errors.New("masih ada pengajuan penarikan yang sedang diproses")
	ErrInvalidDateRange    = errors.New("date_from tidak boleh setelah date_to")
)

// InsufficientBalanceError dikembalikan jika nominal melebihi saldo.
// ForAdmin menentukan pesan ditujukan ke admin ("saldo nasabah") atau nasabah ("saldo Anda").
type InsufficientBalanceError struct {
	Balance  int64
	ForAdmin bool
}

func (e *InsufficientBalanceError) Error() string {
	return "saldo tidak mencukupi, saldo saat ini " + utils.FormatRupiah(e.Balance)
}

// InvalidTransitionError dikembalikan jika aksi tidak sesuai status pengajuan saat ini
type InvalidTransitionError struct {
	Current string
	Action  string
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("pengajuan berstatus %q tidak bisa di-%s", e.Current, e.Action)
}

// Service berisi logika bisnis penarikan saldo
type Service struct {
	repo *Repository
}

// NewService membuat service penarikan
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// =====================================================================
// Nasabah
// =====================================================================

// Create membuat pengajuan penarikan baru (status pending) dan memberi tahu admin.
func (s *Service) Create(ctx context.Context, nasabah *auth.User, req CreateRequest) (*Item, error) {
	var id string
	err := s.repo.DB(ctx).Transaction(func(tx *gorm.DB) error {
		// Kunci baris nasabah: saldo & pengajuan aktif dicek tanpa diganggu transaksi lain
		n, err := lockNasabahTx(tx, nasabah.ID)
		if err != nil {
			return err
		}
		if n.Status != auth.StatusActive {
			return ErrNasabahNotActive
		}
		if req.Amount > n.Balance {
			return &InsufficientBalanceError{Balance: n.Balance}
		}
		if active, err := activeRequestTx(tx, nasabah.ID); err != nil {
			return err
		} else if active != "" {
			return ErrActiveRequestExists
		}

		id, err = insertTx(tx, nasabah.ID, req.Amount)
		if err != nil {
			if utils.UniqueViolation(err) == "uq_withdrawals_one_active" {
				return ErrActiveRequestExists // lapis kedua: unique index di database
			}
			return err
		}

		return notification.CreateForAdminsTx(tx, n.SchoolID, notification.New{
			Type:  notification.TypeWithdrawalRequested,
			Title: "Pengajuan Penarikan Baru",
			Body: fmt.Sprintf("%s mengajukan penarikan saldo %s. Silakan verifikasi di menu Penarikan.",
				n.FullName, utils.FormatRupiah(req.Amount)),
			ReferenceID: &id,
		})
	})
	if err != nil {
		return nil, err
	}

	log.Info().Str("withdrawal_id", id).Str("nasabah_id", nasabah.ID).Int64("amount", req.Amount).
		Msg("Pengajuan penarikan dibuat")
	return s.repo.Find(ctx, filter{SchoolID: nasabah.SchoolID, NasabahID: nasabah.ID}, id, false)
}

// Cancel membatalkan pengajuan milik nasabah yang masih pending
func (s *Service) Cancel(ctx context.Context, nasabah *auth.User, id string) (*Item, error) {
	err := s.repo.DB(ctx).Transaction(func(tx *gorm.DB) error {
		w, err := lockRequestTx(tx, id, nasabah.SchoolID, nasabah.ID)
		if err != nil {
			return err
		}
		if w.Status != StatusPending {
			return &InvalidTransitionError{Current: w.Status, Action: "batalkan"}
		}
		return updateStatusTx(tx, w.ID, StatusCancelled, nil, cancelNote)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, filter{SchoolID: nasabah.SchoolID, NasabahID: nasabah.ID}, id, false)
}

// ListMine mengambil riwayat pengajuan milik nasabah
func (s *Service) ListMine(ctx context.Context, nasabah *auth.User, q MyListQuery) ([]Item, int64, error) {
	return s.repo.List(ctx, filter{SchoolID: nasabah.SchoolID, NasabahID: nasabah.ID, Status: q.Status}, q.PageQuery, false)
}

// GetMine mengambil detail pengajuan milik nasabah
func (s *Service) GetMine(ctx context.Context, nasabah *auth.User, id string) (*Item, error) {
	return s.repo.Find(ctx, filter{SchoolID: nasabah.SchoolID, NasabahID: nasabah.ID}, id, false)
}

// =====================================================================
// Admin
// =====================================================================

// ListAdmin mengambil daftar pengajuan di sekolah admin + jumlah per status
func (s *Service) ListAdmin(ctx context.Context, admin *auth.User, q AdminListQuery) (*AdminListResponse, error) {
	f := filter{SchoolID: admin.SchoolID, NasabahID: q.NasabahID, Status: q.Status, Search: q.Search}
	if q.DateFrom != "" && q.DateTo != "" && q.DateFrom > q.DateTo {
		return nil, ErrInvalidDateRange
	}
	if q.DateFrom != "" {
		t, _ := time.ParseInLocation("2006-01-02", q.DateFrom, time.Local)
		f.From = &t
	}
	if q.DateTo != "" {
		t, _ := time.ParseInLocation("2006-01-02", q.DateTo, time.Local)
		t = t.AddDate(0, 0, 1) // date_to inklusif
		f.To = &t
	}

	items, total, err := s.repo.List(ctx, f, q.PageQuery, true)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.StatusCounts(ctx, admin.SchoolID)
	if err != nil {
		return nil, err
	}
	return &AdminListResponse{Items: items, Meta: utils.NewPageMeta(q.PageQuery, total), StatusCounts: counts}, nil
}

// GetAdmin mengambil detail pengajuan di sekolah admin
func (s *Service) GetAdmin(ctx context.Context, admin *auth.User, id string) (*Item, error) {
	return s.repo.Find(ctx, filter{SchoolID: admin.SchoolID}, id, true)
}

// transition menjalankan satu aksi admin dalam DB transaction: kunci pengajuan,
// cek status asal, jalankan aksi, lalu catat audit log.
func (s *Service) transition(ctx context.Context, admin *auth.User, id, action, auditAction string,
	allowedFrom []string, ip string, apply func(tx *gorm.DB, w *lockedRequest) (map[string]any, error)) (*Item, error) {

	err := s.repo.DB(ctx).Transaction(func(tx *gorm.DB) error {
		w, err := lockRequestTx(tx, id, admin.SchoolID, "")
		if err != nil {
			return err
		}
		if !slices.Contains(allowedFrom, w.Status) {
			return &InvalidTransitionError{Current: w.Status, Action: action}
		}

		meta, err := apply(tx, w)
		if err != nil {
			return err
		}
		meta["nasabah_id"] = w.NasabahID
		meta["nasabah_name"] = w.NasabahName
		meta["amount"] = w.Amount
		meta["status_before"] = w.Status

		return audit.RecordTx(tx, audit.Entry{
			ActorID: admin.ID, Action: auditAction, Entity: audit.EntityWithdrawal,
			EntityID: w.ID, IPAddress: ip, Metadata: meta,
		})
	})
	if err != nil {
		return nil, err
	}

	log.Info().Str("withdrawal_id", id).Str("action", auditAction).Str("admin_id", admin.ID).
		Msg("Pengajuan penarikan diproses")
	return s.repo.Find(ctx, filter{SchoolID: admin.SchoolID}, id, true)
}

// Approve menyetujui pengajuan: uang siap diserahkan tunai (saldo BELUM didebit)
func (s *Service) Approve(ctx context.Context, admin *auth.User, id string, req ApproveRequest, ip string) (*Item, error) {
	note := noteOrDefault(req.Note, defaultApproveNote)
	return s.transition(ctx, admin, id, "setujui", audit.ActionWithdrawalApprove,
		[]string{StatusPending}, ip,
		func(tx *gorm.DB, w *lockedRequest) (map[string]any, error) {
			if err := updateStatusTx(tx, w.ID, StatusApproved, &admin.ID, note); err != nil {
				return nil, err
			}
			err := notification.CreateTx(tx, notification.New{
				UserID: w.NasabahID, Type: notification.TypeWithdrawalApproved, ReferenceID: &w.ID,
				Title: "Penarikan Disetujui",
				Body: fmt.Sprintf("Pengajuan penarikan %s telah disetujui. Silakan ambil ke %s di Bank Sampah Sekolah.",
					utils.FormatRupiah(w.Amount), admin.FullName),
			})
			return map[string]any{"status_after": StatusApproved, "note": note}, err
		})
}

// Reject menolak pengajuan (pending/approved) dengan alasan wajib; saldo tidak berubah
func (s *Service) Reject(ctx context.Context, admin *auth.User, id string, req RejectRequest, ip string) (*Item, error) {
	note := strings.TrimSpace(req.Note)
	return s.transition(ctx, admin, id, "tolak", audit.ActionWithdrawalReject,
		[]string{StatusPending, StatusApproved}, ip,
		func(tx *gorm.DB, w *lockedRequest) (map[string]any, error) {
			if err := updateStatusTx(tx, w.ID, StatusRejected, &admin.ID, note); err != nil {
				return nil, err
			}
			err := notification.CreateTx(tx, notification.New{
				UserID: w.NasabahID, Type: notification.TypeWithdrawalRejected, ReferenceID: &w.ID,
				Title: "Penarikan Ditolak",
				Body: fmt.Sprintf("Pengajuan penarikan %s ditolak. Alasan: %s. Saldo Anda tidak berubah.",
					utils.FormatRupiah(w.Amount), note),
			})
			return map[string]any{"status_after": StatusRejected, "note": note}, err
		})
}

// Complete menandai uang sudah diterima nasabah: saldo didebit + mutasi "debit" dicatat
func (s *Service) Complete(ctx context.Context, admin *auth.User, id string, req ApproveRequest, ip string) (*Item, error) {
	note := noteOrDefault(req.Note, defaultCompleteNote)
	return s.transition(ctx, admin, id, "selesaikan", audit.ActionWithdrawalComplete,
		[]string{StatusApproved}, ip,
		func(tx *gorm.DB, w *lockedRequest) (map[string]any, error) {
			entry, err := mutation.DebitTx(tx, w.NasabahID, w.Amount, mutation.RefWithdrawal, &w.ID,
				fmt.Sprintf("Penarikan saldo tunai %s", utils.FormatRupiah(w.Amount)))
			if errors.Is(err, mutation.ErrInsufficientBalance) {
				var bal int64
				if qerr := tx.Raw(`SELECT balance FROM users WHERE id = ?`, w.NasabahID).Scan(&bal).Error; qerr != nil {
					return nil, qerr
				}
				return nil, &InsufficientBalanceError{Balance: bal, ForAdmin: true}
			}
			if err != nil {
				return nil, err
			}
			if err := updateStatusTx(tx, w.ID, StatusCompleted, &admin.ID, note); err != nil {
				return nil, err
			}
			err = notification.CreateTx(tx, notification.New{
				UserID: w.NasabahID, Type: notification.TypeWithdrawalCompleted, ReferenceID: &w.ID,
				Title: "Penarikan Selesai",
				Body: fmt.Sprintf("Penarikan %s telah selesai dan uang tunai sudah Anda terima. Saldo Anda sekarang %s.",
					utils.FormatRupiah(w.Amount), utils.FormatRupiah(entry.BalanceAfter)),
			})
			return map[string]any{
				"status_after":   StatusCompleted,
				"note":           note,
				"balance_before": entry.BalanceBefore,
				"balance_after":  entry.BalanceAfter,
				"mutation_id":    entry.ID,
			}, err
		})
}

// noteOrDefault memakai catatan admin jika diisi, atau teks bawaan
func noteOrDefault(note *string, fallback string) string {
	if note != nil {
		if v := strings.TrimSpace(*note); v != "" {
			return v
		}
	}
	return fallback
}
