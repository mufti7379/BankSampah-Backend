package config

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitLogger menginisialisasi logger zerolog global berdasarkan environment aplikasi.
// - Development: output berwarna ke console (human-readable) dengan level debug.
// - Production: output JSON terstruktur ke stdout dengan level info.
func InitLogger(appEnv string) {
	// Format waktu standar RFC3339 untuk semua log
	zerolog.TimeFieldFormat = time.RFC3339

	if appEnv == "production" {
		// Production: JSON terstruktur, level info, tanpa warna
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Logger = zerolog.New(os.Stdout).
			With().
			Timestamp().
			Str("service", "banksampahku-api").
			Logger()
	} else {
		// Development: output console berwarna, level debug, mudah dibaca developer
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = zerolog.New(
			zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: "2006-01-02 15:04:05",
			},
		).With().
			Timestamp().
			Logger()
	}

	log.Info().
		Str("environment", appEnv).
		Str("log_level", zerolog.GlobalLevel().String()).
		Msg("Logger berhasil diinisialisasi")
}
