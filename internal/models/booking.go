package models

// BookingStatus defines the lifecycle status of a booking
type BookingStatus string

const (
	// StatusPending: default saat user buat booking, menunggu approval admin
	StatusPending BookingStatus = "pending"
	// StatusConfirmed: admin sudah approve
	StatusConfirmed BookingStatus = "confirmed"
	// StatusRejected: admin tolak booking, disertai rejection_reason
	StatusRejected BookingStatus = "rejected"
	// StatusCancelled: user atau admin cancel booking yang pending/confirmed
	StatusCancelled BookingStatus = "cancelled"
	// StatusCompleted: admin mark booking sebagai selesai
	StatusCompleted BookingStatus = "completed"
)

// Booking represents a room reservation
type Booking struct {
	ID              string        `json:"id" db:"id"`
	UserID          string        `json:"userId" db:"user_id"`
	RoomID          string        `json:"roomId" db:"room_id"`
	BookingDate     int64         `json:"bookingDate" db:"booking_date"`
	CheckInTime     string        `json:"checkInTime" db:"check_in_time"`
	CheckOutTime    string        `json:"checkOutTime" db:"check_out_time"`
	NumberOfGuests  int           `json:"numberOfGuests" db:"number_of_guests"`
	Status          BookingStatus `json:"status" db:"status"`
	Purpose         *string       `json:"purpose" db:"purpose"`
	RejectionReason *string       `json:"rejectionReason" db:"rejection_reason"`
	ApprovedBy      *string       `json:"approvedBy" db:"approved_by"`
	ApprovedAt      *int64        `json:"approvedAt" db:"approved_at"`
	// Denormalized fields (same as Firestore behavior)
	RoomName              *string `json:"roomName" db:"room_name"`
	RoomLocation          *string `json:"roomLocation" db:"room_location"`
	RoomImageURL          *string `json:"roomImageUrl" db:"room_image_url"`
	BookedForName         *string `json:"bookedForName" db:"booked_for_name"`
	BookedForCompany      *string `json:"bookedForCompany" db:"booked_for_company"`
	Pihak1                *string `json:"pihak1" db:"pihak_1"`
	Pihak2                *string `json:"pihak2" db:"pihak_2"`
	PicInput              *string `json:"picInput" db:"pic_input"`
	ActualCheckInTime     *string `json:"actualCheckInTime" db:"actual_check_in_time"`
	ActualCheckOutTime    *string `json:"actualCheckOutTime" db:"actual_check_out_time"`
	ActualDurationMinutes *int    `json:"actualDurationMinutes" db:"actual_duration_minutes"`
	UserName              *string `json:"userName" db:"user_name"`
	UserEmail             *string `json:"userEmail" db:"user_email"`
	CreatedAt             int64   `json:"createdAt" db:"created_at"`
	UpdatedAt             *int64  `json:"updatedAt" db:"updated_at"`
	// Feedback (optional, loaded separately)
	Feedback *Feedback `json:"feedback,omitempty"`
}

type CreateBookingRequest struct {
	RoomID           string  `json:"roomId" binding:"required"`
	BookingDate      int64   `json:"bookingDate" binding:"required"`
	CheckInTime      string  `json:"checkInTime" binding:"required"`
	CheckOutTime     string  `json:"checkOutTime" binding:"required"`
	NumberOfGuests   int     `json:"numberOfGuests" binding:"required,min=1"`
	BookedForName    *string `json:"bookedForName"`
	BookedForCompany *string `json:"bookedForCompany"`
	Pihak1           *string `json:"pihak1"`
	Pihak2           *string `json:"pihak2"`
	ParaPihak        *string `json:"paraPihak"`
	Divisi           *string `json:"divisi"`
	Purpose          *string `json:"purpose"`
	PicInput         *string `json:"picInput"`
}

type ApproveBookingRequest struct {
	Note *string `json:"note"`
}

type CheckInCheckOutRequest struct {
	ActualCheckInTime  *string `json:"actualCheckInTime"`
	ActualCheckOutTime *string `json:"actualCheckOutTime"`
	MarkComplete       bool    `json:"markComplete"`
}

type RejectBookingRequest struct {
	Reason string `json:"reason" binding:"required,min=5"`
}

type BookingFilter struct {
	UserID string        `form:"userId"`
	RoomID string        `form:"roomId"`
	Status BookingStatus `form:"status"`
}

type BookingStatusHistory struct {
	ID         string `json:"id" db:"id"`
	BookingID  string `json:"bookingId" db:"booking_id"`
	FromStatus string `json:"fromStatus" db:"from_status"`
	ToStatus   string `json:"toStatus" db:"to_status"`
	ChangedBy  string `json:"changedBy" db:"changed_by"`
	Note       string `json:"note" db:"note"`
	CreatedAt  int64  `json:"createdAt" db:"created_at"`
}
