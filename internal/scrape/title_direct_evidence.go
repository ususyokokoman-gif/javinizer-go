package scrape

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// titleCandidateSearcher is intentionally optional. Sources that can search by
// free-form title may implement it without changing the core models.Scraper
// interface used by every scraper.
type titleCandidateSearcher interface {
	SearchTitleCandidates(ctx context.Context, title string, limit int) ([]*models.ScraperResult, error)
}

// collectDirectTitleEvidence asks registered title-identification sources that
// support free-form title search. This lookup is deliberately independent of
// whether a source is enabled for final metadata scraping: Google web lookup is
// likewise an identification aid, and selecting --scrapers must not disable a
// stronger verified ID signal. Today JavDB implements this seam. Each returned
// row has already been re-opened as a detail page by the source scraper, so its
// ID and source URL are stronger evidence than a generic search-engine snippet.
func (s *Scraper) collectDirectTitleEvidence(ctx context.Context, title string) []titleWebSearchResult {
	if s == nil || s.registry == nil || strings.TrimSpace(title) == "" {
		return nil
	}

	out := make([]titleWebSearchResult, 0, 4)
	for _, sourceName := range []string{"javdb"} {
		instance, ok := s.registry.GetInstance(sourceName)
		if !ok || instance == nil {
			continue
		}
		searcher, ok := instance.(titleCandidateSearcher)
		if !ok {
			continue
		}
		results, err := searcher.SearchTitleCandidates(ctx, title, 3)
		if err != nil {
			logging.Infof("[scrape] direct title source %s failed for %q: %v", sourceName, truncateRunes(title, 100), err)
			continue
		}
		for rank, result := range results {
			if result == nil || strings.TrimSpace(result.ID) == "" {
				continue
			}
			resultTitle := strings.TrimSpace(result.Title)
			if resultTitle == "" {
				resultTitle = strings.TrimSpace(result.OriginalTitle)
			}
			snippet := strings.TrimSpace(strings.Join([]string{result.OriginalTitle, "品番", result.ID}, " "))
			logging.Infof(
				"[scrape] direct title candidate source=%s rank=%d id=%s coverage=%.3f title=%q",
				sourceName,
				rank+1,
				result.ID,
				queryCoverage(title, resultTitle),
				truncateRunes(resultTitle, 140),
			)
			out = append(out, titleWebSearchResult{
				Title:   resultTitle,
				Snippet: snippet,
				URL:     strings.TrimSpace(result.SourceURL),
			})
		}
		logging.Infof("[scrape] direct title source %s produced %d verified candidates", sourceName, len(results))
	}
	return out
}
