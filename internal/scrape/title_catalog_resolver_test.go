package scrape

import "testing"

func TestNewTitleCatalogResolverInitializesJavDBDirectSearcher(t *testing.T) {
	resolver := NewTitleCatalogResolver(&Config{})
	if resolver == nil || resolver.scraper == nil {
		t.Fatal("resolver scraper is nil")
	}
	if resolver.scraper.registry == nil {
		t.Fatal("title resolver registry is nil")
	}

	instance, ok := resolver.scraper.registry.GetInstance("javdb")
	if !ok || instance == nil {
		t.Fatal("JavDB direct-title instance is not initialized")
	}
	if _, ok := instance.(titleCandidateSearcher); !ok {
		t.Fatalf("JavDB instance %T does not implement titleCandidateSearcher", instance)
	}
}
