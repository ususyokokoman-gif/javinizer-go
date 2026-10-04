package scrape

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TitleCatalogResolver is a lightweight title -> catalog ID resolver.
// It intentionally skips metadata scraping and persistence. The resolver uses
// local dump candidates verified by Jev before falling back to public Web
// evidence and its configured Jev catalog gate.
type TitleCatalogResolver struct {
	scraper          *Scraper
	titleLookup      models.R18DevTitleLookup
	firstLocalSearch sync.Once
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
	} else {
		logging.Infof("LOCAL_TITLE_SEARCH=UNAVAILABLE reason=no_local_dump")
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.scraper.lookupCatalogIDOnWeb(ctx, title)
}

func (r *TitleCatalogResolver) resolveFromLocalTitle(ctx context.Context, title string) (string, bool) {
	began := time.Now()
	first := false
	r.firstLocalSearch.Do(func() {
		first = true
		logging.Infof("TIMING scope=resolver phase=first_local_title_search event=START timestamp=%s elapsed_ms=0", began.UTC().Format(time.RFC3339Nano))
	})
	matches, err := r.titleLookup.SearchByTitle(ctx, title, 5)
	if first {
		status := "ok"
		if err != nil {
			status = "error"
		}
		logging.Infof("TIMING scope=resolver phase=first_local_title_search event=END timestamp=%s elapsed_ms=%.3f status=%s", time.Now().UTC().Format(time.RFC3339Nano), float64(time.Since(began).Microseconds())/1000, status)
	}
	if err != nil {
		status := "UNAVAILABLE"
		if errors.Is(err, models.ErrDumpMiss) {
			status = "MISS"
		}
		logging.Infof("LOCAL_TITLE_SEARCH=%s", status)
		if !errors.Is(err, models.ErrDumpMiss) &&
			!errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
			logging.Warnf("[scrape] local title lookup failed; falling back to web: %v", err)
		}
		return "", false
	}
	if len(matches) == 0 {
		logging.Infof("LOCAL_TITLE_SEARCH=MISS")
		return "", false
	}

	top := matches[0]
	if strings.TrimSpace(top.DVDID) == "" || top.Score < 0.72 {
		logging.Infof("LOCAL_TITLE_SEARCH=MISS reason=weak_candidate title=%q top_score=%.3f", truncateRunes(title, 100), top.Score)
		return "", false
	}
	if len(matches) > 1 {
		margin := top.Score - matches[1].Score
		// Distinct IDs with effectively the same lexical score are genuinely
		// ambiguous even when both titles look exact. Do not pick one by sort
		// order.
		if margin < 0.025 {
			logging.Infof("LOCAL_TITLE_SEARCH=MISS reason=ambiguous top=%s score=%.3f second=%s score=%.3f", top.DVDID, top.Score, matches[1].DVDID, matches[1].Score)
			return "", false
		}
	}

	evidence := make([]titleWebSearchResult, 0, minInt(5, len(matches)))
	for i, match := range matches {
		if i >= 5 {
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

	if !r.scraper.jevCatalogGateEnabled() {
		logging.Infof("LOCAL_TITLE_SEARCH=MISS reason=jev_unavailable")
		return "", false
	}

	logging.Infof("[scrape] local title candidate=%s score=%.3f; validating with Jev before web", top.DVDID, top.Score)
	id, err := r.scraper.finalizeCatalogCandidate(ctx, title, top.DVDID, evidence)
	if err != nil {
		logging.Infof("LOCAL_TITLE_SEARCH=MISS reason=jev_rejected candidate=%s; falling back to web: %v", top.DVDID, err)
		return "", false
	}
	logging.Infof("LOCAL_TITLE_SEARCH=HIT candidate=%s; web search skipped", id)
	return id, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
