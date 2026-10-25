package utils

import (
	"testing"
	"time"
)

func TestFormatRupiah(t *testing.T) {
	cases := map[int64]string{
		0: "Rp0", 500: "Rp500", 8750: "Rp8.750", 10750: "Rp10.750",
		145500: "Rp145.500", 1000000: "Rp1.000.000", -25000: "-Rp25.000",
	}
	for in, want := range cases {
		if got := FormatRupiah(in); got != want {
			t.Errorf("FormatRupiah(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDecimal2(t *testing.T) {
	cases := map[int64]string{350: "3,5", 250: "2,5", 1: "0,01", 200: "2", 1308: "13,08", 999999: "9999,99"}
	for in, want := range cases {
		if got := FormatDecimal2(in); got != want {
			t.Errorf("FormatDecimal2(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTanggal(t *testing.T) {
	d := time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC)
	if got := FormatTanggal(d); got != "10 Februari 2025" {
		t.Errorf("FormatTanggal = %q", got)
	}
}
