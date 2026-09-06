package scrape

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var keepWordsTemplateRE = regexp.MustCompile(`(?i)<KEEPWORDS:([^>]*)>`)

// extractKeepWordsFromTemplate returns the allow-list words from every
// <KEEPWORDS:...> tag in the output filename template. Modifier options begin
// at the first semicolon and are not part of the word list.
func extractKeepWordsFromTemplate(template string) []string {
	matches := keepWordsTemplateRE.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	words := make([]string, 0)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		payload := match[1]
		if i := strings.IndexByte(payload, ';'); i >= 0 {
			payload = payload[:i]
		}
		for _, raw := range strings.Split(payload, "|") {
			word := strings.TrimSpace(norm.NFKC.String(raw))
			if word == "" {
				continue
			}
			key := strings.ToLower(word)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			words = append(words, word)
		}
	}
	return words
}

// stripConfiguredKeepWords removes KEEPWORDS annotations from the search-only
// copy of a filename. The original filename is untouched, so KEEPWORDS still
// remain available to the output template after metadata is resolved.
//
// Pure ASCII alphanumeric words use token boundaries so a configured "AI"
// does not remove the "ai" inside a real title word such as "MAID". Tokens
// containing punctuation or non-ASCII characters are treated as literal
// annotations and removed case-insensitively.
func stripConfiguredKeepWords(input string, words []string) string {
	if strings.TrimSpace(input) == "" || len(words) == 0 {
		return input
	}

	s := norm.NFKC.String(input)
	ordered := append([]string(nil), words...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return len([]rune(ordered[i])) > len([]rune(ordered[j]))
	})

	for _, raw := range ordered {
		word := strings.TrimSpace(norm.NFKC.String(raw))
		if word == "" {
			continue
		}
		if isASCIIAlnum(word) {
			// Keep both surrounding characters while deleting only the token.
			// Repeat because one match can consume the delimiter needed for an
			// immediately adjacent second occurrence.
			re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(word) + `([^A-Za-z0-9]|$)`)
			for {
				next := re.ReplaceAllString(s, "$1 $2")
				if next == s {
					break
				}
				s = next
			}
			continue
		}
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(word))
		s = re.ReplaceAllString(s, " ")
	}

	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}

func isASCIIAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// resolveTitleViaWebWithConfiguredNoise removes the user's KEEPWORDS from the
// search query before the normal web-first title resolver runs. Catalog IDs are
// never modified.
func (s *Scraper) resolveTitleViaWebWithConfiguredNoise(ctx context.Context, cmd ScrapeCmd) ScrapeCmd {
	query := strings.TrimSpace(cmd.MovieID)
	if query == "" || looksLikeCatalogID(query) {
		return s.resolveTitleViaWeb(ctx, cmd)
	}

	var words []string
	if s != nil && s.cfg != nil {
		words = s.cfg.FilenameKeepWords
	}
	cleaned := stripConfiguredKeepWords(query, words)
	if cleaned != "" {
		cmd.MovieID = cleaned
	}
	return s.resolveTitleViaWeb(ctx, cmd)
}
