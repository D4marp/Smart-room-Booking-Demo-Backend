package handlers

import (
	"testing"
	"time"

	"github.com/bookify-rooms/backend/internal/utils"
)

func TestKioskTokenLivesLonger(t *testing.T) {
	h := NewAuthHandler(nil, "secret", "168h", "8760h")
	if got := h.expiryFor("booking"); got != "8760h" {
		t.Fatalf("booking expiry = %s, want 8760h", got)
	}
	if got := h.expiryFor("admin"); got != "168h" {
		t.Fatalf("admin expiry = %s, want 168h", got)
	}
	tok, err := utils.GenerateToken("u1", "booking", "secret", h.expiryFor("booking"))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := utils.ValidateToken(tok, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if left := time.Until(claims.ExpiresAt.Time); left < 360*24*time.Hour {
		t.Fatalf("kiosk token valid only %v", left)
	}
	if NewAuthHandler(nil, "s", "168h", "").expiryFor("booking") != "168h" {
		t.Fatal("empty kiosk expiry must fall back to the normal expiry")
	}
}
