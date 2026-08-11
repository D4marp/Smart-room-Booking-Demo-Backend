package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCORSRouter(allowedOrigins []string) *gin.Engine {
	r := gin.New()
	r.Use(CORS(allowedOrigins))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestCORSAllowsMatchingOrigin(t *testing.T) {
	router := newCORSRouter([]string{"https://admin.example.com"})
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected allow-origin header to echo origin, got %q", got)
	}
}

func TestCORSOmitsHeaderForUnknownOrigin(t *testing.T) {
	router := newCORSRouter([]string{"https://admin.example.com"})
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow-origin header for unknown origin, got %q", got)
	}
}

func TestCORSWildcardAllowsAnyOrigin(t *testing.T) {
	router := newCORSRouter([]string{"*"})
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard allow-origin, got %q", got)
	}
}

func TestCORSHandlesPreflightRequest(t *testing.T) {
	router := newCORSRouter([]string{"*"})
	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS preflight, got %d", w.Code)
	}
}
