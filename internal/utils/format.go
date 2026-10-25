package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var bulanIndonesia = [...]string{
	"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

// FormatRupiah memformat angka rupiah dengan pemisah ribuan titik, misal 10750 → "Rp10.750"
func FormatRupiah(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return sign + "Rp" + s
}

// FormatDecimal2 memformat bilangan berskala 1/100 dengan koma desimal ala Indonesia
// tanpa nol berlebih, misal 350 → "3,5" ; 1 → "0,01" ; 200 → "2"
func FormatDecimal2(centi int64) string {
	whole, frac := centi/100, centi%100
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	return strconv.FormatInt(whole, 10) + "," + strings.TrimRight(fmt.Sprintf("%02d", frac), "0")
}

// FormatTanggal memformat tanggal ke Bahasa Indonesia, misal "3 Oktober 2026"
func FormatTanggal(t time.Time) string {
	return fmt.Sprintf("%d %s %d", t.Day(), bulanIndonesia[t.Month()-1], t.Year())
}
