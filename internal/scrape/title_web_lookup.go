package scrape

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
)

var googleBrowserFallback = fetchGoogleSearchWithHeadlessBrowser

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
		logging.Infof("[scrape] web search provider=%s query=%q results=%d merged=%d", provider, truncateRunes(q, 120), len(results), len(merged))
		if id, ok := chooseCatalogCandidate(title, merged); ok && candidateHasTrustedEvidence(id, merged) {
			if !directOK {
				return id, nil
			}
			if catalogComparable(id) == catalogComparable(directID) && len(candidateTrustedSources(id, merged)) >= 2 {
				logging.Infof("[scrape] direct title candidate %s corroborated by %d trusted source families", id, len(candidateTrustedSources(id, merged)))
				return id, nil
			}
		}
	}

	if !directOK {
		if id, ok := chooseCatalogCandidate(title, merged); ok {
			return id, nil
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
	return id, nil
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
		return nil, fmt.Errorf("unsupported web search provider %q; Google is the only direct provider", provider)
	}
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
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Google search request failed: %w", err)
		}
		return retryGoogleSearchWithBrowser(ctx, endpoint, "request failed", err)
	}
	if resp == nil {
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned nil response", nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if shouldUseHeadlessGoogleFallback(resp.StatusCode) {
			return retryGoogleSearchWithBrowser(ctx, endpoint, fmt.Sprintf("returned HTTP %d", resp.StatusCode), nil)
		}
		return nil, fmt.Errorf("Google search returned HTTP %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned unparsable HTML", err)
	}
	results := parseTitleWebResults("google", doc)
	if isGoogleSearchInterstitial(doc, results) {
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned an interstitial instead of search results", nil)
	}
	if len(results) == 0 {
		return retryGoogleSearchWithBrowser(ctx, endpoint, "returned no parseable organic results", nil)
	}
	return results, nil
}
