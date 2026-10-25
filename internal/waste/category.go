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

// Error domain kategori sampah
var (
	ErrCategoryNotFound        = errors.New("kategori sampah tidak ditemukan")
	ErrCategoryNameTaken       = errors.New("nama kategori sudah digunakan")
	ErrCategoryAlreadyInactive = errors.New("kategori sudah nonaktif")
	ErrNoChanges               = errors.New("tidak ada data yang diubah")
	ErrSchoolNotFound          = errors.New("sekolah tidak ditemukan")
)

// =====================================================================
// Model & DTO
// =====================================================================

// Category memetakan tabel waste_categories
type Category struct {
	ID          string    `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	SchoolID    *string   `gorm:"column:school_id;type:uuid"`
	Name        string    `gorm:"column:name"`
	Description *string   `gorm:"column:description"`
	IsActive    bool      `gorm:"column:is_active"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`

	TypeCount int64 `gorm:"column:type_count;->"` // hasil agregasi, read-only
}

func (Category) TableName() string { return "waste_categories" }

// CreateCategoryRequest adalah body POST /admin/waste-categories
type CreateCategoryRequest struct {
	Name        string  `json:"name" binding:"required,min=2,max=100"`
	Description *string `json:"description" binding:"omitempty,max=500"`
}

// UpdateCategoryRequest adalah body PATCH /admin/waste-categories/:id (semua opsional).
// is_active=true dipakai untuk mengaktifkan kembali kategori yang dinonaktifkan.
type UpdateCategoryRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=2,max=100"`
	Description *string `json:"description" binding:"omitempty,max=500"`
	IsActive    *bool   `json:"is_active"`
}

// ListCategoryQuery adalah query GET /admin/waste-categories
type ListCategoryQuery struct {
	utils.PageQuery
	Search string `form:"search" binding:"omitempty,max=100"`
	Status string `form:"status" binding:"omitempty,oneof=active inactive all"` // default: all
}

// PublicCategoryQuery adalah query GET /waste-categories (publik)
type PublicCategoryQuery struct {
	SchoolID string `form:"school_id" binding:"omitempty,uuid"`
}

// CategoryResponse adalah data kategori untuk admin
type CategoryResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	IsActive    bool      `json:"is_active"`
	TypeCount   int64     `json:"type_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PublicCategoryResponse adalah data kategori untuk publik (landing page & aplikasi nasabah)
type PublicCategoryResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	TypeCount   int64   `json:"type_count"`
}

func (c *Category) toResponse() CategoryResponse {
	return CategoryResponse{
		ID: c.ID, Name: c.Name, Description: c.Description, IsActive: c.IsActive,
		TypeCount: c.TypeCount, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// =====================================================================
// Repository
// =====================================================================

// CategoryRepository menangani akses data waste_categories
type CategoryRepository struct {
	db *gorm.DB
}

// NewCategoryRepository membuat repository kategori
func NewCategoryRepository(db *gorm.DB) *CategoryRepository {
	return &CategoryRepository{db: db}
}

// base memilih kategori milik sekolah tertentu beserta jumlah jenis sampah aktifnya
func (r *CategoryRepository) base(ctx context.Context, schoolID *string) *gorm.DB {
	return r.db.WithContext(ctx).Model(&Category{}).
		Select(`waste_categories.*,
			(SELECT COUNT(*) FROM waste_types t
			  WHERE t.category_id = waste_categories.id AND t.is_active) AS type_count`).
		Where("waste_categories.school_id IS NOT DISTINCT FROM ?", schoolID)
}

// List mengambil daftar kategori dengan filter & pagination
func (r *CategoryRepository) List(ctx context.Context, schoolID *string, q ListCategoryQuery) ([]Category, int64, error) {
	query := r.base(ctx, schoolID)
	if s := strings.TrimSpace(q.Search); s != "" {
		query = query.Where("waste_categories.name ILIKE ?", "%"+utils.EscapeLike(s)+"%")
	}
	switch q.Status {
	case "active":
		query = query.Where("waste_categories.is_active")
	case "inactive":
		query = query.Where("NOT waste_categories.is_active")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Category
	err := query.Order("waste_categories.is_active DESC, waste_categories.name ASC").
		Limit(q.Limit).Offset(q.Offset()).Find(&items).Error
	return items, total, err
}

// ListActive mengambil semua kategori aktif (untuk endpoint publik)
func (r *CategoryRepository) ListActive(ctx context.Context, schoolID string) ([]Category, error) {
	var items []Category
	err := r.base(ctx, &schoolID).Where("waste_categories.is_active").
		Order("waste_categories.name ASC").Find(&items).Error
	return items, err
}

// FindByID mengambil satu kategori milik sekolah
func (r *CategoryRepository) FindByID(ctx context.Context, schoolID *string, id string) (*Category, error) {
	var c Category
	err := r.base(ctx, schoolID).Where("waste_categories.id = ?", id).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCategoryNotFound
	}
	return &c, err
}

// NameTaken mengecek nama kategori (case-insensitive) di sekolah yang sama
func (r *CategoryRepository) NameTaken(ctx context.Context, schoolID *string, name, excludeID string) (bool, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&Category{}).
		Where("school_id IS NOT DISTINCT FROM ? AND LOWER(name) = LOWER(?)", schoolID, name)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&count).Error
	return count > 0, err
}

// Create menyimpan kategori baru
func (r *CategoryRepository) Create(ctx context.Context, c *Category) error {
	return r.db.WithContext(ctx).Omit("type_count").Create(c).Error
}

// Update memperbarui kolom tertentu
func (r *CategoryRepository) Update(ctx context.Context, id string, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&Category{}).Where("id = ?", id).Updates(fields).Error
}

// ResolveSchoolID mengembalikan schoolID jika ada, atau sekolah pertama (single-tenant MVP)
func (r *CategoryRepository) ResolveSchoolID(ctx context.Context, schoolID string) (string, error) {
	var id string
	q := r.db.WithContext(ctx).Table("schools").Select("id")
	if schoolID != "" {
		q = q.Where("id = ?", schoolID)
	} else {
		q = q.Order("created_at ASC")
	}
	if err := q.Limit(1).Scan(&id).Error; err != nil {
		return "", err
	}
	if id == "" {
		return "", ErrSchoolNotFound
	}
	return id, nil
}

// =====================================================================
// Service
// =====================================================================

// publicCacheTTL sesuai PRD Bab 8: cache GET /waste-categories selama 60 detik
const publicCacheTTL = 60 * time.Second

// CategoryService berisi logika bisnis kategori sampah
type CategoryService struct {
	repo  *CategoryRepository
	cache *utils.TTLCache[string, []PublicCategoryResponse] // key: school_id
}

// NewCategoryService membuat service kategori
func NewCategoryService(repo *CategoryRepository) *CategoryService {
	return &CategoryService{
		repo:  repo,
		cache: utils.NewTTLCache[string, []PublicCategoryResponse](publicCacheTTL),
	}
}

// Create menambah kategori baru
func (s *CategoryService) Create(ctx context.Context, admin *auth.User, req CreateCategoryRequest) (*Category, error) {
	name := strings.TrimSpace(req.Name)
	taken, err := s.repo.NameTaken(ctx, admin.SchoolID, name, "")
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrCategoryNameTaken
	}

	c := &Category{
		SchoolID:    admin.SchoolID,
		Name:        name,
		Description: trimPtr(req.Description),
		IsActive:    true,
	}
	if err := s.repo.Create(ctx, c); err != nil {
		if utils.UniqueViolation(err) != "" {
			return nil, ErrCategoryNameTaken
		}
		return nil, err
	}
	s.invalidate(admin.SchoolID)
	return c, nil
}

// List mengambil daftar kategori untuk admin
func (s *CategoryService) List(ctx context.Context, admin *auth.User, q ListCategoryQuery) ([]Category, int64, error) {
	return s.repo.List(ctx, admin.SchoolID, q)
}

// Update mengubah kategori; mengembalikan perubahan {field: {before, after}} untuk audit
func (s *CategoryService) Update(ctx context.Context, admin *auth.User, id string, req UpdateCategoryRequest) (*Category, map[string]any, error) {
	c, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, nil, err
	}

	fields := map[string]any{}
	changes := map[string]any{}
	track := func(column string, before, after any) {
		fields[column] = after
		changes[column] = map[string]any{"before": before, "after": after}
	}

	if req.Name != nil {
		if name := strings.TrimSpace(*req.Name); name != c.Name {
			// Hanya ubah huruf besar/kecil nama sendiri tidak dianggap bentrok
			if !strings.EqualFold(name, c.Name) {
				taken, err := s.repo.NameTaken(ctx, admin.SchoolID, name, c.ID)
				if err != nil {
					return nil, nil, err
				}
				if taken {
					return nil, nil, ErrCategoryNameTaken
				}
			}
			track("name", c.Name, name)
		}
	}
	if req.Description != nil {
		if desc := trimPtr(req.Description); !equalPtr(desc, c.Description) {
			track("description", c.Description, desc)
		}
	}
	if req.IsActive != nil && *req.IsActive != c.IsActive {
		track("is_active", c.IsActive, *req.IsActive)
	}

	if len(fields) == 0 {
		return nil, nil, ErrNoChanges
	}
	if err := s.repo.Update(ctx, c.ID, fields); err != nil {
		if utils.UniqueViolation(err) != "" {
			return nil, nil, ErrCategoryNameTaken
		}
		return nil, nil, err
	}
	s.invalidate(admin.SchoolID)

	updated, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	return updated, changes, err
}

// Deactivate menonaktifkan kategori (soft delete). Data & riwayat transaksi tetap
// utuh; kategori nonaktif tidak tampil di publik dan tidak bisa dipilih saat penimbangan.
func (s *CategoryService) Deactivate(ctx context.Context, admin *auth.User, id string) (*Category, error) {
	c, err := s.repo.FindByID(ctx, admin.SchoolID, id)
	if err != nil {
		return nil, err
	}
	if !c.IsActive {
		return nil, ErrCategoryAlreadyInactive
	}
	if err := s.repo.Update(ctx, c.ID, map[string]any{"is_active": false}); err != nil {
		return nil, err
	}
	s.invalidate(admin.SchoolID)
	c.IsActive = false
	return c, nil
}

// ListPublic mengambil kategori aktif untuk publik, di-cache 60 detik per sekolah.
// Mengembalikan juga status cache (HIT/MISS) untuk header X-Cache.
func (s *CategoryService) ListPublic(ctx context.Context, schoolID string) ([]PublicCategoryResponse, bool, error) {
	resolved, err := s.repo.ResolveSchoolID(ctx, schoolID)
	if err != nil {
		return nil, false, err
	}
	if cached, ok := s.cache.Get(resolved); ok {
		return cached, true, nil
	}

	items, err := s.repo.ListActive(ctx, resolved)
	if err != nil {
		return nil, false, err
	}
	result := make([]PublicCategoryResponse, len(items))
	for i, c := range items {
		result[i] = PublicCategoryResponse{ID: c.ID, Name: c.Name, Description: c.Description, TypeCount: c.TypeCount}
	}
	s.cache.Set(resolved, result)
	return result, false, nil
}

// invalidate menghapus cache publik sekolah agar perubahan admin langsung terlihat
func (s *CategoryService) invalidate(schoolID *string) {
	if schoolID != nil {
		s.cache.Delete(*schoolID)
	}
}

// =====================================================================
// Handler
// =====================================================================

// CreateCategory: POST /api/v1/admin/waste-categories
func (h *Handler) CreateCategory(c *gin.Context) {
	var req CreateCategoryRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	cat, err := h.categories.Create(c.Request.Context(), auth.CurrentUser(c), req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditEntityID(c, cat.ID)
	middleware.SetAuditMetadata(c, map[string]any{"name": cat.Name})
	utils.RespondSuccess(c, http.StatusCreated, "Kategori sampah berhasil ditambahkan", cat.toResponse())
}

// ListCategories: GET /api/v1/admin/waste-categories?search=&status=&page=&limit=
func (h *Handler) ListCategories(c *gin.Context) {
	var q ListCategoryQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	items, total, err := h.categories.List(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		utils.RespondInternal(c, err)
		return
	}
	out := make([]CategoryResponse, len(items))
	for i := range items {
		out[i] = items[i].toResponse()
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar kategori sampah berhasil diambil",
		utils.PaginatedData[CategoryResponse]{Items: out, Meta: utils.NewPageMeta(q.PageQuery, total)})
}

// UpdateCategory: PATCH /api/v1/admin/waste-categories/:id
func (h *Handler) UpdateCategory(c *gin.Context) {
	var req UpdateCategoryRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	cat, changes, err := h.categories.Update(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditMetadata(c, map[string]any{"changes": changes})
	utils.RespondSuccess(c, http.StatusOK, "Kategori sampah berhasil diperbarui", cat.toResponse())
}

// DeactivateCategory: DELETE /api/v1/admin/waste-categories/:id (soft deactivate)
func (h *Handler) DeactivateCategory(c *gin.Context) {
	cat, err := h.categories.Deactivate(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditMetadata(c, map[string]any{"name": cat.Name})
	utils.RespondSuccess(c, http.StatusOK,
		"Kategori sampah berhasil dinonaktifkan. Riwayat transaksi tetap tersimpan.", cat.toResponse())
}

// ListPublicCategories: GET /api/v1/waste-categories?school_id= (tanpa login, cache 60 detik)
func (h *Handler) ListPublicCategories(c *gin.Context) {
	var q PublicCategoryQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	items, hit, err := h.categories.ListPublic(c.Request.Context(), q.SchoolID)
	if err != nil {
		h.respondError(c, err)
		return
	}
	if hit {
		c.Header("X-Cache", "HIT")
	} else {
		c.Header("X-Cache", "MISS")
	}
	c.Header("Cache-Control", "public, max-age=60")
	utils.RespondSuccess(c, http.StatusOK, "Daftar kategori sampah berhasil diambil", items)
}
