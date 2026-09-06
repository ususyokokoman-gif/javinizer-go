//go:build submissionlive

package scrape

import (
	"context"
	"strings"
	"testing"
	"time"

	httpclient "github.com/javinizer/javinizer-go/internal/httpclient"
)

// TestSubmissionLiveTitleToCatalogID is deliberately a real-network contract test.
// It is not part of ordinary unit CI. The submission workflow runs it under the
// same desktop+production source tags used for the distributed EXE so that a
// release cannot be called "verified" solely from mocked search HTML.
func TestSubmissionLiveTitleToCatalogID(t *testing.T) {
	client, err := httpclient.NewHTTPClient(nil, 20*time.Second)
	if err != nil {
		t.Fatalf("construct production HTTP client: %v", err)
	}

	cfg := &Config{FilenameKeepWords: []string{"SPECIAL", "4K"}}
	s := &Scraper{httpClient: client, cfg: cfg}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	// Stable historical title whose catalog ID is SSIS-001. The synthetic
	// KEEPWORDS suffix proves that the live query path removes configured
	// annotations before contacting the search provider.
	raw := "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間_SPECIAL_4K"
	cleaned := stripConfiguredKeepWords(raw, cfg.FilenameKeepWords)
	if strings.Contains(strings.ToUpper(cleaned), "SPECIAL") || strings.Contains(strings.ToUpper(cleaned), "4K") {
		t.Fatalf("KEEPWORDS remained before live lookup: raw=%q cleaned=%q", raw, cleaned)
	}
	title := normalizeTitleForWebSearch(cleaned)
	t.Logf("LIVE_INPUT raw=%q", raw)
	t.Logf("LIVE_CLEANED=%q", cleaned)
	t.Logf("LIVE_NORMALIZED=%q", title)

	// Record provider-level evidence before the combined resolver runs. This is
	// intentionally real network I/O; a provider block/markup change must be
	// visible in the submitted test log instead of being hidden by mocks.
	probeQuery := title + " 品番"
	for _, provider := range []string{"duckduckgo", "bing"} {
		results, probeErr := s.fetchTitleWebSearch(ctx, provider, probeQuery)
		t.Logf("LIVE_PROVIDER provider=%s query=%q results=%d err=%v", provider, probeQuery, len(results), probeErr)
		for i, result := range results {
			if i >= 3 {
				break
			}
			t.Logf("LIVE_RESULT provider=%s rank=%d title=%q url=%q", provider, i+1, truncateRunes(result.Title, 120), truncateRunes(result.URL, 160))
		}
	}

	id, lookupErr := s.lookupCatalogIDOnWeb(ctx, title)
	if lookupErr != nil {
		t.Fatalf("live lookup failed: normalized=%q err=%v", title, lookupErr)
	}
	if id != "SSIS-001" {
		t.Fatalf("live lookup catalog ID = %q, want %q", id, "SSIS-001")
	}

	got := s.resolveTitleViaWebWithConfiguredNoise(ctx, ScrapeCmd{MovieID: raw})
	if got.MovieID != "SSIS-001" {
		t.Fatalf("full live resolver MovieID = %q, want %q", got.MovieID, "SSIS-001")
	}
	t.Logf("LIVE_RESOLVED=%s", got.MovieID)
}
