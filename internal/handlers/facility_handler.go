package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookify-rooms/backend/internal/models"
	"github.com/bookify-rooms/backend/internal/utils"
)

type FacilityHandler struct {
	db *sql.DB
}

func NewFacilityHandler(db *sql.DB) *FacilityHandler {
	return &FacilityHandler{db: db}
}

// CreateFacilityRequest untuk POST /api/facilities
type CreateFacilityRequest struct {
	Name string `json:"name" binding:"required,min=2,max=100"`
}

// UpdateFacilityRequest untuk PUT /api/facilities/:id
type UpdateFacilityRequest struct {
	Name string `json:"name" binding:"required,min=2,max=100"`
}

func (h *FacilityHandler) ListFacilities(c *gin.Context) {
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT id, name, created_at, updated_at FROM facilities ORDER BY name ASC`)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to fetch facilities")
		return
	}
	defer rows.Close()

	items := []models.Facility{}
	for rows.Next() {
		var f models.Facility
		if err := rows.Scan(&f.ID, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
			continue
		}
		items = append(items, f)
	}

	utils.Success(c, http.StatusOK, items)
}

// CreateFacility tambah fasilitas baru
func (h *FacilityHandler) CreateFacility(c *gin.Context) {
	var req CreateFacilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// Check if facility dengan nama ini sudah ada
	var count int
	err := h.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM facilities WHERE LOWER(name) = LOWER(?)`,
		req.Name).Scan(&count)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to check facility")
		return
	}
	if count > 0 {
		utils.Error(c, http.StatusConflict, "facility dengan nama ini sudah ada")
		return
	}

	now := time.Now().UnixMilli()
	facility := models.Facility{
		ID:        uuid.New().String(),
		Name:      req.Name,
		CreatedAt: now,
	}

	_, err = h.db.ExecContext(context.Background(),
		`INSERT INTO facilities (id, name, created_at) VALUES (?, ?, ?)`,
		facility.ID, facility.Name, facility.CreatedAt)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to create facility")
		return
	}

	utils.Success(c, http.StatusCreated, facility)
}

// UpdateFacility update nama fasilitas
func (h *FacilityHandler) UpdateFacility(c *gin.Context) {
	facilityID := c.Param("id")
	if facilityID == "" {
		utils.Error(c, http.StatusBadRequest, "facility id required")
		return
	}

	var req UpdateFacilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// Check jika nama baru sudah ada (kecuali facility itu sendiri)
	var count int
	err := h.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM facilities WHERE LOWER(name) = LOWER(?) AND id != ?`,
		req.Name, facilityID).Scan(&count)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to check facility")
		return
	}
	if count > 0 {
		utils.Error(c, http.StatusConflict, "facility dengan nama ini sudah ada")
		return
	}

	now := time.Now().UnixMilli()
	result, err := h.db.ExecContext(context.Background(),
		`UPDATE facilities SET name = ?, updated_at = ? WHERE id = ?`,
		req.Name, now, facilityID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update facility")
		return
	}

	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		utils.Error(c, http.StatusNotFound, "facility not found")
		return
	}

	facility := models.Facility{
		ID:        facilityID,
		Name:      req.Name,
		CreatedAt: 0, // tidak perlu fetch
		UpdatedAt: &now,
	}

	utils.Success(c, http.StatusOK, facility)
}

// DeleteFacility hapus fasilitas
func (h *FacilityHandler) DeleteFacility(c *gin.Context) {
	facilityID := c.Param("id")
	if facilityID == "" {
		utils.Error(c, http.StatusBadRequest, "facility id required")
		return
	}

	result, err := h.db.ExecContext(context.Background(),
		`DELETE FROM facilities WHERE id = ?`,
		facilityID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to delete facility")
		return
	}

	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		utils.Error(c, http.StatusNotFound, "facility not found")
		return
	}

	utils.Success(c, http.StatusOK, gin.H{"message": "facility deleted"})
}
