//go:build keepwords

package scrape

import (
	"net/http"
	"testing"
)

func TestShouldUseHeadlessGoogleFallback(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden} {
		if !shouldUseHeadlessGoogleFallback(status) {
			t.Fatalf("status %d should use headless Google fallback", status)
		}
	}

	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		if shouldUseHeadlessGoogleFallback(status) {
			t.Fatalf("status %d should not use headless Google fallback", status)
		}
	}
}
