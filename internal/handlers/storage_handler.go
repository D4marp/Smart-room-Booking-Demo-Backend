package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/utils"
)

type StorageHandler struct {
	db         *sql.DB
	uploadsDir string
	baseURL    string
}

func NewStorageHandler(db *sql.DB, uploadsDir, baseURL string) *StorageHandler {
	os.MkdirAll(uploadsDir, 0755)
	return &StorageHandler{db: db, uploadsDir: uploadsDir, baseURL: baseURL}
}

func (h *StorageHandler) UploadRoomImage(c *gin.Context) {
	roomID := c.Param("id")

	var exists bool
	h.db.QueryRowContext(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM rooms WHERE id = ?)", roomID).Scan(&exists)
	if !exists {
		utils.Error(c, http.StatusNotFound, "room not found")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "failed to read image: "+err.Error())
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	if !allowedExts[ext] {
		utils.Error(c, http.StatusBadRequest, "only jpg, jpeg, png, webp are allowed")
		return
	}

	roomDir := filepath.Join(h.uploadsDir, "rooms", roomID)
	if err := os.MkdirAll(roomDir, 0755); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to create directory")
		return
	}

	filename := fmt.Sprintf("%d_%s%s", time.Now().UnixMilli(), uuid.New().String()[:8], ext)
	destPath := filepath.Join(roomDir, filename)

	dst, err := os.Create(destPath)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to save image")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to write image")
		return
	}

	imageURL := fmt.Sprintf("%s/uploads/rooms/%s/%s", h.baseURL, roomID, filename)

	// Read current image_urls JSON, append new URL, write back
	var imageURLsJSON string
	h.db.QueryRowContext(context.Background(),
		"SELECT image_urls FROM rooms WHERE id = ?", roomID).Scan(&imageURLsJSON)

	var imageURLs []string
	json.Unmarshal([]byte(imageURLsJSON), &imageURLs)
	imageURLs = append(imageURLs, imageURL)
	newJSON, _ := json.Marshal(imageURLs)

	_, err = h.db.ExecContext(context.Background(),
		"UPDATE rooms SET image_urls = ?, updated_at = ? WHERE id = ?",
		string(newJSON), time.Now().UnixMilli(), roomID,
	)
	if err != nil {
		os.Remove(destPath)
		utils.Error(c, http.StatusInternalServerError, "failed to update room")
		return
	}

	utils.Success(c, http.StatusCreated, gin.H{
		"imageUrl": imageURL,
		"filename": filename,
	})
}

func (h *StorageHandler) DeleteRoomImage(c *gin.Context) {
	roomID := c.Param("id")

	var req struct {
		ImageURL string `json:"imageUrl" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// Read current image_urls, remove target URL, write back
	var imageURLsJSON string
	h.db.QueryRowContext(context.Background(),
		"SELECT image_urls FROM rooms WHERE id = ?", roomID).Scan(&imageURLsJSON)

	var imageURLs []string
	json.Unmarshal([]byte(imageURLsJSON), &imageURLs)

	filtered := make([]string, 0, len(imageURLs))
	for _, u := range imageURLs {
		if u != req.ImageURL {
			filtered = append(filtered, u)
		}
	}
	newJSON, _ := json.Marshal(filtered)

	h.db.ExecContext(context.Background(),
		"UPDATE rooms SET image_urls = ?, updated_at = ? WHERE id = ?",
		string(newJSON), time.Now().UnixMilli(), roomID,
	)

	parts := strings.Split(req.ImageURL, "/uploads/")
	if len(parts) > 1 {
		filePath := filepath.Join(h.uploadsDir, parts[1])
		os.Remove(filePath)
	}

	utils.SuccessMessage(c, http.StatusOK, "image deleted", nil)
}
