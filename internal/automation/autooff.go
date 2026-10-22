package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// TickAutoOff switches a room off AutoOffMinutes after a class ends when no OFF
// was issued by a checkout (nobody pressed the tablet button).
//
// Rules:
//   - only confirmed/completed bookings of controlled rooms, today;
//   - the command is created once now >= end + AutoOffMinutes and stays valid
//     for 15 minutes (a longer outage means we do not act on old data);
//   - never when the next class is running or starts within 15 minutes;
//   - never when a checkout OFF already exists for the booking.
//
// It reuses reason "checkout" (the on-prem backend only accepts that reason for
// OFF) and marks the origin in the extra field trigger = "auto_off".
func (s *Service) TickAutoOff(ctx context.Context) {
	if s.cfg.AutoOffMinutes <= 0 || len(s.rooms) == 0 {
		return
	}
	if s.cfg.Mode != ModePull && s.cfg.Mode != ModeDryRun {
		return // push (live) mode has no "auto off" call in the on-prem contract
	}
	now := s.now()
	local := now.In(wib)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, wib)
	delay := time.Duration(s.cfg.AutoOffMinutes) * time.Minute

	rows, err := s.db.QueryContext(ctx,
		`SELECT b.id, r.name, b.check_out_time
		 FROM bookings b JOIN rooms r ON r.id = b.room_id
		 WHERE b.status IN ('confirmed','completed') AND b.booking_date = ?
		   AND r.name IN (`+placeholders(len(s.rooms))+`)`,
		append([]any{midnight.UnixMilli()}, s.roomArgs()...)...)
	if err != nil {
		log.Printf("automation: auto-off query failed: %v", err)
		return
	}
	type cand struct{ id, room, out string }
	var cands []cand
	for rows.Next() {
		var c cand
		if rows.Scan(&c.id, &c.room, &c.out) == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()

	for _, c := range cands {
		clock, err := time.Parse("15:04", c.out)
		if err != nil {
			continue
		}
		end := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, wib)
		due := end.Add(delay)
		if now.Before(due) || !now.Before(due.Add(maxAge)) {
			continue // not yet time, or too late to act
		}
		eventID := fmt.Sprintf("evt-%s-autooff-%s", c.id, end.Format("200601021504"))
		if _, exists := s.load(ctx, eventID); exists {
			continue
		}
		// an OFF already issued for this booking (tablet checkout)?
		var n int
		s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM automation_events
			 WHERE booking_id = ? AND event_type IN ('checkout','autooff')
			   AND status IN ('queued','acked','sent','dry_run')`, c.id).Scan(&n)
		if n > 0 {
			continue
		}
		next, err := nextBookingStart(ctx, s.db, c.room, c.id, now)
		if err != nil {
			log.Printf("automation: auto-off next-class lookup failed: %v", err)
			continue
		}
		if next != nil && next.Sub(now) <= nextClassGuard {
			continue // a class is running / about to start: room stays on (re-checked next tick)
		}
		var nextISO *string
		if next != nil {
			v := iso(*next)
			nextISO = &v
		}
		cmd := Command{
			CommandID: eventID, EventID: eventID, Action: "off", Reason: "checkout", Trigger: "auto_off",
			RoomCode: c.room, ClassKey: ClassKey(c.room), BookingID: c.id, Source: "room_booking",
			NextBookingStartsAt: nextISO,
			ValidFrom:           iso(due), ExpiresAt: iso(due.Add(maxAge)),
		}
		body, _ := json.Marshal(cmd)
		s.insert(ctx, eventID, "autooff", c.id, c.room, nil, "pending", body,
			fmt.Sprintf("room still on %d min after the class ended", s.cfg.AutoOffMinutes))
		s.send(ctx, eventID, "", body, cmd, row{status: "pending"}, func() bool { return true })
		log.Printf("automation: auto-off for %s (%s), class ended at %s", c.room, c.id, end.Format("15:04"))
	}
}
