package report

import (
	"testing"
	"time"
)

func TestResolvePeriodDefaultCurrentMonth(t *testing.T) {
	p, pr, err := resolvePeriod(PeriodQuery{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	wantFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	if p.From != wantFrom || p.To != now.Format("2006-01-02") {
		t.Errorf("default = %s..%s, want %s..%s", p.From, p.To, wantFrom, now.Format("2006-01-02"))
	}
	if pr.Days != now.Day() {
		t.Errorf("days = %d, want %d", pr.Days, now.Day())
	}
	// ToTime eksklusif = hari setelah "to" pukul 00:00
	if !p.ToTime.Equal(p.FromTime.AddDate(0, 0, pr.Days)) {
		t.Errorf("ToTime = %v", p.ToTime)
	}
}

func TestResolvePeriodValidation(t *testing.T) {
	if _, _, err := resolvePeriod(PeriodQuery{From: "2026-10-05", To: "2026-10-01"}); err != ErrInvalidRange {
		t.Errorf("from > to: err = %v", err)
	}
	if _, _, err := resolvePeriod(PeriodQuery{From: "2025-01-01", To: "2026-01-02"}); err != ErrRangeTooLong {
		t.Errorf("367 hari: err = %v", err)
	}
	if _, pr, err := resolvePeriod(PeriodQuery{From: "2025-01-01", To: "2026-01-01"}); err != nil || pr.Days != 366 {
		t.Errorf("366 hari: days=%d err=%v", pr.Days, err)
	}
}

func TestSafeText(t *testing.T) {
	cases := map[string]string{
		"Aisyah Ramadhani":           "Aisyah Ramadhani",
		"BSK-7HQ2D9LM":               "BSK-7HQ2D9LM",
		`=HYPERLINK("http://x","y")`: `'=HYPERLINK("http://x","y")`,
		"+62812":                     "'+62812",
		"-1+1":                       "'-1+1",
		"@SUM(A1)":                   "'@SUM(A1)",
		"":                           "",
	}
	for in, want := range cases {
		if got := safeText(in); got != want {
			t.Errorf("safeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPercent(t *testing.T) {
	if got := percent(1, 3); got != 33.33 {
		t.Errorf("percent(1,3) = %v", got)
	}
	if got := percent(5, 0); got != 0 {
		t.Errorf("percent(5,0) = %v", got)
	}
}
