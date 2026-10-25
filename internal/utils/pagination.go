package utils

import (
	"strings"
)

// Batas pagination (PRD Bab 8: default 20 baris per halaman)
const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

// PageQuery adalah query string pagination standar (?page=1&limit=20)
type PageQuery struct {
	Page  int `form:"page" binding:"omitempty,min=1"`
	Limit int `form:"limit" binding:"omitempty,min=1,max=100"`
}

// Normalize mengisi nilai default page & limit
func (p *PageQuery) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Limit < 1 {
		p.Limit = DefaultPageLimit
	}
	if p.Limit > MaxPageLimit {
		p.Limit = MaxPageLimit
	}
}

// Offset menghitung offset SQL dari page & limit
func (p PageQuery) Offset() int {
	return (p.Page - 1) * p.Limit
}

// PageMeta adalah metadata pagination di respons list
type PageMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int64 `json:"total_pages"`
}

// NewPageMeta membuat metadata pagination
func NewPageMeta(p PageQuery, total int64) PageMeta {
	pages := (total + int64(p.Limit) - 1) / int64(p.Limit)
	return PageMeta{Page: p.Page, Limit: p.Limit, Total: total, TotalPages: pages}
}

// PaginatedData adalah isi "data" untuk respons list ber-pagination
type PaginatedData[T any] struct {
	Items []T      `json:"items"`
	Meta  PageMeta `json:"meta"`
}

// EscapeLike meng-escape karakter wildcard LIKE (% _ \) dari input pencarian user
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
