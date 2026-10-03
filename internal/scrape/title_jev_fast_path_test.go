package scrape

import (
	"context"
	"testing"
)

func TestChooseJevFastCandidateAllowsSingleStrongResult(t *testing.T) {
	title := "狙われた通学路 共謀痴漢電車 桃乃木かな"
	results := []titleWebSearchResult{
		{
			Title:   "狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072",
			Snippet: "桃乃木かな 品番 IPX-072",
			URL:     "https://example.com/work/ipx-072",
		},
	}
	got, ok := chooseJevFastCandidate(title, results)
	if !ok {
		t.Fatal("strong single-result candidate was not offered to Jev")
	}
	if got != "IPX-072" {
		t.Fatalf("candidate=%q, want IPX-072", got)
	}
}

func TestChooseJevFastCandidateRejectsCloseConflict(t *testing.T) {
	title := "作品タイトル 女優名"
	results := []titleWebSearchResult{
		{
			Title:   "作品タイトル 女優名 AAA-123",
			Snippet: "品番 AAA-123",
			URL:     "https://example.com/a",
		},
		{
			Title:   "作品タイトル 女優名 BBB-456",
			Snippet: "品番 BBB-456",
			URL:     "https://example.net/b",
		},
	}
	if got, ok := chooseJevFastCandidate(title, results); ok {
		t.Fatalf("ambiguous fast candidate=%q, want no early candidate", got)
	}
}

func TestTitleCatalogResolverBypassesNetworkForEmbeddedCatalogID(t *testing.T) {
	resolver := &TitleCatalogResolver{scraper: &Scraper{}}
	got, err := resolver.Resolve(context.Background(), "sample title IPX-072 桃乃木かな")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != "IPX-072" {
		t.Fatalf("resolved=%q, want IPX-072", got)
	}
}
