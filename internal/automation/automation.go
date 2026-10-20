// Package automation connects the room-booking backend to the on-prem
// building automation (AC, lights, door) of UNESA.
//
// It is the *client* of the on-prem API:
//
//	POST {base}/api/v1/room-booking/prestart  — 15 min before a class starts
//	POST {base}/api/v1/room-booking/checkout  — when the Philips tablet checks out
//
// Requests are signed with HMAC-SHA256 over "<timestamp>.<rawBody>". The
// service never sends anything unless Mode == "live"; "dry_run" only records
// what it would have sent.
package automation

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	ModeOff    = "off"
	ModeDryRun = "dry_run"
	ModeLive   = "live"
	// ModePull queues ON/OFF commands that Node-RED fetches over HTTPS
	// (GET .../automation/commands) and confirms (POST .../ack).
	ModePull = "pull"

	prestartMin = 13 * time.Minute // partner accepts T-17..T-13; we send in T-15..T-13
	prestartTgt = 15 * time.Minute
	maxAge      = 15 * time.Minute // never act on a checkout older than this
	maxAttempts = 8

	nextClassGuard = 15 * time.Minute // partner rule: no OFF if next class is this close
)

type Config struct {
	Mode            string
	BaseURL         string
	Secret          string
	RoomCodes       []string // only these rooms are controlled
	CheckoutSources []string // checkout event sources that trigger OFF
}

type Service struct {
	db     *sql.DB
	cfg    Config
	client *http.Client
	now    func() time.Time
	rooms  map[string]bool
	srcs   map[string]bool
}

var wib = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return l
	}
	return time.FixedZone("WIB", 7*3600)
}()

func New(db *sql.DB, cfg Config) *Service {
	s := &Service{
		db:     db,
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		now:    time.Now,
		rooms:  map[string]bool{},
		srcs:   map[string]bool{},
	}
	for _, r := range cfg.RoomCodes {
		s.rooms[strings.TrimSpace(r)] = true
	}
	for _, r := range cfg.CheckoutSources {
		s.srcs[strings.TrimSpace(r)] = true
	}
	return s
}

// Sign returns "sha256=" + hex(HMAC-SHA256(secret, timestamp + "." + body)).
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Run drives both workers until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	if s.cfg.Mode != ModeDryRun && s.cfg.Mode != ModeLive && s.cfg.Mode != ModePull {
		return
	}
	if s.cfg.Mode == ModeLive && (s.cfg.BaseURL == "" || len(s.cfg.Secret) < 32) {
		log.Printf("automation: mode=live needs AUTOMATION_BASE_URL and a secret of >= 32 chars; NOT starting")
		return
	}
	log.Printf("automation: started mode=%s rooms=%v sources=%v", s.cfg.Mode, s.cfg.RoomCodes, s.cfg.CheckoutSources)
	s.initCursor(ctx)
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		s.TickPrestart(ctx)
		s.TickCheckout(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type result struct {
	status int
	body   string
	err    error
}

func (r result) ok() bool {
	return r.err == nil && (r.status == 202 || (r.status == 200 && strings.Contains(r.body, `"success":true`)))
}

// retryable: network errors and 5xx. 4xx (401/403/409/422) are final.
func (r result) retryable() bool {
	return r.err != nil || r.status >= 500
}

func (s *Service) post(ctx context.Context, path string, body []byte) result {
	ts := s.now().UTC().Format("2006-01-02T15:04:05.000Z")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return result{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Booking-Timestamp", ts)
	req.Header.Set("X-Booking-Signature", Sign(s.cfg.Secret, ts, body))
	resp, err := s.client.Do(req)
	if err != nil {
		return result{err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	return result{status: resp.StatusCode, body: string(raw)}
}

func iso(t time.Time) string { return t.In(wib).Format(time.RFC3339) }

func backoff(attempts int) time.Duration {
	d := 20 * time.Second << min(attempts, 4) // 20s..320s
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// ---- persistence ----------------------------------------------------------

type row struct {
	status   string
	attempts int
	nextAt   int64
}

func (s *Service) load(ctx context.Context, eventID string) (row, bool) {
	var r row
	err := s.db.QueryRowContext(ctx,
		"SELECT status, attempts, next_attempt_at FROM automation_events WHERE event_id = ?", eventID).
		Scan(&r.status, &r.attempts, &r.nextAt)
	return r, err == nil
}

func (s *Service) insert(ctx context.Context, eventID, typ, bookingID, room string, srcID *int64, status string, payload []byte, note string) {
	now := s.now().UnixMilli()
	var src any
	if srcID != nil {
		src = *srcID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO automation_events
		 (event_id, event_type, booking_id, room_code, source_event_id, status, error, payload, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		eventID, typ, nullable(bookingID), nullable(room), src, status, nullable(note), string(payload), now, now)
	if err != nil {
		log.Printf("automation: insert %s failed: %v", eventID, err)
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) finish(ctx context.Context, eventID, status string, attempts int, nextAt int64, r result, note string) {
	var errText any
	if r.err != nil {
		errText = r.err.Error()
	} else if note != "" {
		errText = note
	}
	var httpStatus any
	if r.status != 0 {
		httpStatus = r.status
	}
	s.db.ExecContext(ctx,
		`UPDATE automation_events SET status=?, attempts=?, next_attempt_at=?, http_status=?, response=?, error=?, updated_at=?
		 WHERE event_id=?`,
		status, attempts, nextAt, httpStatus, nullable(r.body), errText, s.now().UnixMilli(), eventID)
}

// send executes (or, in dry_run, simulates) one POST and records the outcome.
// stillValid is re-checked before every retry.
func (s *Service) send(ctx context.Context, eventID, path string, body []byte, cmd Command, prev row, stillValid func() bool) {
	if s.cfg.Mode == ModePull {
		s.enqueue(ctx, eventID, cmd)
		return
	}
	if s.cfg.Mode == ModeDryRun {
		s.finish(ctx, eventID, "dry_run", prev.attempts, 0, result{}, "dry run: not sent")
		log.Printf("automation[dry_run]: would POST %s %s", path, string(body))
		return
	}
	attempts := prev.attempts + 1
	r := s.post(ctx, path, body)
	switch {
	case r.ok():
		s.finish(ctx, eventID, "sent", attempts, 0, r, "")
		log.Printf("automation: %s accepted (%d)", eventID, r.status)
	case r.status == 409:
		// Partner refused: the next class is active or starts within 15 minutes.
		s.finish(ctx, eventID, "rejected_409", attempts, 0, r, "partner: next class too close / T-15 mismatch")
		log.Printf("automation: %s rejected 409: %s", eventID, r.body)
	case r.retryable() && attempts < maxAttempts && stillValid():
		s.finish(ctx, eventID, "retry", attempts, s.now().Add(backoff(attempts)).UnixMilli(), r, "")
		log.Printf("automation: %s will retry (attempt %d): status=%d err=%v", eventID, attempts, r.status, r.err)
	default:
		s.finish(ctx, eventID, "failed", attempts, 0, r, "")
		log.Printf("automation: %s FAILED: status=%d err=%v body=%s", eventID, r.status, r.err, r.body)
	}
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (s *Service) roomArgs() []any {
	out := make([]any, 0, len(s.rooms))
	for r := range s.rooms {
		out = append(out, r)
	}
	return out
}

// ---- T-15 prestart ---------------------------------------------------------

type prestartPayload struct {
	EventID   string `json:"eventId"`
	BookingID string `json:"bookingId"`
	RoomCode  string `json:"roomCode"`
	StartsAt  string `json:"startsAt"`
	EventType string `json:"eventType"`
}

// TickPrestart sends booking.prestart for confirmed, not-yet-started bookings
// whose start is 13-15 minutes away. State is read live from the database, so
// a cancelled or moved class is never sent with its old time.
func (s *Service) TickPrestart(ctx context.Context) {
	if len(s.rooms) == 0 {
		return
	}
	now := s.now()
	local := now.In(wib)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, wib)

	q := `SELECT b.id, r.name, b.check_in_time
	      FROM bookings b JOIN rooms r ON r.id = b.room_id
	      WHERE b.status = 'confirmed' AND b.booking_date = ?
	        AND (b.actual_check_in_time IS NULL OR b.actual_check_in_time = '')
	        AND (b.actual_check_out_time IS NULL OR b.actual_check_out_time = '')
	        AND r.name IN (` + placeholders(len(s.rooms)) + `)`
	args := append([]any{midnight.UnixMilli()}, s.roomArgs()...)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("automation: prestart query failed: %v", err)
		return
	}
	type cand struct{ id, room, in string }
	var cands []cand
	for rows.Next() {
		var c cand
		if rows.Scan(&c.id, &c.room, &c.in) == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()

	for _, c := range cands {
		clock, err := time.Parse("15:04", c.in)
		if err != nil {
			continue
		}
		start := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, wib)
		delta := start.Sub(now)
		if delta > prestartTgt || delta <= 0 {
			continue // not yet T-15, or already started
		}
		eventID := fmt.Sprintf("evt-%s-prestart-%s", c.id, start.Format("200601021504"))
		payload, _ := json.Marshal(prestartPayload{
			EventID: eventID, BookingID: c.id, RoomCode: c.room,
			StartsAt: iso(start), EventType: "booking.prestart",
		})
		prev, exists := s.load(ctx, eventID)
		minDelta := prestartMin
		if s.cfg.Mode == ModePull {
			minDelta = 0 // Node-RED may be a bit late; ON until the class starts is still useful
		}
		if delta < minDelta { // window (T-13) already closed
			if !exists {
				s.insert(ctx, eventID, "prestart", c.id, c.room, nil, "missed", payload, "T-15 window missed (service was down or booking created late)")
				log.Printf("automation: %s MISSED T-15 window (%s before start)", eventID, delta.Round(time.Second))
			}
			continue
		}
		if !exists {
			s.insert(ctx, eventID, "prestart", c.id, c.room, nil, "pending", payload, "")
			prev = row{status: "pending"}
		}
		if prev.status != "pending" && !(prev.status == "retry" && prev.nextAt <= now.UnixMilli()) {
			continue
		}
		startAt := start
		startsAt := iso(start)
		cmd := Command{
			CommandID: eventID, EventID: eventID, Action: "on", Reason: "prestart",
			RoomCode: c.room, ClassKey: ClassKey(c.room), BookingID: c.id, Source: "room_booking",
			StartsAt:  &startsAt,
			ValidFrom: iso(start.Add(-prestartTgt)), ExpiresAt: iso(start.Add(10 * time.Minute)),
		}
		s.send(ctx, eventID, "/api/v1/room-booking/prestart", payload, cmd, prev, func() bool {
			return startAt.Sub(s.now()) >= prestartMin
		})
	}
}

// ---- checkout --------------------------------------------------------------

type checkoutPayload struct {
	EventID             string  `json:"eventId"`
	BookingID           string  `json:"bookingId"`
	RoomCode            string  `json:"roomCode"`
	CheckedOutAt        string  `json:"checkedOutAt"`
	NextBookingStartsAt *string `json:"nextBookingStartsAt"`
	EventType           string  `json:"eventType"`
}

// initCursor makes the very first run ignore historical checkouts.
func (s *Service) initCursor(ctx context.Context) {
	var n int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM automation_events").Scan(&n)
	if n > 0 {
		return
	}
	var max sql.NullInt64
	s.db.QueryRowContext(ctx, "SELECT MAX(id) FROM integration_events").Scan(&max)
	id := max.Int64
	s.insert(ctx, "cursor-init", "cursor", "", "", &id, "meta", nil, "starting point: older checkouts are ignored")
}

type checkoutEvent struct {
	ID        int64
	BookingID string
	RoomCode  string
	Payload   string
	CreatedAt int64
}

// TickCheckout turns booking.checked_out events (from the tablet button) into
// POST /checkout, after computing the next class of the room.
func (s *Service) TickCheckout(ctx context.Context) {
	now := s.now()

	// 1) retries that are due
	rr, err := s.db.QueryContext(ctx,
		`SELECT event_id, source_event_id, attempts FROM automation_events
		 WHERE event_type='checkout' AND status='retry' AND next_attempt_at <= ?`, now.UnixMilli())
	if err == nil {
		type rt struct {
			eventID string
			src     int64
			att     int
		}
		var due []rt
		for rr.Next() {
			var x rt
			if rr.Scan(&x.eventID, &x.src, &x.att) == nil {
				due = append(due, x)
			}
		}
		rr.Close()
		for _, x := range due {
			s.processCheckout(ctx, x.src, row{status: "retry", attempts: x.att})
		}
	}

	// 2) new events past the cursor
	var cursor sql.NullInt64
	s.db.QueryRowContext(ctx, "SELECT MAX(source_event_id) FROM automation_events").Scan(&cursor)
	ev, err := s.db.QueryContext(ctx,
		`SELECT id FROM integration_events WHERE id > ? AND event_type = 'booking.checked_out' ORDER BY id ASC LIMIT 50`,
		cursor.Int64)
	if err != nil {
		log.Printf("automation: checkout scan failed: %v", err)
		return
	}
	var ids []int64
	for ev.Next() {
		var id int64
		if ev.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	ev.Close()
	for _, id := range ids {
		s.processCheckout(ctx, id, row{})
	}
}

func (s *Service) processCheckout(ctx context.Context, sourceID int64, prev row) {
	var e checkoutEvent
	e.ID = sourceID
	var bid, room sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT booking_id, room_code, payload, created_at FROM integration_events WHERE id = ?", sourceID).
		Scan(&bid, &room, &e.Payload, &e.CreatedAt)
	if err != nil {
		return
	}
	e.BookingID, e.RoomCode = bid.String, room.String
	eventID := fmt.Sprintf("evt-%s-checkout", e.BookingID)
	src := sourceID

	skip := func(reason string) {
		if prev.status == "retry" {
			s.finish(ctx, eventID, "skipped", prev.attempts, 0, result{}, reason)
		} else {
			s.insert(ctx, eventID, "checkout", e.BookingID, e.RoomCode, &src, "skipped", nil, reason)
		}
		log.Printf("automation: checkout %s skipped: %s", eventID, reason)
	}

	var data struct {
		Source  string `json:"source"`
		Booking struct {
			CheckedOutAt *string `json:"checkedOutAt"`
		} `json:"booking"`
	}
	if json.Unmarshal([]byte(e.Payload), &data) != nil {
		skip("unreadable event payload")
		return
	}
	if !s.rooms[e.RoomCode] {
		skip("room " + e.RoomCode + " is not under automation")
		return
	}
	if !s.srcs[data.Source] {
		skip("source " + data.Source + " does not trigger room OFF")
		return
	}
	occurred := time.UnixMilli(e.CreatedAt)
	if s.now().Sub(occurred) > maxAge {
		if prev.status == "retry" {
			s.finish(ctx, eventID, "expired", prev.attempts, 0, result{}, "checkout older than 15 minutes; not acting")
		} else {
			s.insert(ctx, eventID, "checkout", e.BookingID, e.RoomCode, &src, "expired", nil, "checkout older than 15 minutes; not acting")
		}
		return
	}
	checkedOut := occurred
	if data.Booking.CheckedOutAt != nil {
		if t, err := time.Parse(time.RFC3339, *data.Booking.CheckedOutAt); err == nil {
			checkedOut = t
		}
	}
	next, err := nextBookingStart(ctx, s.db, e.RoomCode, e.BookingID, s.now())
	if err != nil {
		log.Printf("automation: next-booking lookup failed: %v", err)
		return // try again next tick; the event is still unprocessed
	}
	p := checkoutPayload{
		EventID: eventID, BookingID: e.BookingID, RoomCode: e.RoomCode,
		CheckedOutAt: iso(checkedOut), EventType: "booking.checked_out",
	}
	if next != nil {
		v := iso(*next)
		p.NextBookingStartsAt = &v
	}
	body, _ := json.Marshal(p)
	if s.cfg.Mode == ModePull && next != nil && next.Sub(s.now()) <= nextClassGuard {
		// Same rule the on-prem API applies: never switch the room off when
		// the next class is running or starts within 15 minutes.
		note := "next class starts within 15 minutes (or is running); room stays on"
		if prev.status == "retry" || prev.status == "queued" {
			s.finish(ctx, eventID, "rejected_409", prev.attempts, 0, result{}, note)
		} else {
			s.insert(ctx, eventID, "checkout", e.BookingID, e.RoomCode, &src, "rejected_409", body, note)
		}
		log.Printf("automation: checkout %s not queued: %s", eventID, note)
		return
	}
	var nextISO *string
	if next != nil {
		v := iso(*next)
		nextISO = &v
	}
	cmd := Command{
		CommandID: eventID, EventID: eventID, Action: "off", Reason: "checkout",
		RoomCode: e.RoomCode, ClassKey: ClassKey(e.RoomCode), BookingID: e.BookingID, Source: "room_booking",
		NextBookingStartsAt: nextISO,
		ValidFrom:           iso(s.now()), ExpiresAt: iso(occurred.Add(maxAge)),
	}
	if prev.status != "retry" {
		s.insert(ctx, eventID, "checkout", e.BookingID, e.RoomCode, &src, "pending", body, "")
	} else {
		s.db.ExecContext(ctx, "UPDATE automation_events SET payload=? WHERE event_id=?", string(body), eventID)
	}
	s.send(ctx, eventID, "/api/v1/room-booking/checkout", body, cmd, prev, func() bool {
		return s.now().Sub(occurred) <= maxAge
	})
}

// nextBookingStart finds the earliest confirmed, not-yet-finished booking of the
// room that starts after `after` (looking up to 14 days ahead). nil = none.
func nextBookingStart(ctx context.Context, db *sql.DB, roomCode, excludeID string, after time.Time) (*time.Time, error) {
	local := after.In(wib)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, wib)
	to := from.AddDate(0, 0, 14)
	rows, err := db.QueryContext(ctx,
		`SELECT b.booking_date, b.check_in_time, b.check_out_time
		 FROM bookings b JOIN rooms r ON r.id = b.room_id
		 WHERE r.name = ? AND b.status = 'confirmed' AND b.id <> ?
		   AND (b.actual_check_out_time IS NULL OR b.actual_check_out_time = '')
		   AND b.booking_date >= ? AND b.booking_date <= ?`,
		roomCode, excludeID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *time.Time
	for rows.Next() {
		var dateMs int64
		var in, out string
		if rows.Scan(&dateMs, &in, &out) != nil {
			continue
		}
		inClock, err1 := time.Parse("15:04", in)
		outClock, err2 := time.Parse("15:04", out)
		if err1 != nil || err2 != nil {
			continue
		}
		d := time.UnixMilli(dateMs).In(wib)
		start := time.Date(d.Year(), d.Month(), d.Day(), inClock.Hour(), inClock.Minute(), 0, 0, wib)
		end := time.Date(d.Year(), d.Month(), d.Day(), outClock.Hour(), outClock.Minute(), 0, 0, wib)
		if start.Before(after) {
			if !end.After(after) {
				continue // already over
			}
			// A class is running right now: report "now" so the partner
			// refuses to switch the room off.
			start = after
		}
		if best == nil || start.Before(*best) {
			v := start
			best = &v
		}
	}
	return best, rows.Err()
}
