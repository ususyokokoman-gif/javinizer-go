package scrape

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

type ambiguousTitleDirectScraper struct {
	settings models.ScraperSettings
}

func (s *ambiguousTitleDirectScraper) Name() string { return "javdb" }
func (s *ambiguousTitleDirectScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, fmt.Errorf("not used")
}
func (s *ambiguousTitleDirectScraper) GetURL(context.Context, string) (string, error) {
	return "", fmt.Errorf("not used")
}
func (s *ambiguousTitleDirectScraper) IsEnabled() bool { return true }
func (s *ambiguousTitleDirectScraper) Config() *models.ScraperSettings {
	return &s.settings
}
func (s *ambiguousTitleDirectScraper) Close() error { return nil }
func (s *ambiguousTitleDirectScraper) SearchTitleCandidates(_ context.Context, title string, _ int) ([]*models.ScraperResult, error) {
	return []*models.ScraperResult{
		{
			Source:        "javdb",
			SourceURL:     "https://javdb.com/v/a",
			ID:            "MIDE-215",
			Title:         title + " 佐山愛",
			OriginalTitle: title + " 佐山愛",
		},
		{
			Source:        "javdb",
			SourceURL:     "https://javdb.com/v/b",
			ID:            "MIDE-243",
			Title:         title + " 神咲詩織",
			OriginalTitle: title + " 神咲詩織",
		},
	}, nil
}

type countingRejectHTTPClient struct {
	calls int
}

func (c *countingRejectHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, fmt.Errorf("unexpected HTTP request")
}

func TestLookupCatalogIDOnWebFailsClosedBeforeGoogleForAmbiguousDirectTitle(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&ambiguousTitleDirectScraper{})
	httpClient := &countingRejectHTTPClient{}
	s := &Scraper{
		registry:   registry,
		httpClient: httpClient,
		cfg:        &Config{},
	}

	id, err := s.lookupCatalogIDOnWeb(context.Background(), "今日、あなたの上司に犯されました。")
	if err == nil {
		t.Fatal("ambiguous verified title unexpectedly resolved without error")
	}
	if id != "" {
		t.Fatalf("ambiguous verified title resolved to %q", id)
	}
	if !strings.Contains(err.Error(), "ambiguous title") {
		t.Fatalf("error = %q, want explicit ambiguous-title failure", err)
	}
	if httpClient.calls != 0 {
		t.Fatalf("Google/search HTTP called %d times after direct ambiguity was proven; want 0", httpClient.calls)
	}
}
