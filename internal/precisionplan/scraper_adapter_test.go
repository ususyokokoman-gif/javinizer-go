package precisionplan

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

type fakeNativeScraper struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
	calls   int
}

func (f *fakeNativeScraper) Name() string    { return f.name }
func (f *fakeNativeScraper) IsEnabled() bool { return f.enabled }
func (f *fakeNativeScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	f.calls++
	return f.result, f.err
}
func TestScraperAdapterRejectsDisabledAndSourceMismatch(t *testing.T) {
	f := &fakeNativeScraper{name: "libredmm", result: &models.ScraperResult{
		Source: "other", ID: "MIUM-1423", OriginalTitle: "日本語", ContentID: "work"}}
	a := ScraperAdapter{Scraper: f}
	if _, err := a.Search(context.Background(), "MIUM-1423"); err == nil || f.calls != 0 {
		t.Fatalf("disabled scraper was invoked: %v", err)
	}
	f.enabled = true
	if _, err := a.Search(context.Background(), "MIUM-1423"); err == nil {
		t.Fatal("foreign source accepted")
	}
}
func TestScraperAdapterUsesOnlyJapaneseOriginalField(t *testing.T) {
	f := &fakeNativeScraper{name: "libredmm", enabled: true, result: &models.ScraperResult{
		Source: "libredmm", Language: "ja", ID: "MIUM-1423", ContentID: "movie1",
		Title: "日本語風の表示タイトル", OriginalTitle: "English original", SourceURL: "https://libredmm.com/movie",
	}}
	c := SearchCandidates(context.Background(), []QueryGroup{{SKU: "MIUM-1423"}}, []SearchProvider{ScraperAdapter{Scraper: f}}, LookupPolicy{MaxRequests: 1})
	if len(c.Candidates) != 1 || c.Candidates[0].State != Review || c.Candidates[0].JapaneseOriginal != "" {
		t.Fatalf("English original mistakenly adopted as Japanese: %+v", c)
	}
	f.result.OriginalTitle = "正規の日本語原題"
	c = SearchCandidates(context.Background(), []QueryGroup{{SKU: "MIUM-1423"}}, []SearchProvider{ScraperAdapter{Scraper: f}}, LookupPolicy{MaxRequests: 1})
	if c.Candidates[0].JapaneseOriginal != "正規の日本語原題" || c.Candidates[0].State != Review {
		t.Fatalf("Japanese result unexpectedly confirmed or lost: %+v", c)
	}
}
func TestScraperAdapterTyped429AndBlocked(t *testing.T) {
	f := &fakeNativeScraper{name: "libredmm", enabled: true}
	a := ScraperAdapter{Scraper: f}
	f.err = models.NewScraperStatusError("libredmm", 429, "too many requests")
	_, err := a.Search(context.Background(), "MIUM-1423")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("typed 429 not recognized: %v", err)
	}
	f.err = models.NewScraperStatusError("libredmm", 451, "region blocked")
	_, err = a.Search(context.Background(), "MIUM-1423")
	if !errors.Is(err, ErrRegionBlocked) {
		t.Fatalf("typed region block not recognized: %v", err)
	}
}
func TestScraperAdapterDoesNotMakeWebCallOnNil(t *testing.T) {
	a := ScraperAdapter{}
	if _, err := a.Search(context.Background(), "MIUM-1423"); err == nil {
		t.Fatal("nil scraper was accepted")
	}
}
