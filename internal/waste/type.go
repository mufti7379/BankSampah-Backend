package waste

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Error domain jenis sampah
var (
	ErrTypeNotFound        = errors.New("jenis sampah tidak ditemukan")
	ErrTypeNameTaken       = errors.New("nama jenis sampah sudah digunakan di kategori ini")
	ErrTypeAlreadyInactive = errors.New("jenis sampah sudah nonaktif")
	ErrCategoryInvalid     = errors.New("kategori tidak ditemukan")
	ErrCategoryInactive    = errors.New("kategori sedang nonaktif")
)

// Satuan yang didukung (PRD: default per kg)
const DefaultUnit = "kg"

// =====================================================================
// Model & DTO
// =====================================================================

// WasteType memetakan tabel waste_types (+ kolom hasil JOIN, read-only)
type WasteType struct {
	ID         string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	CategoryID string    `gorm:"column:category_id;type:uuid"`
	Name       string    `gorm:"column:name"`
	Unit       string    `gorm:"column:unit"`
	IsActive   bool      `gorm:"column:is_active"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`

	CategoryName       string     `gorm:"column:category_name;->"`
	CategoryActive     bool       `gorm:"column:category_active;->"`
	CurrentPrice       *int64     `gorm:"column:current_price;->"`        // harga aktif hari ini (Rupiah)
	PriceEffectiveDate *time.Time `gorm:"column:price_effective_date;->"` // tanggal berlaku harga aktif
}

func (WasteType) TableName() string { return "waste_types" }

// CreateTypeRequest adalah body POST /admin/waste-types
type CreateTypeRequest struct {
	CategoryID string `json:"category_id" binding:"required,uuid"`
	Name       string `json:"name" binding:"required,min=2,max=100"`
	Unit       string `json:"unit" binding:"omitempty,oneof=kg pcs liter"`
}

// UpdateTypeRequest adalah body PATCH /admin/waste-types/:id (semua opsional).
// category_id dipakai untuk memindahkan jenis sampah ke kategori lain.
type UpdateTypeRequest struct {
	CategoryID *string `json:"category_id" binding:"omitempty,uuid"`
	Name       *string `json:"name" binding:"omitempty,min=2,max=100"`
	Unit       *string `json:"unit" binding:"omitempty,oneof=kg pcs liter"`
	IsActive   *bool   `json:"is_active"`
}

// ListTypeQuery adalah query GET /admin/waste-types
type ListTypeQuery struct {
	utils.PageQuery
	CategoryID string `form:"category_id" binding:"omitempty,uuid"`
	Search     string `form:"search" binding:"omitempty,max=100"`
	Status     string `form:"status" binding:"omitempty,oneof=active inactive all"` // default: all
}

// TypeResponse adalah data jenis sampah untuk admin
type TypeResponse struct {
	ID                 string    `json:"id"`
	CategoryID         string    `json:"category_id"`
	CategoryName       string    `json:"category_name"`
	CategoryActive     bool      `json:"category_active"`
	Name               string    `json:"name"`
	Unit               string    `json:"unit"`
	IsActive           bool      `json:"is_active"`
	CurrentPrice       *int64    `json:"current_price"`        // null jika belum ada harga berlaku
	PriceEffectiveDate *string   `json:"price_effective_date"` // YYYY-MM-DD
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (t *WasteType) toResponse() TypeResponse {
	var effective *string
	if t.PriceEffectiveDate != nil {
		s := t.PriceEffectiveDate.Format("2006-01-02")
		effective = &s
	}
	return TypeResponse{
		ID: t.ID, CategoryID: t.CategoryID, CategoryName: t.CategoryName, CategoryActive: t.CategoryActive,
		Name: t.Name, Unit: t.Unit, IsActive: t.IsActive,
		CurrentPrice: t.CurrentPrice, PriceEffectiveDate: effective,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// =====================================================================
// Repository
// =====================================================================

// TypeRepository menangani akses data waste_types
type TypeRepository struct {
	db *gorm.DB
}

// NewTypeRepository membuat repository jenis sampah
func NewTypeRepository(db *gorm.DB) *TypeRepository {
	return &TypeRepository{db: db}
}

// scoped membatasi jenis sampah ke sekolah admin (lewat kategori induknya)
func (r *TypeRepository) scoped(ctx context.Context, schoolID *string) *gorm.DB {
	return r.db.WithContext(ctx).Table("waste_types AS t").
		Joins("JOIN waste_categories c ON c.id = t.category_id").
		Where("c.school_id IS NOT DISTINCT FROM ?", schoolID)
}

// withDetails menambahkan nama kategori & harga aktif hari ini.
// Harga aktif = harga dengan effective_date <= hari ini yang paling baru (PRD Bab 6C).
// "Hari ini" dihitung di aplikasi (APP_TIMEZONE), bukan CURRENT_DATE milik database.
func withDetails(q *gorm.DB) *gorm.DB {
	return q.Select(`t.*, c.name AS category_name, c.is_active AS category_active,
			p.price_per_unit AS current_price, p.effective_date AS price_effective_date`).
		Joins(`LEFT JOIN LATERAL (
			SELECT ph.price_per_unit, ph.effective_date FROM price_histories ph
			WHERE ph.waste_type_id = t.id AND ph.effective_date <= ?
			ORDER BY ph.effective_date DESC LIMIT 1
		) p ON TRUE`, Today())
}

// List mengambil daftar jenis sampah dengan filter & pagination
func (r *TypeRepository) List(ctx context.Context, schoolID *string, q ListTypeQuery) ([]WasteType, int64, error) {
	filter := func(db *gorm.DB) *gorm.DB {
		if q.CategoryID != "" {
			db = db.Where("t.category_id = ?", q.CategoryID)
		}
		if s := strings.TrimSpace(q.Search); s != "" {
			db = db.Where("t.name ILIKE ?", "%"+utils.EscapeLike(s)+"%")
		}
		switch q.Status {
		case "active":
			db = db.Where("t.is_active AND c.is_active")
		case "inactive":
			db = db.Where("NOT (t.is_active AND c.is_active)")
		}
		return db
	}

	var total int64
	if err := filter(r.scoped(ctx, schoolID)).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []WasteType
	err := withDetails(filter(r.scoped(ctx, schoolID))).
		Order("c.name ASC, t.name ASC").
		Limit(q.Limit).Offset(q.Offset()).
		Find(&items).Error
	return items, total, err
}

// FindByID mengambil satu jenis sampah milik sekolah admin
func (r *TypeRepository) FindByID(ctx context.Context, schoolID *string, id string) (*WasteType, error) {
	var t WasteType
	err := withDetails(r.scoped(ctx, schoolID)).Where("t.id = ?", id).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTypeNotFound
	}
	return &t, err
}

// NameTaken mengecek nama jenis (case-insensitive) di kategori yang sama
func (r *TypeRepository) NameTaken(ctx context.Context, categoryID, name, excludeID string) (bool, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&WasteType{}).
		Where("category_id = ? AND LOWER(name) = LOWER(?)", categoryID, name)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&count).Error
	return count > 0, err
}

// Create menyimpan jenis sampah baru
func (r *TypeRepository) Create(ctx context.Context, t *WasteType) error {
	return r.db.WithContext(ctx).
		Omit("category_name", "category_active", "current_price", "price_effective_date").
		Create(t).Error
}

// Update memperbarui kolom tertentu
func (r *TypeRepository) Update(ctx context.Context, id string, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&WasteType{}).Where("id = ?", id).Updates(fields).Error
}

// =====================================================================
// Service
// =====================================================================

// TypeService berisi logika bisnis jenis sampah
type TypeService struct {
	repo       *TypeRepository
	categories *CategoryService
}

// NewTypeService membuat service jenis sampah
func NewTypeService(repo *TypeRepository, categories *CategoryService) *TypeService {
	return &TypeService{repo: repo, categories: categories}
}

// validateCategory memastikan kategori ada, milik sekolah admin, dan aktif
func (s *TypeService) validateCategory(ctx context.Context, admin *auth.User, categoryID string) (*Category, error) {
	cat, err := s.categories.repo.FindByID(ctx, admin.SchoolID, categoryID)
	if errors.Is(err, ErrCategoryNotFound) {
		return nil, ErrCategoryInvalid
	}
	if err != nil {
		return nil, err
	}
	if !cat.IsActive {
		return nil, ErrCategoryInactive
	}
	return cat, nil
}

// Create menambah jenis sampah ke kategori aktif
func (s *TypeService) Create(ctx context.Context, admin *auth.User, req CreateTypeRequest) (*WasteType, error) {
	if _, err := s.validateCategory(ctx, admin, req.CategoryID); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	taken, err := s.repo.NameTaken(ctx, req.CategoryID, name, "")
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrTypeNameTaken
	}

	unit := req.Unit
	if unit == "" {
		unit = DefaultUnit
	}
	t := &WasteType{CategoryID: req.CategoryID, Name: name, Unit: unit, IsActive: true}
	if err := s.repo.Create(ctx, t); err != nil {
		if utils.UniqueViolation(err) != "" {
			return nil, ErrTypeNameTaken
		}
		return nil, err
	}
	s.categories.invalidate(admin.SchoolID) // type_count di endpoint publik berubah

	return s.repo.FindByID(ctx, admin.SchoolID, t.ID)
}

// List mengambil daftar jenis sampah untuk admin
func (s *TypeService) List(ctx context.Context, admin *auth.User, q ListTypeQuery) ([]WasteType, int64, error) {
	return s.repo.List(ctx, admin.SchoolID, q)
}

// Get mengambil detail satu jenis sampah
func (s *TypeService) Get(ctx context.Context, admin *auth.User, id string) (*WasteType, error) {
	return s.repo.FindByID(ctx, admin.SchoolID, id)
}

// Update mengubah jenis sampah; mengembalikan perubahan {field: {before, after}} untuk audit
func (s *TypeService) Update(ctx context.Context, admin *auth.User, id string, req UpdateTypeRequest) (*WasteType, map[string]any, error) {
	t, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, nil, err
	}

	fields := map[string]any{}
	changes := map[string]any{}
	track := func(column string, before, after any) {
		fields[column] = after
		changes[column] = map[string]any{"before": before, "after": after}
	}

	// Kategori tujuan (bisa kategori baru jika dipindah)
	targetCategoryID := t.CategoryID
	targetCategoryActive := t.CategoryActive
	if req.CategoryID != nil && *req.CategoryID != t.CategoryID {
		cat, err := s.validateCategory(ctx, admin, *req.CategoryID)
		if err != nil {
			return nil, nil, err
		}
		targetCategoryID, targetCategoryActive = cat.ID, cat.IsActive
		track("category_id", t.CategoryID, cat.ID)
	}

	name := t.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name != t.Name {
			track("name", t.Name, name)
		}
	}
	// Cek bentrok nama jika nama berubah (selain huruf besar/kecil) atau kategori berubah
	if targetCategoryID != t.CategoryID || !strings.EqualFold(name, t.Name) {
		taken, err := s.repo.NameTaken(ctx, targetCategoryID, name, t.ID)
		if err != nil {
			return nil, nil, err
		}
		if taken {
			return nil, nil, ErrTypeNameTaken
		}
	}

	if req.Unit != nil && *req.Unit != t.Unit {
		track("unit", t.Unit, *req.Unit)
	}
	if req.IsActive != nil && *req.IsActive != t.IsActive {
		// Tidak bisa mengaktifkan jenis sampah di dalam kategori yang nonaktif
		if *req.IsActive && !targetCategoryActive {
			return nil, nil, ErrCategoryInactive
		}
		track("is_active", t.IsActive, *req.IsActive)
	}

	if len(fields) == 0 {
		return nil, nil, ErrNoChanges
	}
	if err := s.repo.Update(ctx, t.ID, fields); err != nil {
		if utils.UniqueViolation(err) != "" {
			return nil, nil, ErrTypeNameTaken
		}
		return nil, nil, err
	}
	s.categories.invalidate(admin.SchoolID)

	updated, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	return updated, changes, err
}

// Deactivate menonaktifkan jenis sampah (soft delete). Riwayat penimbangan & harga tetap utuh;
// jenis nonaktif tidak bisa dipilih lagi saat penimbangan.
func (s *TypeService) Deactivate(ctx context.Context, admin *auth.User, id string) (*WasteType, error) {
	t, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, err
	}
	if !t.IsActive {
		return nil, ErrTypeAlreadyInactive
	}
	if err := s.repo.Update(ctx, t.ID, map[string]any{"is_active": false}); err != nil {
		return nil, err
	}
	s.categories.invalidate(admin.SchoolID)
	t.IsActive = false
	return t, nil
}

// =====================================================================
// Handler
// =====================================================================

// CreateType: POST /api/v1/admin/waste-types
func (h *Handler) CreateType(c *gin.Context) {
	var req CreateTypeRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	t, err := h.types.Create(c.Request.Context(), auth.CurrentUser(c), req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditEntityID(c, t.ID)
	middleware.SetAuditMetadata(c, map[string]any{"name": t.Name, "category": t.CategoryName, "unit": t.Unit})

	msg := "Jenis sampah berhasil ditambahkan"
	if t.CurrentPrice == nil {
		msg += ". Jangan lupa atur harganya agar bisa dipakai saat penimbangan"
	}
	utils.RespondSuccess(c, http.StatusCreated, msg, t.toResponse())
}

// ListTypes: GET /api/v1/admin/waste-types?category_id=&search=&status=&page=&limit=
func (h *Handler) ListTypes(c *gin.Context) {
	var q ListTypeQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	items, total, err := h.types.List(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		utils.RespondInternal(c, err)
		return
	}
	out := make([]TypeResponse, len(items))
	for i := range items {
		out[i] = items[i].toResponse()
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar jenis sampah berhasil diambil",
		utils.PaginatedData[TypeResponse]{Items: out, Meta: utils.NewPageMeta(q.PageQuery, total)})
}

// GetType: GET /api/v1/admin/waste-types/:id
func (h *Handler) GetType(c *gin.Context) {
	t, err := h.types.Get(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Detail jenis sampah berhasil diambil", t.toResponse())
}

// UpdateType: PATCH /api/v1/admin/waste-types/:id
func (h *Handler) UpdateType(c *gin.Context) {
	var req UpdateTypeRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	t, changes, err := h.types.Update(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditMetadata(c, map[string]any{"changes": changes})
	utils.RespondSuccess(c, http.StatusOK, "Jenis sampah berhasil diperbarui", t.toResponse())
}

// DeactivateType: DELETE /api/v1/admin/waste-types/:id (soft deactivate)
func (h *Handler) DeactivateType(c *gin.Context) {
	t, err := h.types.Deactivate(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditMetadata(c, map[string]any{"name": t.Name, "category": t.CategoryName})
	utils.RespondSuccess(c, http.StatusOK,
		"Jenis sampah berhasil dinonaktifkan. Riwayat penimbangan & harga tetap tersimpan.", t.toResponse())
}
