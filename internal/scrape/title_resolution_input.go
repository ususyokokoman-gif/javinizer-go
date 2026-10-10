package scrape

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// TitleInputKind describes what kind of evidence the filename stem actually
// contains. The old bulk path implicitly treated every stem as a human title;
// this distinction lets opaque download IDs use exact reverse lookup instead
// of wasting local title-search and broad title queries.
type TitleInputKind string

const (
	TitleInputCatalog TitleInputKind = "catalog"
	TitleInputTitle   TitleInputKind = "title"
	TitleInputOpaque  TitleInputKind = "opaque_id"
)

type PreparedTitleInput struct {
	Raw   string
	Query string
	Kind  TitleInputKind
}

var (
	legacyVersionCatalogHintRE = regexp.MustCompile("(?i)^([a-z]{2,12})-?_v([0-9]{2,6})(?:[-_\\s\\[【(（]|$)")
	trailingFilenameNoiseRE    = regexp.MustCompile(`(?i)(?:[\s_.-]+(?:\d{3,4}p|[248]k|fhd|uhd|hdr10?|dv|dolbyvision|hevc|h26[45]|x26[45]|av1|aac|flac|web[-_.]?dl|webrip|bdrip|bluray|uncensored|uncen|sub|subs|subtitle|字幕|中字|中文字幕|中文|无码))+$`)
	trailingPartNumberRE       = regexp.MustCompile(`^(.+?)[_-]([1-9]\d?)$`)
)

// PrepareTitleResolutionInput converts a filename/path/title-like value into the
// cheapest useful lookup key. It only strips deterministic transport/quality
// suffixes; it does not guess words out of an opaque identifier.
func PrepareTitleResolutionInput(input string) PreparedTitleInput {
	raw := strings.TrimSpace(norm.NFKC.String(input))
	if raw == "" {
		return PreparedTitleInput{}
	}

	// Callers may pass either a bare filename stem or a full path.
	if base := filepath.Base(raw); base != "." && base != string(filepath.Separator) {
		raw = base
	}
	raw = videoExtensionRE.ReplaceAllString(raw, "")
	query := strings.TrimSpace(raw)
	for {
		next := strings.TrimSpace(trailingFilenameNoiseRE.ReplaceAllString(query, ""))
		next = strings.Trim(next, " ._-")
		if next == query || next == "" {
			break
		}
		query = next
	}

	// Downloaders frequently append _1/_2 to a long opaque source key. Strip
	// that suffix only when the base independently looks opaque; this avoids
	// changing real catalog numbers such as IPX-072.
	if parts := trailingPartNumberRE.FindStringSubmatch(query); len(parts) == 3 && looksOpaqueFilenameKey(parts[1]) {
		query = parts[1]
	}

	// Real-world library filenames frequently start with a catalog ID and then
	// append the title with an underscore, for example:
	//   ADN-444_作品タイトル_出演者.mp4
	// The generic standalone extractor deliberately treats underscore as an
	// identifier character to avoid accepting catalog-looking substrings from
	// opaque download IDs. For the filename start position only, however, an
	// underscore is a deterministic field separator. Accept that narrow case
	// and reduce the lookup key to the exact catalog ID so variants group
	// together and can be verified locally before any Web access.
	if id := extractLeadingFilenameCatalogCandidate(query); id != "" {
		return PreparedTitleInput{Raw: raw, Query: id, Kind: TitleInputCatalog}
	}
	// Precision-first: a catalog-looking token found in the middle of a
	// descriptive filename is only candidate evidence. It must never promote
	// the whole input to catalog/confirmed automatically. Only the proven
	// leading-boundary path above is allowed to become TitleInputCatalog.
	if looksOpaqueFilenameKey(query) {
		return PreparedTitleInput{Raw: raw, Query: query, Kind: TitleInputOpaque}
	}
	return PreparedTitleInput{Raw: raw, Query: query, Kind: TitleInputTitle}
}

func extractLeadingFilenameCatalogCandidate(s string) string {
	s = strings.TrimSpace(norm.NFKC.String(s))
	if s == "" {
		return ""
	}
	loc := webCatalogCandidateRE.FindStringIndex(s)
	if len(loc) != 2 || loc[0] != 0 {
		return ""
	}
	if loc[1] < len(s) {
		r, _ := utf8.DecodeRuneInString(s[loc[1]:])
		switch r {
		case '_', ' ', '\t', '[', '【', '(', '（':
			// Safe filename/title separators immediately after a leading ID.
		default:
			return ""
		}
	}
	id := normalizeWebCatalogCandidate(s[loc[0]:loc[1]])
	if id == "" {
		return ""
	}
	if catalogCandidateHasUnsafeAttachedSuffix(id) {
		return ""
	}
	return id
}

func extractGluedLeadingCatalogCandidate(s string) string {
	s = strings.TrimSpace(norm.NFKC.String(s))
	if s == "" {
		return ""
	}
	loc := webCatalogCandidateRE.FindStringIndex(s)
	if len(loc) != 2 || loc[0] != 0 || loc[1] >= len(s) {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(s[loc[1]:])
	// Only treat a non-separated leading token as a boundary ambiguity when
	// human-readable Japanese text begins immediately after it. This catches
	// real corpus forms such as DASS-931神対応... while avoiding generated
	// numeric keys such as unknown-192602540083...
	if !(unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || strings.ContainsRune("「『【（", r)) {
		return ""
	}
	id := normalizeWebCatalogCandidate(s[loc[0]:loc[1]])
	if id == "" || catalogCandidateHasUnsafeAttachedSuffix(id) {
		return ""
	}
	return id
}

func catalogCandidateHasUnsafeAttachedSuffix(id string) bool {
	// A long alphabetic tail glued directly to the numeric portion is much
	// more likely to be an opaque release key than a catalog edition suffix.
	// Keep known short edition forms (EC, TK, BOD, etc.) intact, but fail
	// closed for 4+ trailing letters such as ABCRE-0275IXYZ.
	lastDigit := -1
	runes := []rune(strings.TrimSpace(norm.NFKC.String(id)))
	for i, r := range runes {
		if unicode.IsDigit(r) {
			lastDigit = i
		}
	}
	if lastDigit < 0 || lastDigit+1 >= len(runes) {
		return false
	}
	tail := runes[lastDigit+1:]
	if len(tail) < 4 {
		return false
	}
	for _, r := range tail {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func extractReviewOnlyCatalogCandidates(query string) []string {
	// Review-only hints may be looser than confirmation evidence because they
	// can never authorize automatic organization. This intentionally sees
	// glued/mid-filename catalog-looking tokens that the standalone extractor
	// rejects, then requires exact local-DB existence before returning review.
	out := append([]string(nil), extractStandaloneCatalogCandidates(query)...)
	out = append(out, webCatalogCandidateRE.FindAllString(query, -1)...)
	if m := legacyVersionCatalogHintRE.FindStringSubmatch(strings.TrimSpace(query)); len(m) == 3 {
		out = append(out, strings.ToUpper(m[1])+"-"+m[2])
	}
	return uniqueNormalizedCatalogIDs(out)
}

func looksOpaqueFilenameKey(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	runes := []rune(s)
	if len(runes) < 6 || len(runes) > 180 {
		return false
	}

	letters, digits := 0, 0
	for _, r := range runes {
		switch {
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			return false
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		case r == '_', r == '-', r == '.':
			// Common release/download-ID separators.
		default:
			return false
		}
	}
	if digits == len(runes) {
		// 6-7桁程度の配信/管理IDも実データに多い。タイトルとしてWeb検索しない。
		return len(runes) >= 6
	}
	if letters == len(runes) {
		// 長い英字だけの生成キーは、人間可読タイトルよりopaqueとして扱う方が安全。
		return len(runes) >= 12
	}
	if letters == 0 {
		return false
	}
	if digits == 0 {
		// 英字＋区切りだけの長い生成キーもfail-closedでopaqueへ。
		return len(runes) >= 12 && (strings.ContainsRune(s, '_') || strings.ContainsRune(s, '-') || strings.ContainsRune(s, '.'))
	}

	// Mixed single-token identifiers become opaque once they are long enough,
	// or when an underscore strongly suggests a generated/download key.
	return len(runes) >= 12 || strings.ContainsRune(s, '_')
}
