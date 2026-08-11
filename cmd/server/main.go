package main

import (
	"log"

	"github.com/bookify-rooms/backend/internal/config"
	"github.com/bookify-rooms/backend/internal/database"
	"github.com/bookify-rooms/backend/internal/server"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := database.RunMigrations(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	if err := database.SeedSuperAdmin(db, cfg.SuperAdminEmail, cfg.SuperAdminPassword, "Super Admin"); err != nil {
		log.Fatalf("Failed to seed superadmin: %v", err)
	}

	srv := server.New(cfg, db)
	log.Printf("Server starting on :%s", cfg.Port)
	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
