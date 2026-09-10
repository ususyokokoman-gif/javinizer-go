//go:build !desktop

package auth

import "net/http"

func isDesktopLocalRequest(_ *http.Request) bool {
	return false
}
