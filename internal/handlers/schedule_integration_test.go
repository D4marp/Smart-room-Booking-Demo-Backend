package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/database"
	"github.com/bookify-rooms/backend/internal/realtime"
)

// Runs only when TEST_MYSQL_DSN points at a disposable database.
func TestImportScheduleIntegration(t *testing.T) {
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
	db.Exec("DELETE FROM bookings")
	db.Exec("DELETE FROM classes")
	db.Exec("DELETE FROM rooms")
	db.Exec("DELETE FROM users")
	if _, err := db.Exec(`INSERT INTO users (id, email, password, name, role, created_at) VALUES ('u1','a@b.c','x','Admin','admin',1)`); err != nil {
		t.Skipf("cannot seed user (schema differs): %v", err)
	}
	if _, err := db.Exec(`INSERT INTO rooms (id, name, description, location, city, room_class, image_urls, amenities, has_ac, is_available, max_guests, contact_number, created_at) VALUES ('r1','Q1.01.03','','Gedung','Surabaya','Meeting','[]','[]',1,1,40,'',1)`); err != nil {
		t.Skipf("cannot seed room: %v", err)
	}
	// pre-existing manual booking that Replace must remove
	db.Exec(`INSERT INTO bookings (id,user_id,room_id,booking_date,check_in_time,check_out_time,number_of_guests,status,created_at)
	         VALUES ('old','u1','r1',9999999999999,'10:00','11:00',1,'confirmed',1)`)

	h := NewBookingHandler(db, realtime.NewManager())
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/import", func(c *gin.Context) { c.Set("userID", "u1") }, h.ImportSchedule)

	body, _ := json.Marshal(ImportScheduleRequest{
		RoomID: "r1", StartDate: "2026-10-05", EndDate: "2026-10-18", Replace: true,
		Items: []ScheduleItem{
			{Day: "Senin", Time: "07.00 - 10.20", Course: "Perilaku Organisasi", SKS: 4, StudyProgram: "S1 Psikologi", Class: "2023B", Participants: 38, Lecturers: "A; B"},
			{Day: "Rabu", Time: "15.30 - 15.30", Course: "Metopen", SKS: 3, StudyProgram: "S1 Psikologi", Class: "2025H", Participants: 39, Lecturers: "C"},
		},
	})
	for i := 0; i < 2; i++ { // second run proves Replace is idempotent
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("run %d: status %d body %s", i, w.Code, w.Body.String())
		}
	}
	var n, classes, history int
	db.QueryRow("SELECT COUNT(*) FROM bookings").Scan(&n)
	db.QueryRow("SELECT COUNT(*) FROM classes").Scan(&classes)
	db.QueryRow("SELECT COUNT(*) FROM booking_status_history").Scan(&history)
	if n != 4 || classes != 2 || history != 4 {
		t.Fatalf("bookings=%d classes=%d history=%d, want 4/2/4", n, classes, history)
	}
	var code, p1, out string
	db.QueryRow("SELECT class_code, pihak_1, check_out_time FROM bookings WHERE class_code='2025H' LIMIT 1").Scan(&code, &p1, &out)
	if p1 != "C" || out != "18:00" {
		t.Fatalf("got pihak_1=%q out=%q", p1, out)
	}
}
