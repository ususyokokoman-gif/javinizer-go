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

// fetchGeneralTitleWebSearch keeps Google as the preferred provider but does
// not make title identification depend on one public search engine. DuckDuckGo
// Yahoo Japan and Bing are independent network-search fallbacks. Every provider
// must return ordinary organic result cards; the caller applies the same catalog evidence
// rules regardless of which provider answered.
func (s *Scraper) fetchGeneralTitleWebSearch(ctx context.Context, query string) ([]titleWebSearchResult, string, error) {
	googleResults, googleErr := s.fetchTitleWebSearch(ctx, "google", query)
	if googleErr == nil && len(googleResults) > 0 {
		return googleResults, "google", nil
	}

	ddgResults, ddgErr := s.fetchDuckDuckGoTitleSearch(ctx, query)
	if ddgErr == nil && len(ddgResults) > 0 {
		if googleErr != nil {
			logging.Infof("[scrape] Google unavailable for query=%q; DuckDuckGo fallback returned %d results", truncateRunes(query, 120), len(ddgResults))
		}
		return ddgResults, "duckduckgo", nil
	}

	yahooResults, yahooErr := s.fetchYahooJapanTitleSearch(ctx, query)
	if yahooErr == nil && len(yahooResults) > 0 {
		logging.Infof("[scrape] Google/DuckDuckGo unavailable for query=%q; Yahoo Japan fallback returned %d results", truncateRunes(query, 120), len(yahooResults))
		return yahooResults, "yahoojp", nil
	}

	bingResults, bingErr := s.fetchBingTitleSearch(ctx, query)
	if bingErr == nil && len(bingResults) > 0 {
		logging.Infof("[scrape] Google/DuckDuckGo/Yahoo Japan unavailable for query=%q; Bing fallback returned %d results", truncateRunes(query, 120), len(bingResults))
		return bingResults, "bing", nil
	}

	if googleErr == nil {
		googleErr = fmt.Errorf("Google returned no usable results")
	}
	if ddgErr == nil {
		ddgErr = fmt.Errorf("DuckDuckGo returned no usable results")
	}
	if yahooErr == nil {
		yahooErr = fmt.Errorf("Yahoo Japan returned no usable results")
	}
	if bingErr == nil {
		bingErr = fmt.Errorf("Bing returned no usable results")
	}
	return nil, "", fmt.Errorf("general web search failed: Google: %v; DuckDuckGo: %v; Yahoo Japan: %v; Bing: %v", googleErr, ddgErr, yahooErr, bingErr)
}

func (s *Scraper) fetchDuckDuckGoTitleSearch(ctx context.Context, query string) ([]titleWebSearchResult, error) {
	if s == nil || s.httpClient == nil {
		return nil, fmt.Errorf("DuckDuckGo search has no HTTP client")
	}

	endpoints := []string{
		"https://html.duckduckgo.com/html/",
		"https://duckduckgo.com/html/",
	}
	var lastErr error
	for _, base := range endpoints {
		u, err := url.Parse(base)
		if err != nil {
			lastErr = err
			continue
		}
		values := u.Query()
		values.Set("q", query)
		values.Set("kp", "-2")
		values.Set("kl", "jp-jp")
		u.RawQuery = values.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			lastErr = err
			continue
		}
		setTitleSearchHeaders(req, s)

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("DuckDuckGo request failed: %w", err)
			continue
		}
		if resp == nil {
			lastErr = fmt.Errorf("DuckDuckGo returned nil response")
			continue
		}
		body := resp.Body
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body.Close()
			lastErr = fmt.Errorf("DuckDuckGo returned HTTP %d", resp.StatusCode)
			continue
		}
		doc, parseErr := goquery.NewDocumentFromReader(io.LimitReader(body, maxWebSearchBody))
		body.Close()
		if parseErr != nil {
			lastErr = fmt.Errorf("parse DuckDuckGo search page: %w", parseErr)
			continue
		}
		results := parseDuckDuckGoResults(doc)
		if len(results) == 0 {
			lastErr = fmt.Errorf("DuckDuckGo returned no parseable organic results")
			continue
		}
		return results, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("DuckDuckGo search failed")
	}
	return nil, lastErr
}

func (s *Scraper) fetchYahooJapanTitleSearch(ctx context.Context, query string) ([]titleWebSearchResult, error) {
	if s == nil || s.httpClient == nil {
		return nil, fmt.Errorf("Yahoo Japan search has no HTTP client")
	}

	u, err := url.Parse("https://search.yahoo.co.jp/search")
	if err != nil {
		return nil, err
	}
	values := u.Query()
	values.Set("p", query)
	values.Set("ei", "UTF-8")
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	setTitleSearchHeaders(req, s)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Yahoo Japan request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("Yahoo Japan returned nil response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Yahoo Japan returned HTTP %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse Yahoo Japan search page: %w", err)
	}
	results := parseYahooJapanResults(doc)
	if len(results) == 0 {
		return nil, fmt.Errorf("Yahoo Japan returned no parseable organic results")
	}
	return results, nil
}

func parseYahooJapanResults(doc *goquery.Document) []titleWebSearchResult {
	if doc == nil {
		return nil
	}
	results := make([]titleWebSearchResult, 0, 10)
	seen := make(map[string]struct{})
	appendResult := func(title, snippet, href string) {
		title = strings.TrimSpace(spaceRE.ReplaceAllString(title, " "))
		snippet = strings.TrimSpace(spaceRE.ReplaceAllString(snippet, " "))
		href = strings.TrimSpace(href)
		if title == "" || href == "" {
			return
		}
		key := title + "\x00" + href
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		results = append(results, titleWebSearchResult{Title: title, Snippet: snippet, URL: href})
	}

	doc.Find(".sw-Card.Algo, .sw-CardBase .Algo").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		link := sel.Find("a.sw-Card__titleInner").First()
		if link.Length() == 0 {
			return true
		}
		href, _ := link.Attr("href")
		title := sel.Find("h3.sw-Card__titleMain").First().Text()
		if strings.TrimSpace(title) == "" {
			title = link.Text()
		}
		snippet := sel.Find(".sw-Card__summary").First().Text()
		appendResult(title, snippet, href)
		return len(results) < 10
	})

	if len(results) == 0 {
		doc.Find("a.sw-Card__titleInner").EachWithBreak(func(_ int, link *goquery.Selection) bool {
			href, _ := link.Attr("href")
			card := link.Closest(".sw-Card.Algo, .sw-CardBase")
			title := link.Find("h3.sw-Card__titleMain").First().Text()
			if strings.TrimSpace(title) == "" {
				title = link.Text()
			}
			snippet := card.Find(".sw-Card__summary").First().Text()
			appendResult(title, snippet, href)
			return len(results) < 10
		})
	}
	return results
}

func (s *Scraper) fetchBingTitleSearch(ctx context.Context, query string) ([]titleWebSearchResult, error) {
	if s == nil || s.httpClient == nil {
		return nil, fmt.Errorf("Bing search has no HTTP client")
	}

	u, err := url.Parse("https://www.bing.com/search")
	if err != nil {
		return nil, err
	}
	values := u.Query()
	values.Set("q", query)
	values.Set("count", "10")
	values.Set("setlang", "ja-jp")
	values.Set("cc", "jp")
	values.Set("safesearch", "off")
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	setTitleSearchHeaders(req, s)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Bing request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("Bing returned nil response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Bing returned HTTP %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse Bing search page: %w", err)
	}
	results := parseBingResults(doc)
	if len(results) == 0 {
		return nil, fmt.Errorf("Bing returned no parseable organic results")
	}
	return results, nil
}

func setTitleSearchHeaders(req *http.Request, s *Scraper) {
	if req == nil {
		return
	}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.UserAgent) != "" {
		ua = s.cfg.UserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en-US;q=0.7,en;q=0.5")
}

func parseDuckDuckGoResults(doc *goquery.Document) []titleWebSearchResult {
	if doc == nil {
		return nil
	}
	results := make([]titleWebSearchResult, 0, 10)
	seen := make(map[string]struct{})
	appendResult := func(title, snippet, href string) {
		title = strings.TrimSpace(spaceRE.ReplaceAllString(title, " "))
		snippet = strings.TrimSpace(spaceRE.ReplaceAllString(snippet, " "))
		href = normalizeDuckDuckGoResultURL(strings.TrimSpace(href))
		if title == "" || href == "" {
			return
		}
		key := title + "\x00" + href
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		results = append(results, titleWebSearchResult{Title: title, Snippet: snippet, URL: href})
	}

	doc.Find(".result").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		link := sel.Find("a.result__a").First()
		if link.Length() == 0 {
			link = sel.Find("a.result-link").First()
		}
		if link.Length() > 0 {
			href, _ := link.Attr("href")
			snippet := sel.Find(".result__snippet").First().Text()
			appendResult(link.Text(), snippet, href)
		}
		return len(results) < 10
	})

	if len(results) == 0 {
		doc.Find("a.result__a, a.result-link").EachWithBreak(func(_ int, link *goquery.Selection) bool {
			href, _ := link.Attr("href")
			appendResult(link.Text(), link.Parent().Text(), href)
			return len(results) < 10
		})
	}
	return results
}

func parseBingResults(doc *goquery.Document) []titleWebSearchResult {
	if doc == nil {
		return nil
	}
	results := make([]titleWebSearchResult, 0, 10)
	seen := make(map[string]struct{})
	appendResult := func(title, snippet, href string) {
		title = strings.TrimSpace(spaceRE.ReplaceAllString(title, " "))
		snippet = strings.TrimSpace(spaceRE.ReplaceAllString(snippet, " "))
		href = strings.TrimSpace(href)
		if title == "" || href == "" {
			return
		}
		key := title + "\x00" + href
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		results = append(results, titleWebSearchResult{Title: title, Snippet: snippet, URL: href})
	}

	doc.Find("#b_results li.b_algo").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		link := sel.Find("h2 a").First()
		if link.Length() > 0 {
			href, _ := link.Attr("href")
			snippet := sel.Find(".b_caption p").First().Text()
			if strings.TrimSpace(snippet) == "" {
				snippet = sel.Find(".b_snippet").First().Text()
			}
			appendResult(link.Text(), snippet, href)
		}
		return len(results) < 10
	})

	if len(results) == 0 {
		doc.Find("#b_results h2 a").EachWithBreak(func(_ int, link *goquery.Selection) bool {
			href, _ := link.Attr("href")
			appendResult(link.Text(), link.Parent().Parent().Text(), href)
			return len(results) < 10
		})
	}
	return results
}

func normalizeDuckDuckGoResultURL(raw string) string {
	if raw == "" {
		return ""
	}
	candidate := raw
	if strings.HasPrefix(candidate, "//") {
		candidate = "https:" + candidate
	} else if strings.HasPrefix(candidate, "/") {
		candidate = "https://duckduckgo.com" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return raw
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com") {
		if target := strings.TrimSpace(parsed.Query().Get("uddg")); target != "" {
			return target
		}
	}
	return candidate
}
