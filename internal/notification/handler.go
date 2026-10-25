package notification

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint notifikasi in-app untuk user yang login (admin & nasabah)
type Handler struct {
	service *Service
}

// NewHandler membuat handler notifikasi
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mendaftarkan route ke group /api/v1/notifications (sudah RequireAuth)
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.List)
	rg.GET("/unread-count", h.UnreadCount)
	rg.PATCH("/read-all", h.MarkAllRead)
	rg.PATCH("/:id/read", middleware.ValidUUIDParam("id", "Notifikasi tidak ditemukan"), h.MarkRead)
}

// List: GET /api/v1/notifications?status=unread|read|all&type=&page=&limit=
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	res, err := h.service.List(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar notifikasi berhasil diambil", res)
}

// UnreadCount: GET /api/v1/notifications/unread-count (untuk badge lonceng)
func (h *Handler) UnreadCount(c *gin.Context) {
	n, err := h.service.UnreadCount(c.Request.Context(), auth.CurrentUser(c))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Jumlah notifikasi belum dibaca", UnreadCountResponse{UnreadCount: n})
}

// MarkRead: PATCH /api/v1/notifications/:id/read
func (h *Handler) MarkRead(c *gin.Context) {
	item, err := h.service.MarkRead(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Notifikasi ditandai sudah dibaca", item)
}

// MarkAllRead: PATCH /api/v1/notifications/read-all
func (h *Handler) MarkAllRead(c *gin.Context) {
	n, err := h.service.MarkAllRead(c.Request.Context(), auth.CurrentUser(c))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Semua notifikasi ditandai sudah dibaca", ReadAllResponse{Updated: n})
}

func (h *Handler) respondError(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) {
		utils.RespondError(c, http.StatusNotFound, "Notifikasi tidak ditemukan")
		return
	}
	utils.RespondInternal(c, err)
}
