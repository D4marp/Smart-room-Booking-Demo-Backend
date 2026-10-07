package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/bookify-rooms/backend/internal/models"
)

// Event types published to integrators (polling feed + webhook).
const (
	EventBookingCreated     = "booking.created"
	EventBookingConfirmed   = "booking.confirmed"
	EventBookingRejected    = "booking.rejected"
	EventBookingCancelled   = "booking.cancelled"
	EventBookingRescheduled = "booking.rescheduled" // start/end time changed
	EventBookingUpdated     = "booking.updated"     // lecturer/course/etc. changed
	EventBookingCheckedIn   = "booking.checked_in"
	EventBookingCheckedOut  = "booking.checked_out"
	EventBookingCompleted   = "booking.completed"
	EventScheduleReplaced   = "schedule.replaced" // bulk timetable import: re-read /schedules
)

var jakarta = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return loc
	}
	return time.FixedZone("WIB", 7*3600)
}()

// IntegrationBooking is the stable, documented shape of a booking for
// external systems. Times are WIB (+07:00); roomCode is the room's name.
type IntegrationBooking struct {
	BookingID    string   `json:"bookingId"`
	RoomID       string   `json:"roomId"`
	RoomCode     string   `json:"roomCode"`
	Status       string   `json:"status"` // pending|confirmed|rejected|cancelled|completed
	Date         string   `json:"date"`   // YYYY-MM-DD
	StartTime    string   `json:"startTime"`
	EndTime      string   `json:"endTime"`
	StartAt      string   `json:"startAt"` // RFC3339 with +07:00
	EndAt        string   `json:"endAt"`
	Course       string   `json:"course"`
	ClassCode    string   `json:"classCode"`
	Lecturers    []string `json:"lecturers"`
	Participants int      `json:"participants"`
	CheckedInAt  *string  `json:"checkedInAt"`  // RFC3339 or null
	CheckedOutAt *string  `json:"checkedOutAt"` // RFC3339 or null
	UpdatedAt    int64    `json:"updatedAt"`    // epoch ms
}

var trailingClass = regexp.MustCompile(`\s*\([^()]*\)\s*$`)

func splitLecturerList(raw string) []string {
	out := []string{}
	for _, n := range strings.Split(raw, ";") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func atClock(day time.Time, hhmm string) (time.Time, bool) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, jakarta), true
}

func toIntegrationBooking(b models.Booking, roomCode string) IntegrationBooking {
	day := time.UnixMilli(b.BookingDate).In(jakarta)
	out := IntegrationBooking{
		BookingID:    b.ID,
		RoomID:       b.RoomID,
		RoomCode:     roomCode,
		Status:       string(b.Status),
		Date:         day.Format("2006-01-02"),
		StartTime:    b.CheckInTime,
		EndTime:      b.CheckOutTime,
		Lecturers:    []string{},
		Participants: b.NumberOfGuests,
	}
	if start, ok := atClock(day, b.CheckInTime); ok {
		out.StartAt = start.Format(time.RFC3339)
	}
	if end, ok := atClock(day, b.CheckOutTime); ok {
		out.EndAt = end.Format(time.RFC3339)
	}
	if b.Purpose != nil {
		out.Course = strings.TrimSpace(trailingClass.ReplaceAllString(*b.Purpose, ""))
	}
	if b.ClassCode != nil {
		out.ClassCode = *b.ClassCode
	}
	if b.Pihak1 != nil {
		out.Lecturers = splitLecturerList(*b.Pihak1)
	}
	if b.ActualCheckInTime != nil && *b.ActualCheckInTime != "" {
		if t, ok := atClock(day, *b.ActualCheckInTime); ok {
			s := t.Format(time.RFC3339)
			out.CheckedInAt = &s
		}
	}
	if b.ActualCheckOutTime != nil && *b.ActualCheckOutTime != "" {
		if t, ok := atClock(day, *b.ActualCheckOutTime); ok {
			s := t.Format(time.RFC3339)
			out.CheckedOutAt = &s
		}
	}
	if b.UpdatedAt != nil {
		out.UpdatedAt = *b.UpdatedAt
	} else {
		out.UpdatedAt = b.CreatedAt
	}
	return out
}

type dbQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadIntegrationBooking(ctx context.Context, q dbQuerier, bookingID string) (IntegrationBooking, bool) {
	var b models.Booking
	var roomCode sql.NullString
	row := q.QueryRowContext(ctx,
		"SELECT "+bookingCols+", (SELECT name FROM rooms WHERE rooms.id = bookings.room_id) FROM bookings WHERE id = ?",
		bookingID)
	if err := scanBookingWithRoom(row, &b, &roomCode); err != nil {
		return IntegrationBooking{}, false
	}
	return toIntegrationBooking(b, roomCode.String), true
}

func scanBookingWithRoom(row interface{ Scan(dest ...any) error }, b *models.Booking, roomCode *sql.NullString) error {
	return row.Scan(
		&b.ID, &b.UserID, &b.RoomID, &b.BookingDate,
		&b.CheckInTime, &b.CheckOutTime, &b.NumberOfGuests,
		&b.Status, &b.Purpose,
		&b.RejectionReason, &b.ApprovedBy, &b.ApprovedAt,
		&b.RoomName, &b.RoomLocation, &b.RoomImageURL,
		&b.BookedForName, &b.BookedForCompany,
		&b.Pihak1, &b.Pihak2, &b.PicInput, &b.ClassID, &b.ClassCode,
		&b.ActualCheckInTime, &b.ActualCheckOutTime, &b.ActualDurationMinutes,
		&b.UserName, &b.UserEmail,
		&b.CreatedAt, &b.UpdatedAt,
		roomCode,
	)
}

// recordIntegrationEvent appends an event for integrators. `extra` is merged
// into the payload next to the booking snapshot (e.g. previous times, source).
// Failures are logged and never break the user-facing action.
func recordIntegrationEvent(ctx context.Context, q dbQuerier, eventType, bookingID string, extra map[string]any) {
	payload := map[string]any{}
	roomCode := ""
	if bookingID != "" {
		if snap, ok := loadIntegrationBooking(ctx, q, bookingID); ok {
			payload["booking"] = snap
			roomCode = snap.RoomCode
		}
	}
	for k, v := range extra {
		payload[k] = v
	}
	if rc, ok := extra["roomCode"].(string); ok && rc != "" {
		roomCode = rc
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("integration event marshal failed: %v", err)
		return
	}
	now := time.Now().UnixMilli()
	if _, err := q.ExecContext(ctx,
		`INSERT INTO integration_events (event_type, booking_id, room_code, payload, created_at, next_attempt_at)
		 VALUES (?,?,?,?,?,?)`,
		eventType, nullableString(bookingID), nullableString(roomCode), string(raw), now, now); err != nil {
		log.Printf("integration event insert failed (%s): %v", eventType, err)
	}
}
