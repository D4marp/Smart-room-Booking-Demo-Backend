package automation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/bookify-rooms/backend/internal/database"
)

func TestClassKey(t *testing.T) {
	if got := ClassKey("Q1.05.03"); got != "q10503" {
		t.Fatalf("ClassKey = %s", got)
	}
}

func TestPullFlow(t *testing.T) {
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
	for _, tbl := range []string{"automation_events", "integration_events", "booking_status_history", "bookings", "rooms", "users"} {
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

	now := time.Now().In(wib)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wib)
	at := func(h, m int) time.Time { return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, wib) }
	book := func(id, in, out, status string) {
		must(`INSERT INTO bookings (id,user_id,room_id,booking_date,check_in_time,check_out_time,number_of_guests,status,created_at)
		      VALUES (?,?,?,?,?,?,30,?,1)`, id, "u1", "r1", day.UnixMilli(), in, out, status)
	}
	clock := at(6, 50)
	svc := New(db, Config{Mode: ModePull, RoomCodes: []string{"Q1.05.03"}, CheckoutSources: []string{"kiosk_button"}})
	svc.now = func() time.Time { return clock }
	pull := NewPuller(db, []string{"ac", "lamp", "door"}) // strict: all three required here
	pull.now = func() time.Time { return clock }
	ctx := t.Context()
	svc.initCursor(ctx)

	status := func(id string) string {
		var s string
		db.QueryRow("SELECT status FROM automation_events WHERE event_id LIKE ?", "%"+id+"%").Scan(&s)
		return s
	}
	list := func() []Command {
		t.Helper()
		c, err := pull.List(ctx, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// 1) T-10 (a bit late) still produces an ON command for Node-RED
	book("b1", "07:00", "08:40", "confirmed")
	svc.TickPrestart(ctx)
	cmds := list()
	if len(cmds) != 1 || cmds[0].Action != "on" || cmds[0].ClassKey != "q10503" || cmds[0].RoomCode != "Q1.05.03" || cmds[0].BookingID != "b1" {
		t.Fatalf("on command = %+v", cmds)
	}
	svc.TickPrestart(ctx) // no duplicate
	if len(list()) != 1 {
		t.Fatal("duplicate command queued")
	}

	// 2) cancelled before Node-RED fetched it -> never offered again
	must("UPDATE bookings SET status='cancelled' WHERE id='b1'")
	if n := len(list()); n != 0 || status("b1-prestart") != "cancelled" {
		t.Fatalf("cancelled booking still offered: %d / %s", n, status("b1-prestart"))
	}

	// 3) moved class -> stale command dropped
	clock = at(7, 20)
	book("b2", "07:30", "09:00", "confirmed")
	svc.TickPrestart(ctx)
	must("UPDATE bookings SET check_in_time='08:30', check_out_time='10:00' WHERE id='b2'")
	if n := len(list()); n != 0 || status("b2-prestart") != "cancelled" {
		t.Fatalf("moved booking still offered: %d / %s", n, status("b2-prestart"))
	}

	// 4) ack: all three devices -> acked, idempotent, gone from the queue
	clock = at(8, 50)
	book("b3", "09:00", "10:30", "confirmed")
	svc.TickPrestart(ctx)
	id := "evt-b3-prestart-" + day.Format("20060102") + "0900"
	if len(list()) != 1 {
		t.Fatalf("b3 not queued")
	}
	ok := map[string]string{"ac": "command_sent", "lamp": "command_sent", "door": "command_sent"}
	if st, err := pull.Ack(ctx, id, true, ok, ""); err != nil || st != "acked" {
		t.Fatalf("ack = %s %v", st, err)
	}
	if st, err := pull.Ack(ctx, id, true, ok, ""); err != nil || st != "acked" {
		t.Fatalf("second ack should be idempotent: %s %v", st, err)
	}
	if len(list()) != 0 {
		t.Fatal("acked command still offered")
	}

	// 5) partial failure keeps the command queued, five failures give up
	clock = at(8, 55)
	book("b4", "09:10", "10:00", "confirmed")
	svc.TickPrestart(ctx)
	id4 := "evt-b4-prestart-" + day.Format("20060102") + "0910"
	bad := map[string]string{"ac": "command_sent", "lamp": "error", "door": "command_sent"}
	for i := 1; i <= 4; i++ {
		if st, _ := pull.Ack(ctx, id4, false, bad, "lamp unreachable"); st != "queued" {
			t.Fatalf("attempt %d -> %s, want queued", i, st)
		}
	}
	if len(list()) != 1 {
		t.Fatal("command should still be offered after failures")
	}
	if st, _ := pull.Ack(ctx, id4, false, bad, "lamp unreachable"); st != "failed" {
		t.Fatalf("5th failure -> %s, want failed", st)
	}
	// success claimed but a device is missing = not a success
	clock = at(9, 5)
	book("b4x", "09:20", "10:00", "confirmed")
	svc.TickPrestart(ctx)
	idx := "evt-b4x-prestart-" + day.Format("20060102") + "0920"
	st, err := pull.Ack(ctx, idx, true, map[string]string{"ac": "command_sent", "lamp": "command_sent"}, "")
	var inc *IncompleteAckError
	if st != "queued" || !errors.As(err, &inc) || len(inc.Missing) != 1 || inc.Missing[0] != "door" {
		t.Fatalf("missing door must be rejected as incomplete, got %s %v", st, err)
	}
	var att int
	db.QueryRow("SELECT attempts FROM automation_events WHERE event_id=?", idx).Scan(&att)
	if att != 0 {
		t.Fatalf("a rejected (incomplete) ack must not consume an attempt, attempts=%d", att)
	}
	// AC + lamp only (door not controlled): accepted when only those two are required
	book("b4y", "09:18", "10:00", "confirmed")
	svc.TickPrestart(ctx)
	idy := "evt-b4y-prestart-" + day.Format("20060102") + "0918"
	lenient := NewPuller(db, nil)
	lenient.now = pull.now
	if st, err := lenient.Ack(ctx, idy, true, map[string]string{"ac": "command_sent", "lamp": "command_sent"}, ""); err != nil || st != "acked" {
		t.Fatalf("ac+lamp must be enough with the default required devices, got %s %v", st, err)
	}

	// 6) expiry: not executed in time -> expired
	clock = at(9, 31) // b4x (09:20) expired at 09:30
	if len(list()) != 0 {
		t.Fatal("expired commands must not be offered")
	}
	if status("b4x-prestart") != "expired" {
		t.Fatalf("b4x status = %s, want expired", status("b4x-prestart"))
	}

	// 7) checkout -> OFF queued (next class far away), re-checked on fetch
	clock = at(11, 0)
	book("b5", "13:00", "14:30", "confirmed")
	payload, _ := json.Marshal(map[string]any{"source": "kiosk_button", "booking": map[string]any{"checkedOutAt": clock.Format(time.RFC3339)}})
	must(`INSERT INTO integration_events (event_type,booking_id,room_code,payload,created_at,next_attempt_at)
	      VALUES ('booking.checked_out','b3','Q1.05.03',?,?,?)`, string(payload), clock.UnixMilli(), clock.UnixMilli())
	svc.TickCheckout(ctx)
	cmds = list()
	if len(cmds) != 1 || cmds[0].Action != "off" || cmds[0].NextBookingStartsAt == nil {
		t.Fatalf("off command = %+v", cmds)
	}
	// a class gets booked right after -> the OFF is withdrawn before execution
	book("b6", "11:10", "12:00", "confirmed")
	if n := len(list()); n != 0 || status("b3-checkout") != "rejected_409" {
		t.Fatalf("OFF must be withdrawn when a class starts within 15 min: %d / %s", n, status("b3-checkout"))
	}

	// 8) checkout when the next class is already too close is never queued
	clock = at(12, 5)
	payload, _ = json.Marshal(map[string]any{"source": "kiosk_button", "booking": map[string]any{"checkedOutAt": clock.Format(time.RFC3339)}})
	must(`INSERT INTO integration_events (event_type,booking_id,room_code,payload,created_at,next_attempt_at)
	      VALUES ('booking.checked_out','b6','Q1.05.03',?,?,?)`, string(payload), clock.UnixMilli(), clock.UnixMilli())
	book("b7", "12:15", "13:00", "confirmed") // starts in 10 minutes
	svc.TickCheckout(ctx)
	if len(list()) != 0 || status("b6-checkout") != "rejected_409" {
		t.Fatalf("OFF queued despite next class in 10 min: %s", status("b6-checkout"))
	}
}
