package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/utils"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(handlers ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.GET("/protected", append(handlers, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"userID": c.GetString("userID"), "role": c.GetString("role")})
	})...)
	return r
}

func TestAuthRejectsMissingHeader(t *testing.T) {
	router := newTestRouter(Auth("secret"))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthRejectsInvalidToken(t *testing.T) {
	router := newTestRouter(Auth("secret"))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthAcceptsValidToken(t *testing.T) {
	token, err := utils.GenerateToken("user-1", "admin", "secret", "1h")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	router := newTestRouter(Auth("secret"))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireRoleAllowsMatchingRole(t *testing.T) {
	router := gin.New()
	router.GET("/admin-only", func(c *gin.Context) {
		c.Set("role", "admin")
		c.Next()
	}, RequireRole("admin", "superadmin"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireRoleRejectsNonMatchingRole(t *testing.T) {
	router := gin.New()
	router.GET("/admin-only", func(c *gin.Context) {
		c.Set("role", "booking")
		c.Next()
	}, RequireRole("admin", "superadmin"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
