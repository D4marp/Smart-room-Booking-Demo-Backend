package middleware

import (
	"crypto/sha256"
	"crypto/subtle"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/utils"
)

// IntegrationAuth protects machine-to-machine endpoints with a shared API key
// sent in the X-API-Key header. Keys come from INTEGRATION_API_KEYS; with no
// keys configured the integration API is disabled (503) rather than open.
func IntegrationAuth(keys []string) gin.HandlerFunc {
	hashes := make([][32]byte, 0, len(keys))
	for _, k := range keys {
		hashes = append(hashes, sha256.Sum256([]byte(k)))
	}
	return func(c *gin.Context) {
		if len(hashes) == 0 {
			utils.Error(c, 503, "integration API is not configured")
			c.Abort()
			return
		}
		given := sha256.Sum256([]byte(c.GetHeader("X-API-Key")))
		ok := 0
		for _, h := range hashes { // constant-time over every configured key
			ok |= subtle.ConstantTimeCompare(h[:], given[:])
		}
		if ok != 1 {
			utils.Error(c, 401, "invalid or missing API key")
			c.Abort()
			return
		}
		c.Next()
	}
}
