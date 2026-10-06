package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrape"
	_ "github.com/mattn/go-sqlite3"
)

const sharedResolutionCacheVersion = 2

type resolutionCache struct {
	db   *sql.DB
	path string
}

func defaultResolutionCachePath() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = filepath.Dir(os.Args[0])
	}
	return filepath.Join(base, "JAVINIZER", "title-resolution-cache.db")
}

func openResolutionCache(path string) (*resolutionCache, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultResolutionCachePath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve cache path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	db, err := sql.Open("sqlite3", abs+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	schema := `
CREATE TABLE IF NOT EXISTS resolved_keys (
	resolver_version INTEGER NOT NULL,
	input_kind TEXT NOT NULL,
	lookup_key TEXT NOT NULL,
	catalog_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (resolver_version, input_kind, lookup_key)
);
CREATE INDEX IF NOT EXISTS idx_resolved_keys_catalog ON resolved_keys(catalog_id);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize cache database: %w", err)
	}
	return &resolutionCache{db: db, path: abs}, nil
}

func (c *resolutionCache) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

func (c *resolutionCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func normalizedResolutionCacheKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func (c *resolutionCache) Get(kind scrape.TitleInputKind, key string) (string, bool, error) {
	if c == nil || c.db == nil {
		return "", false, nil
	}
	key = normalizedResolutionCacheKey(key)
	if key == "" {
		return "", false, nil
	}
	var id string
	err := c.db.QueryRow(
		"SELECT catalog_id FROM resolved_keys WHERE resolver_version=? AND input_kind=? AND lookup_key=? LIMIT 1",
		sharedResolutionCacheVersion, string(kind), key,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query shared resolution cache: %w", err)
	}
	return id, strings.TrimSpace(id) != "", nil
}

func (c *resolutionCache) Put(kind scrape.TitleInputKind, key, catalogID string) error {
	if c == nil || c.db == nil {
		return nil
	}
	key = normalizedResolutionCacheKey(key)
	catalogID = strings.TrimSpace(catalogID)
	if key == "" || catalogID == "" {
		return nil
	}
	_, err := c.db.Exec(`
INSERT INTO resolved_keys (resolver_version, input_kind, lookup_key, catalog_id, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(resolver_version, input_kind, lookup_key)
DO UPDATE SET catalog_id=excluded.catalog_id, updated_at=excluded.updated_at
`, sharedResolutionCacheVersion, string(kind), key, catalogID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert shared resolution cache: %w", err)
	}
	return nil
}
