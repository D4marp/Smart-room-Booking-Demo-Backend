package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/realtime"
	"github.com/bookify-rooms/backend/internal/utils"
)

type BookingHandler struct {
	db      *sql.DB
	manager *realtime.Manager
}

func NewBookingHandler(db *sql.DB, manager *realtime.Manager) *BookingHandler {
	return &BookingHandler{db: db, manager: manager}
}

func (h *BookingHandler) broadcastBookings() {
	rows, err := h.db.QueryContext(context.Background(),
		"SELECT "+bookingCols+" FROM bookings ORDER BY created_at DESC")
	if err != nil {
		return
	}
	defer rows.Close()

	bookings := []models.Booking{}
	for rows.Next() {
		var b models.Booking
		if err := scanBooking(rows, &b); err == nil {
			bookings = append(bookings, b)
		}
	}
	h.manager.Bookings.Broadcast(bookings)
}

const bookingCols = `
	id, user_id, room_id, booking_date, check_in_time, check_out_time,
	number_of_guests, status, purpose,
	rejection_reason, approved_by, approved_at,
	room_name, room_location, room_image_url,
	booked_for_name, booked_for_company,
	COALESCE(pihak_1, para_pihak) AS pihak_1,
	COALESCE(pihak_2, divisi) AS pihak_2,
	pic_input,
	actual_check_in_time, actual_check_out_time, actual_duration_minutes,
	user_name, user_email,
	created_at, updated_at
`

func scanBooking(rows interface {
	Scan(dest ...interface{}) error
}, b *models.Booking) error {
	return rows.Scan(
		&b.ID, &b.UserID, &b.RoomID, &b.BookingDate,
		&b.CheckInTime, &b.CheckOutTime, &b.NumberOfGuests,
		&b.Status, &b.Purpose,
		&b.RejectionReason, &b.ApprovedBy, &b.ApprovedAt,
		&b.RoomName, &b.RoomLocation, &b.RoomImageURL,
		&b.BookedForName, &b.BookedForCompany,
		&b.Pihak1, &b.Pihak2, &b.PicInput,
		&b.ActualCheckInTime, &b.ActualCheckOutTime, &b.ActualDurationMinutes,
		&b.UserName, &b.UserEmail,
		&b.CreatedAt, &b.UpdatedAt,
	)
}

func (h *BookingHandler) ListBookings(c *gin.Context) {
	currentUserID := c.GetString("userID")
	role := c.GetString("role")

	var filter models.BookingFilter
	c.ShouldBindQuery(&filter)

	query := "SELECT " + bookingCols + " FROM bookings WHERE 1=1"
	args := []interface{}{}

	if role != "admin" && role != "superadmin" {
		query += " AND user_id = ?"
		args = append(args, currentUserID)
	} else if filter.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, filter.UserID)
	}

	if filter.RoomID != "" {
		query += " AND room_id = ?"
		args = append(args, filter.RoomID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, string(filter.Status))
	}

	query += " ORDER BY created_at DESC"

	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch bookings")
		return
	}
	defer rows.Close()

	bookings := []models.Booking{}
	for rows.Next() {
		var b models.Booking
		if err := scanBooking(rows, &b); err == nil {
			bookings = append(bookings, b)
		}
	}
	utils.Success(c, http.StatusOK, bookings)
}

func (h *BookingHandler) GetPendingBookings(c *gin.Context) {
	rows, err := h.db.QueryContext(context.Background(),
		"SELECT "+bookingCols+" FROM bookings WHERE status = 'pending' ORDER BY created_at ASC")
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch pending bookings")
		return
	}
	defer rows.Close()

	bookings := []models.Booking{}
	for rows.Next() {
		var b models.Booking
		if err := scanBooking(rows, &b); err == nil {
			bookings = append(bookings, b)
		}
	}
	utils.Success(c, http.StatusOK, bookings)
}

func (h *BookingHandler) GetBooking(c *gin.Context) {
	id := c.Param("id")
	currentUserID := c.GetString("userID")
	role := c.GetString("role")

	var b models.Booking
	err := scanBooking(
		h.db.QueryRowContext(context.Background(),
			"SELECT "+bookingCols+" FROM bookings WHERE id = ?", id),
		&b,
	)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}

	if role != "admin" && role != "superadmin" && role != "booking" && b.UserID != currentUserID {
		utils.Error(c, http.StatusForbidden, "access denied")
		return
	}

	// Load feedback if it exists
	feedback, feedbackErr := loadFeedbackByBookingID(h.db, id)

	if feedbackErr == nil {
		b.Feedback = feedback
	}

	utils.Success(c, http.StatusOK, b)
}

func (h *BookingHandler) CreateBooking(c *gin.Context) {
	userID := c.GetString("userID")

	var req models.CreateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var room models.Room
	var imageURLsJSON string
	err := h.db.QueryRowContext(context.Background(),
		`SELECT id, name, location, image_urls, max_guests, is_available
		 FROM rooms WHERE id = ?`, req.RoomID).
		Scan(&room.ID, &room.Name, &room.Location, &imageURLsJSON,
			&room.MaxGuests, &room.IsAvailable)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "room not found")
		return
	}
	room.ImageURLs.Scan(imageURLsJSON)

	if !room.IsAvailable {
		utils.Error(c, http.StatusBadRequest, "room is not available")
		return
	}
	if req.NumberOfGuests > room.MaxGuests {
		utils.Error(c, http.StatusBadRequest, "number of guests exceeds room capacity")
		return
	}

	var conflictCount int
	h.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bookings
		 WHERE room_id = ? AND booking_date = ?
		   AND status IN ('pending', 'confirmed')
		   AND check_in_time < ? AND check_out_time > ?`,
		req.RoomID, req.BookingDate, req.CheckOutTime, req.CheckInTime,
	).Scan(&conflictCount)
	if conflictCount > 0 {
		utils.Error(c, http.StatusConflict, "Attention : Time Slot Is Unavailable or Pending Approval ! ")
		return
	}

	var uName, uEmail string
	h.db.QueryRowContext(context.Background(),
		"SELECT name, email FROM users WHERE id = ?", userID).
		Scan(&uName, &uEmail)

	now := time.Now().UnixMilli()
	roomImageURL := ""
	if len(room.ImageURLs) > 0 {
		roomImageURL = room.ImageURLs[0]
	}

	pihak1 := req.Pihak1
	if pihak1 == nil {
		pihak1 = req.ParaPihak
	}
	pihak2 := req.Pihak2
	if pihak2 == nil {
		pihak2 = req.Divisi
	}

	booking := models.Booking{
		ID:               uuid.New().String(),
		UserID:           userID,
		RoomID:           req.RoomID,
		BookingDate:      req.BookingDate,
		CheckInTime:      req.CheckInTime,
		CheckOutTime:     req.CheckOutTime,
		NumberOfGuests:   req.NumberOfGuests,
		Status:           models.StatusPending,
		Purpose:          req.Purpose,
		RoomName:         &room.Name,
		RoomLocation:     &room.Location,
		RoomImageURL:     &roomImageURL,
		BookedForName:    req.BookedForName,
		BookedForCompany: req.BookedForCompany,
		Pihak1:           pihak1,
		Pihak2:           pihak2,
		PicInput:         req.PicInput,
		UserName:         &uName,
		UserEmail:        &uEmail,
		CreatedAt:        now,
	}

	_, err = h.db.ExecContext(context.Background(),
		`INSERT INTO bookings (id, user_id, room_id, booking_date, check_in_time, check_out_time,
		                      number_of_guests, status, purpose,
		                      room_name, room_location, room_image_url,
		                      booked_for_name, booked_for_company, pihak_1, pihak_2, pic_input,
		                      user_name, user_email, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		booking.ID, booking.UserID, booking.RoomID, booking.BookingDate,
		booking.CheckInTime, booking.CheckOutTime, booking.NumberOfGuests,
		booking.Status, booking.Purpose,
		booking.RoomName, booking.RoomLocation, booking.RoomImageURL,
		booking.BookedForName, booking.BookedForCompany, booking.Pihak1, booking.Pihak2,
		booking.PicInput,
		booking.UserName, booking.UserEmail, booking.CreatedAt,
	)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to create booking")
		return
	}

	h.recordHistory(booking.ID, "", "pending", userID, "booking created")
	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusCreated,
		"booking submitted, waiting for admin approval", booking)
}

func (h *BookingHandler) ApproveBooking(c *gin.Context) {
	id := c.Param("id")
	adminID := c.GetString("userID")

	var req models.ApproveBookingRequest
	c.ShouldBindJSON(&req)

	var currentStatus models.BookingStatus
	err := h.db.QueryRowContext(context.Background(),
		"SELECT status FROM bookings WHERE id = ?", id).Scan(&currentStatus)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}
	if currentStatus != models.StatusPending {
		utils.Error(c, http.StatusBadRequest,
			"only pending bookings can be approved (current: "+string(currentStatus)+")")
		return
	}

	now := time.Now().UnixMilli()
	h.db.ExecContext(context.Background(),
		`UPDATE bookings SET status='confirmed', approved_by=?, approved_at=?, updated_at=?
		 WHERE id = ?`, adminID, now, now, id)

	note := "approved by admin"
	if req.Note != nil {
		note = *req.Note
	}
	h.recordHistory(id, "pending", "confirmed", adminID, note)
	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusOK, "booking approved",
		gin.H{"id": id, "status": "confirmed"})
}

func (h *BookingHandler) RejectBooking(c *gin.Context) {
	id := c.Param("id")
	adminID := c.GetString("userID")

	var req models.RejectBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "rejection reason is required: "+err.Error())
		return
	}

	var currentStatus models.BookingStatus
	err := h.db.QueryRowContext(context.Background(),
		"SELECT status FROM bookings WHERE id = ?", id).Scan(&currentStatus)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}
	if currentStatus != models.StatusPending {
		utils.Error(c, http.StatusBadRequest,
			"only pending bookings can be rejected (current: "+string(currentStatus)+")")
		return
	}

	now := time.Now().UnixMilli()
	h.db.ExecContext(context.Background(),
		`UPDATE bookings SET status='rejected', rejection_reason=?,
		 approved_by=?, approved_at=?, updated_at=? WHERE id=?`,
		req.Reason, adminID, now, now, id)

	h.recordHistory(id, "pending", "rejected", adminID, req.Reason)
	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusOK, "booking rejected",
		gin.H{"id": id, "status": "rejected", "reason": req.Reason})
}

func (h *BookingHandler) CancelBooking(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	role := c.GetString("role")

	var ownerID string
	var status models.BookingStatus
	err := h.db.QueryRowContext(context.Background(),
		"SELECT user_id, status FROM bookings WHERE id = ?", id).
		Scan(&ownerID, &status)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}

	isAdmin := role == "admin" || role == "superadmin"
	if !isAdmin && ownerID != userID {
		utils.Error(c, http.StatusForbidden, "access denied")
		return
	}
	if status != models.StatusConfirmed && status != models.StatusPending {
		utils.Error(c, http.StatusBadRequest,
			"only pending or confirmed bookings can be cancelled")
		return
	}

	now := time.Now().UnixMilli()
	h.db.ExecContext(context.Background(),
		"UPDATE bookings SET status='cancelled', updated_at=? WHERE id=?", now, id)

	h.recordHistory(id, string(status), "cancelled", userID, "cancelled by user/admin")
	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusOK, "booking cancelled",
		gin.H{"id": id, "status": "cancelled"})
}

func (h *BookingHandler) CompleteBooking(c *gin.Context) {
	id := c.Param("id")
	adminID := c.GetString("userID")

	var status models.BookingStatus
	err := h.db.QueryRowContext(context.Background(),
		"SELECT status FROM bookings WHERE id = ?", id).Scan(&status)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}
	if status != models.StatusConfirmed {
		utils.Error(c, http.StatusBadRequest, "only confirmed bookings can be completed")
		return
	}

	now := time.Now().UnixMilli()
	if _, err := h.db.ExecContext(context.Background(),
		"UPDATE bookings SET status='completed', updated_at=? WHERE id=?", now, id); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to complete booking")
		return
	}

	h.recordHistory(id, "confirmed", "completed", adminID, "marked completed by admin")
	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusOK, "booking completed",
		gin.H{"id": id, "status": "completed"})
}

func (h *BookingHandler) UpdateCheckInCheckOut(c *gin.Context) {
	id := c.Param("id")
	currentUserID := c.GetString("userID")
	role := c.GetString("role")

	var req models.CheckInCheckOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.ActualCheckInTime == nil && req.ActualCheckOutTime == nil {
		utils.Error(c, http.StatusBadRequest, "at least one actual time must be provided")
		return
	}

	var ownerID string
	var status models.BookingStatus
	var currentCheckIn sql.NullString
	var currentCheckOut sql.NullString
	var scheduledCheckOutTime string
	err := h.db.QueryRowContext(context.Background(),
		`SELECT user_id, status, actual_check_in_time, actual_check_out_time, check_out_time
		 FROM bookings WHERE id = ?`, id).
		Scan(&ownerID, &status, &currentCheckIn, &currentCheckOut, &scheduledCheckOutTime)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "booking not found")
		return
	}

	isAdmin := role == "admin" || role == "superadmin"
	isBookingRole := role == "booking" // kiosk operator can check-in any booking
	if !isAdmin && !isBookingRole && ownerID != currentUserID {
		utils.Error(c, http.StatusForbidden, "access denied")
		return
	}
	if status == models.StatusRejected || status == models.StatusCancelled {
		utils.Error(c, http.StatusBadRequest, "cannot update actual times for cancelled or rejected booking")
		return
	}
	if req.ActualCheckInTime != nil && currentCheckIn.Valid && currentCheckIn.String != "" {
		utils.Error(c, http.StatusConflict, "actual check-in already recorded")
		return
	}
	if req.ActualCheckOutTime != nil && currentCheckOut.Valid && currentCheckOut.String != "" {
		utils.Error(c, http.StatusConflict, "actual check-out already recorded")
		return
	}

	actualCheckIn := currentCheckIn.String
	actualCheckOut := currentCheckOut.String
	if req.ActualCheckInTime != nil {
		actualCheckIn = *req.ActualCheckInTime
	}
	if req.ActualCheckOutTime != nil {
		actualCheckOut = *req.ActualCheckOutTime
	}

	var actualDurationMinutes sql.NullInt64
	if actualCheckIn != "" && actualCheckOut != "" {
		startMinutes, startErr := parseTimeToMinutes(actualCheckIn)
		endMinutes, endErr := parseTimeToMinutes(actualCheckOut)
		if startErr != nil || endErr != nil {
			utils.Error(c, http.StatusBadRequest, "actual times must use HH:mm format")
			return
		}
		if endMinutes < startMinutes {
			endMinutes += 24 * 60
		}
		actualDurationMinutes = sql.NullInt64{Int64: int64(endMinutes - startMinutes), Valid: true}
	}

	// If actual check-out is earlier than scheduled, update scheduled checkout time
	updatedCheckOutTime := scheduledCheckOutTime
	if actualCheckOut != "" && scheduledCheckOutTime != "" {
		actualOutMinutes, err1 := parseTimeToMinutes(actualCheckOut)
		scheduledOutMinutes, err2 := parseTimeToMinutes(scheduledCheckOutTime)
		if err1 == nil && err2 == nil && actualOutMinutes < scheduledOutMinutes {
			// Actual checkout is earlier, update scheduled time
			updatedCheckOutTime = actualCheckOut
		}
	}

	now := time.Now().UnixMilli()
	_, err = h.db.ExecContext(context.Background(),
		`UPDATE bookings
		 SET actual_check_in_time = ?, actual_check_out_time = ?, actual_duration_minutes = ?, check_out_time = ?, updated_at = ?
		 WHERE id = ?`,
		nullableString(actualCheckIn), nullableString(actualCheckOut), actualDurationMinutes, updatedCheckOutTime, now, id)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update check-in/check-out times")
		return
	}

	// If client requested auto-complete (mobile checkout), and actual check-out provided,
	// and booking is currently confirmed, mark booking as completed.
	if req.MarkComplete && actualCheckOut != "" && status == models.StatusConfirmed {
		now2 := time.Now().UnixMilli()
		_, err2 := h.db.ExecContext(context.Background(),
			"UPDATE bookings SET status='completed', updated_at=? WHERE id = ?", now2, id)
		if err2 == nil {
			h.recordHistory(id, string(status), string(models.StatusCompleted), currentUserID, "auto-completed by user on checkout")
			go h.broadcastBookings()
			utils.SuccessMessage(c, http.StatusOK, "check-in/check-out updated and booking completed", gin.H{
				"id":                    id,
				"actualCheckInTime":     actualCheckIn,
				"actualCheckOutTime":    actualCheckOut,
				"actualDurationMinutes": actualDurationMinutes.Int64,
				"status":                "completed",
			})
			return
		}
	}

	go h.broadcastBookings()
	utils.SuccessMessage(c, http.StatusOK, "check-in/check-out updated", gin.H{
		"id":                    id,
		"actualCheckInTime":     actualCheckIn,
		"actualCheckOutTime":    actualCheckOut,
		"actualDurationMinutes": actualDurationMinutes.Int64,
	})
}

func (h *BookingHandler) GetRoomBookings(c *gin.Context) {
	roomID := c.Param("id")
	dateStr := c.Query("date")

	query := "SELECT " + bookingCols + ` FROM bookings
	          WHERE room_id = ? AND status IN ('pending', 'confirmed')`
	args := []interface{}{roomID}

	if dateStr != "" {
		dateMs, err := strconv.ParseInt(dateStr, 10, 64)
		if err == nil {
			query += " AND booking_date = ?"
			args = append(args, dateMs)
		}
	}
	query += " ORDER BY check_in_time ASC"

	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch room bookings")
		return
	}
	defer rows.Close()

	bookings := []models.Booking{}
	now := time.Now()

	for rows.Next() {
		var b models.Booking
		if err := scanBooking(rows, &b); err == nil {
			// Auto check-in: if scheduled check-in time has passed and no actual check-in yet
			if (b.ActualCheckInTime == nil || *b.ActualCheckInTime == "") && b.CheckInTime != "" {
				bookingDate := time.UnixMilli(b.BookingDate)
				bookingDateOnly := time.Date(bookingDate.Year(), bookingDate.Month(), bookingDate.Day(), 0, 0, 0, 0, bookingDate.Location())
				todayOnly := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

				// Only auto check-in for today's bookings
				if bookingDateOnly.Equal(todayOnly) {
					// Parse scheduled check-in time
					scheduledCheckIn, err := time.Parse("15:04", b.CheckInTime)
					if err == nil {
						// Create datetime with check-in time
						checkInDateTime := time.Date(now.Year(), now.Month(), now.Day(),
							scheduledCheckIn.Hour(), scheduledCheckIn.Minute(), 0, 0, now.Location())

						// If current time >= check-in time, auto-check-in
						if now.After(checkInDateTime) || now.Equal(checkInDateTime) {
							// Auto set actual check-in time to scheduled check-in time
							actualCheckInTime := b.CheckInTime
							updateErr := h.db.QueryRowContext(context.Background(),
								`UPDATE bookings SET actual_check_in_time = ?, updated_at = ?
								 WHERE id = ? AND (actual_check_in_time IS NULL OR actual_check_in_time = '')
								 RETURNING id`,
								actualCheckInTime, time.Now().UnixMilli(), b.ID).Scan(&b.ID)

							if updateErr == nil {
								// Update in-memory booking
								b.ActualCheckInTime = &actualCheckInTime
							}
						}
					}
				}
			}
			bookings = append(bookings, b)
		}
	}
	utils.Success(c, http.StatusOK, bookings)
}

func (h *BookingHandler) recordHistory(bookingID, from, to, changedBy, note string) {
	h.db.ExecContext(context.Background(),
		`INSERT INTO booking_status_history
		 (id, booking_id, from_status, to_status, changed_by, note, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		uuid.New().String(), bookingID, from, to, changedBy, note, time.Now().UnixMilli(),
	)
}

func parseTimeToMinutes(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
