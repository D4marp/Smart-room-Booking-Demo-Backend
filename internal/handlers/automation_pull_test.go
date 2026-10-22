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
	"github.com/bookify-rooms/backend/internal/middleware"
)

// Runs only when TEST_MYSQL_DSN points at a disposable database.
func TestAutomationPullEndpoints(t *testing.T) {
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
	for _, tbl := range []string{"automation_events", "booking_status_history", "bookings", "rooms", "users"} {
		db.Exec("DELETE FROM " + tbl)
	}
	must := func(q string, a ...any) {
		t.Helper()
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	must(`INSERT INTO users (id,email,password,name,role,created_at) VALUES ('u1','a@b.c','x','Admin','admin',1)`)
	must(`INSERT INTO rooms (id,name,description,location,city,room_class,image_urls,amenities,has_ac,is_available,max_guests,contact_number,created_at)
	      VALUES ('r1','Q1.05.03','','G','S','M','[]','[]',1,1,40,'',1)`)

	now := time.Now().In(jakarta)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jakarta).UnixMilli()
	start := now.Add(10 * time.Minute)
	in := start.Format("15:04")
	must(`INSERT INTO bookings (id,user_id,room_id,booking_date,check_in_time,check_out_time,number_of_guests,status,created_at)
	      VALUES ('b1','u1','r1',?,?, '23:59',30,'confirmed',1)`, midnight, in)
	cmd, _ := json.Marshal(map[string]any{
		"commandId": "evt-b1-prestart-x", "eventId": "evt-b1-prestart-x", "action": "on", "reason": "prestart",
		"roomCode": "Q1.05.03", "classKey": "q10503", "bookingId": "b1", "source": "room_booking",
		"startsAt":  start.Truncate(time.Minute).Format(time.RFC3339),
		"validFrom": now.Add(-time.Minute).Format(time.RFC3339), "expiresAt": now.Add(time.Hour).Format(time.RFC3339),
	})
	must(`INSERT INTO automation_events (event_id,event_type,booking_id,room_code,status,payload,created_at,updated_at)
	      VALUES ('evt-b1-prestart-x','prestart','b1','Q1.05.03','queued',?,1,1)`, string(cmd))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAutomationPullHandler(db, nil) // default: ac + lamp
	g := r.Group("/automation")
	g.Use(middleware.IntegrationAuth([]string{"node-red-key"}))
	g.GET("/commands", h.ListCommands)
	g.POST("/commands/:commandId/ack", h.AckCommand)

	call := func(method, url, key string, body any) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, url, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	if code, _ := call("GET", "/automation/commands", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("no key -> %d", code)
	}
	if code, _ := call("GET", "/automation/commands", "integration-read-key", nil); code != http.StatusUnauthorized {
		t.Fatalf("another API key must not work -> %d", code)
	}
	code, out := call("GET", "/automation/commands?room=Q1.05.03", "node-red-key", nil)
	cmds := out["data"].(map[string]any)["commands"].([]any)
	if code != 200 || len(cmds) != 1 || cmds[0].(map[string]any)["action"] != "on" || cmds[0].(map[string]any)["classKey"] != "q10503" {
		t.Fatalf("commands = %d %v", code, out)
	}
	if code, _ := call("POST", "/automation/commands/nope/ack", "node-red-key", map[string]any{"success": true}); code != http.StatusNotFound {
		t.Fatalf("unknown command -> %d", code)
	}
	// a success claim that lacks a required device is rejected loudly (422), not silently accepted
	partial := map[string]any{"success": true, "devices": map[string]any{"ac": "command_sent"}}
	if code, out := call("POST", "/automation/commands/evt-b1-prestart-x/ack", "node-red-key", partial); code != http.StatusUnprocessableEntity {
		t.Fatalf("incomplete ack -> %d %v, want 422", code, out)
	}
	// the backend on the mini PC reports AC + lamp only (no door), in the plain format
	ack := map[string]any{"success": true, "devices": map[string]any{"ac": "command_sent", "lamp": "command_sent"}}
	code, out = call("POST", "/automation/commands/evt-b1-prestart-x/ack", "node-red-key", ack)
	if code != 200 || out["data"].(map[string]any)["status"] != "acked" {
		t.Fatalf("ack = %d %v", code, out)
	}
	_, out = call("GET", "/automation/commands", "node-red-key", nil)
	if n := len(out["data"].(map[string]any)["commands"].([]any)); n != 0 {
		t.Fatalf("acked command still offered (%d)", n)
	}
}
