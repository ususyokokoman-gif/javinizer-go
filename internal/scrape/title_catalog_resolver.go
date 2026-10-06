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
	"github.com/javinizer/javinizer-go/internal/scraper/javdb"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
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

// newTitleDirectSourceRegistry boots only the direct title-identification source
// needed by the high-volume resolver. Previously the lightweight resolver left
// Scraper.registry nil, which made collectDirectTitleEvidence return immediately
// and silently disabled JavDB title search.
func newTitleDirectSourceRegistry() ScraperInstanceResolver {
	reg := scraperutil.NewScraperRegistry()
	javdb.Register(reg)
	registration, ok := reg.Get("javdb")
	if !ok || registration.Constructor == nil {
		logging.Warnf("[scrape] direct title source javdb registration unavailable")
		return reg
	}
	settings := registration.Defaults
	settings.Enabled = true
	instance, err := registration.Constructor(scraperutil.ScraperDeps{
		Settings:       settings,
		TimeoutSeconds: 15,
	})
	if err != nil {
		logging.Warnf("[scrape] direct title source javdb init failed: %v", err)
		return reg
	}
	reg.RegisterInstance(instance)
	return reg
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
	// Bulk title identification is deliberately conservative with public search:
	// Google is last-resort and headless-browser retries are disabled.
	resolved.PreferNonGoogleTitleSearch = true
	resolved.DisableHeadlessTitleSearch = true

	client := &http.Client{Timeout: 30 * time.Second}
	return &TitleCatalogResolver{
		scraper: &Scraper{
			httpClient:       client,
			cfg:              &resolved,
			registry:         newTitleDirectSourceRegistry(),
			breaker:          newScraperCircuitBreaker(circuitBreakerThreshold),
			titleSearchGuard: newTitleSearchProviderGuard(),
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
	prepared := PrepareTitleResolutionInput(title)
	if prepared.Query == "" {
		return "", fmt.Errorf("title is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logging.Infof("TITLE_INPUT kind=%s raw=%q query=%q", prepared.Kind, truncateRunes(prepared.Raw, 100), truncateRunes(prepared.Query, 100))

	// Deterministic first: only a standalone catalog token may bypass all
	// external validation. Catalog-looking substrings inside opaque generated
	// names are deliberately excluded by extractStandaloneCatalogCandidates.
	if ids := uniqueNormalizedCatalogIDs(extractStandaloneCatalogCandidates(prepared.Query)); len(ids) == 1 {
		return ids[0], nil
	}

	if prepared.Kind == TitleInputOpaque {
		logging.Infof("LOCAL_TITLE_SEARCH=SKIP reason=opaque_filename_key")
		return r.scraper.lookupCatalogIDByOpaqueKey(ctx, prepared.Query)
	}

	if r.titleLookup != nil {
		if id, ok := r.resolveFromLocalTitle(ctx, prepared.Query); ok {
			return id, nil
		}
	} else {
		logging.Infof("LOCAL_TITLE_SEARCH=UNAVAILABLE reason=no_local_dump")
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.scraper.lookupCatalogIDOnWeb(ctx, prepared.Query)
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
	uniqueExact := top.Score >= 0.999999
	if len(matches) > 1 {
		second := matches[1]
		// A single exact normalized title match is deterministic identity
		// evidence. A close-but-not-exact base edition (for example SONE-999
		// beside SONE-999BOD) must not erase an exact edition-specific match.
		if uniqueExact && second.Score >= 0.999999 && !strings.EqualFold(strings.TrimSpace(top.DVDID), strings.TrimSpace(second.DVDID)) {
			uniqueExact = false
		}
		margin := top.Score - second.Score
		if !uniqueExact && margin < 0.025 {
			logging.Infof("LOCAL_TITLE_SEARCH=MISS reason=ambiguous top=%s score=%.3f second=%s score=%.3f", top.DVDID, top.Score, second.DVDID, second.Score)
			return "", false
		}
	}
	if uniqueExact {
		id := strings.TrimSpace(top.DVDID)
		logging.Infof("LOCAL_TITLE_SEARCH=HIT candidate=%s reason=unique_exact_local_title; Jev/web skipped", id)
		return id, true
	}

	evidence := r.buildLocalTitleEvidence(ctx, matches)

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

func (r *TitleCatalogResolver) buildLocalTitleEvidence(ctx context.Context, matches []models.DumpTitleMatch) []titleWebSearchResult {
	evidence := make([]titleWebSearchResult, 0, minInt(5, len(matches))+1)
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
				"r18.dev local title-index record; catalog_id=%s; title_ja=%s; title_en=%s; lexical_score=%.3f",
				strings.TrimSpace(match.DVDID),
				strings.TrimSpace(match.TitleJa),
				strings.TrimSpace(match.TitleEn),
				match.Score,
			),
			URL: "https://r18.dev/",
		})
	}

	// SearchByTitle intentionally returns a compact candidate row. When the same
	// local store also exposes full dump metadata, enrich the top candidate with
	// independent identity fields before asking Jev. This keeps exact/near-exact
	// local matches from being rejected merely because the gate saw only a title
	// string and no actress/maker/release context.
	dump, ok := r.titleLookup.(models.R18DevDumpLookup)
	if !ok || len(matches) == 0 || strings.TrimSpace(matches[0].DVDID) == "" {
		return evidence
	}
	movie, err := dump.LookupMovie(ctx, matches[0].DVDID)
	if err != nil || movie == nil {
		if err != nil && !errors.Is(err, models.ErrDumpMiss) {
			logging.Warnf("[scrape] local metadata enrichment failed for %s: %v", matches[0].DVDID, err)
		}
		return evidence
	}

	actresses := make([]string, 0, len(movie.Actresses))
	for _, actress := range movie.Actresses {
		name := firstNonEmpty(strings.TrimSpace(actress.NameKanji), strings.TrimSpace(actress.NameKana), strings.TrimSpace(actress.NameRomaji))
		if name != "" {
			actresses = append(actresses, name)
		}
	}
	maker := ""
	if movie.Maker != nil {
		maker = firstNonEmpty(strings.TrimSpace(movie.Maker.NameJa), strings.TrimSpace(movie.Maker.NameEn))
	}
	label := ""
	if movie.Label != nil {
		label = firstNonEmpty(strings.TrimSpace(movie.Label.NameJa), strings.TrimSpace(movie.Label.NameEn))
	}
	series := ""
	if movie.Series != nil {
		series = firstNonEmpty(strings.TrimSpace(movie.Series.NameJa), strings.TrimSpace(movie.Series.NameEn))
	}
	evidence = append([]titleWebSearchResult{{
		Title: firstNonEmpty(strings.TrimSpace(movie.TitleJa), strings.TrimSpace(movie.TitleEn)),
		Snippet: fmt.Sprintf(
			"r18.dev local full metadata record; catalog_id=%s; content_id=%s; title_ja=%s; title_en=%s; actresses=%s; maker=%s; label=%s; series=%s; release_date=%s; runtime_mins=%d",
			strings.TrimSpace(movie.DVDID),
			strings.TrimSpace(movie.ContentID),
			strings.TrimSpace(movie.TitleJa),
			strings.TrimSpace(movie.TitleEn),
			strings.Join(actresses, ", "),
			maker,
			label,
			series,
			strings.TrimSpace(movie.ReleaseDate),
			movie.Runtime,
		),
		URL: "https://r18.dev/",
	}}, evidence...)
	return evidence
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
