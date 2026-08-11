package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/utils"
)

func Auth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			utils.Error(c, 401, "unauthorized")
			c.Abort()
			return
		}
		claims, err := utils.ValidateToken(strings.TrimPrefix(header, "Bearer "), jwtSecret)
		if err != nil {
			utils.Error(c, 401, "invalid or expired token")
			c.Abort()
			return
		}
		c.Set("userID", claims.UserID)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequireRole izinkan akses jika role user ada di list roles yang diizinkan.
// Superadmin selalu punya akses ke semua route admin.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString("role")
		// superadmin bypass semua role check kecuali jika role yang diminta
		// secara eksplisit membatasi (misalnya endpoint khusus superadmin saja)
		for _, r := range roles {
			if r == role {
				c.Next()
				return
			}
		}
		utils.Error(c, 403, "forbidden: requires role "+strings.Join(roles, " or "))
		c.Abort()
	}
}
