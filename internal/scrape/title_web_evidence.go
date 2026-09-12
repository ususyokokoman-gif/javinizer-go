package scrape

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var compactTrustedCatalogRE = regexp.MustCompile(`(?i)^(?:h_\d+)?([a-z]{2,12})(\d{3,7})$`)

// buildTitleWebQueries keeps the cheap broad queries first, then asks Google
// specifically for evidence from AV metadata sources. Google is discovery and
// corroboration only; URL-derived IDs are accepted only from trusted hosts.
func buildTitleWebQueries(title string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	quoted := `"` + strings.ReplaceAll(title, `"`, "") + `"`
	queries := []string{
		title,
		title + " 品番",
		quoted + " 品番",
		"site:dmm.co.jp " + quoted,
		"site:fanza.co.jp " + quoted,
		"site:mgstage.com " + quoted,
		"site:r18.dev " + quoted,
		"site:javdb.com " + quoted,
	}
	seen := make(map[string]struct{}, len(queries))
	out := make([]string, 0, len(queries))
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	return out
}

func mergeTitleWebResults(groups ...[]titleWebSearchResult) []titleWebSearchResult {
	seen := make(map[string]struct{})
	out := make([]titleWebSearchResult, 0)
	for _, group := range groups {
		for _, result := range group {
			keyURL := strings.TrimSpace(result.URL)
			key := strings.ToLower(keyURL)
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(result.Title)) + "\x00" + strings.ToLower(strings.TrimSpace(result.Snippet))
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, result)
		}
	}
	return out
}

// trustedCatalogSource returns an independent evidence-family name. Hostname
// parsing is exact/suffix based so attacker-controlled names such as
// dmm.co.jp.example.com never become trusted.
func trustedCatalogSource(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	matches := func(base string) bool { return host == base || strings.HasSuffix(host, "."+base) }
	switch {
	case matches("dmm.co.jp"), matches("dmm.com"):
		return "dmm"
	case matches("fanza.co.jp"):
		return "fanza"
	case matches("javdb.com"):
		return "javdb"
	case matches("r18.dev"), matches("r18.com"):
		return "r18"
	case matches("mgstage.com"):
		return "mgs"
	case matches("javlibrary.com"):
		return "javlibrary"
	default:
		return ""
	}
}

func isKnownJAVResultURLStrict(raw string) bool {
	return trustedCatalogSource(raw) != ""
}

// extractTrustedURLCatalogCandidates deliberately refuses to inspect arbitrary
// URLs. It only examines individual path/query tokens from trusted hosts, so a
// search-engine redirect or a maker prefix can never become a phantom catalog ID.
func extractTrustedURLCatalogCandidates(raw string) []string {
	source := trustedCatalogSource(raw)
	if source == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	decodedPath, _ := url.PathUnescape(parsed.EscapedPath())
	pieces := []string{decodedPath}
	for key, values := range parsed.Query() {
		lowerKey := strings.ToLower(key)
		if lowerKey != "cid" && lowerKey != "id" && lowerKey != "dvd_id" && lowerKey != "product_id" && lowerKey != "productid" {
			continue
		}
		pieces = append(pieces, values...)
	}
	// DMM/FANZA encode cid inside path segments such as /=/cid=ssis00001/.
	if source == "dmm" || source == "fanza" {
		if idx := strings.Index(strings.ToLower(decodedPath), "cid="); idx >= 0 {
			tail := decodedPath[idx+4:]
			if cut := strings.IndexAny(tail, "/?&#"); cut >= 0 {
				tail = tail[:cut]
			}
			pieces = append(pieces, tail)
		}
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, 2)
	appendID := func(id string) {
		id = normalizeWebCatalogCandidate(id)
		if id == "" {
			return
		}
		key := catalogComparable(id)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	for _, piece := range pieces {
		for _, token := range strings.FieldsFunc(piece, func(r rune) bool {
			return r == '/' || r == '=' || r == '&' || r == '?' || r == '#' || r == ':' || r == ';'
		}) {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			// DMM compact forms (ssis00001, h_086ssis001) must be handled
			// before the generic parser, otherwise h_086 can be mistaken for
			// a real H-086... catalog prefix.
			if id := normalizeCompactTrustedCatalogID(token); id != "" {
				appendID(id)
				continue
			}
			for _, id := range extractCatalogCandidates(token) {
				appendID(id)
			}
		}
	}
	return out
}

func normalizeCompactTrustedCatalogID(raw string) string {
	s := strings.ToLower(strings.TrimSpace(norm.NFKC.String(raw)))
	s = strings.Trim(s, "-_. /\\()[]{}")
	match := compactTrustedCatalogRE.FindStringSubmatch(s)
	if len(match) != 3 {
		return ""
	}
	prefix := strings.ToUpper(match[1])
	n, err := strconv.Atoi(match[2])
	if err != nil || n <= 0 {
		return ""
	}
	digits := strconv.Itoa(n)
	if len(digits) < 3 {
		digits = strings.Repeat("0", 3-len(digits)) + digits
	}
	return normalizeWebCatalogCandidate(prefix + "-" + digits)
}

func candidateTrustedSources(id string, results []titleWebSearchResult) map[string]struct{} {
	sources := make(map[string]struct{})
	want := catalogComparable(id)
	if want == "" {
		return sources
	}
	for _, result := range results {
		source := trustedCatalogSource(result.URL)
		if source == "" {
			continue
		}
		matched := containsCatalogID(result.Title, id) || containsCatalogID(result.Snippet, id)
		if !matched {
			for _, urlID := range extractTrustedURLCatalogCandidates(result.URL) {
				if catalogComparable(urlID) == want {
					matched = true
					break
				}
			}
		}
		if matched {
			sources[source] = struct{}{}
		}
	}
	return sources
}

func candidateHasTrustedEvidence(id string, results []titleWebSearchResult) bool {
	return len(candidateTrustedSources(id, results)) > 0
}
