package scrape

import (
	"sort"
	"strings"
)

func chooseCatalogCandidate(query string, results []titleWebSearchResult) (string, bool) {
	if len(results) == 0 { return "", false }
	results = mergeTitleWebResults(results)
	scores := make(map[string]float64)
	occurrences := make(map[string]int)
	trusted := make(map[string]map[string]struct{})
	for rank, result := range results {
		combined := strings.TrimSpace(result.Title + " " + result.Snippet)
		coverage := queryCoverage(query, combined)
		if len([]rune(compactComparable(query))) >= 8 && coverage < 0.12 { continue }
		textIDs := extractCatalogCandidates(combined)
		urlIDs := extractTrustedURLCatalogCandidates(result.URL)
		ids := append(append([]string{}, textIDs...), urlIDs...)
		seen := make(map[string]struct{})
		source := trustedCatalogSource(result.URL)
		for _, id := range ids {
			key := catalogComparable(id)
			if key == "" { continue }
			if _, ok := seen[key]; ok { continue }
			seen[key] = struct{}{}
			score := coverage*3.0 + 0.75/float64(rank+1)
			if containsCatalogID(result.Title, id) { score += 1.5 }
			if containsCatalogID(result.Snippet, id) { score += 0.75 }
			if q, r := compactComparable(query), compactComparable(combined); q != "" && strings.Contains(r, q) { score += 1.0 }
			if source != "" {
				score += 1.5
				if trusted[id] == nil { trusted[id] = make(map[string]struct{}) }
				trusted[id][source] = struct{}{}
			}
			for _, urlID := range urlIDs { if catalogComparable(urlID) == key { score += 3.0; break } }
			scores[id] += score
			occurrences[id]++
		}
	}
	for id, sources := range trusted { if len(sources) > 1 { scores[id] += float64(len(sources)-1)*2.0 } }
	ranked := make([]scoredCatalogCandidate, 0, len(scores))
	for id, score := range scores {
		if len(trusted[id]) == 0 && occurrences[id] < 2 { continue }
		ranked = append(ranked, scoredCatalogCandidate{ID:id, Score:score})
	}
	if len(ranked) == 0 { return "", false }
	sort.Slice(ranked, func(i,j int) bool { if ranked[i].Score == ranked[j].Score { return ranked[i].ID < ranked[j].ID }; return ranked[i].Score > ranked[j].Score })
	if ranked[0].Score < 4.0 { return "", false }
	if len(ranked) > 1 {
		margin := ranked[0].Score-ranked[1].Score
		if margin < 1.0 || ranked[1].Score >= ranked[0].Score*0.85 { return "", false }
	}
	return ranked[0].ID, true
}


// chooseJevFastCandidate intentionally uses a lower deterministic evidence bar
// than chooseCatalogCandidate. It never accepts a candidate by itself: the
// candidate must still pass the Jev >= configured threshold gate. This lets
// Jev act as a cheap System-One early-exit gate before expensive multi-provider
// corroboration or headless-browser fallback.
func chooseJevFastCandidate(query string, results []titleWebSearchResult) (string, bool) {
	results = mergeTitleWebResults(results)
	if len(results) == 0 {
		return "", false
	}

	scores := make(map[string]float64)
	display := make(map[string]string)
	for rank, result := range results {
		combined := strings.TrimSpace(result.Title + " " + result.Snippet)
		coverage := queryCoverage(query, combined)
		if coverage < 0.45 {
			continue
		}
		ids := append(extractCatalogCandidates(combined), extractTrustedURLCatalogCandidates(result.URL)...)
		seen := make(map[string]struct{})
		for _, id := range ids {
			id = normalizeWebCatalogCandidate(id)
			key := catalogComparable(id)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			display[key] = id

			score := coverage*4.0 + 0.8/float64(rank+1)
			if containsCatalogID(result.Title, id) {
				score += 1.5
			}
			if containsCatalogID(result.Snippet, id) {
				score += 0.75
			}
			if trustedCatalogSource(result.URL) != "" {
				score += 1.25
			}
			for _, urlID := range extractTrustedURLCatalogCandidates(result.URL) {
				if catalogComparable(urlID) == key {
					score += 2.0
					break
				}
			}
			scores[key] += score
		}
	}

	ranked := make([]scoredCatalogCandidate, 0, len(scores))
	for key, score := range scores {
		if score >= 3.0 {
			ranked = append(ranked, scoredCatalogCandidate{ID: display[key], Score: score})
		}
	}
	if len(ranked) == 0 {
		return "", false
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].Score > ranked[j].Score
	})
	if len(ranked) > 1 {
		margin := ranked[0].Score - ranked[1].Score
		if margin < 1.25 || ranked[1].Score >= ranked[0].Score*0.88 {
			return "", false
		}
	}
	return ranked[0].ID, true
}
