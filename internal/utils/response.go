package utils

import (
	"github.com/gin-gonic/gin"
)

// APIResponse adalah format standar seluruh response JSON API BankSampahKu.
//
//	Sukses : {"success": true,  "message": "...", "data": {...}}
//	Gagal  : {"success": false, "message": "...", "errors": {"field": "pesan"}}
type APIResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    any               `json:"data,omitempty"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// RespondSuccess mengirim response sukses dengan data opsional.
func RespondSuccess(c *gin.Context, status int, message string, data any) {
	c.JSON(status, APIResponse{Success: true, Message: message, Data: data})
}

// RespondError mengirim response gagal dan menghentikan chain handler.
func RespondError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, APIResponse{Success: false, Message: message})
}

// RespondValidationError mengirim response 422 berisi detail error per field.
func RespondValidationError(c *gin.Context, errors map[string]string) {
	c.AbortWithStatusJSON(422, APIResponse{
		Success: false,
		Message: "Data yang dikirim tidak valid",
		Errors:  errors,
	})
}
