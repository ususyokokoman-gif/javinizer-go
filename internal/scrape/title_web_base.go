package scrape

import (
	"context"
	"regexp"
	"strings"
	"unicode"

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

type titleWebSearchResult struct { Title, Snippet, URL string }
type scoredCatalogCandidate struct { ID string; Score float64 }

func (s *Scraper) resolveTitleViaWeb(ctx context.Context, cmd ScrapeCmd) ScrapeCmd {
	query := strings.TrimSpace(cmd.MovieID)
	if query == "" || s == nil || s.httpClient == nil { return cmd }
	if isHTTPTitleInput(cmd.RawInput) || isHTTPTitleInput(query) || looksLikeCatalogID(query) { return cmd }
	title := normalizeTitleForWebSearch(query)
	if len([]rune(compactComparable(title))) < 4 { return cmd }
	id, err := s.lookupCatalogIDOnWeb(ctx, title)
	if err != nil { logging.Infof("[scrape] title lookup failed for %q: %v", truncateRunes(title, 100), err); return cmd }
	if id != "" { logging.Infof("[scrape] title web lookup resolved %q -> %s", truncateRunes(title, 80), id); cmd.MovieID = id }
	return cmd
}

func isHTTPTitleInput(s string) bool { s = strings.ToLower(strings.TrimSpace(s)); return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") }
func looksLikeCatalogID(s string) bool { s = strings.TrimSpace(norm.NFKC.String(s)); return knownCatalogIDRE.MatchString(s) && normalizeWebCatalogCandidate(s) != "" }

func normalizeTitleForWebSearch(input string) string {
	s := strings.TrimSpace(norm.NFKC.String(input)); s = videoExtensionRE.ReplaceAllString(s, "")
	s = bracketGroupRE.ReplaceAllStringFunc(s, func(group string) string { inner := strings.Trim(group, "[](){}【】（）［］ \t\r\n"); if isTitleNoise(inner) { return " " }; return " " + inner + " " })
	s = strings.NewReplacer("_", " ", "＿", " ", "|", " ", "｜", " ", "-", " ", "–", " ", "—", " ", "―", " ").Replace(s)
	out := make([]string, 0)
	for _, field := range strings.Fields(s) { clean := strings.Trim(field, " .,:;!！?？/\\[](){}【】（）［］<>＜＞'\""); if clean == "" || isTitleNoise(clean) || domainTokenRE.MatchString(strings.ToLower(clean)) { continue }; out = append(out, clean) }
	return truncateRunes(strings.TrimSpace(spaceRE.ReplaceAllString(strings.Join(out, " "), " ")), 180)
}

func isTitleNoise(token string) bool { t := strings.ToLower(strings.TrimSpace(norm.NFKC.String(token))); t = strings.Trim(t, " .,:;!！?？/\\[](){}【】（）［］<>＜＞'\"-_"); if t == "" { return true }; if _, ok := titleNoiseTokens[t]; ok { return true }; return qualityTokenRE.MatchString(t) }
func truncateRunes(s string, max int) string { r := []rune(s); if len(r) <= max { return s }; return string(r[:max]) }
func containsCatalogID(text, id string) bool { return strings.Contains(catalogComparable(text), catalogComparable(id)) }
func catalogComparable(s string) string { s = strings.ToUpper(norm.NFKC.String(s)); var b strings.Builder; for _, r := range s { if unicode.IsLetter(r) || unicode.IsDigit(r) { b.WriteRune(r) } }; return b.String() }
func compactComparable(s string) string { s = strings.ToLower(norm.NFKC.String(s)); var b strings.Builder; for _, r := range s { if unicode.IsLetter(r) || unicode.IsDigit(r) { b.WriteRune(r) } }; return b.String() }
