package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

// Config menyimpan seluruh konfigurasi aplikasi yang dibaca dari file .env
type Config struct {
	// Aplikasi
	AppEnv     string // "development" | "production"
	AppPort    string // Port HTTP server (default: 8080)
	AppBaseURL string // Base URL aplikasi (misal: http://localhost:8080)
	Timezone   string // Zona waktu bisnis untuk menentukan "hari ini" (default: Asia/Jakarta)

	// CORS
	CORSAllowedOrigins []string // Daftar origin yang diizinkan (dipisah koma)

	// Database
	DatabaseURL   string // Connection string PostgreSQL
	DBAutoMigrate bool   // Jalankan migrasi "up" otomatis saat server start

	// JWT
	JWTSecret       string // Secret key untuk signing JWT token
	JWTExpiredHours int    // Masa berlaku JWT dalam jam (default: 168 = 7 hari)

	// Keamanan Password
	PasswordPepper string // Kunci rahasia HMAC yang dicampur ke password sebelum bcrypt

	// Rate Limiter
	RateLimitPerMinute     int      // Batas request per menit per IP untuk seluruh API (default: 120)
	AuthRateLimitPerMinute int      // Batas lebih ketat per IP untuk endpoint /auth (default: 20)
	TrustedProxies         []string // IP reverse proxy (Nginx) yang dipercaya untuk X-Forwarded-For
	LoginAttemptLimit      int      // Maksimal percobaan login gagal (default: 5)
	LoginLockMinutes       int      // Durasi kunci akun setelah melebihi batas login (default: 15 menit)

	// QR Code
	QRPrefix string // Prefix untuk kode QR nasabah (default: "BSK-")

	// Email (SMTP) - jika SMTPHost kosong, email hanya dicetak ke log (development)
	SMTPHost      string
	SMTPPort      int
	SMTPUsername  string
	SMTPPassword  string
	SMTPFromEmail string
	SMTPFromName  string

	// Reset Password
	PasswordResetURL        string // URL halaman reset di frontend (token ditambahkan sebagai ?token=)
	PasswordResetTTLMinutes int    // Masa berlaku link reset (default: 30 menit)
}

// cfg menyimpan instance konfigurasi yang sudah dimuat (singleton)
var cfg *Config

// Load membaca file .env dan menginisialisasi konfigurasi aplikasi.
// File .env bersifat opsional - jika tidak ditemukan, akan menggunakan
// variabel lingkungan sistem operasi yang sudah ada.
func Load() (*Config, error) {
	// Muat file .env jika tersedia (abaikan error jika tidak ditemukan)
	err := godotenv.Load()
	if err != nil {
		log.Warn().Msg("File .env tidak ditemukan, menggunakan variabel lingkungan sistem")
	} else {
		log.Info().Msg("Berhasil memuat konfigurasi dari file .env")
	}

	cfg = &Config{
		// Aplikasi
		AppEnv:     getEnv("APP_ENV", "development"),
		AppPort:    getEnv("APP_PORT", "8080"),
		AppBaseURL: getEnv("APP_BASE_URL", "http://localhost:8080"),
		Timezone:   getEnv("APP_TIMEZONE", "Asia/Jakarta"),

		// CORS: dipisah koma untuk mendukung multiple origin
		CORSAllowedOrigins: getEnvAsSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),

		// Database
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		DBAutoMigrate: getEnvAsBool("DB_AUTO_MIGRATE", false),

		// JWT
		JWTSecret:       getEnv("JWT_SECRET", ""),
		JWTExpiredHours: getEnvAsInt("JWT_EXPIRED_HOURS", 168),

		// Keamanan Password
		PasswordPepper: getEnv("PASSWORD_PEPPER", ""),

		// Rate Limiter
		RateLimitPerMinute:     getEnvAsInt("RATE_LIMIT_PER_MINUTE", 120),
		AuthRateLimitPerMinute: getEnvAsInt("AUTH_RATE_LIMIT_PER_MINUTE", 20),
		TrustedProxies:         getEnvAsSlice("TRUSTED_PROXIES", nil),
		LoginAttemptLimit:      getEnvAsInt("LOGIN_ATTEMPT_LIMIT", 5),
		LoginLockMinutes:       getEnvAsInt("LOGIN_LOCK_MINUTES", 15),

		// QR Code
		QRPrefix: getEnv("QR_PREFIX", "BSK-"),

		// Email (SMTP)
		SMTPHost:      getEnv("SMTP_HOST", ""),
		SMTPPort:      getEnvAsInt("SMTP_PORT", 587),
		SMTPUsername:  getEnv("SMTP_USERNAME", ""),
		SMTPPassword:  getEnv("SMTP_PASSWORD", ""),
		SMTPFromEmail: getEnv("SMTP_FROM_EMAIL", "no-reply@banksampahku.id"),
		SMTPFromName:  getEnv("SMTP_FROM_NAME", "BankSampahKu"),

		// Reset Password
		PasswordResetURL:        getEnv("PASSWORD_RESET_URL", "http://localhost:5173/reset-password"),
		PasswordResetTTLMinutes: getEnvAsInt("PASSWORD_RESET_TTL_MINUTES", 30),
	}

	// Validasi konfigurasi wajib
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Get mengembalikan instance konfigurasi yang sudah dimuat.
// Panggil Load() terlebih dahulu sebelum menggunakan Get().
func Get() *Config {
	if cfg == nil {
		log.Fatal().Msg("Konfigurasi belum dimuat! Panggil config.Load() terlebih dahulu.")
	}
	return cfg
}

// validate memastikan semua variabel lingkungan wajib telah diisi
func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL wajib diisi di file .env")
	}
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET wajib diisi di file .env")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET terlalu pendek, minimal 32 karakter")
	}
	if c.PasswordPepper == "" {
		return fmt.Errorf("PASSWORD_PEPPER wajib diisi di file .env")
	}
	if len(c.PasswordPepper) < 32 {
		return fmt.Errorf("PASSWORD_PEPPER terlalu pendek, minimal 32 karakter")
	}
	if c.JWTExpiredHours <= 0 {
		return fmt.Errorf("JWT_EXPIRED_HOURS harus lebih dari 0")
	}
	if c.PasswordResetTTLMinutes <= 0 {
		return fmt.Errorf("PASSWORD_RESET_TTL_MINUTES harus lebih dari 0")
	}
	if c.IsProduction() && c.SMTPHost == "" {
		return fmt.Errorf("SMTP_HOST wajib diisi di production agar email reset password terkirim")
	}
	return nil
}

// IsDevelopment mengembalikan true jika aplikasi berjalan di mode development
func (c *Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

// IsProduction mengembalikan true jika aplikasi berjalan di mode production
func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

// --- Fungsi helper untuk membaca variabel lingkungan ---

// getEnv membaca variabel lingkungan dengan nilai default jika kosong
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// getEnvAsInt membaca variabel lingkungan dan mengubahnya menjadi integer
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Warn().Str("key", key).Str("value", valueStr).
			Msg("Gagal mengonversi nilai env ke integer, menggunakan default")
		return defaultValue
	}
	return value
}

// getEnvAsBool membaca variabel lingkungan dan mengubahnya menjadi boolean
func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		log.Warn().Str("key", key).Str("value", valueStr).
			Msg("Gagal mengonversi nilai env ke boolean, menggunakan default")
		return defaultValue
	}
	return value
}

// getEnvAsSlice membaca variabel lingkungan dan memisahkannya berdasarkan koma
func getEnvAsSlice(key string, defaultValue []string) []string {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	parts := strings.Split(valueStr, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
