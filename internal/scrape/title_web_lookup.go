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

func (s *Scraper) lookupCatalogIDOnWeb(ctx context.Context, title string) (string, error) {
	// First ask metadata sources that can search by title directly. A JavDB
	// candidate is re-opened as a detail page before it reaches this layer, so
	// it is independent structured evidence rather than a search-engine guess.
	merged := mergeTitleWebResults(s.collectDirectTitleEvidence(ctx, title))
	if id, ok := chooseCatalogCandidate(title, merged); ok && candidateHasTrustedEvidence(id, merged) {
		return id, nil
	}

	queries := buildTitleWebQueries(title)
	var lastErr error
	for _, q := range queries {
		results, err := s.fetchTitleWebSearch(ctx, "google", q)
		if err != nil {
			lastErr = err
			logging.Infof("[scrape] Google search query=%q failed: %v", truncateRunes(q, 120), err)
			continue
		}
		merged = mergeTitleWebResults(merged, results)
		logging.Infof("[scrape] Google search query=%q results=%d merged=%d", truncateRunes(q, 120), len(results), len(merged))
		if id, ok := chooseCatalogCandidate(title, merged); ok && candidateHasTrustedEvidence(id, merged) {
			return id, nil
		}
	}
	if id, ok := chooseCatalogCandidate(title, merged); ok {
		return id, nil
	}
	if lastErr != nil && len(merged) == 0 {
		return "", lastErr
	}
	return "", fmt.Errorf("no sufficiently corroborated catalog-ID candidate")
}

func (s *Scraper) fetchTitleWebSearch(ctx context.Context, provider, query string) ([]titleWebSearchResult, error) {
	if provider != "google" {
		return nil, fmt.Errorf("unsupported web search provider %q; Google is the only search-engine provider", provider)
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
		return nil, fmt.Errorf("Google search request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("Google search returned nil response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Google search returned HTTP %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse Google search page: %w", err)
	}
	results := parseTitleWebResults("google", doc)
	if isGoogleSearchInterstitial(doc, results) {
		return fetchGoogleSearchWithHeadlessBrowser(ctx, endpoint)
	}
	return results, nil
}