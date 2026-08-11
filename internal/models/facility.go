package models

// Facility represents a selectable room facility.
type Facility struct {
	ID        string `json:"id" db:"id"`
	Name      string `json:"name" db:"name"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
	UpdatedAt *int64 `json:"updatedAt" db:"updated_at"`
}
