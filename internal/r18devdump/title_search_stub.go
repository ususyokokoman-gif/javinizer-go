//go:build !sqlite_fts5

package r18devdump

import (
	"context"
	"database/sql"

	"github.com/javinizer/javinizer-go/internal/models"
)

func buildTitleSearchIndex(context.Context, *sql.Tx) error { return nil }

func (s *Store) SearchByTitle(context.Context, string, int) ([]models.DumpTitleMatch, error) {
	return nil, models.ErrDumpTitleSearchUnavailable
}
