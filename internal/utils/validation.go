package utils

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog/log"
)

var (
	// phoneIDRegex: nomor HP Indonesia, misal 081234567890 / +6281234567890 / 6281234567890
	phoneIDRegex = regexp.MustCompile(`^(\+62|62|0)8[0-9]{7,12}$`)
	uuidRegex    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// init mengatur validator bawaan Gin:
//   - nama field di pesan error memakai tag json/form (full_name), bukan nama struct (FullName)
//   - menambah tag kustom "phone_id" untuk nomor HP Indonesia
func init() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		// Body JSON memakai tag json, query string memakai tag form
		for _, tag := range []string{"json", "form"} {
			name := strings.SplitN(fld.Tag.Get(tag), ",", 2)[0]
			if name == "-" {
				return ""
			}
			if name != "" {
				return name
			}
		}
		return fld.Name
	})
	_ = v.RegisterValidation("phone_id", func(fl validator.FieldLevel) bool {
		return phoneIDRegex.MatchString(fl.Field().String())
	})
}

// IsPhoneID mengecek format nomor HP Indonesia
func IsPhoneID(s string) bool {
	return phoneIDRegex.MatchString(s)
}

// IsUUID mengecek format UUID (dipakai untuk memvalidasi path param :id)
func IsUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

// BindJSON mem-parsing body JSON; jika gagal, langsung mengirim respons 400/422
// dengan pesan error per field dalam Bahasa Indonesia. Kembalikan false jika gagal.
func BindJSON(c *gin.Context, req any) bool {
	return bindWith(c, req, c.ShouldBindJSON)
}

// BindQuery mem-parsing query string (form tag) dengan perilaku sama seperti BindJSON
func BindQuery(c *gin.Context, req any) bool {
	return bindWith(c, req, c.ShouldBindQuery)
}

func bindWith(c *gin.Context, req any, bind func(any) error) bool {
	err := bind(req)
	if err == nil {
		return true
	}
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		fields := make(map[string]string, len(verrs))
		for _, fe := range verrs {
			fields[fieldKey(fe)] = validationMessage(fe)
		}
		RespondValidationError(c, fields)
		return false
	}
	RespondError(c, http.StatusBadRequest, "Format request tidak valid")
	return false
}

// fieldKey menentukan nama field di pesan error. Field di dalam array memakai
// path lengkap agar frontend tahu baris mana yang salah, misal "items[1].weight".
func fieldKey(fe validator.FieldError) string {
	ns := fe.Namespace() // contoh: "CreateRequest.items[1].weight"
	if i := strings.Index(ns, "."); i >= 0 {
		ns = ns[i+1:]
	}
	if strings.Contains(ns, "[") {
		return ns
	}
	return fe.Field()
}

// validationMessage menerjemahkan tag validator ke pesan Bahasa Indonesia
func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "Wajib diisi"
	case "email":
		return "Format email tidak valid"
	case "min":
		switch fe.Kind() {
		case reflect.String:
			return fmt.Sprintf("Minimal %s karakter", fe.Param())
		case reflect.Slice:
			return fmt.Sprintf("Minimal %s item", fe.Param())
		}
		return fmt.Sprintf("Minimal %s", fe.Param())
	case "max":
		switch fe.Kind() {
		case reflect.String:
			return fmt.Sprintf("Maksimal %s karakter", fe.Param())
		case reflect.Slice:
			return fmt.Sprintf("Maksimal %s item", fe.Param())
		}
		return fmt.Sprintf("Maksimal %s", fe.Param())
	case "gt":
		return fmt.Sprintf("Harus lebih dari %s", fe.Param())
	case "gte":
		return fmt.Sprintf("Minimal %s", fe.Param())
	case "lte":
		return fmt.Sprintf("Maksimal %s", fe.Param())
	case "uuid":
		return "Format ID tidak valid"
	case "oneof":
		return "Pilihan tidak valid, gunakan salah satu: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	case "eqfield":
		return "Konfirmasi password tidak sama"
	case "datetime":
		switch fe.Param() {
		case "2006-01-02":
			return "Format tanggal harus YYYY-MM-DD (contoh: 2025-02-10)"
		case "2006-01":
			return "Format bulan harus YYYY-MM (contoh: 2025-02)"
		}
		return "Format tanggal/waktu tidak valid"
	case "phone_id":
		return "Format nomor HP tidak valid (contoh: 081234567890)"
	default:
		return "Nilai tidak valid"
	}
}

// RespondInternal mencatat error internal dan mengirim pesan generik ke client
func RespondInternal(c *gin.Context, err error) {
	log.Error().Err(err).Str("path", c.Request.URL.Path).Msg("Kesalahan internal server")
	RespondError(c, http.StatusInternalServerError, "Terjadi kesalahan pada server, silakan coba lagi")
}
