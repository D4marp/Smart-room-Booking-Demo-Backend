package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func GenerateToken(userID, role, secret, expiry string) (string, error) {
	duration, err := time.ParseDuration(expiry)
	if err != nil {
		duration = 168 * time.Hour
	}
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// KioskTokenLifetime is how long, counted from issue time, a token of the
// room-tablet role ("booking") stays valid regardless of the (shorter) expiry
// embedded in it. Tablets installed before long-lived kiosk tokens existed
// carry 7-day tokens; this keeps them working without anyone visiting the
// room. The signature is always verified. 0 disables the extension.
var KioskTokenLifetime time.Duration

const kioskRole = "booking"

func ValidateToken(tokenStr, secret string) (*Claims, error) {
	keyFunc := func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	}
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, keyFunc)
	if err != nil && errors.Is(err, jwt.ErrTokenExpired) && KioskTokenLifetime > 0 {
		// Re-parse without time checks (signature is still verified) and apply
		// the kiosk policy ourselves.
		lax, laxErr := jwt.ParseWithClaims(tokenStr, &Claims{}, keyFunc, jwt.WithoutClaimsValidation())
		if laxErr == nil && lax.Valid {
			if c, ok := lax.Claims.(*Claims); ok && c.Role == kioskRole &&
				c.IssuedAt != nil && time.Since(c.IssuedAt.Time) < KioskTokenLifetime {
				return c, nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
