package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/utils"
)

// minutesPerSKS is the length of one SKS used to infer a missing end time
// (3 SKS = 150 min, 4 SKS = 200 min — matches the real timetable).
const minutesPerSKS = 50

// ScheduleItem is one weekly timetable row (as found in the faculty's Excel).
type ScheduleItem struct {
	Day          string  `json:"day"`  // Senin..Minggu
	Time         string  `json:"time"` // "07.00 - 10.20"
	Course       string  `json:"course"`
	SKS          float64 `json:"sks"`
	StudyProgram string  `json:"studyProgram"`
	Class        string  `json:"class"` // e.g. 2025L
	Participants int     `json:"participants"`
	Lecturers    string  `json:"lecturers"` // "A; B"
}

type ImportScheduleRequest struct {
	RoomID    string         `json:"roomId" binding:"required"`
	StartDate string         `json:"startDate" binding:"required"` // YYYY-MM-DD
	EndDate   string         `json:"endDate" binding:"required"`   // YYYY-MM-DD
	Items     []ScheduleItem `json:"items" binding:"required,min=1"`
	// Replace removes the room's existing manual bookings from StartDate on
	// and every previously imported class booking before inserting.
	Replace bool `json:"replace"`
}

type plannedBooking struct {
	Date      time.Time
	CheckIn   string
	CheckOut  string
	Item      ScheduleItem
	Purpose   string
	Lecturers string
}

var dayIndex = map[string]time.Weekday{
	"senin": time.Monday, "selasa": time.Tuesday, "rabu": time.Wednesday,
	"kamis": time.Thursday, "jumat": time.Friday, "jum'at": time.Friday,
	"sabtu": time.Saturday, "minggu": time.Sunday,
}

var timePart = regexp.MustCompile(`^(\d{1,2})[.:](\d{2})$`)

func parseClock(s string) (int, error) {
	m := timePart.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	var h, min int
	fmt.Sscanf(m[1], "%d", &h)
	fmt.Sscanf(m[2], "%d", &min)
	if h > 23 || min > 59 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return h*60 + min, nil
}

func formatClock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// parseSlot turns "07.00 - 10.20" into check-in/out strings. When the end time
// is missing or not after the start (e.g. "15.30 - 15.30") it is derived from
// the SKS count.
func parseSlot(raw string, sks float64) (string, string, error) {
	parts := strings.Split(strings.ReplaceAll(raw, "–", "-"), "-")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid time range %q", raw)
	}
	start, err := parseClock(parts[0])
	if err != nil {
		return "", "", err
	}
	end, err := parseClock(parts[1])
	if err != nil {
		return "", "", err
	}
	if end <= start {
		if sks <= 0 {
			return "", "", fmt.Errorf("end time not after start in %q", raw)
		}
		end = start + int(sks*minutesPerSKS)
	}
	if end > 24*60 {
		return "", "", fmt.Errorf("end time past midnight in %q", raw)
	}
	return formatClock(start), formatClock(end), nil
}

func splitLecturers(raw string) string {
	var names []string
	for _, n := range strings.Split(raw, ";") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, "; ")
}

// expandSchedule repeats each weekly row on its weekday between start and end
// (inclusive). Dates are midnight in loc.
func expandSchedule(items []ScheduleItem, start, end time.Time, loc *time.Location) ([]plannedBooking, error) {
	var out []plannedBooking
	for i, it := range items {
		wd, ok := dayIndex[strings.ToLower(strings.TrimSpace(it.Day))]
		if !ok {
			return nil, fmt.Errorf("row %d: unknown day %q", i+1, it.Day)
		}
		in, outT, err := parseSlot(it.Time, it.SKS)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i+1, err)
		}
		course := strings.TrimSpace(it.Course)
		class := strings.TrimSpace(it.Class)
		if course == "" || class == "" {
			return nil, fmt.Errorf("row %d: course and class are required", i+1)
		}
		it.Class = class
		purpose := course + " (" + class + ")"
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			if d.Weekday() != wd {
				continue
			}
			day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
			out = append(out, plannedBooking{
				Date: day, CheckIn: in, CheckOut: outT, Item: it,
				Purpose: purpose, Lecturers: splitLecturers(it.Lecturers),
			})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if !out[a].Date.Equal(out[b].Date) {
			return out[a].Date.Before(out[b].Date)
		}
		return out[a].CheckIn < out[b].CheckIn
	})
	// Reject overlaps inside the room on the same day.
	for i := 1; i < len(out); i++ {
		p, q := out[i-1], out[i]
		if p.Date.Equal(q.Date) && q.CheckIn < p.CheckOut {
			return nil, fmt.Errorf("overlap on %s: %s (%s-%s) vs %s (%s-%s)",
				q.Date.Format("2006-01-02"), p.Purpose, p.CheckIn, p.CheckOut,
				q.Purpose, q.CheckIn, q.CheckOut)
		}
	}
	return out, nil
}

// ImportSchedule creates classes and one confirmed booking per weekly slot
// occurrence, so the kiosk shows them without further admin work.
func (h *BookingHandler) ImportSchedule(c *gin.Context) {
	userID := c.GetString("userID")

	var req ImportScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.FixedZone("WIB", 7*3600)
	}
	start, err1 := time.ParseInLocation("2006-01-02", req.StartDate, loc)
	end, err2 := time.ParseInLocation("2006-01-02", req.EndDate, loc)
	if err1 != nil || err2 != nil || end.Before(start) {
		utils.Error(c, http.StatusBadRequest, "startDate/endDate must be YYYY-MM-DD and endDate >= startDate")
		return
	}
	if end.Sub(start) > 400*24*time.Hour {
		utils.Error(c, http.StatusBadRequest, "date range too long")
		return
	}

	planned, err := expandSchedule(req.Items, start, end, loc)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := context.Background()
	var roomName, roomLocation, imagesJSON string
	err = h.db.QueryRowContext(ctx,
		"SELECT name, location, image_urls FROM rooms WHERE id = ?", req.RoomID).
		Scan(&roomName, &roomLocation, &imagesJSON)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "room not found")
		return
	}
	var room models.Room
	room.ImageURLs.Scan(imagesJSON)
	roomImage := ""
	if len(room.ImageURLs) > 0 {
		roomImage = room.ImageURLs[0]
	}

	var uName, uEmail string
	h.db.QueryRowContext(ctx, "SELECT name, email FROM users WHERE id = ?", userID).
		Scan(&uName, &uEmail)

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()

	// Upsert classes (unique by code + study program).
	classIDs := map[string]string{}
	for _, it := range req.Items {
		code := strings.TrimSpace(it.Class)
		program := strings.TrimSpace(it.StudyProgram)
		key := code + "|" + program
		if _, done := classIDs[key]; done {
			continue
		}
		var id string
		err := tx.QueryRowContext(ctx,
			"SELECT id FROM classes WHERE code = ? AND study_program = ?", code, program).Scan(&id)
		if err != nil {
			id = uuid.New().String()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO classes (id, code, study_program, student_count, created_at)
				 VALUES (?,?,?,?,?)`, id, code, program, it.Participants, now); err != nil {
				utils.Error(c, http.StatusInternalServerError, "failed to save class "+code)
				return
			}
		} else if _, err := tx.ExecContext(ctx,
			"UPDATE classes SET student_count = ?, updated_at = ? WHERE id = ?",
			it.Participants, now, id); err != nil {
			utils.Error(c, http.StatusInternalServerError, "failed to update class "+code)
			return
		}
		classIDs[key] = id
	}

	var deleted int64
	if req.Replace {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM bookings
			 WHERE room_id = ? AND (class_id IS NOT NULL OR booking_date >= ?)`,
			req.RoomID, start.UnixMilli())
		if err != nil {
			utils.Error(c, http.StatusInternalServerError, "failed to clear existing bookings")
			return
		}
		deleted, _ = res.RowsAffected()
	}

	for _, p := range planned {
		classID := classIDs[p.Item.Class+"|"+strings.TrimSpace(p.Item.StudyProgram)]
		guests := p.Item.Participants
		if guests < 1 {
			guests = 1
		}
		bookingID := uuid.New().String()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO bookings (id, user_id, room_id, booking_date, check_in_time, check_out_time,
			                      number_of_guests, status, purpose, approved_by, approved_at,
			                      room_name, room_location, room_image_url,
			                      pihak_1, pihak_2, class_id, class_code,
			                      user_name, user_email, created_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			bookingID, userID, req.RoomID, p.Date.UnixMilli(), p.CheckIn, p.CheckOut,
			guests, models.StatusConfirmed, p.Purpose, userID, now,
			roomName, roomLocation, roomImage,
			nullableString(p.Lecturers), nullableString(strings.TrimSpace(p.Item.StudyProgram)),
			classID, p.Item.Class,
			uName, uEmail, now,
		); err != nil {
			utils.Error(c, http.StatusInternalServerError, "failed to insert booking: "+err.Error())
			return
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO booking_status_history (id, booking_id, from_status, to_status, changed_by, note, created_at)
			 VALUES (?,?,?,?,?,?,?)`,
			uuid.New().String(), bookingID, "", "confirmed", userID, "imported from class schedule", now,
		); err != nil {
			utils.Error(c, http.StatusInternalServerError, "failed to record history")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to commit import")
		return
	}

	recordIntegrationEvent(ctx, h.db, EventScheduleReplaced, "", map[string]any{
		"roomCode":        roomName,
		"from":            req.StartDate,
		"to":              req.EndDate,
		"bookingsCreated": len(planned),
		"bookingsDeleted": deleted,
	})
	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusCreated, "schedule imported", gin.H{
		"classes":         len(classIDs),
		"bookingsCreated": len(planned),
		"bookingsDeleted": deleted,
	})
}

// ListClasses returns every class with its booking count.
func (h *BookingHandler) ListClasses(c *gin.Context) {
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT c.id, c.code, c.study_program, c.student_count, c.created_at, c.updated_at,
		        (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id)
		 FROM classes c ORDER BY c.code ASC`)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to list classes")
		return
	}
	defer rows.Close()

	classes := []models.Class{}
	for rows.Next() {
		var cl models.Class
		if err := rows.Scan(&cl.ID, &cl.Code, &cl.StudyProgram, &cl.StudentCount,
			&cl.CreatedAt, &cl.UpdatedAt, &cl.BookingCount); err == nil {
			classes = append(classes, cl)
		}
	}
	utils.Success(c, http.StatusOK, classes)
}
