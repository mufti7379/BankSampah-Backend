package nasabah

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// defaultQRSize adalah ukuran default gambar QR (px) - cukup tajam untuk dicetak di kartu
const defaultQRSize = 512

// Handler menangani endpoint /api/v1/admin/nasabah/*
type Handler struct {
	service *Service
	audit   audit.Recorder
}

// NewHandler membuat handler nasabah
func NewHandler(service *Service, rec audit.Recorder) *Handler {
	return &Handler{service: service, audit: rec}
}

// RegisterRoutes mendaftarkan route ke group /api/v1/admin (sudah RequireAuth + RequireRole admin)
func (h *Handler) RegisterRoutes(admin *gin.RouterGroup) {
	rg := admin.Group("/nasabah")
	requireUUID := middleware.ValidUUIDParam("id", "Nasabah tidak ditemukan")
	rg.POST("", middleware.Audit(h.audit, audit.ActionNasabahCreate, audit.EntityUser), h.Create)
	rg.GET("", h.List)
	rg.GET("/:id", requireUUID, h.Detail)
	rg.GET("/:id/qr.png", requireUUID, h.QRImage)
	rg.PATCH("/:id", requireUUID, middleware.Audit(h.audit, audit.ActionNasabahUpdate, audit.EntityUser), h.Update)
	rg.PATCH("/:id/deactivate", requireUUID, middleware.Audit(h.audit, audit.ActionUserDeactivate, audit.EntityUser), h.Deactivate)
	rg.PATCH("/:id/activate", requireUUID, middleware.Audit(h.audit, audit.ActionNasabahApprove, audit.EntityUser), h.Activate)
}

// Create: POST /api/v1/admin/nasabah
func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	if req.Password != nil && *req.Password != "" {
		if err := utils.ValidatePasswordStrength(*req.Password); err != nil {
			utils.RespondValidationError(c, map[string]string{"password": err.Error()})
			return
		}
	}

	user, initialPassword, err := h.service.Create(c.Request.Context(), auth.CurrentUser(c), req)
	if err != nil {
		h.respondError(c, err)
		return
	}

	middleware.SetAuditEntityID(c, user.ID)
	middleware.SetAuditMetadata(c, map[string]any{
		"full_name": user.FullName,
		"email":     user.Email,
		"qr_code":   user.QRCode,
	})

	msg := "Nasabah berhasil ditambahkan"
	if initialPassword != nil {
		msg += ". Catat password sementara berikut, password ini hanya ditampilkan sekali"
	}
	utils.RespondSuccess(c, http.StatusCreated, msg, CreateResponse{
		Response:        toResponse(user),
		InitialPassword: initialPassword,
	})
}

// List: GET /api/v1/admin/nasabah?search=&status=&class_name=&sort=&page=&limit=
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	users, total, err := h.service.List(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		utils.RespondInternal(c, err)
		return
	}

	items := make([]Response, len(users))
	for i := range users {
		items[i] = toResponse(&users[i])
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar nasabah berhasil diambil", utils.PaginatedData[Response]{
		Items: items,
		Meta:  utils.NewPageMeta(q.PageQuery, total),
	})
}

// Detail: GET /api/v1/admin/nasabah/:id
func (h *Handler) Detail(c *gin.Context) {
	detail, err := h.service.Detail(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Detail nasabah berhasil diambil", detail)
}

// Update: PATCH /api/v1/admin/nasabah/:id
func (h *Handler) Update(c *gin.Context) {
	var req UpdateRequest
	if !utils.BindJSON(c, &req) {
		return
	}

	user, changes, err := h.service.Update(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req)
	if err != nil {
		h.respondError(c, err)
		return
	}

	middleware.SetAuditMetadata(c, map[string]any{"changes": changes})
	utils.RespondSuccess(c, http.StatusOK, "Data nasabah berhasil diperbarui", toResponse(user))
}

// Deactivate: PATCH /api/v1/admin/nasabah/:id/deactivate
func (h *Handler) Deactivate(c *gin.Context) {
	user, before, err := h.service.Deactivate(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditMetadata(c, map[string]any{"status": map[string]any{"before": before, "after": user.Status}})
	utils.RespondSuccess(c, http.StatusOK, "Nasabah berhasil dinonaktifkan", toResponse(user))
}

// Activate: PATCH /api/v1/admin/nasabah/:id/activate
// Menyetujui pendaftaran mandiri (pending) atau mengaktifkan kembali nasabah nonaktif.
func (h *Handler) Activate(c *gin.Context) {
	user, before, err := h.service.Activate(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	middleware.SetAuditMetadata(c, map[string]any{"status": map[string]any{"before": before, "after": user.Status}})

	// Pending → NASABAH_APPROVE (persetujuan daftar mandiri); inactive → USER_ACTIVATE
	msg := "Pendaftaran nasabah berhasil disetujui"
	if before != auth.StatusPending {
		msg = "Nasabah berhasil diaktifkan kembali"
		middleware.SetAuditAction(c, audit.ActionUserActivate)
	}
	utils.RespondSuccess(c, http.StatusOK, msg, toResponse(user))
}

// QRImage: GET /api/v1/admin/nasabah/:id/qr.png?size=512&download=true
// Menghasilkan gambar PNG QR Code nasabah untuk ditampilkan / dicetak di kartu.
func (h *Handler) QRImage(c *gin.Context) {
	var q QRQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	if q.Size == 0 {
		q.Size = defaultQRSize
	}

	user, err := h.service.Get(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	if user.QRCode == nil || *user.QRCode == "" {
		utils.RespondError(c, http.StatusNotFound, "Nasabah ini belum memiliki QR Code")
		return
	}

	// Level koreksi High (30%): QR tetap terbaca walau kartu tergores/kotor
	png, err := qrcode.Encode(*user.QRCode, qrcode.High, q.Size)
	if err != nil {
		utils.RespondInternal(c, err)
		return
	}

	disposition := "inline"
	if q.Download {
		disposition = "attachment"
	}
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="qr-%s.png"`, disposition, *user.QRCode))
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, "image/png", png)
}

// respondError memetakan error domain ke HTTP status
func (h *Handler) respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		utils.RespondError(c, http.StatusNotFound, "Nasabah tidak ditemukan")
	case errors.Is(err, ErrEmailTaken):
		utils.RespondValidationError(c, map[string]string{"email": "Email sudah terdaftar"})
	case errors.Is(err, ErrInvalidPhone):
		utils.RespondValidationError(c, map[string]string{"phone": "Format nomor HP tidak valid (contoh: 081234567890)"})
	case errors.Is(err, ErrNoChanges):
		utils.RespondError(c, http.StatusBadRequest, "Tidak ada data yang diubah")
	case errors.Is(err, ErrAlreadyActive):
		utils.RespondError(c, http.StatusConflict, "Nasabah sudah dalam status aktif")
	case errors.Is(err, ErrAlreadyInactive):
		utils.RespondError(c, http.StatusConflict, "Nasabah sudah dalam status nonaktif")
	default:
		utils.RespondInternal(c, err)
	}
}
