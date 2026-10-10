package main

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

func TestLegacySharedCacheTableCannotConfirmUnderV2Policy(t *testing.T) {
	cache, err := openResolutionCache(t.TempDir() + "/cache.db")
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	if _, err := cache.db.Exec(`
CREATE TABLE IF NOT EXISTS resolved_keys (
	resolver_version INTEGER NOT NULL,
	input_kind TEXT NOT NULL,
	lookup_key TEXT NOT NULL,
	catalog_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (resolver_version, input_kind, lookup_key)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.db.Exec(
		"INSERT INTO resolved_keys(resolver_version,input_kind,lookup_key,catalog_id,updated_at) VALUES(1001,'catalog','abc-123','ABC-123','old')",
	); err != nil {
		t.Fatal(err)
	}

	if id, ok, err := cache.Get(scrape.TitleInputCatalog, "ABC-123", "db-generation-current"); err != nil {
		t.Fatal(err)
	} else if ok || id != "" {
		t.Fatalf("legacy cache bypassed v2 policy: id=%q ok=%v", id, ok)
	}
}
