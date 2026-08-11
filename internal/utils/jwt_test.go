package utils

import (
	"testing"
	"time"
)

func TestGenerateAndValidateToken(t *testing.T) {
	token, err := GenerateToken("user-123", "admin", "test-secret", "1h")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := ValidateToken(token, "test-secret")
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Fatalf("expected UserID %q, got %q", "user-123", claims.UserID)
	}
	if claims.Role != "admin" {
		t.Fatalf("expected Role %q, got %q", "admin", claims.Role)
	}
}

func TestGenerateTokenWithInvalidExpiryFallsBackToDefault(t *testing.T) {
	token, err := GenerateToken("user-123", "admin", "test-secret", "not-a-duration")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	claims, err := ValidateToken(token, "test-secret")
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}

	expiresIn := claims.ExpiresAt.Time.Sub(time.Now())
	if expiresIn < 167*time.Hour || expiresIn > 168*time.Hour {
		t.Fatalf("expected ~168h expiry fallback, got %v", expiresIn)
	}
}

func TestValidateTokenWithWrongSecretFails(t *testing.T) {
	token, err := GenerateToken("user-123", "admin", "test-secret", "1h")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	if _, err := ValidateToken(token, "wrong-secret"); err == nil {
		t.Fatal("expected ValidateToken to fail with wrong secret")
	}
}

func TestValidateTokenWithExpiredTokenFails(t *testing.T) {
	token, err := GenerateToken("user-123", "admin", "test-secret", "-1h")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	if _, err := ValidateToken(token, "test-secret"); err == nil {
		t.Fatal("expected ValidateToken to fail with expired token")
	}
}

func TestValidateTokenWithGarbageStringFails(t *testing.T) {
	if _, err := ValidateToken("not-a-jwt", "test-secret"); err == nil {
		t.Fatal("expected ValidateToken to fail with malformed token")
	}
}
