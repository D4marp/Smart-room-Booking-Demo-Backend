package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/realtime"
	"github.com/bookify-rooms/backend/internal/utils"
)

type RoomHandler struct {
	db      *sql.DB
	manager *realtime.Manager
}

func NewRoomHandler(db *sql.DB, manager *realtime.Manager) *RoomHandler {
	return &RoomHandler{db: db, manager: manager}
}

func (h *RoomHandler) broadcastRooms() {
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT id, name, description, location, city, room_class, image_urls, amenities,
		        has_ac, is_available, max_guests, contact_number, floor, building,
		        created_at, updated_at
		 FROM rooms ORDER BY created_at DESC`)
	if err != nil {
		return
	}
	defer rows.Close()

	rooms := []models.Room{}
	for rows.Next() {
		var r models.Room
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.Location, &r.City, &r.RoomClass,
			&r.ImageURLs, &r.Amenities, &r.HasAC, &r.IsAvailable,
			&r.MaxGuests, &r.ContactNumber, &r.Floor, &r.Building,
			&r.CreatedAt, &r.UpdatedAt,
		); err == nil {
			rooms = append(rooms, r)
		}
	}
	h.manager.Rooms.Broadcast(rooms)
}

func (h *RoomHandler) ListRooms(c *gin.Context) {
	var filter models.RoomFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	query := `SELECT id, name, description, location, city, room_class, image_urls, amenities,
	                 has_ac, is_available, max_guests, contact_number, floor, building,
	                 created_at, updated_at
	          FROM rooms WHERE 1=1`
	args := []interface{}{}

	if filter.City != "" {
		query += " AND city = ?"
		args = append(args, filter.City)
	}
	if filter.RoomClass != "" {
		query += " AND room_class = ?"
		args = append(args, string(filter.RoomClass))
	}
	if filter.HasAC != nil {
		query += " AND has_ac = ?"
		args = append(args, *filter.HasAC)
	}
	if filter.MinGuests != nil {
		query += " AND max_guests >= ?"
		args = append(args, *filter.MinGuests)
	}
	if filter.Available != nil {
		query += " AND is_available = ?"
		args = append(args, *filter.Available)
	}
	if filter.Search != "" {
		s := "%" + strings.ToLower(filter.Search) + "%"
		query += " AND (LOWER(name) LIKE ? OR LOWER(location) LIKE ? OR LOWER(city) LIKE ?)"
		args = append(args, s, s, s)
	}

	query += " ORDER BY created_at DESC"

	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch rooms")
		return
	}
	defer rows.Close()

	rooms := []models.Room{}
	for rows.Next() {
		var r models.Room
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.Location, &r.City, &r.RoomClass,
			&r.ImageURLs, &r.Amenities, &r.HasAC, &r.IsAvailable,
			&r.MaxGuests, &r.ContactNumber, &r.Floor, &r.Building,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			continue
		}
		rooms = append(rooms, r)
	}

	utils.Success(c, http.StatusOK, rooms)
}

func (h *RoomHandler) GetRoom(c *gin.Context) {
	id := c.Param("id")

	var r models.Room
	err := h.db.QueryRowContext(context.Background(),
		`SELECT id, name, description, location, city, room_class, image_urls, amenities,
		        has_ac, is_available, max_guests, contact_number, floor, building,
		        created_at, updated_at
		 FROM rooms WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.Location, &r.City, &r.RoomClass,
			&r.ImageURLs, &r.Amenities, &r.HasAC, &r.IsAvailable,
			&r.MaxGuests, &r.ContactNumber, &r.Floor, &r.Building,
			&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "room not found")
		return
	}

	utils.Success(c, http.StatusOK, r)
}

func (h *RoomHandler) CreateRoom(c *gin.Context) {
	var req models.CreateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if req.Floor == nil || strings.TrimSpace(*req.Floor) == "" {
		utils.Error(c, http.StatusBadRequest, "floor is required")
		return
	}

	if strings.TrimSpace(req.Description) == "" {
		req.Description = "-"
	}
	if strings.TrimSpace(req.City) == "" {
		req.City = "Unknown"
	}
	if strings.TrimSpace(string(req.RoomClass)) == "" {
		req.RoomClass = models.RoomClassMeeting
	}
	if strings.TrimSpace(req.ContactNumber) == "" {
		req.ContactNumber = "-"
	}
	if req.MaxGuests < 1 {
		req.MaxGuests = 1
	}

	if req.Amenities == nil {
		req.Amenities = []string{}
	}

	amenitiesJSON, _ := json.Marshal(req.Amenities)

	now := time.Now().UnixMilli()
	room := models.Room{
		ID:            uuid.New().String(),
		Name:          req.Name,
		Description:   req.Description,
		Location:      req.Location,
		City:          req.City,
		RoomClass:     req.RoomClass,
		ImageURLs:     models.StringSlice{},
		Amenities:     models.StringSlice(req.Amenities),
		HasAC:         req.HasAC,
		IsAvailable:   true,
		MaxGuests:     req.MaxGuests,
		ContactNumber: req.ContactNumber,
		Floor:         req.Floor,
		Building:      req.Building,
		CreatedAt:     now,
	}

	_, err := h.db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, name, description, location, city, room_class,
		                   image_urls, amenities, has_ac, is_available,
		                   max_guests, contact_number, floor, building, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		room.ID, room.Name, room.Description, room.Location, room.City, room.RoomClass,
		"[]", string(amenitiesJSON), room.HasAC, room.IsAvailable,
		room.MaxGuests, room.ContactNumber, room.Floor, room.Building, room.CreatedAt,
	)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to create room")
		return
	}

	go h.broadcastRooms()
	utils.Success(c, http.StatusCreated, room)
}

func (h *RoomHandler) UpdateRoom(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UnixMilli()

	var amenitiesJSON *string
	if req.Amenities != nil {
		b, _ := json.Marshal(req.Amenities)
		s := string(b)
		amenitiesJSON = &s
	}

	_, err := h.db.ExecContext(context.Background(),
		`UPDATE rooms SET
			name           = COALESCE(?, name),
			description    = COALESCE(?, description),
			location       = COALESCE(?, location),
			city           = COALESCE(?, city),
			room_class     = COALESCE(?, room_class),
			amenities      = COALESCE(?, amenities),
			has_ac         = COALESCE(?, has_ac),
			is_available   = 1,
			max_guests     = COALESCE(?, max_guests),
			contact_number = COALESCE(?, contact_number),
			floor          = COALESCE(?, floor),
			building       = COALESCE(?, building),
			updated_at     = ?
		 WHERE id = ?`,
		req.Name, req.Description, req.Location, req.City, req.RoomClass,
		amenitiesJSON, req.HasAC, req.MaxGuests,
		req.ContactNumber, req.Floor, req.Building, now, id,
	)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update room")
		return
	}

	go h.broadcastRooms()
	h.GetRoom(c)
}

func (h *RoomHandler) DeleteRoom(c *gin.Context) {
	id := c.Param("id")

	result, err := h.db.ExecContext(context.Background(),
		"DELETE FROM rooms WHERE id = ?", id)
	affected, _ := result.RowsAffected()
	if err != nil || affected == 0 {
		utils.Error(c, http.StatusNotFound, "room not found")
		return
	}

	go h.broadcastRooms()
	utils.SuccessMessage(c, http.StatusOK, "room deleted", nil)
}
