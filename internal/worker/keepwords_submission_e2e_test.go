//go:build submissione2e

package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	appconfig "github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/spf13/afero"
)

type submissionMatcher struct{ result string }

func (m *submissionMatcher) MatchString(string) string                           { return m.result }
func (m *submissionMatcher) Match([]models.FileMatchInfo) []matcher.MatchResult  { return nil }
func (m *submissionMatcher) MatchFile(models.FileMatchInfo) *matcher.MatchResult { return nil }

type submissionHTTP struct {
	mu      sync.Mutex
	queries []string
}

func (h *submissionHTTP) Do(req *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.queries = append(h.queries, req.URL.Query().Get("q"))
	h.mu.Unlock()

	body := `<html><body><div class="result"><a class="result__a" href="https://r18.dev/example/ABW-123">MAID 本当の作品タイトル ABW-123</a><div class="result__snippet">MAID 本当の作品タイトル 品番 ABW-123</div></div></body></html>`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (h *submissionHTTP) allQueries() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.queries...)
}

type submissionScraper struct {
	mu       sync.Mutex
	searchID string
}

func (s *submissionScraper) Name() string { return "capture" }
func (s *submissionScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	s.mu.Lock()
	s.searchID = id
	s.mu.Unlock()
	return &models.ScraperResult{Source: "capture", ID: id, ContentID: id, Title: "resolved"}, nil
}
func (s *submissionScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *submissionScraper) IsEnabled() bool                                { return true }
func (s *submissionScraper) Config() *models.ScraperSettings                { return &models.ScraperSettings{} }
func (s *submissionScraper) Close() error                                   { return nil }
func (s *submissionScraper) capturedID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.searchID
}

type submissionRegistry struct{ scraper models.Scraper }

func (r *submissionRegistry) GetInstance(name string) (models.Scraper, bool) {
	if name == "capture" {
		return r.scraper, true
	}
	return nil, false
}
func (r *submissionRegistry) GetInstancesByPriorityForInput(_ []string, _ string) []models.Scraper {
	return []models.Scraper{r.scraper}
}
func (r *submissionRegistry) GetAllInstances() []models.Scraper { return []models.Scraper{r.scraper} }
func (r *submissionRegistry) Names() []string                   { return []string{"capture"} }

type submissionAggregator struct{}

func (submissionAggregator) Aggregate(results []*models.ScraperResult) (*models.Movie, *aggregator.AggregateResult, error) {
	return submissionAggregate(results)
}
func (submissionAggregator) AggregateWithPriority(results []*models.ScraperResult, _ []string) (*models.Movie, *aggregator.AggregateResult, error) {
	return submissionAggregate(results)
}
func (submissionAggregator) ReloadReplacementCaches(context.Context) {}

func submissionAggregate(results []*models.ScraperResult) (*models.Movie, *aggregator.AggregateResult, error) {
	if len(results) != 1 || results[0] == nil {
		return nil, nil, fmt.Errorf("unexpected scraper results: %#v", results)
	}
	return &models.Movie{ID: results[0].ID, ContentID: results[0].ContentID, Title: results[0].Title}, &aggregator.AggregateResult{}, nil
}

func submissionASCIIAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// submissionContainsAnnotation mirrors the feature's intended matching rule:
// short ASCII words such as AI are annotations only at token boundaries, while
// punctuation/non-ASCII words are literal annotations. This prevents the test
// itself from falsely treating the AI inside the real title token MAID as a leak.
func submissionContainsAnnotation(text, word string) bool {
	if submissionASCIIAlnum(word) {
		re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(word) + `([^A-Za-z0-9]|$)`)
		return re.MatchString(text)
	}
	return strings.Contains(strings.ToUpper(text), strings.ToUpper(word))
}

func TestSubmissionE2E_UnmatchedFilenameKeepWordsToResolvedCatalogID(t *testing.T) {
	const filenameWithoutExt = `MAID 本当の作品タイトル_SPECIAL_4K_8K_VR_AI_字幕_中文字幕_-UC_UNCENSORED`
	const filePath = `C:\videos\MAID 本当の作品タイトル_SPECIAL_4K_8K_VR_AI_字幕_中文字幕_-UC_UNCENSORED.mp4`
	keepWords := []string{"SPECIAL", "4K", "8K", "VR", "AI", "字幕", "中文字幕", "-UC", "UNCENSORED"}

	// Deliberately make the legacy second-pass matcher return a false ID. The
	// worker must ignore it for an unmatched file and hand the complete title to
	// the web-first scrape layer.
	inputs := scrapePhaseInputs{Matcher: &submissionMatcher{result: "FAKE-999"}}
	cmd, fromMatcher := buildScrapeCmd(filePath, models.FileMatchInfo{}, inputs, ScrapePhaseConfig{
		SelectedScrapers: []string{"capture"},
	})
	if fromMatcher {
		t.Fatalf("unmatched filename was incorrectly marked as matcher-derived: %#v", cmd)
	}
	if cmd.MovieID != filenameWithoutExt {
		t.Fatalf("worker handoff MovieID = %q, want %q", cmd.MovieID, filenameWithoutExt)
	}

	appCfg := &appconfig.Config{}
	appCfg.Output.Template.FileFormat = `<ID><KEEPWORDS:SPECIAL|4K|8K|VR|AI|字幕|中文字幕|-UC|UNCENSORED;PREFIX= - ;DELIM= > - <TITLE>`
	scrapeCfg := scrape.ConfigFromAppConfig(appCfg)
	if scrapeCfg == nil || len(scrapeCfg.FilenameKeepWords) != len(keepWords) {
		t.Fatalf("KEEPWORDS config bridge = %#v, want %d words", scrapeCfg, len(keepWords))
	}

	httpCapture := &submissionHTTP{}
	downstream := &submissionScraper{}
	registry := &submissionRegistry{scraper: downstream}
	engine := scrape.New(registry, submissionAggregator{}, nil, nil, httpCapture, scrapeCfg, nil, afero.NewMemMapFs())

	result, err := engine.Scrape(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if result == nil || result.Status != scrape.StatusCompleted || result.Movie == nil {
		t.Fatalf("Scrape() result = %#v", result)
	}
	if got := downstream.capturedID(); got != "ABW-123" {
		t.Fatalf("downstream scraper received %q, want %q", got, "ABW-123")
	}
	if result.Movie.ID != "ABW-123" {
		t.Fatalf("final movie ID = %q, want %q", result.Movie.ID, "ABW-123")
	}

	queries := httpCapture.allQueries()
	if len(queries) == 0 {
		t.Fatal("no web search HTTP request was made")
	}
	for _, q := range queries {
		for _, unwanted := range append(append([]string(nil), keepWords...), "FAKE-999") {
			if submissionContainsAnnotation(q, unwanted) {
				t.Fatalf("web query leaked annotation/false matcher ID %q: %q", unwanted, q)
			}
		}
		if !strings.Contains(q, "本当の作品タイトル") {
			t.Fatalf("web query lost the real title: %q", q)
		}
		if !strings.Contains(strings.ToUpper(q), "MAID") {
			t.Fatalf("short KEEPWORD AI damaged real title token MAID: %q", q)
		}
	}
}
