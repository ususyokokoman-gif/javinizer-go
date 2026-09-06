//go:build submissionlive

package scrape

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestSubmissionLiveTitleToCatalogID is deliberately a real-network contract test.
// It is not part of ordinary unit CI. The submission workflow runs it under the
// same desktop+production source tags used for the distributed EXE so that a
// release cannot be called "verified" solely from mocked search HTML.
func TestSubmissionLiveTitleToCatalogID(t *testing.T) {
	client := &http.Client{Timeout: 20 * time.Second}
	s := &Scraper{
		httpClient: client,
		cfg: &Config{
			FilenameKeepWords: []string{"SPECIAL", "4K"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	// Stable historical title whose catalog ID is SSIS-001. The synthetic
	// KEEPWORDS suffix proves that the live query path removes configured
	// annotations before contacting the search provider.
	cmd := ScrapeCmd{MovieID: "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間_SPECIAL_4K"}
	got := s.resolveTitleViaWebWithConfiguredNoise(ctx, cmd)
	if got.MovieID != "SSIS-001" {
		t.Fatalf("live web title resolution = %q, want %q", got.MovieID, "SSIS-001")
	}
}
