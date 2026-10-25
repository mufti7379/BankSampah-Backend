package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/auth"
)

const (
	auditEntityIDKey = "audit_entity_id"
	auditMetadataKey = "audit_metadata"
	auditSkipKey     = "audit_skip"
	auditActionKey   = "audit_action"

	auditWriteTimeout = 5 * time.Second
)

// Audit mencatat aksi admin ke audit_logs secara otomatis SETELAH handler sukses (2xx).
// Request yang gagal/ditolak tidak dicatat. Pasang per-route setelah RequireAuth:
//
//	admin.POST("/nasabah", middleware.Audit(rec, audit.ActionNasabahCreate, audit.EntityUser), h.Create)
//
// entity_id diambil dari SetAuditEntityID (jika dipanggil handler) atau dari param ":id".
// Metadata tambahan (misal before/after) diisi handler lewat SetAuditMetadata.
func Audit(rec audit.Recorder, action, entity string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		status := c.Writer.Status()
		if status < 200 || status >= 300 || c.GetBool(auditSkipKey) {
			return
		}

		entry := audit.Entry{
			Action:    action,
			Entity:    entity,
			EntityID:  c.GetString(auditEntityIDKey),
			IPAddress: c.ClientIP(),
			Metadata: map[string]any{
				// Dikelompokkan agar tidak bentrok dengan metadata dari handler
				"request": map[string]any{
					"method": c.Request.Method,
					"path":   c.Request.URL.Path,
					"status": status,
				},
			},
		}
		if override := c.GetString(auditActionKey); override != "" {
			entry.Action = override
		}
		if entry.EntityID == "" {
			entry.EntityID = c.Param("id")
		}
		if user := auth.CurrentUser(c); user != nil {
			entry.ActorID = user.ID
		}
		if extra, ok := c.Get(auditMetadataKey); ok {
			if m, ok := extra.(map[string]any); ok {
				for k, v := range m {
					entry.Metadata[k] = v
				}
			}
		}

		// Response sudah terkirim; pakai context baru agar insert tidak ikut
		// dibatalkan jika client menutup koneksi.
		ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
		defer cancel()
		if err := rec.Record(ctx, entry); err != nil {
			log.Error().Err(err).Str("action", action).Str("entity_id", entry.EntityID).
				Msg("Gagal mencatat audit log")
		}
	}
}

// SetAuditEntityID mengisi entity_id audit (misal ID record yang baru dibuat)
func SetAuditEntityID(c *gin.Context, id string) {
	c.Set(auditEntityIDKey, id)
}

// SetAuditMetadata menambah metadata audit (disimpan sebagai JSONB), misal
// map[string]any{"before": old, "after": new}
func SetAuditMetadata(c *gin.Context, metadata map[string]any) {
	c.Set(auditMetadataKey, metadata)
}

// SetAuditAction mengganti aksi audit untuk request ini, untuk endpoint yang
// aksinya bergantung pada kondisi data (misal approve vs reactivate)
func SetAuditAction(c *gin.Context, action string) {
	c.Set(auditActionKey, action)
}

// SkipAudit menandai request agar tidak dicatat middleware, karena log sudah
// ditulis di dalam DB transaction bisnis via audit.RecordTx (misal penimbangan).
func SkipAudit(c *gin.Context) {
	c.Set(auditSkipKey, true)
}
