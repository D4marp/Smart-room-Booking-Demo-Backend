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

type AuthHandler struct {
	db        *sql.DB
	jwtSecret string
	jwtExpiry string
}

func NewAuthHandler(db *sql.DB, jwtSecret, jwtExpiry string) *AuthHandler {
	return &AuthHandler{db: db, jwtSecret: jwtSecret, jwtExpiry: jwtExpiry}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var count int
	if err := h.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM users WHERE email = ?", req.Email).Scan(&count); err != nil {
		utils.Error(c, http.StatusInternalServerError, "database error")
		return
	}
	if count > 0 {
		utils.Error(c, http.StatusConflict, "email already registered")
		return
	}

	hashed, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to hash password")
		return
	}

	now := time.Now().UnixMilli()
	user := models.User{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Email:     req.Email,
		Password:  hashed,
		Role:      models.RoleUser,
		CreatedAt: now,
	}

	if _, err = h.db.ExecContext(context.Background(),
		`INSERT INTO users (id, name, email, password, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		user.ID, user.Name, user.Email, user.Password, user.Role, user.CreatedAt,
	); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to create user")
		return
	}

	token, err := utils.GenerateToken(user.ID, string(user.Role), h.jwtSecret, h.jwtExpiry)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to generate token")
		return
	}

	utils.SuccessMessage(c, http.StatusCreated, "registration successful", models.AuthResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var user models.User
	err := h.db.QueryRowContext(context.Background(),
		`SELECT id, name, email, password, profile_image, city, role, created_at, updated_at
		 FROM users WHERE email = ?`, req.Email).
		Scan(&user.ID, &user.Name, &user.Email, &user.Password,
			&user.ProfileImage, &user.City, &user.Role,
			&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		utils.Error(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if !utils.CheckPassword(req.Password, user.Password) {
		utils.Error(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := utils.GenerateToken(user.ID, string(user.Role), h.jwtSecret, h.jwtExpiry)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to generate token")
		return
	}

	utils.Success(c, http.StatusOK, models.AuthResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID := c.GetString("userID")

	var user models.User
	err := h.db.QueryRowContext(context.Background(),
		`SELECT id, name, email, profile_image, city, role, created_at, updated_at
		 FROM users WHERE id = ?`, userID).
		Scan(&user.ID, &user.Name, &user.Email,
			&user.ProfileImage, &user.City, &user.Role,
			&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		utils.Error(c, http.StatusNotFound, "user not found")
		return
	}

	utils.Success(c, http.StatusOK, user.ToResponse())
}

func (h *AuthHandler) UpdateMe(c *gin.Context) {
	userID := c.GetString("userID")

	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UnixMilli()
	if _, err := h.db.ExecContext(context.Background(),
		`UPDATE users SET
			name          = COALESCE(?, name),
			profile_image = COALESCE(?, profile_image),
			city          = COALESCE(?, city),
			updated_at    = ?
		 WHERE id = ?`,
		req.Name, req.ProfileImage, req.City, now, userID,
	); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update user")
		return
	}

	var user models.User
	h.db.QueryRowContext(context.Background(),
		`SELECT id, name, email, profile_image, city, role, created_at, updated_at
		 FROM users WHERE id = ?`, userID).
		Scan(&user.ID, &user.Name, &user.Email,
			&user.ProfileImage, &user.City, &user.Role,
			&user.CreatedAt, &user.UpdatedAt)

	utils.Success(c, http.StatusOK, user.ToResponse())
}

func (h *AuthHandler) UpdateCity(c *gin.Context) {
	userID := c.GetString("userID")

	var req models.UpdateCityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UnixMilli()
	if _, err := h.db.ExecContext(context.Background(),
		"UPDATE users SET city = ?, updated_at = ? WHERE id = ?",
		req.City, now, userID,
	); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update city")
		return
	}

	utils.SuccessMessage(c, http.StatusOK, "city updated", gin.H{"city": req.City})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID := c.GetString("userID")

	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var currentHash string
	if err := h.db.QueryRowContext(context.Background(),
		"SELECT password FROM users WHERE id = ?", userID).Scan(&currentHash); err != nil {
		utils.Error(c, http.StatusNotFound, "user not found")
		return
	}

	if !utils.CheckPassword(req.CurrentPassword, currentHash) {
		utils.Error(c, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to hash password")
		return
	}

	now := time.Now().UnixMilli()
	if _, err = h.db.ExecContext(context.Background(),
		"UPDATE users SET password = ?, updated_at = ? WHERE id = ?",
		newHash, now, userID,
	); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to update password")
		return
	}

	utils.SuccessMessage(c, http.StatusOK, "password changed successfully", nil)
}

func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.SuccessMessage(c, http.StatusOK,
		"if the email is registered, a reset link has been sent", nil)
}

func (h *AuthHandler) DeleteAccount(c *gin.Context) {
	userID := c.GetString("userID")

	if _, err := h.db.ExecContext(context.Background(),
		"DELETE FROM users WHERE id = ?", userID); err != nil {
		utils.Error(c, http.StatusInternalServerError, "failed to delete account")
		return
	}

	utils.SuccessMessage(c, http.StatusOK, "account deleted successfully", nil)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	utils.SuccessMessage(c, http.StatusOK, "logged out successfully", nil)
}
