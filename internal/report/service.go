package report

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// Error validasi periode
var (
	ErrInvalidRange = errors.New("tanggal 'from' tidak boleh setelah 'to'")
	ErrRangeTooLong = errors.New("rentang laporan terlalu panjang")
)

// Service merakit laporan dari hasil agregasi repository
type Service struct {
	repo *Repository
}

// NewService membuat service laporan
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// resolvePeriod menormalisasi periode. Kosong = bulan berjalan (tgl 1 s.d. hari ini).
func resolvePeriod(q PeriodQuery) (period, Period, error) {
	const layout = "2006-01-02"
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	to := today
	if q.From != "" {
		from, _ = time.ParseInLocation(layout, q.From, time.Local)
	}
	if q.To != "" {
		to, _ = time.ParseInLocation(layout, q.To, time.Local)
	} else if q.From != "" && from.After(today) {
		to = from // from di masa depan tanpa to → satu hari saja
	}

	if from.After(to) {
		return period{}, Period{}, ErrInvalidRange
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if days > MaxRangeDays {
		return period{}, Period{}, ErrRangeTooLong
	}

	p := period{
		From: from.Format(layout), To: to.Format(layout),
		FromTime: from, ToTime: to.AddDate(0, 0, 1),
	}
	return p, Period{From: p.From, To: p.To, Days: days}, nil
}

// Summary menyusun ringkasan periode untuk dashboard & halaman laporan
func (s *Service) Summary(ctx context.Context, admin *auth.User, q PeriodQuery) (*SummaryResponse, error) {
	p, pr, err := resolvePeriod(q)
	if err != nil {
		return nil, err
	}

	res := &SummaryResponse{Period: pr, GeneratedAt: time.Now()}
	if res.Weighings, err = s.repo.WeighingStats(ctx, admin.SchoolID, p); err != nil {
		return nil, err
	}
	if res.Mutations, err = s.repo.MutationStats(ctx, admin.SchoolID, p); err != nil {
		return nil, err
	}
	if res.Withdrawals, err = s.repo.WithdrawalStats(ctx, admin.SchoolID, p); err != nil {
		return nil, err
	}
	if res.Snapshot, err = s.repo.Snapshot(ctx, admin.SchoolID); err != nil {
		return nil, err
	}
	if res.Daily, err = s.repo.Daily(ctx, admin.SchoolID, p); err != nil {
		return nil, err
	}
	return res, nil
}

// ByCategory menyusun rekap per kategori, masing-masing dirinci per jenis sampah
func (s *Service) ByCategory(ctx context.Context, admin *auth.User, q PeriodQuery) (*ByCategoryResponse, error) {
	p, pr, err := resolvePeriod(q)
	if err != nil {
		return nil, err
	}
	types, err := s.repo.ByType(ctx, admin.SchoolID, p)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CategoryWeighingCounts(ctx, admin.SchoolID, p)
	if err != nil {
		return nil, err
	}

	res := &ByCategoryResponse{Period: pr, Categories: []CategoryRow{}}
	index := map[string]int{} // category_id → posisi di res.Categories
	for _, t := range types {
		i, ok := index[t.CategoryID]
		if !ok {
			res.Categories = append(res.Categories, CategoryRow{
				ID: t.CategoryID, Name: t.CategoryName, WeighingCount: counts[t.CategoryID], Types: []TypeRow{},
			})
			i = len(res.Categories) - 1
			index[t.CategoryID] = i
		}
		cat := &res.Categories[i]
		cat.Types = append(cat.Types, TypeRow{
			ID: t.TypeID, Name: t.TypeName, Unit: t.Unit, Quantity: t.Quantity,
			TotalAmount: t.TotalAmount, WeighingCount: t.WeighingCount,
		})
		cat.TotalAmount += t.TotalAmount
		res.TotalAmount += t.TotalAmount
		if t.Unit == "kg" { // jangan campur kg dengan pcs/liter
			cat.TotalWeightKg += t.Quantity
			res.TotalWeightKg += t.Quantity
		}
	}

	// Persentase kontribusi terhadap total rupiah periode
	for i := range res.Categories {
		cat := &res.Categories[i]
		cat.SharePercent = percent(cat.TotalAmount, res.TotalAmount)
		cat.TotalWeightKg = round2(cat.TotalWeightKg)
		for j := range cat.Types {
			cat.Types[j].SharePercent = percent(cat.Types[j].TotalAmount, res.TotalAmount)
		}
	}
	res.TotalWeightKg = round2(res.TotalWeightKg)
	return res, nil
}

// ByNasabah menyusun rekap per nasabah dengan urutan & pagination
func (s *Service) ByNasabah(ctx context.Context, admin *auth.User, q ByNasabahQuery) (*ByNasabahResponse, error) {
	p, pr, err := resolvePeriod(q.PeriodQuery)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.ByNasabah(ctx, admin.SchoolID, p, q)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []NasabahRow{}
	}
	return &ByNasabahResponse{Period: pr, Items: rows, Meta: utils.NewPageMeta(q.PageQuery, total)}, nil
}

// percent menghitung part/total × 100 dengan 2 desimal
func percent(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return round2(float64(part) * 100 / float64(total))
}

// round2 membulatkan ke 2 desimal (hanya untuk tampilan, bukan perhitungan uang)
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
