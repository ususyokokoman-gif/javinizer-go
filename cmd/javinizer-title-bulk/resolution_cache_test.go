package main

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

func TestResolutionCachePersistsAcceptedMappingAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	first, err := openResolutionCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put(scrape.TitleInputOpaque, "C9DQB3YBPVQ80KAE", "IPX-072"); err != nil {
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
	id, ok, err := second.Get(scrape.TitleInputOpaque, "c9dqb3ybpvq80kae")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || id != "IPX-072" {
		t.Fatalf("id=%q ok=%v, want IPX-072 true", id, ok)
	}
}

func TestResolutionCacheSeparatesOpaqueAndTitleKeys(t *testing.T) {
	cache, err := openResolutionCache(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	if err := cache.Put(scrape.TitleInputOpaque, "abc123xyz999", "IPX-072"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(scrape.TitleInputTitle, "abc123xyz999"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("opaque cache entry leaked into title namespace")
	}
}
