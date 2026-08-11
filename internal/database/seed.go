package database

import (
	"database/sql"
	"log"
	"time"

	"github.com/bookify-rooms/backend/internal/utils"
	"github.com/google/uuid"
)

func SeedSuperAdmin(db *sql.DB, email, password, name string) error {
	if email == "" || password == "" {
		log.Println("[seed] SUPERADMIN_EMAIL or SUPERADMIN_PASSWORD not set, skipping")
		return nil
	}

	var exists bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = ?)", email).Scan(&exists)
	if exists {
		log.Printf("[seed] superadmin %s already exists, skipping", email)
		return nil
	}

	hashed, err := utils.HashPassword(password)
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	_, err = db.Exec(
		`INSERT INTO users (id, name, email, password, role, created_at)
		 VALUES (?, ?, ?, ?, 'superadmin', ?)`,
		uuid.New().String(), name, email, hashed, now,
	)
	if err != nil {
		return err
	}

	log.Printf("[seed] superadmin %s created successfully", email)
	return nil
}
