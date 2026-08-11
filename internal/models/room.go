package models

// RoomClass defines the type/class of a room
type RoomClass string

const (
	RoomClassMeeting     RoomClass = "Meeting Room"
	RoomClassConference  RoomClass = "Conference Room"
	RoomClassAuditorium  RoomClass = "Auditorium"
	RoomClassStudy       RoomClass = "Study Room"
	RoomClassTraining    RoomClass = "Training Room"
	RoomClassBoard       RoomClass = "Board Room"
	RoomClassBoardroom   RoomClass = "Boardroom"
	RoomClassOffice      RoomClass = "Office"
	RoomClassClassRoom   RoomClass = "Class Room"
	RoomClassLab         RoomClass = "Lab"
	RoomClassLectureHall RoomClass = "Lecture Hall"
)

// Room represents a bookable room
type Room struct {
	ID            string      `json:"id" db:"id"`
	Name          string      `json:"name" db:"name"`
	Description   string      `json:"description" db:"description"`
	Location      string      `json:"location" db:"location"`
	City          string      `json:"city" db:"city"`
	RoomClass     RoomClass   `json:"roomClass" db:"room_class"`
	ImageURLs     StringSlice `json:"imageUrls" db:"image_urls"`
	Amenities     StringSlice `json:"amenities" db:"amenities"`
	HasAC         bool        `json:"hasAC" db:"has_ac"`
	IsAvailable   bool        `json:"isAvailable" db:"is_available"`
	MaxGuests     int         `json:"maxGuests" db:"max_guests"`
	ContactNumber string      `json:"contactNumber" db:"contact_number"`
	Floor         *string     `json:"floor" db:"floor"`
	Building      *string     `json:"building" db:"building"`
	CreatedAt     int64       `json:"createdAt" db:"created_at"`
	UpdatedAt     *int64      `json:"updatedAt" db:"updated_at"`
}

type CreateRoomRequest struct {
	Name          string    `json:"name" binding:"required,min=2,max=255"`
	Description   string    `json:"description"`
	Location      string    `json:"location" binding:"required"`
	City          string    `json:"city"`
	RoomClass     RoomClass `json:"roomClass"`
	Amenities     []string  `json:"amenities"`
	HasAC         bool      `json:"hasAC"`
	IsAvailable   bool      `json:"isAvailable"`
	MaxGuests     int       `json:"maxGuests"`
	ContactNumber string    `json:"contactNumber"`
	Floor         *string   `json:"floor" binding:"required"`
	Building      *string   `json:"building"`
}

type UpdateRoomRequest struct {
	Name          *string    `json:"name"`
	Description   *string    `json:"description"`
	Location      *string    `json:"location"`
	City          *string    `json:"city"`
	RoomClass     *RoomClass `json:"roomClass"`
	Amenities     []string   `json:"amenities"`
	HasAC         *bool      `json:"hasAC"`
	IsAvailable   *bool      `json:"isAvailable"`
	MaxGuests     *int       `json:"maxGuests"`
	ContactNumber *string    `json:"contactNumber"`
	Floor         *string    `json:"floor"`
	Building      *string    `json:"building"`
}

type RoomFilter struct {
	City      string    `form:"city"`
	RoomClass RoomClass `form:"roomClass"`
	HasAC     *bool     `form:"hasAC"`
	MinGuests *int      `form:"minGuests"`
	Search    string    `form:"search"`
	Available *bool     `form:"available"`
}
