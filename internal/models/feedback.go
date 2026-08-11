package models

// SatisfactionLevel defines the satisfaction level of the feedback
type SatisfactionLevel string

const (
	SatisfactionSatisfied   SatisfactionLevel = "satisfied"
	SatisfactionUnsatisfied SatisfactionLevel = "unsatisfied"
)

// Feedback represents service satisfaction feedback from a booking
type Feedback struct {
	ID                string            `json:"id" db:"id"`
	BookingID         string            `json:"bookingId" db:"booking_id"`
	UserID            string            `json:"userId" db:"user_id"`
	SatisfactionLevel SatisfactionLevel `json:"satisfactionLevel" db:"satisfaction_level"`
	Reason            string            `json:"reason" db:"reason"`
	ComplaintItems    []string          `json:"complaintItems" db:"complaint_items"`
	ComplaintOther    *string           `json:"complaintOther" db:"complaint_other"`
	CreatedAt         int64             `json:"createdAt" db:"created_at"`
}

type CreateFeedbackRequest struct {
	SatisfactionLevel string `json:"satisfactionLevel" binding:"required,oneof=satisfied unsatisfied"`
	Reason            string `json:"reason" binding:"required"`
	ComplaintItems    []string `json:"complaintItems"`
	ComplaintOther    *string  `json:"complaintOther"`
}

type FeedbackResponse struct {
	ID                string            `json:"id"`
	BookingID         string            `json:"bookingId"`
	UserID            string            `json:"userId"`
	SatisfactionLevel SatisfactionLevel `json:"satisfactionLevel"`
	Reason            string            `json:"reason"`
	ComplaintItems    []string          `json:"complaintItems"`
	ComplaintOther    *string           `json:"complaintOther"`
	CreatedAt         int64             `json:"createdAt"`
}
