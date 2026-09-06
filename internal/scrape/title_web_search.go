package scrape

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"golang.org/x/text/unicode/norm"
)

const maxWebSearchBody = 2 << 20 // 2 MiB is enough for a search result page.

var (
	videoExtensionRE = regexp.MustCompile(`(?i)\.(?:mp4|mkv|avi|wmv|mov|m4v|ts|webm)$`)
	spaceRE          = regexp.MustCompile(`\s+`)
	bracketGroupRE   = regexp.MustCompile(`[\[\(\{【（［](.*?)[\]\)\}】）］]`)
	domainTokenRE    = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9.-]*\.(?:com|net|org|tv|cc|me|xyz|to|jp)$`)
	qualityTokenRE   = regexp.MustCompile(`(?i)^(?:[248]k(?:60fps)?|720p|1080p|2160p|4320p|fhd|uhd|hdr10?|dolbyvision|dv|hevc|h26[45]|x26[45]|av1|aac|flac|web[-_. ]?dl|webrip|bdrip|bluray|uncensored|uncen|leak|vr|ai|sub|subs|subtitle|字幕|中字|中文字幕|中文|无码)$`)

	// Search-result candidate matching is deliberately a little broader than
	// the filename matcher. Search snippets often contain catalog families such
	// as 300MIUM-1234 that a conservative filename matcher may not recognize.
	webCatalogCandidateRE = regexp.MustCompile(`(?i)(?:\d{6}[-_]\d{2,3}-(?:1PON|10MU|CARIB)|FC2[\s_-]*PPV[\s_-]*\d{5,9}|h_\d+[a-z]+\d+|[A-Z0-9]{2,12}-\d{2,7}[A-Z]?|\b[A-Z]{2,8}\d{3,6}\b)`)
	knownCatalogIDRE      = regexp.MustCompile(`(?i)^(?:\d{6}[-_]\d{2,3}-(?:1PON|10MU|CARIB)|FC2[\s_-]*PPV[\s_-]*\d{5,9}|h_\d+[a-z]+\d+|[A-Z0-9]{1,12}-\d{2,7}[A-Z]?|[A-Z]{1,8}\d{3,6})$`)
)

var titleNoiseTokens = map[string]struct{}{
	"2k": {}, "4k": {}, "8k": {}, "720p": {}, "1080p": {}, "2160p": {}, "4320p": {},
	"fhd": {}, "uhd": {}, "hdr": {}, "hdr10": {}, "dolbyvision": {}, "dv": {},
	"hevc": {}, "h264": {}, "h265": {}, "x264": {}, "x265": {}, "av1": {},
	"aac": {}, "flac": {}, "webdl": {}, "web-dl": {}, "webrip": {}, "bdrip": {}, "bluray": {},
	"uncensored": {}, "uncen": {}, "leak": {}, "vr": {}, "ai": {},
	"sub": {}, "subs": {}, "subtitle": {}, "字幕": {}, "中字": {}, "中文字幕": {}, "中文": {}, "无码": {},
	"jav": {}, "javdb": {}, "r18": {}, "r18dev": {},
}

var falseCatalogPrefixes = map[string]struct{}{
	"H264": {}, "H265": {}, "X264": {}, "X265": {}, "AV1": {}, "HD": {}, "FHD": {}, "UHD": {},
	"HDR": {}, "WEB": {}, "MP4": {}, "MKV": {}, "HEVC": {}, "AAC": {}, "FLAC": {}, "HTTP": {},
}

type titleWebSearchResult struct {
	Title   string
	Snippet string
	URL     string
}

type scoredCatalogCandidate struct {
	ID    string
	Score float64
}

// resolveTitleViaWeb turns a title-looking MovieID into a catalog ID before
// normal scraper querying. A network/search failure is non-fatal: the original
// query is preserved, so the existing scraper/dump fallbacks still run.
func (s *Scraper) resolveTitleViaWeb(ctx context.Context, cmd ScrapeCmd) ScrapeCmd {
	query := strings.TrimSpace(cmd.MovieID)
	if query == "" || s == nil || s.httpClient == nil {
		return cmd
	}

	// URLs already have a dedicated direct-page path. Real catalog IDs should
	// also bypass web search entirely.
	if isHTTPTitleInput(cmd.RawInput) || isHTTPTitleInput(query) || looksLikeCatalogID(query) {
		return cmd
	}

	title := normalizeTitleForWebSearch(query)
	if len([]rune(compactComparable(title))) < 4 {
		return cmd
	}

	id, err := s.lookupCatalogIDOnWeb(ctx, title)
	if err != nil {
		logging.Debugf("[scrape] title web lookup did not resolve a catalog ID: %v", err)
		return cmd
	}
	if id == "" {
		return cmd
	}

	logging.Infof("[scrape] title web lookup resolved %q -> %s", truncateRunes(title, 80), id)
	cmd.MovieID = id
	return cmd
}

func isHTTPTitleInput(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func looksLikeCatalogID(s string) bool {
	s = strings.TrimSpace(norm.NFKC.String(s))
	if !knownCatalogIDRE.MatchString(s) {
		return false
	}
	return normalizeWebCatalogCandidate(s) != ""
}

// normalizeTitleForWebSearch removes common release/encode/subtitle noise while
// retaining human title text. Unknown bracketed text is kept (without brackets)
// because it may genuinely be part of a work title.
func normalizeTitleForWebSearch(input string) string {
	s := strings.TrimSpace(norm.NFKC.String(input))
	s = videoExtensionRE.ReplaceAllString(s, "")

	s = bracketGroupRE.ReplaceAllStringFunc(s, func(group string) string {
		inner := strings.Trim(group, "[](){}【】（）［］ \t\r\n")
		if isTitleNoise(inner) {
			return " "
		}
		return " " + inner + " "
	})

	// Filename separators are generally search noise once we know the input is
	// title text rather than a catalog ID.
	s = strings.NewReplacer(
		"_", " ", "＿", " ", "|", " ", "｜", " ",
		"-", " ", "–", " ", "—", " ", "―", " ",
	).Replace(s)

	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		clean := strings.Trim(field, " .,:;!！?？/\\[](){}【】（）［］<>＜＞'\"")
		if clean == "" || isTitleNoise(clean) {
			continue
		}
		if domainTokenRE.MatchString(strings.ToLower(clean)) {
			continue
		}
		out = append(out, clean)
	}

	result := strings.TrimSpace(spaceRE.ReplaceAllString(strings.Join(out, " "), " "))
	return truncateRunes(result, 180)
}

func isTitleNoise(token string) bool {
	t := strings.ToLower(strings.TrimSpace(norm.NFKC.String(token)))
	t = strings.Trim(t, " .,:;!！?？/\\[](){}【】（）［］<>＜＞'\"-_")
	if t == "" {
		return true
	}
	if _, ok := titleNoiseTokens[t]; ok {
		return true
	}
	return qualityTokenRE.MatchString(t)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (s *Scraper) lookupCatalogIDOnWeb(ctx context.Context, title string) (string, error) {
	// General search comes first, as requested. Site-hinted queries are retries,
	// not the primary path, and help when adult results are poorly ranked.
	queries := []string{
		title + " 品番",
		title + " JAV",
		title + " r18.dev",
		title + " JavDB",
	}
	providers := []string{"duckduckgo", "bing"}
	var lastErr error

	for _, q := range queries {
		for _, provider := range providers {
			results, err := s.fetchTitleWebSearch(ctx, provider, q)
			if err != nil {
				lastErr = err
				continue
			}
			if id, ok := chooseCatalogCandidate(title, results); ok {
				return id, nil
			}
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no sufficiently strong catalog-ID candidate in web results")
}

func (s *Scraper) fetchTitleWebSearch(ctx context.Context, provider, query string) ([]titleWebSearchResult, error) {
	var endpoint string
	switch provider {
	case "duckduckgo":
		endpoint = "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	case "bing":
		endpoint = "https://www.bing.com/search?q=" + url.QueryEscape(query) + "&setlang=ja-JP"
	default:
		return nil, fmt.Errorf("unknown web search provider %q", provider)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"
	if s.cfg != nil && strings.TrimSpace(s.cfg.UserAgent) != "" {
		ua = s.cfg.UserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ja,en-US;q=0.8,en;q=0.6")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s search request failed: %w", provider, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("%s search returned nil response", provider)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s search returned HTTP %d", provider, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse %s search page: %w", provider, err)
	}
	return parseTitleWebResults(provider, doc), nil
}

func parseTitleWebResults(provider string, doc *goquery.Document) []titleWebSearchResult {
	results := make([]titleWebSearchResult, 0, 10)
	appendResult := func(title, snippet, href string) {
		title = strings.TrimSpace(spaceRE.ReplaceAllString(title, " "))
		snippet = strings.TrimSpace(spaceRE.ReplaceAllString(snippet, " "))
		href = strings.TrimSpace(href)
		if title == "" && snippet == "" {
			return
		}
		results = append(results, titleWebSearchResult{Title: title, Snippet: snippet, URL: href})
	}

	switch provider {
	case "duckduckgo":
		doc.Find(".result").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			a := sel.Find("a.result__a").First()
			href, _ := a.Attr("href")
			appendResult(a.Text(), sel.Find(".result__snippet").First().Text(), href)
			return len(results) < 10
		})
	case "bing":
		doc.Find("li.b_algo").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			a := sel.Find("h2 a").First()
			href, _ := a.Attr("href")
			appendResult(a.Text(), sel.Find("p").First().Text(), href)
			return len(results) < 10
		})
	}

	// Search engines change markup periodically. Generic anchors are a fallback;
	// candidate scoring still requires title similarity plus a plausible ID.
	if len(results) == 0 {
		doc.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href")
			text := strings.TrimSpace(a.Text())
			if text != "" && href != "" {
				appendResult(text, "", href)
			}
			return len(results) < 20
		})
	}
	return results
}

func chooseCatalogCandidate(query string, results []titleWebSearchResult) (string, bool) {
	if len(results) == 0 {
		return "", false
	}
	scores := make(map[string]float64)

	for rank, result := range results {
		combined := strings.TrimSpace(result.Title + " " + result.Snippet)
		coverage := queryCoverage(query, combined)
		queryLen := len([]rune(compactComparable(query)))
		if queryLen >= 8 && coverage < 0.12 {
			continue
		}

		ids := extractCatalogCandidates(result.Title + " " + result.Snippet + " " + result.URL)
		for _, id := range ids {
			score := coverage*6.0 + 1.5/float64(rank+1)
			if containsCatalogID(result.Title, id) {
				score += 2.0
			}
			if containsCatalogID(result.Snippet, id) {
				score += 1.0
			}
			if isKnownJAVResultURL(result.URL) {
				score += 1.25
			}
			compactQ, compactR := compactComparable(query), compactComparable(combined)
			if compactQ != "" && strings.Contains(compactR, compactQ) {
				score += 1.5
			}
			scores[id] += score
		}
	}

	if len(scores) == 0 {
		return "", false
	}
	ranked := make([]scoredCatalogCandidate, 0, len(scores))
	for id, score := range scores {
		ranked = append(ranked, scoredCatalogCandidate{ID: id, Score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].Score > ranked[j].Score
	})

	if ranked[0].Score < 3.0 {
		return "", false
	}
	if len(ranked) > 1 {
		margin := ranked[0].Score - ranked[1].Score
		if margin < 0.65 && ranked[1].Score >= ranked[0].Score*0.85 {
			return "", false
		}
	}
	return ranked[0].ID, true
}

func isKnownJAVResultURL(raw string) bool {
	lower := strings.ToLower(raw)
	for _, host := range []string{"javdb", "r18.dev", "r18.com", "dmm.co.jp", "dmm.com", "javlibrary"} {
		if strings.Contains(lower, host) {
			return true
		}
	}
	return false
}

func extractCatalogCandidates(text string) []string {
	if decoded, err := url.QueryUnescape(text); err == nil {
		text += " " + decoded
	}
	text = norm.NFKC.String(text)
	matches := webCatalogCandidateRE.FindAllString(text, -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		id := normalizeWebCatalogCandidate(match)
		if id == "" {
			continue
		}
		key := catalogComparable(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizeWebCatalogCandidate(candidate string) string {
	c := strings.ToUpper(strings.TrimSpace(norm.NFKC.String(candidate)))
	c = strings.NewReplacer("_", "-", " ", "-", "–", "-", "—", "-").Replace(c)
	c = strings.Trim(c, "-.,:;()[]{}")
	for strings.Contains(c, "--") {
		c = strings.ReplaceAll(c, "--", "-")
	}
	if strings.HasPrefix(c, "FC2-PPV") && !strings.HasPrefix(c, "FC2-PPV-") {
		c = "FC2-PPV-" + strings.TrimPrefix(c, "FC2-PPV")
	}

	if strings.Contains(c, "1PON") || strings.Contains(c, "10MU") || strings.Contains(c, "CARIB") {
		return c
	}

	letters, digits := 0, 0
	for _, r := range c {
		if unicode.IsLetter(r) {
			letters++
		} else if unicode.IsDigit(r) {
			digits++
		}
	}
	if letters < 2 || digits < 2 {
		return ""
	}
	prefix := c
	if idx := strings.IndexByte(prefix, '-'); idx >= 0 {
		prefix = prefix[:idx]
	} else {
		prefix = strings.TrimRightFunc(prefix, unicode.IsDigit)
	}
	if _, blocked := falseCatalogPrefixes[prefix]; blocked {
		return ""
	}
	return c
}

func containsCatalogID(text, id string) bool {
	return strings.Contains(catalogComparable(text), catalogComparable(id))
}

func catalogComparable(s string) string {
	s = strings.ToUpper(norm.NFKC.String(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func compactComparable(s string) string {
	s = strings.ToLower(norm.NFKC.String(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// queryCoverage is asymmetric: search snippets contain extra site chrome, so
// we measure how much of the query's character n-grams appear in the result.
func queryCoverage(query, result string) float64 {
	q := []rune(compactComparable(query))
	r := []rune(compactComparable(result))
	if len(q) == 0 || len(r) == 0 {
		return 0
	}
	if strings.Contains(string(r), string(q)) {
		return 1
	}
	n := 2
	if len(q) < 4 {
		n = 1
	}
	qSet := ngramSet(q, n)
	rSet := ngramSet(r, n)
	if len(qSet) == 0 {
		return 0
	}
	hits := 0
	for gram := range qSet {
		if _, ok := rSet[gram]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(qSet))
}

func ngramSet(runes []rune, n int) map[string]struct{} {
	set := make(map[string]struct{})
	if n <= 0 || len(runes) < n {
		return set
	}
	for i := 0; i+n <= len(runes); i++ {
		set[string(runes[i:i+n])] = struct{}{}
	}
	return set
}
