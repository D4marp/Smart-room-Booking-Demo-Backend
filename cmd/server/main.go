package main

import (
	"context"
	"log"
	"time"

	"github.com/bookify-rooms/backend/internal/automation"
	"github.com/bookify-rooms/backend/internal/config"
	"github.com/bookify-rooms/backend/internal/database"
	"github.com/bookify-rooms/backend/internal/integration"
	"github.com/bookify-rooms/backend/internal/server"
	"github.com/bookify-rooms/backend/internal/utils"
)

func main() {
	cfg := config.Load()

	// Room tablets: keep a kiosk token valid for KIOSK_JWT_EXPIRY after it was issued.
	if d, err := time.ParseDuration(cfg.KioskJWTExpiry); err == nil {
		utils.KioskTokenLifetime = d
	}

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

	if cfg.IntegrationWebhookURL != "" {
		go integration.StartWebhookDispatcher(context.Background(), db,
			cfg.IntegrationWebhookURL, cfg.IntegrationWebhookSecret)
	}

	if cfg.AutomationMode == automation.ModeDryRun || cfg.AutomationMode == automation.ModeLive || cfg.AutomationMode == automation.ModePull {
		svc := automation.New(db, automation.Config{
			Mode:            cfg.AutomationMode,
			BaseURL:         cfg.AutomationBaseURL,
			Secret:          cfg.AutomationHMACSecret,
			RoomCodes:       cfg.AutomationRoomCodes,
			CheckoutSources: cfg.AutomationCheckoutSources,
		})
		go svc.Run(context.Background())
	}

	srv := server.New(cfg, db)
	log.Printf("Server starting on :%s", cfg.Port)
	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
