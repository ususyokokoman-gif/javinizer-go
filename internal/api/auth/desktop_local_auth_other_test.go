//go:build !desktop

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func TestNonDesktopLoopbackStillRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt := core.NewAPIRuntime(&core.APIDeps{})
	r := gin.New()
	r.GET("/protected", requireTokenOrSession(rt), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusNoContent || w.Code == http.StatusOK {
		t.Fatalf("non-desktop loopback request unexpectedly bypassed auth: status=%d body=%s", w.Code, w.Body.String())
	}
}
