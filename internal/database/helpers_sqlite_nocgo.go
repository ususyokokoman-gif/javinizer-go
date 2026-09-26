//go:build !cgo

package database

import "strings"

// isLocked keeps non-SQLite consumers buildable when CGO is unavailable.
// The mattn/go-sqlite3 driver itself cannot operate without CGO, but utilities
// such as bulk-title do not open SQLite at all. String classification preserves
// the generic fallback without referring to CGO-only sqlite3.Error constants.
func isLocked(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database table is locked"))
}
