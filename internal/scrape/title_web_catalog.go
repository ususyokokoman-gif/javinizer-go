package scrape

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func isKnownJAVResultURL(raw string) bool { return isKnownJAVResultURLStrict(raw) }

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

// extractStandaloneCatalogCandidates is deliberately stricter than
// extractCatalogCandidates. It is used for the deterministic filename fast
// path, where a false positive is much worse than a miss. A catalog-looking
// substring embedded inside an opaque release/download identifier must not be
// auto-accepted merely because the regex can see it.
func extractStandaloneCatalogCandidates(text string) []string {
	if decoded, err := url.QueryUnescape(text); err == nil {
		text += " " + decoded
	}
	text = norm.NFKC.String(text)
	locs := webCatalogCandidateRE.FindAllStringIndex(text, -1)
	seen := make(map[string]struct{}, len(locs))
	out := make([]string, 0, len(locs))
	for _, loc := range locs {
		if len(loc) != 2 || !catalogTokenHasSafeBoundaries(text, loc[0], loc[1]) {
			continue
		}
		id := normalizeWebCatalogCandidate(text[loc[0]:loc[1]])
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

func catalogTokenHasSafeBoundaries(text string, start, end int) bool {
	isIdentifierRune := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:start])
		if isIdentifierRune(r) {
			return false
		}
	}
	if end < len(text) {
		r, _ := utf8.DecodeRuneInString(text[end:])
		if isIdentifierRune(r) {
			return false
		}
	}
	return true
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
	qSet, rSet := ngramSet(q, n), ngramSet(r, n)
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
