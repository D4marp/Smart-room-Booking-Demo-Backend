package config

import (
	"strings"
	"testing"
)

func TestNormalizeDatabaseURLHostPortOnly(t *testing.T) {
	t.Setenv("DB_USER", "bookify_user")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "bookify")

	raw := "db-799f0e3a-a387-4c06-ad85-a78344cd9d0c-db-bookify-9652:3306"
	dsn := normalizeDatabaseURL(raw)
	if dsn == "" {
		t.Fatal("expected DSN, got empty string")
	}

	if !strings.Contains(dsn, "bookify_user:secret@tcp(db-799f0e3a-a387-4c06-ad85-a78344cd9d0c-db-bookify-9652:3306)/bookify") {
		t.Fatalf("unexpected DSN: %s", dsn)
	}
}

func TestBuildDatabaseURLStripsPortFromHost(t *testing.T) {
	t.Setenv("DB_HOST", "db-service.internal:3306")
	t.Setenv("DB_PORT", "3306")
	t.Setenv("DB_USER", "bookify_user")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "bookify")

	dsn := buildDatabaseURL()
	if !strings.Contains(dsn, "@tcp(db-service.internal:3306)/bookify") {
		t.Fatalf("unexpected DSN: %s", dsn)
	}
	if strings.Contains(dsn, ":3306:3306") {
		t.Fatalf("DSN contains duplicated port: %s", dsn)
	}
}

func TestNormalizeDatabaseURLMySQLURL(t *testing.T) {
	raw := "mysql://bookify_user:secret@db-host.example.com:3306/bookify"
	dsn := normalizeDatabaseURL(raw)
	if !strings.Contains(dsn, "bookify_user:secret@tcp(db-host.example.com:3306)/bookify") {
		t.Fatalf("unexpected DSN: %s", dsn)
	}
}

func TestBuildDatabaseURLUsesHostFromDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "db-799f0e3a-a387-4c06-ad85-a78344cd9d0c-db-bookify-9652:3306")
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_USER", "bookify_user")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "bookify")

	dsn := buildDatabaseURL()
	if !strings.Contains(dsn, "@tcp(db-799f0e3a-a387-4c06-ad85-a78344cd9d0c-db-bookify-9652:3306)/bookify") {
		t.Fatalf("unexpected DSN: %s", dsn)
	}
}

func TestNormalizeDatabaseURLGoDSN(t *testing.T) {
	raw := "bookify_user:secret@tcp(localhost:3306)/bookify"
	dsn := normalizeDatabaseURL(raw)
	if dsn != raw+"?parseTime=true&charset=utf8mb4" {
		t.Fatalf("unexpected DSN: %s", dsn)
	}
}
