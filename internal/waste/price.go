package waste

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Error domain harga sampah
var (
	ErrPriceDateTaken    = errors.New("harga untuk tanggal ini sudah ada")
	ErrPriceDateTooOld   = errors.New("tanggal berlaku terlalu jauh ke belakang")
	ErrPriceDateTooFar   = errors.New("tanggal berlaku terlalu jauh ke depan")
	ErrNoActivePrice     = errors.New("belum ada harga yang berlaku pada tanggal tersebut")
	ErrInvalidDateFormat = errors.New("format tanggal harus YYYY-MM-DD")
	ErrPriceNotFound     = errors.New("harga tidak ditemukan")
	ErrPriceNotScheduled = errors.New("hanya harga terjadwal yang bisa dibatalkan")
)

// Batas tanggal berlaku harga
const (
	// MaxPriceBackdateDays disamakan dengan batas back-date penimbangan (PRD Bab 6D: maks 7 hari)
	MaxPriceBackdateDays = 7
	// MaxPriceScheduleDays membatasi penjadwalan harga maksimal 1 tahun ke depan
	MaxPriceScheduleDays = 365
	// MaxPricePerUnit adalah batas wajar harga per satuan (Rp 100 juta) untuk mencegah salah ketik
	MaxPricePerUnit = 100_000_000
)

// Status harga pada histori
const (
	PriceStatusActive    = "active"    // harga yang berlaku hari ini
	PriceStatusScheduled = "scheduled" // berlaku di masa depan
	PriceStatusExpired   = "expired"   // sudah digantikan harga yang lebih baru
)

// =====================================================================
// Model & DTO
// =====================================================================

// PriceHistory memetakan tabel price_histories (immutable: tidak pernah di-update/delete)
type PriceHistory struct {
	ID            string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	WasteTypeID   string    `gorm:"column:waste_type_id;type:uuid"`
	PricePerUnit  int64     `gorm:"column:price_per_unit"`
	EffectiveDate time.Time `gorm:"column:effective_date;type:date"`
	SetBy         *string   `gorm:"column:set_by;type:uuid"`
	Note          *string   `gorm:"column:note"`
	CreatedAt     time.Time `gorm:"column:created_at"`

	SetByName *string `gorm:"column:set_by_name;->"` // hasil JOIN users, read-only
}

func (PriceHistory) TableName() string { return "price_histories" }

// CreatePriceRequest adalah body POST /admin/waste-types/:id/prices
type CreatePriceRequest struct {
	PricePerUnit  int64   `json:"price_per_unit" binding:"gt=0,max=100000000"` // PRD: tolak harga <= 0
	EffectiveDate string  `json:"effective_date" binding:"required,datetime=2006-01-02"`
	Note          *string `json:"note" binding:"omitempty,max=500"`
}

// PriceHistoryQuery adalah query GET /admin/waste-types/:id/prices
type PriceHistoryQuery struct {
	utils.PageQuery
}

// ActivePriceQuery adalah query GET /admin/waste-types/:id/price-active
type ActivePriceQuery struct {
	Date string `form:"date" binding:"omitempty,datetime=2006-01-02"` // default: hari ini
}

// PriceSetBy adalah info admin yang menetapkan harga
type PriceSetBy struct {
	ID       string  `json:"id"`
	FullName *string `json:"full_name"`
}

// PriceResponse adalah satu baris histori harga
type PriceResponse struct {
	ID            string      `json:"id"`
	WasteTypeID   string      `json:"waste_type_id"`
	PricePerUnit  int64       `json:"price_per_unit"`
	EffectiveDate string      `json:"effective_date"` // YYYY-MM-DD
	Status        string      `json:"status"`         // active | scheduled | expired
	Note          *string     `json:"note"`
	SetBy         *PriceSetBy `json:"set_by"`
	CreatedAt     time.Time   `json:"created_at"`
}

// PriceHistoryResponse adalah respons histori harga satu jenis sampah
type PriceHistoryResponse struct {
	WasteType TypeResponse                       `json:"waste_type"`
	History   utils.PaginatedData[PriceResponse] `json:"history"`
}

// ActivePriceResponse adalah respons harga aktif pada tanggal tertentu
type ActivePriceResponse struct {
	WasteTypeID   string `json:"waste_type_id"`
	WasteTypeName string `json:"waste_type_name"`
	Unit          string `json:"unit"`
	Date          string `json:"date"`           // tanggal yang ditanyakan
	PriceID       string `json:"price_id"`       // ID baris price_histories (untuk snapshot)
	PricePerUnit  int64  `json:"price_per_unit"` // Rupiah per satuan
	EffectiveDate string `json:"effective_date"` // sejak kapan harga ini berlaku
}

// toResponse mengubah PriceHistory menjadi PriceResponse dengan status relatif terhadap hari ini
func (p *PriceHistory) toResponse(activeID string) PriceResponse {
	status := PriceStatusExpired
	switch {
	case p.ID == activeID:
		status = PriceStatusActive
	case p.EffectiveDate.Format(DateLayout) > Today():
		status = PriceStatusScheduled
	}

	var setBy *PriceSetBy
	if p.SetBy != nil {
		setBy = &PriceSetBy{ID: *p.SetBy, FullName: p.SetByName}
	}
	return PriceResponse{
		ID: p.ID, WasteTypeID: p.WasteTypeID, PricePerUnit: p.PricePerUnit,
		EffectiveDate: p.EffectiveDate.Format(DateLayout), Status: status,
		Note: p.Note, SetBy: setBy, CreatedAt: p.CreatedAt,
	}
}

// =====================================================================
// Repository
// =====================================================================

// PriceRepository menangani akses data price_histories
type PriceRepository struct {
	db *gorm.DB
}

// NewPriceRepository membuat repository harga
func NewPriceRepository(db *gorm.DB) *PriceRepository {
	return &PriceRepository{db: db}
}

// ActivePriceTx mencari harga aktif sebuah jenis sampah pada tanggal tertentu:
// harga dengan effective_date <= date yang paling baru (PRD Bab 6C).
// Menerima *gorm.DB agar bisa dipakai di dalam DB transaction penimbangan (Task 3.2).
func ActivePriceTx(tx *gorm.DB, wasteTypeID, date string) (*PriceHistory, error) {
	var p PriceHistory
	err := tx.Model(&PriceHistory{}).
		Where("waste_type_id = ? AND effective_date <= ?", wasteTypeID, date).
		Order("effective_date DESC").
		Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoActivePrice
	}
	return &p, err
}

// ActivePrice adalah ActivePriceTx tanpa transaksi
func (r *PriceRepository) ActivePrice(ctx context.Context, wasteTypeID, date string) (*PriceHistory, error) {
	return ActivePriceTx(r.db.WithContext(ctx), wasteTypeID, date)
}

// History mengambil histori harga (tanggal berlaku terbaru di atas) dengan pagination
func (r *PriceRepository) History(ctx context.Context, wasteTypeID string, q PriceHistoryQuery) ([]PriceHistory, int64, error) {
	base := r.db.WithContext(ctx).Table("price_histories AS ph").Where("ph.waste_type_id = ?", wasteTypeID)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []PriceHistory
	err := base.Select("ph.*, u.full_name AS set_by_name").
		Joins("LEFT JOIN users u ON u.id = ph.set_by").
		Order("ph.effective_date DESC").
		Limit(q.Limit).Offset(q.Offset()).
		Find(&items).Error
	return items, total, err
}

// ExistsOnDate mengecek apakah sudah ada harga pada tanggal tersebut
func (r *PriceRepository) ExistsOnDate(ctx context.Context, wasteTypeID, date string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&PriceHistory{}).
		Where("waste_type_id = ? AND effective_date = ?", wasteTypeID, date).Count(&count).Error
	return count > 0, err
}

// Create menyimpan harga baru. Tanggal dikirim sebagai teks YYYY-MM-DD (bukan time.Time)
// agar tidak bergeser sehari akibat konversi zona waktu sesi database.
func (r *PriceRepository) Create(ctx context.Context, p *PriceHistory, effectiveDate string) error {
	// Scan ke struct terpisah: Scan langsung ke p akan mengosongkan field lain (misal EffectiveDate)
	var returned struct {
		ID        string
		CreatedAt time.Time
	}
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO price_histories (waste_type_id, price_per_unit, effective_date, set_by, note)
		VALUES (?, ?, ?::date, ?, ?)
		RETURNING id, created_at`,
		p.WasteTypeID, p.PricePerUnit, effectiveDate, p.SetBy, p.Note,
	).Scan(&returned).Error
	if err != nil {
		return err
	}
	p.ID, p.CreatedAt = returned.ID, returned.CreatedAt
	return nil
}

// DeleteScheduled menghapus harga yang BELUM berlaku (effective_date > today).
// Syarat "masih terjadwal" dicek di dalam DELETE yang sama (atomik), sehingga aman
// walaupun dipanggil tepat saat pergantian hari. Mengembalikan baris yang dihapus.
func (r *PriceRepository) DeleteScheduled(ctx context.Context, wasteTypeID, priceID, today string) (*PriceHistory, error) {
	var deleted []PriceHistory
	err := r.db.WithContext(ctx).Raw(`
		DELETE FROM price_histories
		WHERE id = ? AND waste_type_id = ? AND effective_date > ?::date
		RETURNING id, waste_type_id, price_per_unit, effective_date, set_by, note, created_at`,
		priceID, wasteTypeID, today,
	).Scan(&deleted).Error
	if err != nil {
		return nil, err
	}
	if len(deleted) == 1 {
		return &deleted[0], nil
	}

	// Tidak ada yang terhapus: bedakan "tidak ada" vs "sudah berlaku"
	var count int64
	if err := r.db.WithContext(ctx).Model(&PriceHistory{}).
		Where("id = ? AND waste_type_id = ?", priceID, wasteTypeID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrPriceNotFound
	}
	return nil, ErrPriceNotScheduled
}

// =====================================================================
// Service
// =====================================================================

// PriceService berisi logika bisnis harga dinamis
type PriceService struct {
	repo  *PriceRepository
	types *TypeService
}

// NewPriceService membuat service harga
func NewPriceService(repo *PriceRepository, types *TypeService) *PriceService {
	return &PriceService{repo: repo, types: types}
}

// parseDate mem-parsing YYYY-MM-DD di zona waktu aplikasi
func parseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, s, time.Local)
	if err != nil {
		return time.Time{}, ErrInvalidDateFormat
	}
	return t, nil
}

// Create menetapkan harga baru untuk jenis sampah mulai effective_date.
// Mengembalikan juga harga aktif sebelumnya (untuk metadata audit before/after).
func (s *PriceService) Create(ctx context.Context, admin *auth.User, typeID string, req CreatePriceRequest) (*PriceHistory, *WasteType, *PriceHistory, error) {
	wt, err := s.types.Get(ctx, admin, typeID)
	if err != nil {
		return nil, nil, nil, err
	}

	effective, err := parseDate(req.EffectiveDate)
	if err != nil {
		return nil, nil, nil, err
	}
	today, _ := parseDate(Today())
	if effective.Before(today.AddDate(0, 0, -MaxPriceBackdateDays)) {
		return nil, nil, nil, ErrPriceDateTooOld
	}
	if effective.After(today.AddDate(0, 0, MaxPriceScheduleDays)) {
		return nil, nil, nil, ErrPriceDateTooFar
	}

	// UNIQUE (waste_type_id, effective_date): satu tanggal hanya boleh satu harga
	exists, err := s.repo.ExistsOnDate(ctx, wt.ID, req.EffectiveDate)
	if err != nil {
		return nil, nil, nil, err
	}
	if exists {
		return nil, nil, nil, ErrPriceDateTaken
	}

	// Harga yang berlaku pada tanggal tersebut sebelum harga baru ini ditambahkan
	previous, err := s.repo.ActivePrice(ctx, wt.ID, req.EffectiveDate)
	if err != nil && !errors.Is(err, ErrNoActivePrice) {
		return nil, nil, nil, err
	}

	p := &PriceHistory{
		WasteTypeID:   wt.ID,
		PricePerUnit:  req.PricePerUnit,
		EffectiveDate: effective,
		SetBy:         &admin.ID,
		Note:          trimPtr(req.Note),
	}
	if err := s.repo.Create(ctx, p, req.EffectiveDate); err != nil {
		if utils.UniqueViolation(err) != "" {
			return nil, nil, nil, ErrPriceDateTaken // dua request bersamaan di tanggal yang sama
		}
		return nil, nil, nil, err
	}
	p.SetByName = &admin.FullName
	return p, wt, previous, nil
}

// History mengambil histori harga jenis sampah beserta status tiap baris
func (s *PriceService) History(ctx context.Context, admin *auth.User, typeID string, q PriceHistoryQuery) (*PriceHistoryResponse, error) {
	wt, err := s.types.Get(ctx, admin, typeID)
	if err != nil {
		return nil, err
	}

	items, total, err := s.repo.History(ctx, wt.ID, q)
	if err != nil {
		return nil, err
	}

	activeID := ""
	if active, err := s.repo.ActivePrice(ctx, wt.ID, Today()); err == nil {
		activeID = active.ID
	} else if !errors.Is(err, ErrNoActivePrice) {
		return nil, err
	}

	out := make([]PriceResponse, len(items))
	for i := range items {
		out[i] = items[i].toResponse(activeID)
	}
	return &PriceHistoryResponse{
		WasteType: wt.toResponse(),
		History:   utils.PaginatedData[PriceResponse]{Items: out, Meta: utils.NewPageMeta(q.PageQuery, total)},
	}, nil
}

// CancelScheduled membatalkan harga terjadwal (belum berlaku). Harga yang sudah
// aktif/kedaluwarsa tidak bisa dibatalkan karena mungkin sudah dipakai transaksi.
func (s *PriceService) CancelScheduled(ctx context.Context, admin *auth.User, typeID, priceID string) (*PriceHistory, *WasteType, error) {
	wt, err := s.types.Get(ctx, admin, typeID)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.repo.DeleteScheduled(ctx, wt.ID, priceID, Today())
	if err != nil {
		return nil, nil, err
	}
	return p, wt, nil
}

// ActiveOn mengambil harga aktif jenis sampah pada tanggal tertentu (default hari ini)
func (s *PriceService) ActiveOn(ctx context.Context, admin *auth.User, typeID, date string) (*ActivePriceResponse, error) {
	wt, err := s.types.Get(ctx, admin, typeID)
	if err != nil {
		return nil, err
	}
	if date == "" {
		date = Today()
	}

	p, err := s.repo.ActivePrice(ctx, wt.ID, date)
	if err != nil {
		return nil, err
	}
	return &ActivePriceResponse{
		WasteTypeID: wt.ID, WasteTypeName: wt.Name, Unit: wt.Unit, Date: date,
		PriceID: p.ID, PricePerUnit: p.PricePerUnit, EffectiveDate: p.EffectiveDate.Format(DateLayout),
	}, nil
}

// =====================================================================
// Handler
// =====================================================================

// CreatePrice: POST /api/v1/admin/waste-types/:id/prices
func (h *Handler) CreatePrice(c *gin.Context) {
	var req CreatePriceRequest
	if !utils.BindJSON(c, &req) {
		return
	}

	p, wt, previous, err := h.prices.Create(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req)
	if err != nil {
		h.respondError(c, err)
		return
	}

	var before any
	if previous != nil {
		before = map[string]any{
			"price_per_unit": previous.PricePerUnit,
			"effective_date": previous.EffectiveDate.Format(DateLayout),
		}
	}
	middleware.SetAuditEntityID(c, p.ID)
	middleware.SetAuditMetadata(c, map[string]any{
		"waste_type_id":   wt.ID,
		"waste_type_name": wt.Name,
		"before":          before,
		"after": map[string]any{
			"price_per_unit": p.PricePerUnit,
			"effective_date": p.EffectiveDate.Format(DateLayout),
		},
		"note": p.Note,
	})

	resp := p.toResponse("")
	msg := "Harga baru berhasil dijadwalkan dan akan berlaku mulai " + resp.EffectiveDate
	if resp.EffectiveDate <= Today() {
		resp.Status = PriceStatusActive
		msg = "Harga baru berhasil disimpan dan langsung berlaku"
		// Harga back-date bisa saja sudah digantikan harga lain yang lebih baru
		if active, err := h.prices.repo.ActivePrice(c.Request.Context(), wt.ID, Today()); err == nil && active.ID != p.ID {
			resp.Status = PriceStatusExpired
			msg = "Harga berhasil disimpan untuk tanggal " + resp.EffectiveDate +
				", namun hari ini sudah berlaku harga lain yang lebih baru"
		}
	}
	utils.RespondSuccess(c, http.StatusCreated, msg, resp)
}

// ListPrices: GET /api/v1/admin/waste-types/:id/prices?page=&limit=
func (h *Handler) ListPrices(c *gin.Context) {
	var q PriceHistoryQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	res, err := h.prices.History(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Histori harga berhasil diambil", res)
}

// CancelPrice: DELETE /api/v1/admin/waste-types/:id/prices/:priceId
// Membatalkan harga terjadwal (status "scheduled"). Jejaknya tetap ada di audit log.
func (h *Handler) CancelPrice(c *gin.Context) {
	p, wt, err := h.prices.CancelScheduled(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), c.Param("priceId"))
	if err != nil {
		h.respondError(c, err)
		return
	}

	middleware.SetAuditEntityID(c, p.ID)
	middleware.SetAuditMetadata(c, map[string]any{
		"waste_type_id":   wt.ID,
		"waste_type_name": wt.Name,
		"cancelled": map[string]any{
			"price_per_unit": p.PricePerUnit,
			"effective_date": p.EffectiveDate.Format(DateLayout),
			"note":           p.Note,
			"set_by":         p.SetBy,
		},
	})

	resp := p.toResponse("")
	resp.Status = "cancelled"
	utils.RespondSuccess(c, http.StatusOK,
		"Harga terjadwal "+utils.FormatRupiah(p.PricePerUnit)+" untuk tanggal "+resp.EffectiveDate+" berhasil dibatalkan",
		resp)
}

// ActivePrice: GET /api/v1/admin/waste-types/:id/price-active?date=YYYY-MM-DD
func (h *Handler) ActivePrice(c *gin.Context) {
	var q ActivePriceQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	res, err := h.prices.ActiveOn(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), q.Date)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Harga aktif berhasil diambil", res)
}
