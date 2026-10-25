package weighing

import "testing"

func TestToCenti(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
		ok   bool
	}{
		{2.5, 250, true},
		{0.01, 1, true},
		{9999.99, 999999, true},
		{1.1, 110, true}, // 1.1*100 = 110.00000000000001 di float → tetap valid
		{0.29, 29, true},
		{1.234, 123, false}, // lebih dari 2 desimal
	}
	for _, c := range cases {
		got, ok := toCenti(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("toCenti(%v) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSubtotal(t *testing.T) {
	cases := []struct {
		centi, price, want int64
	}{
		{250, 3500, 8750},          // PRD Bab 9: 2.5 kg Botol PET × Rp3.500 = Rp8.750
		{100, 2000, 2000},          // PRD Bab 9: 1 kg Kardus × Rp2.000 = Rp2.000
		{300, 12000, 36000},        // PRD Bab 9: 3 kg Kaleng Aluminium × Rp12.000 = Rp36.000
		{1, 500, 5},                // 0.01 kg × Rp500 = Rp5
		{33, 2500, 825},            // 0.33 × 2500 = 825
		{1, 4250, 43},              // 42.5 → dibulatkan ke atas (half-up) = 43
		{1, 4249, 42},              // 42.49 → 42
		{999999, 65000, 649999350}, // 9999.99 kg × Rp65.000
	}
	for _, c := range cases {
		if got := subtotal(c.centi, c.price); got != c.want {
			t.Errorf("subtotal(%d, %d) = %d, want %d", c.centi, c.price, got, c.want)
		}
	}
}

func TestJoinNames(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"Botol PET"}, "Botol PET"},
		{[]string{"Botol PET", "Kardus"}, "Botol PET, Kardus"},
		{[]string{"A", "B", "C"}, "A, B, C"},
		{[]string{"A", "B", "C", "D", "E"}, "A, B, dan 3 lainnya"},
	}
	for _, c := range cases {
		if got := joinNames(c.in); got != c.want {
			t.Errorf("joinNames(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCentiToString(t *testing.T) {
	cases := map[int64]string{250: "2.50", 1: "0.01", 100: "1.00", 999999: "9999.99", 0: "0.00"}
	for in, want := range cases {
		if got := centiToString(in); got != want {
			t.Errorf("centiToString(%d) = %q, want %q", in, got, want)
		}
	}
}
