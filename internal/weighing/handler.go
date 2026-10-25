package weighing

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint penimbangan untuk admin & nasabah
type Handler struct {
	service *Service
}

// NewHandler membuat handler penimbangan
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterAdminRoutes mendaftarkan route ke group /api/v1/admin (RequireAuth + RequireRole admin)
func (h *Handler) RegisterAdminRoutes(admin *gin.RouterGroup) {
	rg := admin.Group("/weighings")
	rg.GET("/lookup", h.Lookup) // didaftarkan sebelum /:id
	rg.POST("", h.Create)
	rg.GET("", h.List)
	rg.GET("/:id", middleware.ValidUUIDParam("id", "Penimbangan tidak ditemukan"), h.Detail)
}

// RegisterNasabahRoutes mendaftarkan route ke group /api/v1/nasabah (RequireAuth + RequireRole nasabah)
func (h *Handler) RegisterNasabahRoutes(nasabah *gin.RouterGroup) {
	rg := nasabah.Group("/me/weighings")
	rg.GET("", h.ListMine)
	rg.GET("/:id", middleware.ValidUUIDParam("id", "Penimbangan tidak ditemukan"), h.DetailMine)
}

// Lookup: GET /api/v1/admin/weighings/lookup?qr_code=BSK-xxxxxxxx
// Dipanggil setelah admin memindai QR: menampilkan profil & saldo nasabah.
func (h *Handler) Lookup(c *gin.Context) {
	var q LookupQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	res, err := h.service.Lookup(c.Request.Context(), auth.CurrentUser(c), q.QRCode)
	if err != nil {
		h.respondError(c, err)
		return
	}
	msg := "Nasabah ditemukan"
	if !res.CanTransact {
		msg = "Nasabah ditemukan, namun akunnya belum aktif sehingga belum bisa melakukan penimbangan"
	}
	utils.RespondSuccess(c, http.StatusOK, msg, res)
}

// IdempotencyKeyHeader berisi UUID yang dibuat client satu kali per formulir penimbangan.
// Kiriman ulang dengan key yang sama tidak mencatat transaksi dua kali.
const IdempotencyKeyHeader = "Idempotency-Key"

// Create: POST /api/v1/admin/weighings (web & aplikasi Flutter admin)
func (h *Handler) Create(c *gin.Context) {
	key := strings.ToLower(strings.TrimSpace(c.GetHeader(IdempotencyKeyHeader)))
	if key != "" && !utils.IsUUID(key) {
		utils.RespondValidationError(c, map[string]string{"idempotency_key": "Header Idempotency-Key harus berupa UUID"})
		return
	}
	var req CreateRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	// Audit log ditulis di dalam DB transaction oleh service (bukan middleware Audit),
	// agar ikut di-rollback jika penimbangan gagal.
	result, err := h.service.Create(c.Request.Context(), auth.CurrentUser(c), req, RequestMeta{
		IP: c.ClientIP(), Source: auth.ClientPlatform(c), IdempotencyKey: key,
	})
	if err != nil {
		h.respondError(c, err)
		return
	}
	if result.Replayed {
		utils.RespondSuccess(c, http.StatusOK,
			"Transaksi penimbangan ini sudah tersimpan sebelumnya, saldo tidak ditambah dua kali", result)
		return
	}
	utils.RespondSuccess(c, http.StatusCreated,
		"Transaksi penimbangan berhasil disimpan. Saldo nasabah bertambah "+utils.FormatRupiah(result.Balance.Credit),
		result)
}

// List: GET /api/v1/admin/weighings?nasabah_id=&search=&date_from=&date_to=&page=&limit=
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	items, total, err := h.service.List(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar penimbangan berhasil diambil",
		utils.PaginatedData[Summary]{Items: items, Meta: utils.NewPageMeta(q.PageQuery, total)})
}

// Detail: GET /api/v1/admin/weighings/:id
func (h *Handler) Detail(c *gin.Context) {
	detail, err := h.service.Get(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Detail penimbangan berhasil diambil", detail)
}

// ListMine: GET /api/v1/nasabah/me/weighings?date_from=&date_to=&page=&limit=
func (h *Handler) ListMine(c *gin.Context) {
	var q MyListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	items, total, err := h.service.ListMine(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Riwayat penimbangan berhasil diambil",
		utils.PaginatedData[Summary]{Items: items, Meta: utils.NewPageMeta(q.PageQuery, total)})
}

// DetailMine: GET /api/v1/nasabah/me/weighings/:id
func (h *Handler) DetailMine(c *gin.Context) {
	detail, err := h.service.GetMine(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Detail penimbangan berhasil diambil", detail)
}

// respondError memetakan error domain ke HTTP status
func (h *Handler) respondError(c *gin.Context, err error) {
	var ferr FieldErrors
	switch {
	case errors.As(err, &ferr):
		utils.RespondValidationError(c, ferr)
	case errors.Is(err, ErrNotFound):
		utils.RespondError(c, http.StatusNotFound, "Penimbangan tidak ditemukan")
	case errors.Is(err, ErrNasabahNotFound):
		utils.RespondError(c, http.StatusNotFound, "Nasabah dengan QR Code tersebut tidak ditemukan")
	case errors.Is(err, ErrNasabahNotActive):
		utils.RespondError(c, http.StatusUnprocessableEntity,
			"Akun nasabah belum aktif atau sudah dinonaktifkan, penimbangan tidak bisa dicatat")
	case errors.Is(err, ErrNasabahIdentifier):
		utils.RespondValidationError(c, map[string]string{"qr_code": "Isi salah satu: qr_code (hasil scan) atau nasabah_id"})
	case errors.Is(err, ErrIdempotencyConflict):
		utils.RespondError(c, http.StatusConflict,
			"Formulir ini sudah dipakai untuk transaksi nasabah lain. Muat ulang formulir penimbangan lalu coba lagi")
	case errors.Is(err, ErrInvalidDateRange):
		utils.RespondValidationError(c, map[string]string{"date_from": "Tanggal awal tidak boleh setelah tanggal akhir"})
	default:
		utils.RespondInternal(c, err)
	}
}
