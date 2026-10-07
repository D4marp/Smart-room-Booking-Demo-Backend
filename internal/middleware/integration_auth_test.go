package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIntegrationAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(keys []string, header string) int {
		r := gin.New()
		r.GET("/x", IntegrationAuth(keys), func(c *gin.Context) { c.Status(200) })
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if header != "" {
			req.Header.Set("X-API-Key", header)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := run(nil, "anything"); got != 503 {
		t.Errorf("no keys configured: got %d want 503", got)
	}
	if got := run([]string{"k1", "k2"}, ""); got != 401 {
		t.Errorf("missing key: got %d want 401", got)
	}
	if got := run([]string{"k1", "k2"}, "nope"); got != 401 {
		t.Errorf("wrong key: got %d want 401", got)
	}
	if got := run([]string{"k1", "k2"}, "k2"); got != 200 {
		t.Errorf("valid second key: got %d want 200", got)
	}
}
