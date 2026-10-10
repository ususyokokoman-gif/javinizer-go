//go:build !sqlite_fts5

package r18devdump

import (
	"context"
	"database/sql"

	"github.com/javinizer/javinizer-go/internal/models"
)

func buildTitleSearchIndex(context.Context, *sql.DB) error { return nil }

func EnsureTitleSearchIndex(context.Context, string) error {
	return models.ErrDumpTitleSearchUnavailable
}

func (s *Store) SearchByTitle(context.Context, string, int) ([]models.DumpTitleMatch, error) {
	return nil, models.ErrDumpTitleSearchUnavailable
}

func (s *Store) ExactTitleMatches(context.Context, string) ([]models.DumpTitleMatch, error) {
	return nil, models.ErrDumpTitleSearchUnavailable
}
