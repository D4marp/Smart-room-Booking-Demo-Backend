package handlers

import "testing"

// mirrors SQL overlap: existing.check_in < new.check_out AND existing.check_out > new.check_in
func intervalsOverlap(existingIn, existingOut, newIn, newOut string) bool {
	return existingIn < newOut && existingOut > newIn
}

func TestIntervalOverlapLogic(t *testing.T) {
	cases := []struct {
		name string
		eIn  string
		eOut string
		nIn  string
		nOut string
		want bool
	}{
		{"identical slot", "09:00", "11:00", "09:00", "11:00", true},
		{"partial overlap start", "09:00", "11:00", "10:00", "12:00", true},
		{"partial overlap end", "10:00", "12:00", "09:00", "11:00", true},
		{"contained", "09:00", "17:00", "10:00", "11:00", true},
		{"adjacent before (no overlap)", "09:00", "10:00", "10:00", "11:00", false},
		{"adjacent after (no overlap)", "10:00", "11:00", "09:00", "10:00", false},
		{"fully separate", "09:00", "10:00", "14:00", "15:00", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := intervalsOverlap(tc.eIn, tc.eOut, tc.nIn, tc.nOut)
			if got != tc.want {
				t.Fatalf("overlap(%s-%s vs %s-%s) = %v, want %v", tc.eIn, tc.eOut, tc.nIn, tc.nOut, got, tc.want)
			}
		})
	}
}

func BenchmarkIntervalOverlapCheck(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = intervalsOverlap("09:00", "11:00", "10:00", "12:00")
	}
}
