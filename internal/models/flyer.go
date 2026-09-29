package models

// Flyer is a poster/advertisement image shown in the kiosk display carousel.
// RoomID nil means the flyer is global (shown on every room's display).
type Flyer struct {
	ID              string  `json:"id"`
	RoomID          *string `json:"roomId"`
	Title           string  `json:"title"`
	ImageURL        string  `json:"imageUrl"`
	SortOrder       int     `json:"sortOrder"`
	DurationSeconds int     `json:"durationSeconds"`
	IsActive        bool    `json:"isActive"`
	StartAt         *int64  `json:"startAt"`
	EndAt           *int64  `json:"endAt"`
	CreatedAt       int64   `json:"createdAt"`
	UpdatedAt       *int64  `json:"updatedAt"`
}

// UpdateFlyerRequest applies only the non-nil fields. RoomID "" makes the
// flyer global; StartAt/EndAt 0 clears the schedule window bound.
type UpdateFlyerRequest struct {
	Title           *string `json:"title"`
	RoomID          *string `json:"roomId"`
	SortOrder       *int    `json:"sortOrder"`
	DurationSeconds *int    `json:"durationSeconds"`
	IsActive        *bool   `json:"isActive"`
	StartAt         *int64  `json:"startAt"`
	EndAt           *int64  `json:"endAt"`
}
