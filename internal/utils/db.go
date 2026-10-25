package utils

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// UniqueViolation mengembalikan nama constraint jika err adalah pelanggaran
// UNIQUE PostgreSQL (kode 23505), atau string kosong jika bukan.
func UniqueViolation(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}
