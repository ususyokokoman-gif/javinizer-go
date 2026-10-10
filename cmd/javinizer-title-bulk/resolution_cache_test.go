package main

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

func TestResolutionCachePersistsVerifiedCatalogForSameDBGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	first, err := openResolutionCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put(scrape.TitleInputCatalog, "IPX-072", "IPX-072", "db-generation-a"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := openResolutionCache(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	id, ok, err := second.Get(scrape.TitleInputCatalog, "ipx-072", "db-generation-a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || id != "IPX-072" {
		t.Fatalf("id=%q ok=%v, want IPX-072 true", id, ok)
	}
}

func TestResolutionCacheRejectsDifferentDBGeneration(t *testing.T) {
	cache, err := openResolutionCache(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if err := cache.Put(scrape.TitleInputCatalog, "IPX-072", "IPX-072", "db-generation-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(scrape.TitleInputCatalog, "IPX-072", "db-generation-b"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("catalog confirmation leaked across DB generations")
	}
}

func TestResolutionCacheNeverStoresTitleOrOpaqueEvidence(t *testing.T) {
	cache, err := openResolutionCache(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	for _, kind := range []scrape.TitleInputKind{scrape.TitleInputOpaque, scrape.TitleInputTitle} {
		if err := cache.Put(kind, "same-key", "IPX-072", "db-generation-a"); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := cache.Get(kind, "same-key", "db-generation-a"); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Fatalf("%s evidence must not enter shared confirmed cache", kind)
		}
	}
}

func TestResolutionCacheRejectsMismatchedCatalogMapping(t *testing.T) {
	cache, err := openResolutionCache(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	if err := cache.Put(scrape.TitleInputCatalog, "IPX-072", "ABC-999", "db-generation-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(scrape.TitleInputCatalog, "IPX-072", "db-generation-a"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("mismatched catalog mapping was persisted")
	}

	if _, err := cache.db.Exec("INSERT INTO resolved_catalogs_v2(resolver_version,db_identity,lookup_key,catalog_id,updated_at) VALUES(?,?,?,?,?)",
		sharedResolutionCacheVersion, "db-generation-a", "ipx-072", "ABC-999", "corrupt"); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := cache.Get(scrape.TitleInputCatalog, "IPX-072", "db-generation-a"); err != nil {
		t.Fatal(err)
	} else if ok || id != "" {
		t.Fatalf("corrupt cache row became trusted: id=%q ok=%v", id, ok)
	}
}
