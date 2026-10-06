package scrape

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
)

var googleBrowserFallback = fetchGoogleSearchWithHeadlessBrowser

func (s *Scraper) lookupCatalogIDByOpaqueKey(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("opaque filename key is empty")
	}

	// Opaque IDs are not titles. Do one quoted reverse lookup instead of the
	// eight title-query variants. If the exact key is not indexed, later
	// pipeline stages can decide whether heavier evidence extraction is worth
	// the cost.
	query := "\"" + strings.ReplaceAll(key, "\"", "") + "\""
	results, provider, err := s.fetchGeneralTitleWebSearch(ctx, query)
	if err != nil {
		return "", err
	}
	logging.Infof("[scrape] opaque reverse lookup provider=%s key=%q results=%d", provider, truncateRunes(key, 100), len(results))

	id, ok := chooseCatalogCandidate(key, results)
	if !ok {
		return "", fmt.Errorf("opaque filename key produced no sufficiently corroborated catalog-ID candidate")
	}
	return s.finalizeCatalogCandidate(ctx, key, id, results)
}

func (s *Scraper) lookupCatalogIDOnWeb(ctx context.Context, title string) (string, error) {
	// First ask metadata sources that can search by title directly. A JavDB
	// candidate is re-opened as a detail page before it reaches this layer, so
	// it is stronger than a search-engine snippet. We still try to corroborate
	// it with a second trusted source before returning early.
	directEvidence := s.collectDirectTitleEvidence(ctx, title)
	merged := mergeTitleWebResults(directEvidence)

	var directID string
	var directOK bool
	if exactID, ok := chooseExactDirectTitleCandidate(title, directEvidence); ok {
		// Exact, unique detail-page title identity is stronger than the general
		// ranking heuristic, so prefer it whenever it exists.
		directID, directOK = exactID, true
		logging.Infof("[scrape] unique exact direct title candidate selected: %s", directID)
	} else {
		// If two or more verified detail pages are compatible with the supplied
		// title, the local filename itself does not identify one work. Search-
		// engine rank is not evidence of user intent, so fail closed instead of
		// allowing a search engine to arbitrarily break the tie.
		if directTitleEvidenceIsAmbiguous(title, directEvidence) {
			return "", fmt.Errorf("ambiguous title matches multiple verified catalog IDs")
		}
		directID, directOK = chooseCatalogCandidate(title, directEvidence)
		if directOK && !candidateHasTrustedEvidence(directID, directEvidence) {
			directID, directOK = "", false
		}
	}

	queries := buildTitleWebQueries(title)
	if s.cfg != nil && s.cfg.PreferNonGoogleTitleSearch && len(queries) > 3 {
		// High-volume bulk mode stops after the broad/title+ID/quoted variants.
		// Site-qualified query fan-out is too expensive across thousands of
		// files and is reserved for explicit/manual resolution paths.
		queries = queries[:3]
		logging.Infof("[scrape] bulk title search limits query variants to %d", len(queries))
	}
	if !directOK && len(queries) > 0 && s.jevCatalogGateEnabled() {
		if id, accepted, fastErr := s.tryJevFastTitlePath(ctx, title, queries[0]); accepted {
			return id, nil
		} else if fastErr != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			logging.Infof("[scrape] Jev fast path did not finish title=%q: %v", truncateRunes(title, 100), fastErr)
		}
	}
	// A unique exact title from a verified detail page is already strong primary
	// evidence. One independent general-web query is enough to look for a
	// conflict/corroboration signal; repeatedly issuing site-qualified variants
	// only burns public-search rate limits and can make the packaged EXE fail a
	// few seconds after the live contract test. Keep the full query-variant
	// retry set for titles that do not have exact direct evidence.
	if directOK && len(queries) > 1 {
		queries = queries[:1]
		logging.Infof("[scrape] exact direct title evidence limits general web corroboration to primary query")
	}

	var lastErr error
	var webEvidence []titleWebSearchResult
	for _, q := range queries {
		results, provider, err := s.fetchGeneralTitleWebSearch(ctx, q)
		if err != nil {
			lastErr = err
			logging.Infof("[scrape] general web search query=%q failed: %v", truncateRunes(q, 120), err)
			continue
		}
		webEvidence = mergeTitleWebResults(webEvidence, results)
		merged = mergeTitleWebResults(merged, results)
		providerEvidence := provider
		if provider == "bing" {
			// Preserve the full fallback route in packaged-EXE evidence while
			// retaining "bing" as the actual provider returned by the search API.
			providerEvidence = "duckduckgo-miss->bing"
		}
		logging.Infof("[scrape] web search provider=%s query=%q results=%d merged=%d", providerEvidence, truncateRunes(q, 120), len(results), len(merged))
		if id, ok := chooseCatalogCandidate(title, merged); ok {
			if !directOK {
				// chooseCatalogCandidate already requires either trusted evidence
				// or repeated, high-scoring independent result cards with a clear
				// margin. When Jev is configured, use it as the final decision
				// gate immediately instead of spending the remaining per-title
				// deadline on redundant search variants.
				if s.jevCatalogGateEnabled() || candidateHasTrustedEvidence(id, merged) {
					logging.Infof("[scrape] strong web candidate %s selected; sending to Jev/final gate without extra query variants", id)
					return s.finalizeCatalogCandidate(ctx, title, id, merged)
				}
			}
			if directOK && catalogComparable(id) == catalogComparable(directID) && len(candidateTrustedSources(id, merged)) >= 2 {
				logging.Infof("[scrape] direct title candidate %s corroborated by %d trusted source families", id, len(candidateTrustedSources(id, merged)))
				return s.finalizeCatalogCandidate(ctx, title, id, merged)
			}
		}
	}

	if !directOK {
		if id, ok := chooseCatalogCandidate(title, merged); ok {
			return s.finalizeCatalogCandidate(ctx, title, id, merged)
		}
		if lastErr != nil && len(merged) == 0 {
			return "", lastErr
		}
		return "", fmt.Errorf("no sufficiently corroborated catalog-ID candidate")
	}

	id, err := chooseVerifiedDirectFallback(title, directID, webEvidence)
	if err != nil {
		return "", err
	}
	logging.Infof("[scrape] using verified direct title candidate %s after no conflicting strong web evidence was found", id)
	return s.finalizeCatalogCandidate(ctx, title, id, merged)
}

func (s *Scraper) tryJevFastTitlePath(ctx context.Context, title, query string) (string, bool, error) {
	if s == nil || !s.jevCatalogGateEnabled() || strings.TrimSpace(query) == "" {
		return "", false, nil
	}

	// Keep the System-One fast lane genuinely fast. The whole cheap-search +
	// Jev stage gets one shared budget; only unresolved cases fall through to
	// Google/headless-browser corroboration.
	fastCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	type fastProvider struct {
		name  string
		fetch func(context.Context, string) ([]titleWebSearchResult, error)
	}
	providers := []fastProvider{
		{name: "DuckDuckGo", fetch: s.fetchDuckDuckGoTitleSearch},
		{name: "YahooJapan", fetch: s.fetchYahooJapanTitleSearch},
	}

	var evidence []titleWebSearchResult
	var lastErr error
	for _, provider := range providers {
		results, err := provider.fetch(fastCtx, query)
		if err != nil {
			lastErr = err
			logging.Infof("[scrape] Jev fast path provider=%s unavailable: %v", provider.name, err)
			if fastCtx.Err() != nil {
				break
			}
			continue
		}
		evidence = mergeTitleWebResults(evidence, results)
		id, ok := chooseJevFastCandidate(title, evidence)
		if !ok {
			logging.Infof("[scrape] Jev fast path provider=%s produced no decisive candidate yet", provider.name)
			continue
		}

		logging.Infof("[scrape] Jev fast path candidate=%s after provider=%s; validating immediately", id, provider.name)
		validated, validateErr := s.finalizeCatalogCandidate(fastCtx, title, id, evidence)
		if validateErr == nil {
			logging.Infof("[scrape] Jev fast path accepted candidate=%s; skipping deep web search", validated)
			return validated, true, nil
		}
		lastErr = validateErr
		logging.Infof("[scrape] Jev fast path candidate=%s not yet accepted after provider=%s: %v", id, provider.name, validateErr)
		if fastCtx.Err() != nil {
			break
		}
	}

	if fastCtx.Err() != nil && ctx.Err() == nil {
		return "", false, fmt.Errorf("Jev fast path budget exhausted: %w", fastCtx.Err())
	}
	return "", false, lastErr
}

func chooseVerifiedDirectFallback(title, directID string, webEvidence []titleWebSearchResult) (string, error) {
	directID = normalizeWebCatalogCandidate(directID)
	if directID == "" {
		return "", fmt.Errorf("verified direct title candidate is empty")
	}
	webID, webOK := chooseCatalogCandidate(title, webEvidence)
	if webOK && catalogComparable(webID) != catalogComparable(directID) {
		return "", fmt.Errorf("direct title candidate %s conflicts with web evidence for %s", directID, webID)
	}
	return directID, nil
}

func shouldUseHeadlessGoogleFallback(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusForbidden
}

func retryGoogleSearchWithBrowser(ctx context.Context, endpoint, reason string, originalErr error) ([]titleWebSearchResult, error) {
	logging.Infof("[scrape] Google HTTP search %s; retrying with headless browser", reason)
	results, browserErr := googleBrowserFallback(ctx, endpoint)
	if browserErr == nil {
		return results, nil
	}
	if originalErr != nil {
		return nil, fmt.Errorf("Google HTTP search failed: %w; browser fallback failed: %v", originalErr, browserErr)
	}
	return nil, fmt.Errorf("Google HTTP search %s; browser fallback failed: %w", reason, browserErr)
}

func (s *Scraper) fetchTitleWebSearch(ctx context.Context, provider, query string) ([]titleWebSearchResult, error) {
	if provider != "google" {
		return nil, fmt.Errorf("unsupported web search provider %q; Google is the only provider", provider)
	}
	if s == nil || s.httpClient == nil {
		return nil, fmt.Errorf("Google search has no HTTP client")
	}
	if err := s.waitTitleSearchProvider(ctx, "google"); err != nil {
		return nil, err
	}
	headlessAllowed := s.cfg == nil || !s.cfg.DisableHeadlessTitleSearch

	endpoint := "https://www.google.com/search?hl=ja&num=10&filter=0&pws=0&safe=off&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	if s.cfg != nil && strings.TrimSpace(s.cfg.UserAgent) != "" {
		ua = s.cfg.UserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en-US;q=0.7,en;q=0.5")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil || !headlessAllowed {
			return nil, fmt.Errorf("Google search request failed: %w", err)
		}
		return retryGoogleSearchWithBrowser(ctx, endpoint, "request failed", err)
	}
	if resp == nil {
		if !headlessAllowed {
			return nil, fmt.Errorf("Google search returned nil response")
		}
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned nil response", nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			s.markTitleSearchRateLimited("google", resp.Header.Get("Retry-After"))
		}
		if headlessAllowed && shouldUseHeadlessGoogleFallback(resp.StatusCode) {
			return retryGoogleSearchWithBrowser(ctx, endpoint, fmt.Sprintf("returned HTTP %d", resp.StatusCode), nil)
		}
		return nil, fmt.Errorf("Google search returned HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		if !headlessAllowed {
			return nil, fmt.Errorf("parse Google search page: %w", err)
		}
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned unparsable HTML", err)
	}
	results := parseTitleWebResults("google", doc)
	if isGoogleSearchInterstitial(doc, results) {
		s.markTitleSearchRateLimited("google", "")
		if !headlessAllowed {
			return nil, fmt.Errorf("Google returned an interstitial instead of search results")
		}
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned an interstitial instead of search results", nil)
	}
	if len(results) == 0 {
		if !headlessAllowed {
			return nil, fmt.Errorf("Google returned no parseable organic results")
		}
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned no parseable organic results", nil)
	}
	s.markTitleSearchSuccess("google")
	return results, nil
}
