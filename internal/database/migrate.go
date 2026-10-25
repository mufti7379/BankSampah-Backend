package database

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // Driver migrasi PostgreSQL (pgx v5)
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/migrations"
)

// newMigrator membuat instance golang-migrate dengan sumber file SQL yang di-embed.
func newMigrator(databaseURL string) (*migrate.Migrate, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("gagal membaca file migrasi: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, toMigrateURL(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("gagal menginisialisasi migrator: %w", err)
	}
	return m, nil
}

// toMigrateURL mengubah skema postgres:// menjadi pgx5:// yang dikenali driver golang-migrate.
func toMigrateURL(databaseURL string) string {
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(databaseURL, prefix) {
			return "pgx5://" + strings.TrimPrefix(databaseURL, prefix)
		}
	}
	return databaseURL
}

// closeMigrator menutup koneksi source & database milik migrator.
func closeMigrator(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	if srcErr != nil || dbErr != nil {
		log.Warn().AnErr("source_err", srcErr).AnErr("db_err", dbErr).Msg("Gagal menutup migrator")
	}
}

// MigrateUp menjalankan seluruh migrasi yang belum diterapkan.
func MigrateUp(databaseURL string) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Msg("Migrasi database sudah terbaru, tidak ada perubahan")
			return nil
		}
		return fmt.Errorf("gagal menjalankan migrasi up: %w", err)
	}

	logVersion(m)
	return nil
}

// MigrateDown membatalkan migrasi sebanyak `steps` langkah ke belakang.
func MigrateDown(databaseURL string, steps int) error {
	if steps <= 0 {
		return fmt.Errorf("jumlah langkah rollback harus lebih dari 0")
	}

	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Steps(-steps); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Msg("Tidak ada migrasi yang bisa di-rollback")
			return nil
		}
		return fmt.Errorf("gagal menjalankan migrasi down: %w", err)
	}

	logVersion(m)
	return nil
}

// MigrateForce memaksa versi migrasi tertentu dan membersihkan status "dirty".
// Gunakan hanya setelah memperbaiki migrasi yang gagal di tengah jalan secara manual.
func MigrateForce(databaseURL string, version int) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Force(version); err != nil {
		return fmt.Errorf("gagal memaksa versi migrasi %d: %w", version, err)
	}

	logVersion(m)
	return nil
}

// MigrateVersion menampilkan versi migrasi yang sedang aktif di database.
func MigrateVersion(databaseURL string) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	logVersion(m)
	return nil
}

// logVersion mencetak versi migrasi saat ini beserta status dirty.
func logVersion(m *migrate.Migrate) {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		log.Info().Msg("Database belum memiliki migrasi yang diterapkan")
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("Gagal membaca versi migrasi")
		return
	}
	log.Info().Uint("version", version).Bool("dirty", dirty).Msg("Versi migrasi database saat ini")
}
