package mutation

import (
	"context"
	"errors"
	"time"

	"github.com/banksampahku/backend/internal/auth"
)

// Error validasi periode
var (
	ErrPeriodConflict   = errors.New("gunakan month ATAU date_from/date_to, tidak keduanya")
	ErrInvalidDateRange = errors.New("date_from tidak boleh setelah date_to")
)

// Service berisi logika baca mutasi saldo
type Service struct {
	repo *Repository
}

// NewService membuat service mutasi
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// period mengubah month / date_from / date_to menjadi rentang waktu [from, to)
// menurut zona waktu aplikasi, agar mutasi pukul 23.30 WIB tidak masuk ke hari berikutnya.
func period(q FilterQuery) (from, to *time.Time, err error) {
	if q.Month != "" && (q.DateFrom != "" || q.DateTo != "") {
		return nil, nil, ErrPeriodConflict
	}
	if q.Month != "" {
		start, _ := time.ParseInLocation("2006-01", q.Month, time.Local)
		end := start.AddDate(0, 1, 0)
		return &start, &end, nil
	}
	if q.DateFrom != "" && q.DateTo != "" && q.DateFrom > q.DateTo {
		return nil, nil, ErrInvalidDateRange
	}
	if q.DateFrom != "" {
		t, _ := time.ParseInLocation("2006-01-02", q.DateFrom, time.Local)
		from = &t
	}
	if q.DateTo != "" {
		t, _ := time.ParseInLocation("2006-01-02", q.DateTo, time.Local)
		t = t.AddDate(0, 0, 1) // date_to inklusif → batas eksklusif hari berikutnya
		to = &t
	}
	return from, to, nil
}

func buildFilter(schoolID *string, q FilterQuery) (filter, error) {
	from, to, err := period(q)
	if err != nil {
		return filter{}, err
	}
	return filter{SchoolID: schoolID, Type: q.Type, ReferenceType: q.ReferenceType, From: from, To: to}, nil
}

// ListMine mengambil mutasi milik nasabah yang sedang login
func (s *Service) ListMine(ctx context.Context, nasabah *auth.User, q FilterQuery) (*ListResponse, error) {
	f, err := buildFilter(nasabah.SchoolID, q)
	if err != nil {
		return nil, err
	}
	f.NasabahID = nasabah.ID
	return s.repo.List(ctx, f, q.PageQuery, false)
}

// MyBalance mengambil ringkasan saldo nasabah yang sedang login
func (s *Service) MyBalance(ctx context.Context, nasabah *auth.User) (*BalanceResponse, error) {
	return s.repo.Balance(ctx, nasabah.ID)
}

// ListAll mengambil mutasi seluruh nasabah di sekolah admin (global)
func (s *Service) ListAll(ctx context.Context, admin *auth.User, q AdminListQuery) (*ListResponse, error) {
	f, err := buildFilter(admin.SchoolID, q.FilterQuery)
	if err != nil {
		return nil, err
	}
	f.NasabahID, f.Search = q.NasabahID, q.Search
	return s.repo.List(ctx, f, q.PageQuery, true)
}

// ListForNasabah mengambil mutasi satu nasabah (beserta saldonya) untuk admin
func (s *Service) ListForNasabah(ctx context.Context, admin *auth.User, nasabahID string, q FilterQuery) (*NasabahMutationsResponse, error) {
	nasabah, err := s.repo.FindNasabah(ctx, admin.SchoolID, nasabahID)
	if err != nil {
		return nil, err
	}
	f, err := buildFilter(admin.SchoolID, q)
	if err != nil {
		return nil, err
	}
	f.NasabahID = nasabah.ID

	list, err := s.repo.List(ctx, f, q.PageQuery, false)
	if err != nil {
		return nil, err
	}
	return &NasabahMutationsResponse{Nasabah: *nasabah, Mutations: *list}, nil
}
