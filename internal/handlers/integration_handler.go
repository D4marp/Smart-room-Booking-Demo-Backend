package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/utils"
)

// IntegrationHandler serves the machine-to-machine API used by the building
// automation backend (door unlock, AC/lights). It is read-only: no endpoint
// here can drive a device.
type IntegrationHandler struct {
	db *sql.DB
}

func NewIntegrationHandler(db *sql.DB) *IntegrationHandler {
	return &IntegrationHandler{db: db}
}

const integrationBookingSelect = "SELECT " + bookingCols +
	", (SELECT name FROM rooms WHERE rooms.id = bookings.room_id) AS room_code FROM bookings"

type integrationRoom struct {
	RoomID    string `json:"roomId"`
	Code      string `json:"code"`
	Location  string `json:"location"`
	Capacity  int    `json:"capacity"`
	Available bool   `json:"available"`
}

// ListRooms returns the canonical room codes.
func (h *IntegrationHandler) ListRooms(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(),
		"SELECT id, name, location, max_guests, is_available FROM rooms ORDER BY name")
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to list rooms")
		return
	}
	defer rows.Close()
	out := []integrationRoom{}
	for rows.Next() {
		var r integrationRoom
		if err := rows.Scan(&r.RoomID, &r.Code, &r.Location, &r.Capacity, &r.Available); err == nil {
			out = append(out, r)
		}
	}
	utils.Success(c, http.StatusOK, gin.H{"serverTime": time.Now().In(jakarta).Format(time.RFC3339), "rooms": out})
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, jakarta)
	return t, err == nil
}

// ListSchedules returns bookings in a date range (default: today and tomorrow).
//
//	room=Q1.01.03  date=YYYY-MM-DD | from=..&to=..  status=confirmed|all|a,b  updatedSince=<epoch ms>
func (h *IntegrationHandler) ListSchedules(c *gin.Context) {
	today := time.Now().In(jakarta)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, jakarta)
	from, to := today, today.AddDate(0, 0, 1)

	if d := c.Query("date"); d != "" {
		t, ok := parseDay(d)
		if !ok {
			utils.Error(c, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		from, to = t, t
	} else {
		if f := c.Query("from"); f != "" {
			t, ok := parseDay(f)
			if !ok {
				utils.Error(c, http.StatusBadRequest, "from must be YYYY-MM-DD")
				return
			}
			from = t
		}
		if tt := c.Query("to"); tt != "" {
			t, ok := parseDay(tt)
			if !ok {
				utils.Error(c, http.StatusBadRequest, "to must be YYYY-MM-DD")
				return
			}
			to = t
		} else if c.Query("from") != "" {
			to = from
		}
	}
	if to.Before(from) || to.Sub(from) > 62*24*time.Hour {
		utils.Error(c, http.StatusBadRequest, "range must be 0-62 days and to >= from")
		return
	}

	query := integrationBookingSelect + " WHERE booking_date >= ? AND booking_date <= ?"
	args := []any{from.UnixMilli(), to.UnixMilli()}

	if room := strings.TrimSpace(c.Query("room")); room != "" {
		query += " AND room_id IN (SELECT id FROM rooms WHERE name = ?)"
		args = append(args, room)
	}

	status := strings.TrimSpace(c.DefaultQuery("status", "confirmed"))
	if status != "all" {
		parts := []string{}
		for _, s := range strings.Split(status, ",") {
			switch s = strings.TrimSpace(s); s {
			case "pending", "confirmed", "rejected", "cancelled", "completed":
				parts = append(parts, "?")
				args = append(args, s)
			default:
				utils.Error(c, http.StatusBadRequest, "invalid status: "+s)
				return
			}
		}
		query += " AND status IN (" + strings.Join(parts, ",") + ")"
	}

	if v := c.Query("updatedSince"); v != "" {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			utils.Error(c, http.StatusBadRequest, "updatedSince must be epoch milliseconds")
			return
		}
		query += " AND COALESCE(updated_at, created_at) >= ?"
		args = append(args, ms)
	}
	query += " ORDER BY booking_date ASC, check_in_time ASC LIMIT 2000"

	rows, err := h.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to list schedules")
		return
	}
	defer rows.Close()

	items := []IntegrationBooking{}
	for rows.Next() {
		var b models.Booking
		var code sql.NullString
		if err := scanBookingWithRoom(rows, &b, &code); err == nil {
			items = append(items, toIntegrationBooking(b, code.String))
		}
	}
	utils.Success(c, http.StatusOK, gin.H{
		"serverTime": time.Now().In(jakarta).Format(time.RFC3339),
		"from":       from.Format("2006-01-02"),
		"to":         to.Format("2006-01-02"),
		"schedules":  items,
	})
}

// GetSchedule returns one booking by id.
func (h *IntegrationHandler) GetSchedule(c *gin.Context) {
	b, ok := loadIntegrationBooking(c.Request.Context(), h.db, c.Param("bookingId"))
	if !ok {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}
	utils.Success(c, http.StatusOK, gin.H{"serverTime": time.Now().In(jakarta).Format(time.RFC3339), "schedule": b})
}

type integrationEvent struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurredAt"`
	RoomCode   string          `json:"roomCode"`
	BookingID  string          `json:"bookingId"`
	Data       json.RawMessage `json:"data"`
}

// ListEvents is the pollable change feed. Pass the last seen id as afterId;
// events are returned oldest first, and nextAfterId is the cursor to reuse.
func (h *IntegrationHandler) ListEvents(c *gin.Context) {
	afterID, _ := strconv.ParseInt(c.DefaultQuery("afterId", "0"), 10, 64)
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 500 {
		limit = 100
	}

	query := `SELECT id, event_type, COALESCE(booking_id,''), COALESCE(room_code,''), payload, created_at
	          FROM integration_events WHERE id > ?`
	args := []any{afterID}
	if room := strings.TrimSpace(c.Query("room")); room != "" {
		query += " AND room_code = ?"
		args = append(args, room)
	}
	if t := strings.TrimSpace(c.Query("type")); t != "" {
		parts := []string{}
		for _, v := range strings.Split(t, ",") {
			parts = append(parts, "?")
			args = append(args, strings.TrimSpace(v))
		}
		query += " AND event_type IN (" + strings.Join(parts, ",") + ")"
	}
	query += " ORDER BY id ASC LIMIT ?"
	args = append(args, limit)

	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to list events")
		return
	}
	defer rows.Close()

	events := []integrationEvent{}
	next := afterID
	for rows.Next() {
		var e integrationEvent
		var payload string
		var createdAt int64
		if err := rows.Scan(&e.ID, &e.Type, &e.BookingID, &e.RoomCode, &payload, &createdAt); err != nil {
			continue
		}
		e.Data = json.RawMessage(payload)
		e.OccurredAt = time.UnixMilli(createdAt).In(jakarta).Format(time.RFC3339)
		events = append(events, e)
		next = e.ID
	}
	utils.Success(c, http.StatusOK, gin.H{
		"serverTime":  time.Now().In(jakarta).Format(time.RFC3339),
		"events":      events,
		"nextAfterId": next,
	})
}
