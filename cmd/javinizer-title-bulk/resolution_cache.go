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

// 1000番台は安全判定方式専用。旧v1/v2の「accepted」キャッシュと名前空間を分離する。
const sharedResolutionCacheVersion = 1000 + scrape.TitleDecisionPolicyVersion

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
CREATE TABLE IF NOT EXISTS resolved_catalogs_v2 (
	resolver_version INTEGER NOT NULL,
	db_identity TEXT NOT NULL,
	lookup_key TEXT NOT NULL,
	catalog_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (resolver_version, db_identity, lookup_key)
);
CREATE INDEX IF NOT EXISTS idx_resolved_catalogs_v2_catalog ON resolved_catalogs_v2(catalog_id);
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

func (c *resolutionCache) Get(kind scrape.TitleInputKind, key, dbIdentity string) (string, bool, error) {
	if c == nil || c.db == nil || kind != scrape.TitleInputCatalog {
		return "", false, nil
	}
	key = normalizedResolutionCacheKey(key)
	dbIdentity = strings.TrimSpace(dbIdentity)
	if key == "" || dbIdentity == "" {
		return "", false, nil
	}
	var id string
	err := c.db.QueryRow(
		"SELECT catalog_id FROM resolved_catalogs_v2 WHERE resolver_version=? AND db_identity=? AND lookup_key=? LIMIT 1",
		sharedResolutionCacheVersion, dbIdentity, key,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query shared resolution cache: %w", err)
	}
	id = strings.TrimSpace(id)
	// A shared confirmed cache is strong evidence, so fail closed if the row
	// does not map back to the exact catalog lookup key that produced it.
	// This prevents stale/corrupt/cross-key rows from authorizing a rename.
	if id == "" || !strings.EqualFold(id, key) {
		return "", false, nil
	}
	return id, true, nil
}

func (c *resolutionCache) Put(kind scrape.TitleInputKind, key, catalogID, dbIdentity string) error {
	if c == nil || c.db == nil || kind != scrape.TitleInputCatalog {
		return nil
	}
	key = normalizedResolutionCacheKey(key)
	catalogID = strings.TrimSpace(catalogID)
	dbIdentity = strings.TrimSpace(dbIdentity)
	if key == "" || catalogID == "" || dbIdentity == "" {
		return nil
	}
	if !strings.EqualFold(catalogID, key) {
		// Catalog cache entries must be self-proving: a lookup for ABC-123
		// can never persist XYZ-999 as its confirmed result.
		return nil
	}
	_, err := c.db.Exec(`
INSERT INTO resolved_catalogs_v2 (resolver_version, db_identity, lookup_key, catalog_id, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(resolver_version, db_identity, lookup_key)
DO UPDATE SET catalog_id=excluded.catalog_id, updated_at=excluded.updated_at
`, sharedResolutionCacheVersion, dbIdentity, key, catalogID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert shared resolution cache: %w", err)
	}
	return nil
}
