package automation

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/bookify-rooms/backend/internal/database"
)

func TestSignVector(t *testing.T) {
	// Independent vector: python hmac(sha256, key="k", msg="2026-10-11T03:00:00.000Z." + body)
	got := Sign("k", "1700000000", []byte(`{"a":1}`))
	want := "sha256=1b6ad1bc9bf48c52ce01939866d5877c6cc4e87ddad27e16673cb8c4964394a3"
	if got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}

// ---- mock of the on-prem API, following the partner's published contract ----

type received struct {
	Path string
	Body map[string]any
}

type partner struct {
	t       *testing.T
	secret  string
	mu      sync.Mutex
	got     []received
	status  int // forced status (0 = behave per contract)
	clockFn func() time.Time
}

func (p *partner) handler(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	ts := r.Header.Get("X-Booking-Timestamp")
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", ts)
	if err != nil || p.clockFn().Sub(parsed).Abs() > 5*time.Minute {
		w.WriteHeader(401)
		return
	}
	if r.Header.Get("X-Booking-Signature") != Sign(p.secret, ts, raw) {
		w.WriteHeader(401)
		return
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		w.WriteHeader(422)
		return
	}
	p.mu.Lock()
	p.got = append(p.got, received{Path: r.URL.Path, Body: body})
	p.mu.Unlock()
	if p.status != 0 {
		w.WriteHeader(p.status)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/checkout") {
		next, present := body["nextBookingStartsAt"]
		if !present {
			w.WriteHeader(422)
			return
		}
		if s, ok := next.(string); ok {
			nt, _ := time.Parse(time.RFC3339, s)
			if nt.Sub(p.clockFn()) <= 15*time.Minute {
				w.WriteHeader(409)
				return
			}
		}
	}
	w.WriteHeader(202)
	w.Write([]byte(`{"success":true,"status":"command_sent"}`))
}

func (p *partner) requests() []received {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]received(nil), p.got...)
}

func TestAutomationFlow(t *testing.T) {
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
	room := func(id, name string) {
		must(`INSERT INTO rooms (id,name,description,location,city,room_class,image_urls,amenities,has_ac,is_available,max_guests,contact_number,created_at)
		      VALUES (?,?,'','G','S','M','[]','[]',1,1,40,'',1)`, id, name)
	}
	room("r1", "Q1.05.03")
	room("r2", "Q1.01.03") // not one of the controlled rooms

	now := time.Now().In(wib)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wib)
	at := func(h, m int) time.Time { return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, wib) }
	book := func(id, roomID, in, out, status string) {
		must(`INSERT INTO bookings (id,user_id,room_id,booking_date,check_in_time,check_out_time,number_of_guests,status,created_at)
		      VALUES (?,?,?,?,?,?,30,?,1)`, id, "u1", roomID, day.UnixMilli(), in, out, status)
	}
	book("b1", "r1", "07:00", "08:40", "confirmed")
	book("b2", "r1", "09:00", "10:00", "cancelled")
	book("b3", "r2", "07:00", "08:40", "confirmed")

	clock := at(6, 46)
	const secret = "0123456789abcdef0123456789abcdef"
	p := &partner{t: t, secret: secret, clockFn: func() time.Time { return clock }}
	srv := httptest.NewServer(http.HandlerFunc(p.handler))
	defer srv.Close()

	svc := New(db, Config{
		Mode: ModeLive, BaseURL: srv.URL, Secret: secret,
		RoomCodes:       []string{"Q1.05.03", "Q1.05.05"},
		CheckoutSources: []string{"kiosk_button"},
	})
	svc.now = func() time.Time { return clock }
	ctx := t.Context()
	svc.initCursor(ctx)

	// T-14: exactly one prestart (b1). Cancelled b2 and uncontrolled room b3 are ignored.
	svc.TickPrestart(ctx)
	reqs := p.requests()
	if len(reqs) != 1 || !strings.HasSuffix(reqs[0].Path, "/prestart") || reqs[0].Body["bookingId"] != "b1" {
		t.Fatalf("prestart requests = %+v", reqs)
	}
	if reqs[0].Body["roomCode"] != "Q1.05.03" || reqs[0].Body["eventType"] != "booking.prestart" ||
		!strings.HasSuffix(reqs[0].Body["startsAt"].(string), "+07:00") {
		t.Fatalf("prestart body = %+v", reqs[0].Body)
	}
	clock = at(6, 46).Add(20 * time.Second)
	svc.TickPrestart(ctx)
	if n := len(p.requests()); n != 1 {
		t.Fatalf("duplicate prestart sent (%d requests)", n)
	}

	// reschedule b1 to 07:30 -> new T-15 for the new time, with the new startsAt
	must("UPDATE bookings SET check_in_time='07:30', check_out_time='09:10' WHERE id='b1'")
	clock = at(7, 16)
	svc.TickPrestart(ctx)
	reqs = p.requests()
	if len(reqs) != 2 || !strings.HasPrefix(reqs[1].Body["startsAt"].(string), day.Format("2006-01-02")+"T07:30:00") {
		t.Fatalf("after reschedule: %+v", reqs)
	}

	// T-10 for a booking created late: recorded as missed, nothing sent
	book("b6", "r1", "07:26", "08:00", "confirmed")
	svc.TickPrestart(ctx)
	var st string
	db.QueryRow("SELECT status FROM automation_events WHERE booking_id='b6'").Scan(&st)
	if st != "missed" || len(p.requests()) != 2 {
		t.Fatalf("b6 status=%q requests=%d, want missed/2", st, len(p.requests()))
	}

	// ---- checkout ----
	addCheckout := func(bookingID, roomCode, source string, created time.Time) int64 {
		payload, _ := json.Marshal(map[string]any{
			"source":  source,
			"booking": map[string]any{"checkedOutAt": created.Format(time.RFC3339)},
		})
		res, err := db.Exec(`INSERT INTO integration_events (event_type,booking_id,room_code,payload,created_at,next_attempt_at)
			VALUES ('booking.checked_out',?,?,?,?,?)`, bookingID, roomCode, string(payload), created.UnixMilli(), created.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	status := func(eventID string) string {
		var s string
		db.QueryRow("SELECT status FROM automation_events WHERE event_id=?", eventID).Scan(&s)
		return s
	}

	clock = at(9, 20)
	book("b7", "r1", "13:00", "14:40", "confirmed")
	addCheckout("b1", "Q1.05.03", "kiosk_button", clock)
	before := len(p.requests())
	svc.TickCheckout(ctx)
	reqs = p.requests()
	if len(reqs) != before+1 || !strings.HasSuffix(reqs[before].Path, "/checkout") {
		t.Fatalf("checkout not sent: %+v", reqs)
	}
	next, _ := reqs[before].Body["nextBookingStartsAt"].(string)
	if !strings.HasPrefix(next, day.Format("2006-01-02")+"T13:00:00") {
		t.Fatalf("nextBookingStartsAt = %q, want today 13:00", next)
	}
	if status("evt-b1-checkout") != "sent" {
		t.Fatalf("status = %s", status("evt-b1-checkout"))
	}

	// auto check-out and uncontrolled rooms never switch anything off
	addCheckout("b6", "Q1.05.03", "kiosk_auto", clock)
	addCheckout("b3", "Q1.01.03", "kiosk_button", clock)
	svc.TickCheckout(ctx)
	if len(p.requests()) != before+1 || status("evt-b6-checkout") != "skipped" || status("evt-b3-checkout") != "skipped" {
		t.Fatalf("expected both skipped without requests: %d %s %s", len(p.requests()), status("evt-b6-checkout"), status("evt-b3-checkout"))
	}

	// next class in 10 minutes -> partner answers 409 -> recorded, not retried
	clock = at(12, 50)
	book("b8", "r1", "13:00", "13:40", "confirmed")
	must("UPDATE bookings SET status='cancelled' WHERE id='b7'")
	addCheckout("b9", "Q1.05.03", "kiosk_button", clock)
	svc.TickCheckout(ctx)
	if status("evt-b9-checkout") != "rejected_409" {
		t.Fatalf("status = %s, want rejected_409", status("evt-b9-checkout"))
	}

	// partner down (503) -> retry, then succeeds without a duplicate source event
	clock = at(15, 0)
	p.status = 503
	addCheckout("b10", "Q1.05.03", "kiosk_button", clock)
	svc.TickCheckout(ctx)
	if status("evt-b10-checkout") != "retry" {
		t.Fatalf("status = %s, want retry", status("evt-b10-checkout"))
	}
	p.status = 0
	clock = clock.Add(2 * time.Minute)
	svc.TickCheckout(ctx)
	if status("evt-b10-checkout") != "sent" {
		t.Fatalf("status = %s, want sent after retry", status("evt-b10-checkout"))
	}

	// stale checkout (older than 15 min) is never acted on
	old := clock.Add(-30 * time.Minute)
	addCheckout("b11", "Q1.05.03", "kiosk_button", old)
	n := len(p.requests())
	svc.TickCheckout(ctx)
	if len(p.requests()) != n || status("evt-b11-checkout") != "expired" {
		t.Fatalf("stale checkout handled wrongly: %s", status("evt-b11-checkout"))
	}

	// dry_run records but never sends
	svc.cfg.Mode = ModeDryRun
	clock = clock.Add(time.Minute)
	addCheckout("b12", "Q1.05.03", "kiosk_button", clock)
	n = len(p.requests())
	svc.TickCheckout(ctx)
	if len(p.requests()) != n || status("evt-b12-checkout") != "dry_run" {
		t.Fatalf("dry_run sent or mis-recorded: %s", status("evt-b12-checkout"))
	}
}
