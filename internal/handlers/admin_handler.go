package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/realtime"
	"github.com/bookify-rooms/backend/internal/utils"
)

type AdminHandler struct {
	db      *sql.DB
	manager *realtime.Manager
}

func NewAdminHandler(db *sql.DB, manager *realtime.Manager) *AdminHandler {
	return &AdminHandler{db: db, manager: manager}
}

func (h *AdminHandler) GetStats(c *gin.Context) {
	ctx := context.Background()

	var totalRooms, availableRooms int
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rooms").Scan(&totalRooms)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rooms WHERE is_available=1").Scan(&availableRooms)

	var totalUsers, adminCount, bookingCount int
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role='user'").Scan(&totalUsers)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role='admin'").Scan(&adminCount)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role='booking'").Scan(&bookingCount)

	var totalB, pendingB, confirmedB, rejectedB, cancelledB, completedB int
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings").Scan(&totalB)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status='pending'").Scan(&pendingB)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status='confirmed'").Scan(&confirmedB)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status='rejected'").Scan(&rejectedB)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status='cancelled'").Scan(&cancelledB)
	h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status='completed'").Scan(&completedB)

	utils.Success(c, http.StatusOK, gin.H{
		"rooms": gin.H{"total": totalRooms, "available": availableRooms},
		"users": gin.H{"total": totalUsers, "admins": adminCount, "booking": bookingCount},
		"bookings": gin.H{"total": totalB, "pending": pendingB, "confirmed": confirmedB,
			"rejected": rejectedB, "cancelled": cancelledB, "completed": completedB},
	})
}

func (h *AdminHandler) GetAdminBookings(c *gin.Context) {
	status := c.Query("status")
	roomID := c.Query("roomId")
	fromDate := c.Query("fromDate")
	toDate := c.Query("toDate")

	query := `SELECT b.id, b.user_id, b.room_id, b.booking_date,
	                 b.check_in_time, b.check_out_time, b.number_of_guests,
	                 b.status, b.purpose, b.rejection_reason, b.approved_by, b.approved_at,
	                 b.room_name, b.room_location, b.room_image_url,
	                 b.booked_for_name, b.booked_for_company,
	                 COALESCE(b.pihak_1, b.para_pihak) AS pihak_1,
	                 COALESCE(b.pihak_2, b.divisi) AS pihak_2,
	                 b.actual_check_in_time, b.actual_check_out_time, b.actual_duration_minutes,
	                 b.user_name, b.user_email, b.created_at, b.updated_at,
	                 reviewer.name AS reviewer_name
	          FROM bookings b
	          LEFT JOIN users reviewer ON b.approved_by = reviewer.id
	          WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND b.status = ?"
		args = append(args, status)
	}
	if roomID != "" {
		query += " AND b.room_id = ?"
		args = append(args, roomID)
	}
	if fromDate != "" {
		query += " AND b.booking_date >= ?"
		args = append(args, fromDate)
	}
	if toDate != "" {
		query += " AND b.booking_date <= ?"
		args = append(args, toDate)
	}

	query += " ORDER BY b.created_at DESC"

	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch bookings")
		return
	}
	defer rows.Close()

	type AdminBookingView struct {
		models.Booking
		ReviewerName *string `json:"reviewerName"`
	}

	bookings := []AdminBookingView{}
	for rows.Next() {
		var b models.Booking
		var reviewerName *string
		if err := rows.Scan(
			&b.ID, &b.UserID, &b.RoomID, &b.BookingDate,
			&b.CheckInTime, &b.CheckOutTime, &b.NumberOfGuests,
			&b.Status, &b.Purpose, &b.RejectionReason, &b.ApprovedBy, &b.ApprovedAt,
			&b.RoomName, &b.RoomLocation, &b.RoomImageURL,
			&b.BookedForName, &b.BookedForCompany, &b.Pihak1, &b.Pihak2,
			&b.ActualCheckInTime, &b.ActualCheckOutTime, &b.ActualDurationMinutes,
			&b.UserName, &b.UserEmail, &b.CreatedAt, &b.UpdatedAt,
			&reviewerName,
		); err == nil {
			if feedback, feedbackErr := h.loadFeedback(b.ID); feedbackErr == nil {
				b.Feedback = feedback
			}
			bookings = append(bookings, AdminBookingView{Booking: b, ReviewerName: reviewerName})
		}
	}
	utils.Success(c, http.StatusOK, bookings)
}

func (h *AdminHandler) loadFeedback(bookingID string) (*models.Feedback, error) {
	feedback, err := loadFeedbackByBookingID(h.db, bookingID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return feedback, nil
}

func (h *AdminHandler) ListUsers(c *gin.Context) {
	roleFilter := c.Query("role")
	search := strings.ToLower(c.Query("search"))

	query := `SELECT id, name, email, profile_image, city, role, created_at, updated_at
	          FROM users WHERE 1=1`
	args := []interface{}{}

	if roleFilter != "" {
		query += " AND role = ?"
		args = append(args, roleFilter)
	}
	if search != "" {
		s := "%" + search + "%"
		query += " AND (LOWER(name) LIKE ? OR LOWER(email) LIKE ?)"
		args = append(args, s, s)
	}

	query += " ORDER BY created_at DESC"

	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch users")
		return
	}
	defer rows.Close()

	users := []models.UserResponse{}
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.ProfileImage,
			&u.City, &u.Role, &u.CreatedAt, &u.UpdatedAt); err == nil {
			users = append(users, u.ToResponse())
		}
	}
	utils.Success(c, http.StatusOK, users)
}

func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req models.CreateManagedUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if !models.ValidRoles[req.Role] {
		utils.Error(c, http.StatusBadRequest, "invalid role")
		return
	}
	if req.Role == models.RoleSuperAdmin {
		utils.Error(c, http.StatusForbidden, "superadmin role cannot be assigned via API")
		return
	}

	var existingCount int
	err := h.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM users WHERE email = ?", req.Email,
	).Scan(&existingCount)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "database error")
		return
	}
	if existingCount > 0 {
		utils.Error(c, http.StatusConflict, "email already registered")
		return
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to hash password")
		return
	}

	now := time.Now().UnixMilli()
	user := models.User{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Email:        req.Email,
		Password:     hashedPassword,
		ProfileImage: req.ProfileImage,
		City:         req.City,
		Role:         req.Role,
		CreatedAt:    now,
	}

	_, err = h.db.ExecContext(context.Background(),
		`INSERT INTO users (id, name, email, password, profile_image, city, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Name, user.Email, user.Password,
		user.ProfileImage, user.City, user.Role, user.CreatedAt,
	)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to create user")
		return
	}

	utils.SuccessMessage(c, http.StatusCreated, "user created", user.ToResponse())
}

func (h *AdminHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	var u models.User
	err := h.db.QueryRowContext(context.Background(),
		`SELECT id, name, email, profile_image, city, role, created_at, updated_at
		 FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.ProfileImage,
			&u.City, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "user not found")
		return
	}
	utils.Success(c, http.StatusOK, u.ToResponse())
}

func (h *AdminHandler) ChangeUserRole(c *gin.Context) {
	targetID := c.Param("id")
	currentUserID := c.GetString("userID")

	if targetID == currentUserID {
		utils.Error(c, http.StatusBadRequest, "cannot change your own role")
		return
	}

	var req models.ChangeRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if !models.ValidRoles[req.Role] {
		utils.Error(c, http.StatusBadRequest, "invalid role")
		return
	}
	if req.Role == models.RoleSuperAdmin {
		utils.Error(c, http.StatusForbidden, "superadmin role cannot be assigned via API")
		return
	}

	now := time.Now().UnixMilli()
	result, err := h.db.ExecContext(context.Background(),
		"UPDATE users SET role=?, updated_at=? WHERE id=?",
		string(req.Role), now, targetID)
	affected, _ := result.RowsAffected()
	if err != nil || affected == 0 {
		utils.Error(c, http.StatusNotFound, "user not found")
		return
	}

	utils.SuccessMessage(c, http.StatusOK, "user role updated",
		gin.H{"userId": targetID, "newRole": req.Role})
}

func (h *AdminHandler) DeleteUser(c *gin.Context) {
	targetID := c.Param("id")
	currentUserID := c.GetString("userID")

	if targetID == currentUserID {
		utils.Error(c, http.StatusBadRequest, "cannot delete your own account via this endpoint")
		return
	}

	var targetRole models.UserRole
	h.db.QueryRowContext(context.Background(),
		"SELECT role FROM users WHERE id = ?", targetID).Scan(&targetRole)
	if targetRole == models.RoleSuperAdmin {
		utils.Error(c, http.StatusForbidden, "cannot delete another superadmin")
		return
	}

	result, err := h.db.ExecContext(context.Background(),
		"DELETE FROM users WHERE id = ?", targetID)
	affected, _ := result.RowsAffected()
	if err != nil || affected == 0 {
		utils.Error(c, http.StatusNotFound, "user not found")
		return
	}

	utils.SuccessMessage(c, http.StatusOK, "user deleted", nil)
}
