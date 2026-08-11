package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ConnectWithRetry attempts to connect to the database with exponential backoff retry logic.
// This is essential in containerized environments where MySQL might not be ready immediately.
func ConnectWithRetry(dsn string, maxRetries int, initialWaitTime time.Duration) (*sql.DB, error) {
	var lastErr error
	waitTime := initialWaitTime

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("Database connection attempt %d/%d...", attempt, maxRetries)

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			lastErr = err
			log.Printf("Failed to open database (attempt %d): %v", attempt, err)
			if attempt < maxRetries {
				time.Sleep(waitTime)
				waitTime = waitTime * 2
				if waitTime > 30*time.Second {
					waitTime = 30 * time.Second
				}
			}
			continue
		}

		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(30 * time.Minute)
		db.SetConnMaxIdleTime(5 * time.Minute)

		if err := db.Ping(); err != nil {
			lastErr = err
			log.Printf("Failed to ping database (attempt %d): %v, retrying in %v...", attempt, err, waitTime)
			db.Close()
			if attempt < maxRetries {
				time.Sleep(waitTime)
				waitTime = waitTime * 2
				if waitTime > 30*time.Second {
					waitTime = 30 * time.Second
				}
			}
			continue
		}

		log.Printf("✓ Database connected successfully on attempt %d", attempt)
		return db, nil
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, lastErr)
}

// Connect attempts to connect to the database with default retry settings (8 retries, 1s initial wait).
// For production, prefer ConnectWithRetry with custom parameters.
func Connect(dsn string) (*sql.DB, error) {
	// Default retry settings: 8 attempts with exponential backoff starting at 1 second
	// This gives ~4 minutes total wait time in worst case, suitable for most scenarios
	return ConnectWithRetry(dsn, 8, 1*time.Second)
}

func RunMigrations(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename VARCHAR(255) PRIMARY KEY,
			applied_at BIGINT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE filename = ?",
			entry.Name()).Scan(&count)
		if count > 0 {
			continue
		}

		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		// Split on semicolons to handle multiple statements
		statements := strings.Split(string(content), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err = db.Exec(stmt); err != nil {
				return fmt.Errorf("run migration %s: %w", entry.Name(), err)
			}
		}

		db.Exec("INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)",
			entry.Name(), time.Now().UnixMilli())

		fmt.Printf("✓ Migration applied: %s\n", entry.Name())
	}

	if err := ensureBookingPihakColumns(db); err != nil {
		return err
	}

	return nil
}

func ensureBookingPihakColumns(db *sql.DB) error {
	if err := ensureColumn(db, "bookings", "pihak_1",
		"ALTER TABLE bookings ADD COLUMN pihak_1 VARCHAR(255) NULL AFTER booked_for_company"); err != nil {
		return err
	}
	if err := ensureColumn(db, "bookings", "pihak_2",
		"ALTER TABLE bookings ADD COLUMN pihak_2 VARCHAR(255) NULL AFTER pihak_1"); err != nil {
		return err
	}
	if _, err := db.Exec(`
		UPDATE bookings
		SET pihak_1 = COALESCE(pihak_1, para_pihak),
		    pihak_2 = COALESCE(pihak_2, divisi)
		WHERE pihak_1 IS NULL OR pihak_2 IS NULL
	`); err != nil {
		return fmt.Errorf("backfill booking pihak columns: %w", err)
	}
	return nil
}

func ensureColumn(db *sql.DB, tableName, columnName, alterSQL string) error {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		  AND column_name = ?
	`, tableName, columnName).Scan(&count); err != nil {
		return fmt.Errorf("check column %s.%s: %w", tableName, columnName, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("add column %s.%s: %w", tableName, columnName, err)
	}
	return nil
}
