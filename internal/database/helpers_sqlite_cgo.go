//go:build cgo

package database

import (
	"errors"
	"strings"

	"github.com/mattn/go-sqlite3"
)

func isLocked(err error) bool {
	var sqliteErr *sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked
	}
	return lockedErrorText(err)
}

func lockedErrorText(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database table is locked"))
}
