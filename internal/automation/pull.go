package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Command is one ON/OFF instruction that Node-RED fetches and executes.
// Field names match the on-prem adapter payload (roomCode, classKey, action,
// eventId, bookingId, source) so the existing control flow can be reused.
type Command struct {
	CommandID string `json:"commandId"`
	EventID   string `json:"eventId"`
	Action    string `json:"action"` // on | off
	Reason    string `json:"reason"` // prestart | checkout
	RoomCode  string `json:"roomCode"`
	ClassKey  string `json:"classKey"`
	BookingID string `json:"bookingId"`
	Source    string `json:"source"`
	// Trigger tells what caused the command: "schedule" (T-15 ON), "kiosk_button"
	// (checkout on the tablet) or "auto_off" (room left on after the class ended).
	Trigger             string  `json:"trigger"`
	StartsAt            *string `json:"startsAt"`
	NextBookingStartsAt *string `json:"nextBookingStartsAt"`
	ValidFrom           string  `json:"validFrom"`
	ExpiresAt           string  `json:"expiresAt"`
	Attempts            int     `json:"attempts"`
}

// ClassKey turns "Q1.05.03" into "q10503" (the adapter's room key).
func ClassKey(roomCode string) string {
	return strings.ToLower(strings.ReplaceAll(roomCode, ".", ""))
}

// enqueue stores the command for Node-RED to pull.
func (s *Service) enqueue(ctx context.Context, eventID string, cmd Command) {
	raw, _ := json.Marshal(cmd)
	_, err := s.db.ExecContext(ctx,
		`UPDATE automation_events SET status='queued', payload=?, error=NULL, updated_at=? WHERE event_id=?`,
		string(raw), s.now().UnixMilli(), eventID)
	if err != nil {
		log.Printf("automation: queue %s failed: %v", eventID, err)
		return
	}
	log.Printf("automation: queued %s %s for %s (valid until %s)", cmd.Action, eventID, cmd.RoomCode, cmd.ExpiresAt)
}

// DefaultRequiredDevices are the devices that must report command_sent for an
// ack to count as success. The on-prem backend controls AC and lamps only
// (door commands are not part of that integration).
var DefaultRequiredDevices = []string{"ac", "lamp"}

// Puller serves the queue to the on-prem backend / Node-RED.
type Puller struct {
	db       *sql.DB
	now      func() time.Time
	required []string
}

// NewPuller builds the queue reader. required lists the devices an ack must
// confirm (empty = DefaultRequiredDevices).
func NewPuller(db *sql.DB, required []string) *Puller {
	if len(required) == 0 {
		required = DefaultRequiredDevices
	}
	return &Puller{db: db, now: time.Now, required: required}
}

// IncompleteAckError means success:true was claimed but a required device was
// not reported as command_sent. The ack is rejected (HTTP 422) and the command
// stays queued without consuming an attempt.
type IncompleteAckError struct{ Missing []string }

func (e *IncompleteAckError) Error() string {
	return "ack rejected: devices not reported as command_sent: " + strings.Join(e.Missing, ", ")
}

type queued struct {
	eventID, typ, bookingID, room, payload string
	attempts                               int
}

func (p *Puller) mark(ctx context.Context, eventID, status, note string) {
	p.db.ExecContext(ctx,
		"UPDATE automation_events SET status=?, error=?, updated_at=? WHERE event_id=?",
		status, nullable(note), p.now().UnixMilli(), eventID)
}

// List returns the commands that are due, valid *right now*, for the room
// (or all controlled rooms when room is empty). Every call re-checks the
// booking, so a cancelled or moved class never produces a stale command.
func (p *Puller) List(ctx context.Context, room string, limit int) ([]Command, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	q := `SELECT event_id, event_type, COALESCE(booking_id,''), COALESCE(room_code,''), COALESCE(payload,''), attempts
	      FROM automation_events WHERE status='queued'`
	args := []any{}
	if room != "" {
		q += " AND room_code = ?"
		args = append(args, room)
	}
	q += " ORDER BY id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var items []queued
	for rows.Next() {
		var it queued
		if rows.Scan(&it.eventID, &it.typ, &it.bookingID, &it.room, &it.payload, &it.attempts) == nil {
			items = append(items, it)
		}
	}
	rows.Close()

	now := p.now()
	out := []Command{}
	for _, it := range items {
		var cmd Command
		if json.Unmarshal([]byte(it.payload), &cmd) != nil {
			p.mark(ctx, it.eventID, "failed", "unreadable command")
			continue
		}
		cmd.Attempts = it.attempts
		if exp, err := time.Parse(time.RFC3339, cmd.ExpiresAt); err == nil && !now.Before(exp) {
			p.mark(ctx, it.eventID, "expired", "not executed before it expired")
			continue
		}
		if from, err := time.Parse(time.RFC3339, cmd.ValidFrom); err == nil && now.Before(from) {
			continue
		}
		if reason := p.staleReason(ctx, cmd, now); reason != "" {
			status := "cancelled"
			if cmd.Action == "off" {
				status = "rejected_409"
			}
			p.mark(ctx, it.eventID, status, reason)
			log.Printf("automation: command %s dropped: %s", it.eventID, reason)
			continue
		}
		out = append(out, cmd)
	}
	return out, nil
}

// staleReason returns why a queued command must no longer run ("" = still valid).
func (p *Puller) staleReason(ctx context.Context, cmd Command, now time.Time) string {
	switch cmd.Action {
	case "on":
		var status, in, actualIn, actualOut string
		var dateMs int64
		err := p.db.QueryRowContext(ctx,
			`SELECT status, check_in_time, booking_date, COALESCE(actual_check_in_time,''), COALESCE(actual_check_out_time,'')
			 FROM bookings WHERE id = ?`, cmd.BookingID).Scan(&status, &in, &dateMs, &actualIn, &actualOut)
		if err != nil {
			return "booking no longer exists"
		}
		if status != "confirmed" {
			return "booking is " + status
		}
		if actualOut != "" {
			return "booking already checked out"
		}
		if cmd.StartsAt != nil {
			planned, err := time.Parse(time.RFC3339, *cmd.StartsAt)
			if err == nil && planned.In(wib).Format("15:04") != in {
				return fmt.Sprintf("class moved from %s to %s", planned.In(wib).Format("15:04"), in)
			}
		}
	case "off":
		next, err := nextBookingStart(ctx, p.db, cmd.RoomCode, cmd.BookingID, now)
		if err == nil && next != nil && next.Sub(now) <= nextClassGuard {
			return "next class starts within 15 minutes (or is running)"
		}
	}
	return ""
}

// Ack records the executor's result. success=true must confirm every required
// device (see NewPuller) as command_sent, otherwise the ack is rejected with
// IncompleteAckError. A reported failure (success=false) re-queues the command
// until it expires or reaches 5 attempts.
func (p *Puller) Ack(ctx context.Context, commandID string, success bool, devices map[string]string, message string) (string, error) {
	var status string
	var attempts int
	err := p.db.QueryRowContext(ctx,
		"SELECT status, attempts FROM automation_events WHERE event_id=? AND event_type IN ('prestart','checkout','autooff')", commandID).
		Scan(&status, &attempts)
	if err != nil {
		return "", sql.ErrNoRows
	}
	if status == "acked" {
		return "acked", nil // idempotent
	}
	if status != "queued" {
		return status, fmt.Errorf("command is %s and can no longer be acknowledged", status)
	}
	if success {
		var missing []string
		for _, d := range p.required {
			if devices[d] != "command_sent" {
				missing = append(missing, d)
			}
		}
		if len(missing) > 0 {
			return "queued", &IncompleteAckError{Missing: missing}
		}
	}
	detail, _ := json.Marshal(map[string]any{"devices": devices, "message": message})
	now := p.now().UnixMilli()
	switch {
	case success:
		_, err = p.db.ExecContext(ctx,
			"UPDATE automation_events SET status='acked', attempts=?, response=?, error=NULL, updated_at=? WHERE event_id=?",
			attempts+1, string(detail), now, commandID)
		return "acked", err
	case attempts+1 >= 5:
		_, err = p.db.ExecContext(ctx,
			"UPDATE automation_events SET status='failed', attempts=?, response=?, error=?, updated_at=? WHERE event_id=?",
			attempts+1, string(detail), "gave up after 5 failed attempts", now, commandID)
		return "failed", err
	default:
		_, err = p.db.ExecContext(ctx,
			"UPDATE automation_events SET status='queued', attempts=?, response=?, error=?, updated_at=? WHERE event_id=?",
			attempts+1, string(detail), nullable(message), now, commandID)
		return "queued", err
	}
}
