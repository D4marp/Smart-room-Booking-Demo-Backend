package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/database"
	"github.com/bookify-rooms/backend/internal/realtime"
)

// Runs only when TEST_MYSQL_DSN points at a disposable database.
func TestIntegrationAPIFlow(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"integration_events", "booking_status_history", "bookings", "classes", "rooms", "users"} {
		db.Exec("DELETE FROM " + tbl)
	}
	mustExec := func(q string, a ...any) {
		t.Helper()
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`INSERT INTO users (id,email,password,name,role,created_at) VALUES ('u1','a@b.c','x','Admin','admin',1)`)
	mustExec(`INSERT INTO rooms (id,name,description,location,city,room_class,image_urls,amenities,has_ac,is_available,max_guests,contact_number,created_at)
	          VALUES ('r1','Q1.01.03','','Gedung','Surabaya','Meeting','[]','[]',1,1,40,'',1)`)

	day := time.Now().In(jakarta)
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, jakarta).UnixMilli()
	mustExec(`INSERT INTO bookings (id,user_id,room_id,booking_date,check_in_time,check_out_time,number_of_guests,status,purpose,pihak_1,class_code,created_at)
	          VALUES ('b1','u1','r1',?,'08:00','09:40',30,'confirmed','Pengembangan Karier (2023C)','Dosen A; Dosen B','2023C',1)`, midnight)

	bh := NewBookingHandler(db, realtime.NewManager())
	ih := NewIntegrationHandler(db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	as := func(role string) gin.HandlerFunc {
		return func(c *gin.Context) { c.Set("userID", "u1"); c.Set("role", role) }
	}
	r.PATCH("/b/:id", as("admin"), bh.UpdateBooking)
	r.PATCH("/b/:id/co", as("booking"), bh.UpdateCheckInCheckOut)
	r.GET("/rooms", ih.ListRooms)
	r.GET("/schedules", ih.ListSchedules)
	r.GET("/events", ih.ListEvents)

	do := func(method, url string, body any) map[string]any {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, url, rd)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s -> %d %s", method, url, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out["data"].(map[string]any)
	}

	// schedule read (default: today..tomorrow, confirmed)
	sched := do("GET", "/schedules?room=Q1.01.03", nil)["schedules"].([]any)
	if len(sched) != 1 {
		t.Fatalf("want 1 schedule, got %d", len(sched))
	}
	s0 := sched[0].(map[string]any)
	if s0["roomCode"] != "Q1.01.03" || s0["classCode"] != "2023C" || s0["course"] != "Pengembangan Karier" {
		t.Fatalf("unexpected schedule: %v", s0)
	}
	if lec := s0["lecturers"].([]any); len(lec) != 2 {
		t.Fatalf("lecturers: %v", lec)
	}
	if sa := s0["startAt"].(string); len(sa) < 25 || sa[len(sa)-6:] != "+07:00" {
		t.Fatalf("startAt not WIB: %s", sa)
	}

	// reschedule + kiosk checkout
	do("PATCH", "/b/b1", map[string]any{"checkInTime": "08:30", "checkOutTime": "10:10"})
	do("PATCH", "/b/b1/co", map[string]any{"actualCheckOutTime": "09:50", "markComplete": true, "source": "kiosk_button"})

	ev := do("GET", "/events?afterId=0", nil)
	events := ev["events"].([]any)
	types := []string{}
	for _, e := range events {
		types = append(types, e.(map[string]any)["type"].(string))
	}
	want := []string{"booking.rescheduled", "booking.checked_out"}
	if len(types) != 2 || types[0] != want[0] || types[1] != want[1] {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	co := events[1].(map[string]any)["data"].(map[string]any)
	if co["source"] != "kiosk_button" {
		t.Fatalf("checkout source = %v", co["source"])
	}
	snap := co["booking"].(map[string]any)
	if snap["status"] != "completed" || snap["checkedOutAt"] == nil {
		t.Fatalf("checkout snapshot: %v", snap)
	}
	prev := events[0].(map[string]any)["data"].(map[string]any)["previous"].(map[string]any)
	if prev["startTime"] != "08:00" || prev["endTime"] != "09:40" {
		t.Fatalf("previous times: %v", prev)
	}

	// cursor: nothing new after the last id
	next := int64(ev["nextAfterId"].(float64))
	again := do("GET", "/events?afterId="+itoa(next), nil)["events"].([]any)
	if len(again) != 0 {
		t.Fatalf("expected no new events, got %d", len(again))
	}
	// completed booking is hidden from default (confirmed-only) schedule
	if n := len(do("GET", "/schedules?room=Q1.01.03", nil)["schedules"].([]any)); n != 0 {
		t.Fatalf("completed booking should be excluded by default, got %d", n)
	}
	if n := len(do("GET", "/schedules?room=Q1.01.03&status=all", nil)["schedules"].([]any)); n != 1 {
		t.Fatalf("status=all should include it, got %d", n)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
