package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/utils"
)

const maxFlyerBytes = 8 << 20

type FlyerHandler struct {
	db         *sql.DB
	uploadsDir string
	baseURL    string
}

func NewFlyerHandler(db *sql.DB, uploadsDir, baseURL string) *FlyerHandler {
	os.MkdirAll(filepath.Join(uploadsDir, "flyers"), 0755)
	return &FlyerHandler{db: db, uploadsDir: uploadsDir, baseURL: baseURL}
}

const flyerCols = `id, room_id, title, image_url, sort_order, duration_seconds,
	is_active, start_at, end_at, created_at, updated_at`

func scanFlyer(rows interface{ Scan(dest ...any) error }, f *models.Flyer) error {
	var active int
	if err := rows.Scan(&f.ID, &f.RoomID, &f.Title, &f.ImageURL, &f.SortOrder,
		&f.DurationSeconds, &active, &f.StartAt, &f.EndAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return err
	}
	f.IsActive = active == 1
	return nil
}

func (h *FlyerHandler) queryFlyers(query string, args ...interface{}) ([]models.Flyer, error) {
	rows, err := h.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flyers := []models.Flyer{}
	for rows.Next() {
		var f models.Flyer
		if err := scanFlyer(rows, &f); err == nil {
			flyers = append(flyers, f)
		}
	}
	return flyers, nil
}

// ListActiveFlyers is public: the kiosk calls it to build its carousel.
// Returns flyers that are active, inside their schedule window, and either
// global or assigned to the requested room.
func (h *FlyerHandler) ListActiveFlyers(c *gin.Context) {
	roomID := c.Query("roomId")
	now := time.Now().UnixMilli()

	query := "SELECT " + flyerCols + ` FROM flyers
		WHERE is_active = 1
		  AND (start_at IS NULL OR start_at <= ?)
		  AND (end_at IS NULL OR end_at >= ?)`
	args := []interface{}{now, now}

	if roomID != "" {
		query += " AND (room_id IS NULL OR room_id = ?)"
		args = append(args, roomID)
	} else {
		query += " AND room_id IS NULL"
	}
	query += " ORDER BY sort_order ASC, created_at ASC"

	flyers, err := h.queryFlyers(query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch flyers")
		return
	}
	utils.Success(c, http.StatusOK, flyers)
}

// ListAllFlyers is admin-only: every flyer regardless of status/schedule.
func (h *FlyerHandler) ListAllFlyers(c *gin.Context) {
	query := "SELECT " + flyerCols + " FROM flyers"
	args := []interface{}{}
	if roomID := c.Query("roomId"); roomID != "" {
		query += " WHERE room_id = ? OR room_id IS NULL"
		args = append(args, roomID)
	}
	query += " ORDER BY sort_order ASC, created_at ASC"

	flyers, err := h.queryFlyers(query, args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch flyers")
		return
	}
	utils.Success(c, http.StatusOK, flyers)
}

func parseOptionalInt64(value string) *int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	return &n
}

// CreateFlyer accepts multipart/form-data: image (required), title, roomId
// (empty = all rooms), durationSeconds, sortOrder, startAt, endAt (epoch ms).
func (h *FlyerHandler) CreateFlyer(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFlyerBytes+(1<<20))
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "failed to read image: "+err.Error())
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	if !allowed[ext] {
		utils.Error(c, http.StatusBadRequest, "only jpg, jpeg, png, webp are allowed")
		return
	}

	sniff := make([]byte, 512)
	n, _ := io.ReadFull(file, sniff)
	switch http.DetectContentType(sniff[:n]) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		utils.Error(c, http.StatusBadRequest, "file is not a valid image")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to read image")
		return
	}

	var roomID *string
	if rid := strings.TrimSpace(c.PostForm("roomId")); rid != "" && rid != "all" {
		var exists bool
		h.db.QueryRowContext(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM rooms WHERE id = ?)", rid).Scan(&exists)
		if !exists {
			utils.Error(c, http.StatusNotFound, "room not found")
			return
		}
		roomID = &rid
	}

	duration := 8
	if d, err := strconv.Atoi(c.PostForm("durationSeconds")); err == nil && d >= 3 && d <= 120 {
		duration = d
	}
	sortOrder, _ := strconv.Atoi(c.PostForm("sortOrder"))

	id := uuid.New().String()
	filename := fmt.Sprintf("%d_%s%s", time.Now().UnixMilli(), id[:8], ext)
	destPath := filepath.Join(h.uploadsDir, "flyers", filename)

	dst, err := os.Create(destPath)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to save image")
		return
	}
	if _, err := io.Copy(dst, io.LimitReader(file, maxFlyerBytes)); err != nil {
		dst.Close()
		os.Remove(destPath)
		utils.Error(c, http.StatusInternalServerError, "failed to write image")
		return
	}
	dst.Close()

	flyer := models.Flyer{
		ID:              id,
		RoomID:          roomID,
		Title:           strings.TrimSpace(c.PostForm("title")),
		ImageURL:        fmt.Sprintf("%s/uploads/flyers/%s", h.baseURL, filename),
		SortOrder:       sortOrder,
		DurationSeconds: duration,
		IsActive:        true,
		StartAt:         parseOptionalInt64(c.PostForm("startAt")),
		EndAt:           parseOptionalInt64(c.PostForm("endAt")),
		CreatedAt:       time.Now().UnixMilli(),
	}

	_, err = h.db.ExecContext(context.Background(),
		`INSERT INTO flyers (id, room_id, title, image_url, sort_order, duration_seconds,
		                     is_active, start_at, end_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		flyer.ID, flyer.RoomID, flyer.Title, flyer.ImageURL, flyer.SortOrder,
		flyer.DurationSeconds, 1, flyer.StartAt, flyer.EndAt, flyer.CreatedAt)
	if err != nil {
		os.Remove(destPath)
		utils.Error(c, http.StatusInternalServerError, "failed to create flyer")
		return
	}

	utils.Success(c, http.StatusCreated, flyer)
}

func (h *FlyerHandler) UpdateFlyer(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateFlyerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	set := []string{}
	args := []interface{}{}

	if req.Title != nil {
		set = append(set, "title = ?")
		args = append(args, strings.TrimSpace(*req.Title))
	}
	if req.RoomID != nil {
		rid := strings.TrimSpace(*req.RoomID)
		if rid == "" || rid == "all" {
			set = append(set, "room_id = NULL")
		} else {
			var exists bool
			h.db.QueryRowContext(context.Background(),
				"SELECT EXISTS(SELECT 1 FROM rooms WHERE id = ?)", rid).Scan(&exists)
			if !exists {
				utils.Error(c, http.StatusNotFound, "room not found")
				return
			}
			set = append(set, "room_id = ?")
			args = append(args, rid)
		}
	}
	if req.SortOrder != nil {
		set = append(set, "sort_order = ?")
		args = append(args, *req.SortOrder)
	}
	if req.DurationSeconds != nil {
		if *req.DurationSeconds < 3 || *req.DurationSeconds > 120 {
			utils.Error(c, http.StatusBadRequest, "durationSeconds must be between 3 and 120")
			return
		}
		set = append(set, "duration_seconds = ?")
		args = append(args, *req.DurationSeconds)
	}
	if req.IsActive != nil {
		active := 0
		if *req.IsActive {
			active = 1
		}
		set = append(set, "is_active = ?")
		args = append(args, active)
	}
	if req.StartAt != nil {
		if *req.StartAt <= 0 {
			set = append(set, "start_at = NULL")
		} else {
			set = append(set, "start_at = ?")
			args = append(args, *req.StartAt)
		}
	}
	if req.EndAt != nil {
		if *req.EndAt <= 0 {
			set = append(set, "end_at = NULL")
		} else {
			set = append(set, "end_at = ?")
			args = append(args, *req.EndAt)
		}
	}

	if len(set) == 0 {
		utils.Error(c, http.StatusBadRequest, "no fields to update")
		return
	}

	set = append(set, "updated_at = ?")
	args = append(args, time.Now().UnixMilli(), id)

	result, err := h.db.ExecContext(context.Background(),
		"UPDATE flyers SET "+strings.Join(set, ", ")+" WHERE id = ?", args...)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update flyer")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		var exists bool
		h.db.QueryRowContext(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM flyers WHERE id = ?)", id).Scan(&exists)
		if !exists {
			utils.Error(c, http.StatusNotFound, "flyer not found")
			return
		}
	}

	var f models.Flyer
	if err := scanFlyer(h.db.QueryRowContext(context.Background(),
		"SELECT "+flyerCols+" FROM flyers WHERE id = ?", id), &f); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to load flyer")
		return
	}
	utils.Success(c, http.StatusOK, f)
}

func (h *FlyerHandler) DeleteFlyer(c *gin.Context) {
	id := c.Param("id")

	var imageURL string
	if err := h.db.QueryRowContext(context.Background(),
		"SELECT image_url FROM flyers WHERE id = ?", id).Scan(&imageURL); err != nil {
		utils.Error(c, http.StatusNotFound, "flyer not found")
		return
	}

	if _, err := h.db.ExecContext(context.Background(),
		"DELETE FROM flyers WHERE id = ?", id); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to delete flyer")
		return
	}

	if parts := strings.Split(imageURL, "/uploads/flyers/"); len(parts) == 2 {
		os.Remove(filepath.Join(h.uploadsDir, "flyers", filepath.Base(parts[1])))
	}

	utils.SuccessMessage(c, http.StatusOK, "flyer deleted", nil)
}
