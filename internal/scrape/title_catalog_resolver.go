package scrape

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TitleCatalogResolver is a lightweight title -> catalog ID resolver.
// It intentionally skips metadata scraping and persistence. The resolver uses
// public web evidence and, when configured, the Jev catalog gate.
type TitleCatalogResolver struct {
	scraper     *Scraper
	titleLookup models.R18DevTitleLookup
}

// NewTitleCatalogResolver constructs a resolver suitable for high-volume title
// identification. The supplied Config is copied so callers may safely reuse or
// mutate their original config while this resolver is running.
func NewTitleCatalogResolver(cfg *Config) *TitleCatalogResolver {
	return NewTitleCatalogResolverWithLookup(cfg, nil)
}

// NewTitleCatalogResolverWithLookup adds an optional zero-HTTP local title
// lookup. Local candidates still pass through the same Jev >= threshold gate
// before automatic adoption.
func NewTitleCatalogResolverWithLookup(cfg *Config, lookup models.R18DevTitleLookup) *TitleCatalogResolver {
	resolved := Config{}
	if cfg != nil {
		resolved = *cfg
	}
	applyJevCatalogGateEnv(&resolved)
	if resolved.JevCatalogThreshold <= 0 {
		resolved.JevCatalogThreshold = 0.80
	}
	if strings.TrimSpace(resolved.JevCatalogModel) == "" {
		resolved.JevCatalogModel = "jev-latest"
	}
	if strings.TrimSpace(resolved.JevCatalogEndpoint) == "" {
		resolved.JevCatalogEndpoint = "https://api.typesafe.ai/v1/systemone"
	}

	client := &http.Client{Timeout: 30 * time.Second}
	return &TitleCatalogResolver{
		scraper: &Scraper{
			httpClient: client,
			cfg:        &resolved,
		},
		titleLookup: lookup,
	}
}

// Resolve returns the validated catalog ID for a free-form title.
// No metadata lookup is performed after the catalog ID is identified.
func (r *TitleCatalogResolver) Resolve(ctx context.Context, title string) (string, error) {
	if r == nil || r.scraper == nil {
		return "", fmt.Errorf("title catalog resolver is not initialized")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("title is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Deterministic first: when the filename/title already contains exactly one
	// syntactically valid catalog ID, no web search or Jev call is needed.
	// This is both faster and more reliable than asking an external service to
	// rediscover information already present in the filename.
	if ids := uniqueNormalizedCatalogIDs(extractCatalogCandidates(title)); len(ids) == 1 {
		return ids[0], nil
	}

	if r.titleLookup != nil {
		if id, ok := r.resolveFromLocalTitle(ctx, title); ok {
			return id, nil
		}
	}

	return r.scraper.lookupCatalogIDOnWeb(ctx, title)
}

func (r *TitleCatalogResolver) resolveFromLocalTitle(ctx context.Context, title string) (string, bool) {
	matches, err := r.titleLookup.SearchByTitle(ctx, title, 5)
	if err != nil {
		if !errors.Is(err, models.ErrDumpMiss) &&
			!errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
			logging.Warnf("[scrape] local title lookup failed; falling back to web: %v", err)
		}
		return "", false
	}
	if len(matches) == 0 {
		return "", false
	}

	top := matches[0]
	if strings.TrimSpace(top.DVDID) == "" || top.Score < 0.72 {
		logging.Infof("[scrape] local title lookup not decisive title=%q top_score=%.3f", truncateRunes(title, 100), top.Score)
		return "", false
	}
	margin := 1.0
	if len(matches) > 1 {
		margin = top.Score - matches[1].Score
		// Distinct IDs with effectively the same lexical score are genuinely
		// ambiguous even when both titles look exact. Do not pick one by sort
		// order.
		if margin < 0.025 {
			logging.Infof("[scrape] local title lookup ambiguous top=%s score=%.3f second=%s score=%.3f", top.DVDID, top.Score, matches[1].DVDID, matches[1].Score)
			return "", false
		}
	}

	// Deterministic code outranks an external model. A strong unique match in
	// the local canonical dump is accepted immediately; Jev is reserved for
	// medium-confidence candidates. This removes network latency for the common
	// case and avoids Jev under-scoring r18.dev's intentionally censored titles.
	if top.Score >= 0.90 && margin >= 0.05 {
		id := normalizeWebCatalogCandidate(top.DVDID)
		if id != "" {
			logging.Infof("[scrape] local title deterministic accept candidate=%s score=%.3f margin=%.3f; web and Jev skipped", id, top.Score, margin)
			return id, true
		}
	}

	evidence := make([]titleWebSearchResult, 0, minInt(3, len(matches)))
	for i, match := range matches {
		if i >= 3 {
			break
		}
		displayTitle := strings.TrimSpace(match.TitleJa)
		if displayTitle == "" {
			displayTitle = strings.TrimSpace(match.TitleEn)
		}
		evidence = append(evidence, titleWebSearchResult{
			Title: displayTitle,
			Snippet: fmt.Sprintf(
				"r18.dev local title index candidate catalog ID %s; title_en=%s; lexical_score=%.3f",
				match.DVDID,
				strings.TrimSpace(match.TitleEn),
				match.Score,
			),
			URL: "https://r18.dev/",
		})
	}

	logging.Infof("[scrape] local title candidate=%s score=%.3f; validating with Jev before web", top.DVDID, top.Score)
	id, err := r.scraper.finalizeCatalogCandidate(ctx, title, top.DVDID, evidence)
	if err != nil {
		logging.Infof("[scrape] local title candidate=%s not accepted; falling back to web: %v", top.DVDID, err)
		return "", false
	}
	logging.Infof("[scrape] local title accepted candidate=%s; web search skipped", id)
	return id, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
