package database

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// db menyimpan instance koneksi database global (singleton)
var db *gorm.DB

// Connect membuka koneksi ke PostgreSQL menggunakan GORM + pgx driver.
// Parameter databaseURL berformat: postgres://user:pass@host:port/dbname?sslmode=disable
func Connect(databaseURL string, isDevelopment bool) (*gorm.DB, error) {
	// Konfigurasi level log GORM berdasarkan environment
	var gormLogLevel logger.LogLevel
	if isDevelopment {
		gormLogLevel = logger.Info // Tampilkan semua SQL query di mode development
	} else {
		gormLogLevel = logger.Error // Hanya tampilkan error di production
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
		// Nonaktifkan default transaction untuk performa, kita kelola manual per use-case
		SkipDefaultTransaction: true,
		// Gunakan nama tabel singular sesuai konvensi migrasi SQL kita
		// (misal: "users" tetap "users", bukan "user")
		// GORM secara default sudah plural, jadi tidak perlu NamingStrategy kustom
	}

	var err error
	db, err = gorm.Open(postgres.Open(databaseURL), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka koneksi database: %w", err)
	}

	// Ambil instance *sql.DB untuk konfigurasi connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil instance sql.DB: %w", err)
	}

	// Konfigurasi connection pool untuk performa optimal
	sqlDB.SetMaxOpenConns(25)                  // Maksimal koneksi terbuka bersamaan
	sqlDB.SetMaxIdleConns(10)                  // Koneksi idle yang dipertahankan
	sqlDB.SetConnMaxLifetime(30 * time.Minute) // Umur maksimal satu koneksi
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)  // Waktu idle maksimal sebelum koneksi ditutup

	// Verifikasi koneksi ke database berhasil
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("gagal melakukan ping ke database: %w", err)
	}

	log.Info().Msg("Koneksi ke database PostgreSQL berhasil")
	return db, nil
}

// Get mengembalikan instance koneksi database yang sudah terbuka.
// Panggil Connect() terlebih dahulu sebelum menggunakan Get().
func Get() *gorm.DB {
	if db == nil {
		log.Fatal().Msg("Database belum terkoneksi! Panggil database.Connect() terlebih dahulu.")
	}
	return db
}

// Ping memeriksa apakah database masih dapat dijangkau (dipakai oleh health check).
func Ping(ctx context.Context) error {
	if db == nil {
		return fmt.Errorf("database belum terkoneksi")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("gagal mengambil instance sql.DB: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

// Close menutup koneksi database dengan aman.
// Panggil di akhir lifecycle aplikasi (graceful shutdown).
func Close() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("gagal mengambil instance sql.DB untuk close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("gagal menutup koneksi database: %w", err)
	}
	log.Info().Msg("Koneksi database berhasil ditutup")
	return nil
}
