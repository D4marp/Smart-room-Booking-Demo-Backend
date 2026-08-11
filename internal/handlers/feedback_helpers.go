package handlers

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/bookify-rooms/backend/internal/models"
)

func scanFeedbackRow(scanner interface{ Scan(dest ...interface{}) error }) (*models.Feedback, error) {
	var feedback models.Feedback
	var complaintItemsJSON sql.NullString
	var complaintOther sql.NullString

	err := scanner.Scan(
		&feedback.ID,
		&feedback.BookingID,
		&feedback.UserID,
		&feedback.SatisfactionLevel,
		&feedback.Reason,
		&complaintItemsJSON,
		&complaintOther,
		&feedback.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if complaintItemsJSON.Valid && complaintItemsJSON.String != "" {
		_ = json.Unmarshal([]byte(complaintItemsJSON.String), &feedback.ComplaintItems)
	}
	if complaintOther.Valid && complaintOther.String != "" {
		value := complaintOther.String
		feedback.ComplaintOther = &value
	}

	return &feedback, nil
}

func loadFeedbackByBookingID(db *sql.DB, bookingID string) (*models.Feedback, error) {
	return scanFeedbackRow(db.QueryRowContext(context.Background(),
		`SELECT id, booking_id, user_id, satisfaction_level, reason, complaint_items, complaint_other, created_at
		 FROM feedbacks WHERE booking_id = ?`, bookingID))
}