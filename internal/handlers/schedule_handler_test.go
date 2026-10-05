package handlers

import (
	"testing"
	"time"
)

func TestParseSlot(t *testing.T) {
	cases := []struct {
		raw      string
		sks      float64
		in, out  string
		wantFail bool
	}{
		{"07.00 - 10.20", 4, "07:00", "10:20", false},
		{"15.30 - 15.30", 3, "15:30", "18:00", false}, // end derived from SKS
		{"09:30-12:00", 3, "09:30", "12:00", false},
		{"15.30 - 15.30", 0, "", "", true},
		{"abc", 2, "", "", true},
	}
	for _, c := range cases {
		in, out, err := parseSlot(c.raw, c.sks)
		if c.wantFail {
			if err == nil {
				t.Errorf("%q: expected error", c.raw)
			}
			continue
		}
		if err != nil || in != c.in || out != c.out {
			t.Errorf("%q: got %s-%s err=%v want %s-%s", c.raw, in, out, err, c.in, c.out)
		}
	}
}

func TestExpandSchedule(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, loc) // Monday
	end := time.Date(2026, 10, 18, 0, 0, 0, 0, loc)
	items := []ScheduleItem{
		{Day: "Senin", Time: "07.00 - 10.20", Course: "A", SKS: 4, Class: "2023B", Lecturers: " X ; Y ;"},
		{Day: "Kamis", Time: "13.00 - 14.40", Course: "B", SKS: 2, Class: "2023C"},
	}
	got, err := expandSchedule(items, start, end, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 { // 2 Mondays + 2 Thursdays
		t.Fatalf("want 4 bookings, got %d", len(got))
	}
	if got[0].Purpose != "A (2023B)" || got[0].Lecturers != "X; Y" {
		t.Errorf("unexpected first booking: %+v", got[0])
	}
	if got[0].Date.Weekday() != time.Monday || got[1].Date.Weekday() != time.Thursday {
		t.Errorf("wrong weekdays: %v %v", got[0].Date.Weekday(), got[1].Date.Weekday())
	}
}

func TestExpandScheduleRejectsOverlapAndBadDay(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 6)
	overlap := []ScheduleItem{
		{Day: "Senin", Time: "07.00 - 09.00", Course: "A", SKS: 2, Class: "1"},
		{Day: "Senin", Time: "08.00 - 10.00", Course: "B", SKS: 2, Class: "2"},
	}
	if _, err := expandSchedule(overlap, start, end, loc); err == nil {
		t.Error("expected overlap error")
	}
	if _, err := expandSchedule([]ScheduleItem{{Day: "Mon", Time: "07.00 - 09.00", Course: "A", Class: "1"}}, start, end, loc); err == nil {
		t.Error("expected bad-day error")
	}
}
