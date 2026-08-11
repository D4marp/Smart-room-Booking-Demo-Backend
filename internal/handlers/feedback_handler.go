package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/realtime"
	"github.com/bookify-rooms/backend/internal/utils"
)

type FeedbackHandler struct {
	db      *sql.DB
	manager *realtime.Manager
}

func NewFeedbackHandler(db *sql.DB, manager *realtime.Manager) *FeedbackHandler {
	return &FeedbackHandler{db: db, manager: manager}
}

// CreateFeedback creates a new feedback for a booking
// POST /api/bookings/:id/feedback
func (h *FeedbackHandler) CreateFeedback(c *gin.Context) {
	bookingID := c.Param("id")
	userID := c.GetString("userID")
	role := c.GetString("role")

	if userID == "" {
		utils.Error(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	var req models.CreateFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// DEBUG: Log the request
	log.Printf("📝 Feedback request - Satisfaction: %s, Complaints: %v, Other: %v", 
		req.SatisfactionLevel, req.ComplaintItems, req.ComplaintOther)

	if req.SatisfactionLevel == string(models.SatisfactionUnsatisfied) {
		complaintOther := ""
		if req.ComplaintOther != nil {
			complaintOther = strings.TrimSpace(*req.ComplaintOther)
		}
		if len(req.ComplaintItems) == 0 && complaintOther == "" {
			utils.Error(c, http.StatusBadRequest, "unsatisfied feedback must include at least one complaint item or other note")
			return
		}
	}

	// Verify booking exists
	var booking models.Booking
	err := h.db.QueryRowContext(context.Background(),
		`SELECT b.id, b.user_id FROM bookings b WHERE b.id = ?`, bookingID).
		Scan(&booking.ID, &booking.UserID)

	if err == sql.ErrNoRows {
		utils.Error(c, http.StatusNotFound, "Booking not found")
		return
	}
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Database error: "+err.Error())
		return
	}

	// Regular users can only submit feedback for their own booking.
	// Kiosk/admin roles may submit feedback after checkout on behalf of the booking.
	isPrivilegedRole := role == "booking" || role == "admin" || role == "superadmin"
	if !isPrivilegedRole && booking.UserID != userID {
		utils.Error(c, http.StatusForbidden, "You can only submit feedback for your own booking")
		return
	}

	// Check if feedback already exists
	var existingFeedback string
	// Check if feedback already exists (for upsert)
	err = h.db.QueryRowContext(context.Background(),
		"SELECT id FROM feedbacks WHERE booking_id = ?", bookingID).
		Scan(&existingFeedback)
	if err != nil && err != sql.ErrNoRows {
		utils.Error(c, http.StatusInternalServerError, "Database error: "+err.Error())
		return
	}

	now := time.Now().UnixMilli()
	complaintItemsJSON, err := json.Marshal(req.ComplaintItems)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid complaint items")
		return
	}

	var complaintOtherValue *string
	if req.ComplaintOther != nil {
		trimmed := strings.TrimSpace(*req.ComplaintOther)
		if trimmed != "" {
			complaintOtherValue = &trimmed
		}
	}

	var feedbackID string
	if existingFeedback != "" {
		// Update existing feedback
		feedbackID = existingFeedback
		_, err = h.db.ExecContext(context.Background(),
			`UPDATE feedbacks SET satisfaction_level=?, reason=?, complaint_items=?, complaint_other=?, created_at=? WHERE id=?`,
			req.SatisfactionLevel, req.Reason, string(complaintItemsJSON), complaintOtherValue, now, feedbackID)
	} else {
		// Insert new feedback
		feedbackID = uuid.New().String()
		_, err = h.db.ExecContext(context.Background(),
			`INSERT INTO feedbacks (id, booking_id, user_id, satisfaction_level, reason, complaint_items, complaint_other, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			feedbackID, bookingID, userID, req.SatisfactionLevel, req.Reason, string(complaintItemsJSON), complaintOtherValue, now)
	}

	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to create feedback: "+err.Error())
		return
	}

	// Auto-complete the booking after feedback submission
	h.db.ExecContext(context.Background(),
		`UPDATE bookings SET status = ? WHERE id = ? AND status != ?`,
		models.StatusCompleted, bookingID, models.StatusCompleted)

	// Broadcast updated bookings (include feedback info)
	h.broadcastBookings()

	utils.SuccessMessage(c, http.StatusCreated, "Feedback submitted successfully", gin.H{
		"id":                  feedbackID,
		"bookingId":           bookingID,
		"userId":              userID,
		"satisfactionLevel":   req.SatisfactionLevel,
		"reason":              req.Reason,
		"complaintItems":      req.ComplaintItems,
		"complaintOther":      complaintOtherValue,
		"createdAt":           now,
	})
}

// GetFeedback retrieves feedback for a specific booking
// GET /api/bookings/:id/feedback
func (h *FeedbackHandler) GetFeedback(c *gin.Context) {
	bookingID := c.Param("id")

	feedback, err := loadFeedbackByBookingID(h.db, bookingID)

	if err == sql.ErrNoRows {
		utils.Success(c, http.StatusOK, nil)
		return
	}
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Database error: "+err.Error())
		return
	}

	utils.Success(c, http.StatusOK, feedback)
}

// ListFeedbacks retrieves all feedbacks (admin only)
// GET /api/feedbacks
func (h *FeedbackHandler) ListFeedbacks(c *gin.Context) {
	page := 1
	limit := 20

	pageParam := c.DefaultQuery("page", "1")
	if p, err := strconv.Atoi(pageParam); err == nil && p > 0 {
		page = p
	}

	limitParam := c.DefaultQuery("limit", "20")
	if l, err := strconv.Atoi(limitParam); err == nil && l > 0 {
		limit = l
	}

	offset := (page - 1) * limit

	rows, err := h.db.QueryContext(context.Background(),
		`SELECT id, booking_id, user_id, satisfaction_level, reason, complaint_items, complaint_other, created_at
		 FROM feedbacks ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset)

	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Database error: "+err.Error())
		return
	}
	defer rows.Close()

	feedbacks := []models.Feedback{}
	for rows.Next() {
		if f, err := scanFeedbackRow(rows); err == nil {
			feedbacks = append(feedbacks, *f)
		}
	}

	// Get total count
	var total int
	h.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM feedbacks").Scan(&total)

	utils.Success(c, http.StatusOK, gin.H{
		"data":       feedbacks,
		"total":      total,
		"page":       page,
		"limit":      limit,
		"totalPages": (total + limit - 1) / limit,
	})
}

// GetSatisfactionStats returns satisfaction statistics (admin only)
// GET /api/feedbacks/stats?year=2025&month=6
func (h *FeedbackHandler) GetSatisfactionStats(c *gin.Context) {
	year := strings.TrimSpace(c.Query("year"))
	month := strings.TrimSpace(c.Query("month"))

	conditions := []string{}
	args := []interface{}{}

	if year != "" {
		if y, err := strconv.Atoi(year); err == nil && y > 0 {
			conditions = append(conditions, "YEAR(FROM_UNIXTIME(created_at / 1000)) = ?")
			args = append(args, y)
		}
	}
	if month != "" {
		if m, err := strconv.Atoi(month); err == nil && m >= 1 && m <= 12 {
			conditions = append(conditions, "MONTH(FROM_UNIXTIME(created_at / 1000)) = ?")
			args = append(args, m)
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	var satisfied int
	var unsatisfied int

	satisfiedQuery := "SELECT COUNT(*) FROM feedbacks"
	unsatisfiedQuery := "SELECT COUNT(*) FROM feedbacks"
	satisfiedArgs := append([]interface{}{}, args...)
	unsatisfiedArgs := append([]interface{}{}, args...)

	if len(conditions) > 0 {
		satisfiedQuery += whereClause + " AND satisfaction_level = 'satisfied'"
		unsatisfiedQuery += whereClause + " AND satisfaction_level = 'unsatisfied'"
	} else {
		satisfiedQuery += " WHERE satisfaction_level = 'satisfied'"
		unsatisfiedQuery += " WHERE satisfaction_level = 'unsatisfied'"
	}

	h.db.QueryRowContext(context.Background(), satisfiedQuery, satisfiedArgs...).Scan(&satisfied)
	h.db.QueryRowContext(context.Background(), unsatisfiedQuery, unsatisfiedArgs...).Scan(&unsatisfied)

	total := satisfied + unsatisfied
	satisfactionRate := 0.0
	if total > 0 {
		satisfactionRate = float64(satisfied) / float64(total) * 100
	}

	utils.Success(c, http.StatusOK, gin.H{
		"satisfied":        satisfied,
		"unsatisfied":      unsatisfied,
		"total":            total,
		"satisfactionRate": satisfactionRate,
	})
}

func (h *FeedbackHandler) broadcastBookings() {
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

// LoadFeedbackForBooking loads feedback for a single booking
func (h *FeedbackHandler) LoadFeedbackForBooking(db *sql.DB, bookingID string) (*models.Feedback, error) {
	feedback, err := loadFeedbackByBookingID(db, bookingID)

	if err == sql.ErrNoRows {
		return nil, nil // No feedback yet
	}
	if err != nil {
		return nil, err
	}
	return feedback, nil
}
