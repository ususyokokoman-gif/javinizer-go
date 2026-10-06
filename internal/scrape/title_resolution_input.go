package scrape

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

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
	trailingFilenameNoiseRE = regexp.MustCompile(`(?i)(?:[\s_.-]+(?:\d{3,4}p|[248]k|fhd|uhd|hdr10?|dv|dolbyvision|hevc|h26[45]|x26[45]|av1|aac|flac|web[-_.]?dl|webrip|bdrip|bluray|uncensored|uncen|sub|subs|subtitle|字幕|中字|中文字幕|中文|无码))+$`)
	trailingPartNumberRE    = regexp.MustCompile(`^(.+?)[_-]([1-9]\d?)$`)
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

	if ids := uniqueNormalizedCatalogIDs(extractStandaloneCatalogCandidates(query)); len(ids) == 1 {
		return PreparedTitleInput{Raw: raw, Query: query, Kind: TitleInputCatalog}
	}
	if looksOpaqueFilenameKey(query) {
		return PreparedTitleInput{Raw: raw, Query: query, Kind: TitleInputOpaque}
	}
	return PreparedTitleInput{Raw: raw, Query: query, Kind: TitleInputTitle}
}

func looksOpaqueFilenameKey(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	runes := []rune(s)
	if len(runes) < 8 || len(runes) > 180 {
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
		// Long numeric IDs (tweet/snowflake/content IDs) are strong opaque keys.
		return len(runes) >= 10
	}
	if letters == 0 || digits == 0 {
		return false
	}

	// Mixed single-token identifiers become opaque once they are long enough,
	// or when an underscore strongly suggests a generated/download key.
	return len(runes) >= 12 || strings.ContainsRune(s, '_')
}
