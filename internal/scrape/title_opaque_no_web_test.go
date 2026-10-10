package scrape

import (
	"context"
	"testing"
)

func TestResolveDecisionOpaqueNeverStartsWebSearch(t *testing.T) {
	resolver := NewTitleCatalogResolver(&Config{})
	before := resolver.scraper.titleResolutionMetrics()
	decision, err := resolver.ResolveDecision(context.Background(), "xcobuazsdivkokyx.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionUnknown || decision.CatalogID != "" {
		t.Fatalf("decision=%+v, want unknown without candidate", decision)
	}
	after := resolver.scraper.titleResolutionMetrics()
	if after.WebSearches != before.WebSearches {
		t.Fatalf("opaque input performed web search: before=%+v after=%+v", before, after)
	}
}
