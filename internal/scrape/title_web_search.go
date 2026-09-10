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

const maxWebSearchBody = 2 << 20

var (
	videoExtensionRE = regexp.MustCompile(`(?i)\.(?:mp4|mkv|avi|wmv|mov|m4v|ts|webm)$`)
	spaceRE          = regexp.MustCompile(`\s+`)
	bracketGroupRE   = regexp.MustCompile(`[\[\(\{【（［](.*?)[\]\)\}】）］]`)
	domainTokenRE    = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9.-]*\.(?:com|net|org|tv|cc|me|xyz|to|jp)$`)
	qualityTokenRE   = regexp.MustCompile(`(?i)^(?:[248]k(?:60fps)?|720p|1080p|2160p|4320p|fhd|uhd|hdr10?|dolbyvision|dv|hevc|h26[45]|x26[45]|av1|aac|flac|web[-_. ]?dl|webrip|bdrip|bluray|uncensored|uncen|leak|vr|ai|sub|subs|subtitle|字幕|中字|中文字幕|中文|无码)$`)

	webCatalogCandidateRE = regexp.MustCompile(`(?i)(?:\d{6}[-_]\d{2,3}-(?:1PON|10MU|CARIB)|FC2[\s_-]*PPV[\s_-]*\d{5,9}|h_\d+[a-z]+\d+|[A-Z0-9]{2,12}-\d{2,7}[A-Z]?|\b[A-Z]{2,8}\d{3,6}\b)`)
	knownCatalogIDRE      = regexp.MustCompile(`(?i)^(?:\d{6}[-_]\d{2,3}-(?:1PON|10MU|CARIB)|FC2[\s_-]*PPV[\s_-]*\d{5,9}|h_\d+[a-z]+\d+|\d+[a-z]{2,}\d+|[A-Z0-9]{1,12}-\d{2,7}[A-Z]?|[A-Z]{1,8}\d{3,6})$`)
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

func (s *Scraper) resolveTitleViaWeb(ctx context.Context, cmd ScrapeCmd) ScrapeCmd {
	query := strings.TrimSpace(cmd.MovieID)
	if query == "" || s == nil || s.httpClient == nil {
		return cmd
	}
	if isHTTPTitleInput(cmd.RawInput) || isHTTPTitleInput(query) || looksLikeCatalogID(query) {
		return cmd
	}

	title := normalizeTitleForWebSearch(query)
	if len([]rune(compactComparable(title))) < 4 {
		return cmd
	}

	id, err := s.lookupCatalogIDOnWeb(ctx, title)
	if err != nil {
		logging.Infof("[scrape] Google title lookup failed for %q: %v", truncateRunes(title, 100), err)
		return cmd
	}
	if id == "" {
		return cmd
	}

	logging.Infof("[scrape] Google title lookup resolved %q -> %s", truncateRunes(title, 80), id)
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

// lookupCatalogIDOnWeb deliberately uses Google only. KEEPWORDS have already
// been removed by resolveTitleViaWebWithConfiguredNoise before this function is
// reached. Query variants remain Google queries; there is no other provider.
func (s *Scraper) lookupCatalogIDOnWeb(ctx context.Context, title string) (string, error) {
	queries := []string{
		title,
		title + " 品番",
		title + " DMM",
		title + " JAV",
	}
	var lastErr error

	for _, q := range queries {
		results, err := s.fetchTitleWebSearch(ctx, "google", q)
		if err != nil {
			lastErr = err
			logging.Infof("[scrape] Google search query=%q failed: %v", truncateRunes(q, 120), err)
			continue
		}
		logging.Infof("[scrape] Google search query=%q results=%d", truncateRunes(q, 120), len(results))
		if id, ok := chooseCatalogCandidate(title, results); ok {
			return id, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("Google returned no sufficiently strong catalog-ID candidate")
}

func (s *Scraper) fetchTitleWebSearch(ctx context.Context, provider, query string) ([]titleWebSearchResult, error) {
	if provider != "google" {
		return nil, fmt.Errorf("unsupported web search provider %q; Google is the only provider", provider)
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
	if len(results) == 0 {
		pageText := strings.ToLower(strings.TrimSpace(doc.Text()))
		if strings.Contains(pageText, "unusual traffic") || strings.Contains(pageText, "not a robot") || strings.Contains(pageText, "異常なトラフィック") {
			return nil, fmt.Errorf("Google search was blocked by an anti-bot page")
		}
	}
	return results, nil
}

func parseTitleWebResults(provider string, doc *goquery.Document) []titleWebSearchResult {
	if provider != "google" || doc == nil {
		return nil
	}

	results := make([]titleWebSearchResult, 0, 10)
	seen := make(map[string]struct{})
	appendResult := func(title, snippet, href string) {
		title = strings.TrimSpace(spaceRE.ReplaceAllString(title, " "))
		snippet = strings.TrimSpace(spaceRE.ReplaceAllString(snippet, " "))
		href = normalizeGoogleResultURL(strings.TrimSpace(href))
		if title == "" && snippet == "" {
			return
		}
		key := title + "\x00" + href
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		results = append(results, titleWebSearchResult{Title: title, Snippet: snippet, URL: href})
	}

	// Google has used all of these outer result containers. Do not depend on a
	// single generated class name: the h3-bearing result link is the anchor.
	doc.Find("div.MjjYud, div.tF2Cxc, div.Gx5Zad").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		var resultLink *goquery.Selection
		sel.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
			if a.Find("h3").Length() > 0 {
				resultLink = a
				return false
			}
			return true
		})
		if resultLink != nil {
			href, _ := resultLink.Attr("href")
			title := resultLink.Find("h3").First().Text()
			snippet := sel.Find("div.VwiC3b, div.yXK7lf, span.aCOpRe").First().Text()
			if strings.TrimSpace(snippet) == "" {
				snippet = sel.Text()
			}
			appendResult(title, snippet, href)
		}
		return len(results) < 10
	})

	// Stable structural fallback: Google organic results expose the title in h3
	// under a link even when wrapper class names change.
	if len(results) < 10 {
		doc.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
			h3 := a.Find("h3").First()
			if h3.Length() == 0 {
				return true
			}
			href, _ := a.Attr("href")
			appendResult(h3.Text(), a.Parent().Parent().Text(), href)
			return len(results) < 10
		})
	}

	// Last resort keeps catalog IDs present in unusual Google result markup
	// visible to the existing candidate scorer.
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

func normalizeGoogleResultURL(raw string) string {
	if raw == "" {
		return raw
	}
	candidate := raw
	if strings.HasPrefix(candidate, "/url?") {
		candidate = "https://www.google.com" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return raw
	}
	if strings.Contains(strings.ToLower(parsed.Host), "google.") && parsed.Path == "/url" {
		for _, key := range []string{"q", "url"} {
			if target := strings.TrimSpace(parsed.Query().Get(key)); target != "" {
				return target
			}
		}
	}
	return raw
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
		if margin < 1.0 && ranked[1].Score >= ranked[0].Score*0.85 {
			return "", false
		}
	}
	return ranked[0].ID, true
}

func isKnownJAVResultURL(raw string) bool {
	lower := strings.ToLower(raw)
	for _, host := range []string{"javdb", "r18.dev", "r18.com", "dmm.co.jp", "dmm.com", "javlibrary", "fanza"} {
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
