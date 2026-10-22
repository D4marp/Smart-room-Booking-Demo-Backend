package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func tokenWith(t *testing.T, role, secret string, issued, expires time.Time) string {
	t.Helper()
	claims := Claims{UserID: "u1", Role: role, RegisteredClaims: jwt.RegisteredClaims{
		IssuedAt: jwt.NewNumericDate(issued), ExpiresAt: jwt.NewNumericDate(expires)}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestKioskTokenSurvivesItsShortExpiry(t *testing.T) {
	old := KioskTokenLifetime
	defer func() { KioskTokenLifetime = old }()
	KioskTokenLifetime = 365 * 24 * time.Hour
	now := time.Now()

	// 7-day kiosk token issued 10 days ago: embedded exp is past, but still within a year
	expired := tokenWith(t, "booking", "s", now.Add(-10*24*time.Hour), now.Add(-3*24*time.Hour))
	if _, err := ValidateToken(expired, "s"); err != nil {
		t.Fatalf("kiosk token should still be accepted: %v", err)
	}
	// ...but never with a wrong secret (e.g. the tablet still holds a pre-rotation token)
	if _, err := ValidateToken(expired, "other-secret"); err == nil {
		t.Fatal("wrong signature must be rejected")
	}
	// other roles keep their normal expiry
	adminExpired := tokenWith(t, "admin", "s", now.Add(-10*24*time.Hour), now.Add(-3*24*time.Hour))
	if _, err := ValidateToken(adminExpired, "s"); err == nil {
		t.Fatal("expired admin token must be rejected")
	}
	// kiosk token older than the lifetime is rejected
	ancient := tokenWith(t, "booking", "s", now.Add(-400*24*time.Hour), now.Add(-393*24*time.Hour))
	if _, err := ValidateToken(ancient, "s"); err == nil {
		t.Fatal("kiosk token past its lifetime must be rejected")
	}
	// feature off -> plain expiry applies
	KioskTokenLifetime = 0
	if _, err := ValidateToken(expired, "s"); err == nil {
		t.Fatal("with the extension disabled, an expired kiosk token must be rejected")
	}
}
