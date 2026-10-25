package audit

import "testing"

func TestDateRange(t *testing.T) {
	from, to, err := dateRange("2026-10-01", "2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if from.Format("2006-01-02 15:04") != "2026-10-01 00:00" || to.Format("2006-01-02 15:04") != "2026-10-04 00:00" {
		t.Errorf("rentang = %v .. %v", from, to)
	}
	if _, _, err := dateRange("2026-10-05", "2026-10-01"); err != ErrInvalidDateRange {
		t.Errorf("from > to harus ErrInvalidDateRange, dapat %v", err)
	}
	if f, tt, err := dateRange("", ""); f != nil || tt != nil || err != nil {
		t.Error("tanpa filter harus nil semua")
	}
}

// Setiap aksi yang dicatat aplikasi wajib punya label Bahasa Indonesia
func TestAllActionsHaveLabels(t *testing.T) {
	actions := []string{
		ActionWeighingCreate, ActionPriceUpdate, ActionPriceCancel, ActionNasabahCreate, ActionNasabahUpdate,
		ActionNasabahApprove, ActionUserDeactivate, ActionUserActivate, ActionCategoryCreate, ActionCategoryUpdate,
		ActionCategoryDeactivate, ActionWasteTypeCreate, ActionWasteTypeUpdate, ActionWasteTypeDeactivate,
		ActionWithdrawalApprove, ActionWithdrawalReject, ActionWithdrawalComplete, ActionSettingsUpdate,
	}
	for _, a := range actions {
		if actionLabel(a) == a {
			t.Errorf("aksi %s belum punya label", a)
		}
	}
	if got := len(ActionOptions()); got != len(actions) {
		t.Errorf("ActionOptions = %d pilihan, want %d", got, len(actions))
	}
	if actionLabel("AKSI_BARU") != "AKSI_BARU" {
		t.Error("aksi tanpa label harus dikembalikan apa adanya")
	}
}
