//go:build desktop

package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func TestDesktopLocalNoAuth_ProtectedRouteIPv4AndIPv6(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, remoteAddr := range []string{"127.0.0.1:54321", "[::1]:54321"} {
		t.Run(remoteAddr, func(t *testing.T) {
			rt := core.NewAPIRuntime(&core.APIDeps{})
			r := gin.New()
			r.GET("/protected", requireTokenOrSession(rt), func(c *gin.Context) {
				method, _ := c.Get("auth_method")
				username, _ := c.Get("auth_username")
				c.JSON(http.StatusOK, gin.H{"method": method, "username": username})
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.RemoteAddr = remoteAddr
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var got map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got["method"] != "desktop_local" || got["username"] != "local" {
				t.Fatalf("unexpected desktop auth context: %#v", got)
			}
		})
	}
}

func TestDesktopLocalNoAuth_DoesNotTrustForwardedHeadersOrRemotePeers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt := core.NewAPIRuntime(&core.APIDeps{})
	r := gin.New()
	r.GET("/protected", requireTokenOrSession(rt), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.RemoteAddr = "192.0.2.10:54321"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusNoContent || w.Code == http.StatusOK {
		t.Fatalf("non-loopback peer bypassed auth: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDesktopLocalNoAuth_StatusEntersMainUIWithoutCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt := core.NewAPIRuntime(&core.APIDeps{})
	r := gin.New()
	r.GET("/api/v1/auth/status", getAuthStatus(rt))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got contracts.AuthStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !got.Initialized || !got.Authenticated || got.Username != "local" {
		t.Fatalf("desktop auth status=%#v, want initialized+authenticated local", got)
	}
	if got.SessionID != "" {
		t.Fatalf("desktop local mode unexpectedly created/reported a session: %q", got.SessionID)
	}
}
