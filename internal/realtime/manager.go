package realtime

// Manager holds all hubs for all channels
type Manager struct {
	Rooms    *Hub // WS /ws/rooms
	Bookings *Hub // WS /ws/bookings
}

func NewManager() *Manager {
	return &Manager{
		Rooms:    NewHub(),
		Bookings: NewHub(),
	}
}
