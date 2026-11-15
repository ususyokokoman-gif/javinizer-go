//go:build desktop

package auth

import (
	"net"
	"net/http"
	"strings"
)

// isDesktopLocalRequest reports whether the request is an actual loopback TCP
// peer in a desktop build. It deliberately ignores forwarding headers: only
// RemoteAddr, as populated by net/http from the accepted connection, may grant
// the desktop-local authentication bypass.
func isDesktopLocalRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	host := strings.TrimSpace(r.RemoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
