package mutation

import (
	"testing"
	"time"
)

func TestPeriod(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skip("tzdata tidak tersedia")
	}
	time.Local = jakarta

	from, to, err := period(FilterQuery{Month: "2026-02"})
	if err != nil || from.Format(time.RFC3339) != "2026-02-01T00:00:00+07:00" || to.Format(time.RFC3339) != "2026-03-01T00:00:00+07:00" {
		t.Errorf("month: from=%v to=%v err=%v", from, to, err)
	}

	// date_to inklusif: mutasi pukul 23.59 WIB pada date_to tetap masuk
	from, to, err = period(FilterQuery{DateFrom: "2026-10-02", DateTo: "2026-10-02"})
	if err != nil || from.Format(time.RFC3339) != "2026-10-02T00:00:00+07:00" || to.Format(time.RFC3339) != "2026-10-03T00:00:00+07:00" {
		t.Errorf("range: from=%v to=%v err=%v", from, to, err)
	}

	if _, _, err := period(FilterQuery{Month: "2026-02", DateFrom: "2026-02-01"}); err != ErrPeriodConflict {
		t.Errorf("month + date_from harus ErrPeriodConflict, dapat %v", err)
	}
	if _, _, err := period(FilterQuery{DateFrom: "2026-10-03", DateTo: "2026-10-01"}); err != ErrInvalidDateRange {
		t.Errorf("date_from > date_to harus ErrInvalidDateRange, dapat %v", err)
	}
	if from, to, err := period(FilterQuery{}); from != nil || to != nil || err != nil {
		t.Errorf("tanpa filter harus nil semua")
	}
}
