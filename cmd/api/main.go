package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // database zona waktu ikut di-embed (Windows/container minimal tidak punya)

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/config"
	"github.com/banksampahku/backend/internal/database"
	"github.com/banksampahku/backend/internal/mailer"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/mutation"
	"github.com/banksampahku/backend/internal/nasabah"
	"github.com/banksampahku/backend/internal/notification"
	"github.com/banksampahku/backend/internal/report"
	"github.com/banksampahku/backend/internal/utils"
	"github.com/banksampahku/backend/internal/waste"
	"github.com/banksampahku/backend/internal/weighing"
	"github.com/banksampahku/backend/internal/withdrawal"
)

var startTime = time.Now()

func main() {
	// Flag CLI untuk menjalankan migrasi tanpa menyalakan server
	migrateCmd := flag.String("migrate", "", "Perintah migrasi: up | down | version | force")
	migrateSteps := flag.Int("steps", 1, "Jumlah langkah rollback untuk -migrate=down")
	migrateVersion := flag.Int("version", -1, "Versi target untuk -migrate=force")
	seed := flag.Bool("seed", false, "Isi data awal (sekolah, admin, nasabah, sampah, harga) lalu keluar")
	flag.Parse()

	// 1. Muat konfigurasi dari file .env
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Gagal memuat konfigurasi aplikasi")
	}

	// 2. Inisialisasi logger zerolog berdasarkan environment
	config.InitLogger(cfg.AppEnv)

	// 3. Zona waktu bisnis: "hari ini" untuk harga & penimbangan mengikuti waktu Indonesia,
	// walaupun server (misal VPS) berjalan dengan zona UTC
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Fatal().Err(err).Str("timezone", cfg.Timezone).Msg("APP_TIMEZONE tidak valid")
	}
	time.Local = loc

	// Mode CLI migrasi: jalankan perintah lalu keluar
	if *migrateCmd != "" {
		runMigrateCommand(cfg.DatabaseURL, *migrateCmd, *migrateSteps, *migrateVersion)
		return
	}

	// Jalankan migrasi otomatis jika diaktifkan lewat DB_AUTO_MIGRATE=true
	if cfg.DBAutoMigrate {
		if err := database.MigrateUp(cfg.DatabaseURL); err != nil {
			log.Fatal().Err(err).Msg("Migrasi otomatis gagal")
		}
	}

	// Buka koneksi database (GORM + pgx)
	db, err := database.Connect(cfg.DatabaseURL, cfg.IsDevelopment())
	if err != nil {
		log.Fatal().Err(err).Msg("Gagal terhubung ke database")
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Error().Err(err).Msg("Gagal menutup koneksi database")
		}
	}()

	// Hasher password bersama (bcrypt cost 12 + pepper)
	passwordHasher := utils.NewPasswordHasher(cfg.PasswordPepper)

	// Mode CLI seeder: isi data awal lalu keluar
	if *seed {
		if cfg.IsProduction() {
			log.Fatal().Msg("Seeder data contoh tidak boleh dijalankan di production")
		}
		if err := database.Seed(db, passwordHasher); err != nil {
			log.Fatal().Err(err).Msg("Seeder gagal")
		}
		return
	}

	log.Info().
		Str("app", "BankSampahKu API").
		Str("env", cfg.AppEnv).
		Str("port", cfg.AppPort).
		Msg("Memulai inisialisasi server...")

	// 4. Atur mode Gin sesuai environment
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	router, err := newRouter(cfg, db, passwordHasher)
	if err != nil {
		log.Fatal().Err(err).Msg("Gagal menyiapkan router HTTP")
	}

	// Inisialisasi HTTP Server dengan timeout yang aman
	server := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Menjalankan server dalam goroutine terpisah untuk mendukung graceful shutdown
	go func() {
		log.Info().
			Str("port", cfg.AppPort).
			Str("base_url", cfg.AppBaseURL).
			Msgf("Server BankSampahKu aktif di %s", cfg.AppBaseURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Gagal menjalankan HTTP server")
		}
	}()

	// Menunggu sinyal OS (SIGINT, SIGTERM) untuk graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Menerima sinyal shutdown, mematikan server secara aman...")

	// Memberikan toleransi waktu 5 detik untuk menyelesaikan request yang sedang berjalan
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server dimatikan secara paksa akibat timeout shutdown")
	} else {
		log.Info().Msg("Server BankSampahKu berhasil dimatikan dengan aman")
	}
}

// newRouter merakit seluruh middleware & route API. Dipisah dari main() agar
// integration test memakai wiring yang sama persis dengan server sungguhan.
func newRouter(cfg *config.Config, db *gorm.DB, passwordHasher *utils.PasswordHasher) (*gin.Engine, error) {
	router := gin.New()

	// Hanya percayai X-Forwarded-For dari proxy yang terdaftar, agar IP client
	// (dipakai rate limiter & audit log) tidak bisa dipalsukan lewat header.
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("TRUSTED_PROXIES tidak valid: %w", err)
	}

	// Middleware recovery agar server tidak crash jika terjadi panic yang tidak terduga
	router.Use(gin.Recovery())

	// Header keamanan, CORS sesuai CORS_ALLOWED_ORIGINS, dan batas ukuran body 1 MB
	router.Use(middleware.SecurityHeaders(cfg.IsProduction()))
	router.Use(middleware.CORS(cfg.CORSAllowedOrigins))
	router.Use(middleware.BodyLimit(1 << 20))

	// Middleware logging kustom menggunakan Zerolog
	router.Use(func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		clientIP := c.ClientIP()
		statusCode := c.Writer.Status()
		method := c.Request.Method

		if rawQuery != "" {
			path = path + "?" + middleware.RedactQuery(rawQuery)
		}

		loggerEvent := log.Info()
		if statusCode >= 500 {
			loggerEvent = log.Error()
		} else if statusCode >= 400 {
			loggerEvent = log.Warn()
		}

		loggerEvent.
			Int("status", statusCode).
			Str("method", method).
			Str("path", path).
			Str("ip", clientIP).
			Dur("latency", latency).
			Msg("HTTP Request")
	})

	// Endpoint Health Check untuk verifikasi status server
	router.GET("/health", func(c *gin.Context) {
		uptime := time.Since(startTime).Round(time.Second).String()

		// Cek konektivitas database dengan batas waktu singkat
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		status, dbStatus, httpStatus := "ok", "connected", http.StatusOK
		if err := database.Ping(ctx); err != nil {
			log.Error().Err(err).Msg("Health check: database tidak dapat dijangkau")
			status, dbStatus, httpStatus = "degraded", "disconnected", http.StatusServiceUnavailable
		}

		c.JSON(httpStatus, gin.H{
			"status":      status,
			"database":    dbStatus,
			"app_name":    "BankSampahKu API",
			"version":     "1.0.0",
			"uptime":      uptime,
			"environment": cfg.AppEnv,
			"timestamp":   time.Now().Format(time.RFC3339),
			"message":     "Layanan BankSampahKu Backend berjalan normal",
		})
	})

	// ==== Modul Auth ====
	emailSender := mailer.New(mailer.Config{
		Host:      cfg.SMTPHost,
		Port:      cfg.SMTPPort,
		Username:  cfg.SMTPUsername,
		Password:  cfg.SMTPPassword,
		FromEmail: cfg.SMTPFromEmail,
		FromName:  cfg.SMTPFromName,
	})

	authService := auth.NewService(auth.Deps{
		Repo:             auth.NewRepository(db),
		Hasher:           passwordHasher,
		Tokens:           auth.NewTokenManager(cfg.JWTSecret, cfg.JWTExpiredHours),
		Limiter:          auth.NewLoginLimiter(cfg.LoginAttemptLimit, cfg.LoginLockMinutes),
		Revoked:          auth.NewRevocationStore(),
		Mailer:           emailSender,
		QRPrefix:         cfg.QRPrefix,
		PasswordResetURL: cfg.PasswordResetURL,
		PasswordResetTTL: time.Duration(cfg.PasswordResetTTLMinutes) * time.Minute,
	})
	authHandler := auth.NewHandler(authService, cfg.IsProduction())

	// ==== Middleware bersama ====
	requireAuth := middleware.RequireAuth(authService)
	apiRateLimit := middleware.NewIPRateLimiter("api", cfg.RateLimitPerMinute, cfg.RateLimitPerMinute)
	authRateLimit := middleware.NewIPRateLimiter("auth", cfg.AuthRateLimitPerMinute, cfg.AuthRateLimitPerMinute)
	auditRecorder := audit.NewRepository(db)

	// ==== Route API v1 ====
	apiV1 := router.Group("/api/v1", apiRateLimit.Middleware())
	authHandler.RegisterRoutes(apiV1.Group("/auth", authRateLimit.Middleware()), requireAuth)

	// Area admin (web) & nasabah (mobile)
	adminAPI := apiV1.Group("/admin", requireAuth, middleware.RequireRole(auth.RoleAdmin))
	nasabahAPI := apiV1.Group("/nasabah", requireAuth, middleware.RequireRole(auth.RoleNasabah))

	// ==== Modul Nasabah (admin) ====
	nasabahService := nasabah.NewService(nasabah.NewRepository(db), passwordHasher, cfg.QRPrefix)
	nasabah.NewHandler(nasabahService, auditRecorder).RegisterRoutes(adminAPI)

	// ==== Modul Waste (kategori, jenis, harga) ====
	categoryService := waste.NewCategoryService(waste.NewCategoryRepository(db))
	typeService := waste.NewTypeService(waste.NewTypeRepository(db), categoryService)
	priceService := waste.NewPriceService(waste.NewPriceRepository(db), typeService)
	wasteHandler := waste.NewHandler(categoryService, typeService, priceService, auditRecorder)
	wasteHandler.RegisterAdminRoutes(adminAPI)
	wasteHandler.RegisterPublicRoutes(apiV1)

	// ==== Modul Penimbangan ====
	weighingHandler := weighing.NewHandler(weighing.NewService(weighing.NewRepository(db)))
	weighingHandler.RegisterAdminRoutes(adminAPI)
	weighingHandler.RegisterNasabahRoutes(nasabahAPI)

	// ==== Modul Mutasi Saldo ====
	mutationHandler := mutation.NewHandler(mutation.NewService(mutation.NewRepository(db)))
	mutationHandler.RegisterAdminRoutes(adminAPI)
	mutationHandler.RegisterNasabahRoutes(nasabahAPI)

	// ==== Modul Penarikan Saldo ====
	withdrawalHandler := withdrawal.NewHandler(withdrawal.NewService(withdrawal.NewRepository(db)))
	withdrawalHandler.RegisterAdminRoutes(adminAPI)
	withdrawalHandler.RegisterNasabahRoutes(nasabahAPI)

	// ==== Modul Log Aktivitas Admin (baca saja) ====
	audit.NewHandler(auditRecorder).RegisterRoutes(adminAPI)

	// ==== Modul Laporan (admin) ====
	report.NewHandler(report.NewService(report.NewRepository(db))).RegisterRoutes(adminAPI)

	// ==== Modul Notifikasi In-App (admin & nasabah, cukup login) ====
	notificationHandler := notification.NewHandler(notification.NewService(notification.NewRepository(db)))
	notificationHandler.RegisterRoutes(apiV1.Group("/notifications", requireAuth))

	return router, nil
}

// runMigrateCommand mengeksekusi perintah migrasi dari flag CLI lalu keluar.
func runMigrateCommand(databaseURL, cmd string, steps, version int) {
	var err error
	switch cmd {
	case "up":
		err = database.MigrateUp(databaseURL)
	case "down":
		err = database.MigrateDown(databaseURL, steps)
	case "version":
		err = database.MigrateVersion(databaseURL)
	case "force":
		if version < 0 {
			log.Fatal().Msg("Gunakan -version=<angka> bersama -migrate=force")
		}
		err = database.MigrateForce(databaseURL, version)
	default:
		log.Fatal().Str("migrate", cmd).Msg("Perintah migrasi tidak dikenal (gunakan: up | down | version | force)")
	}

	if err != nil {
		log.Fatal().Err(err).Str("migrate", cmd).Msg("Perintah migrasi gagal")
	}
	log.Info().Str("migrate", cmd).Msg("Perintah migrasi selesai")
}
