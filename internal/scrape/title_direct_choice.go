package scrape

import "strings"

// chooseExactDirectTitleCandidate selects a catalog ID only when exactly one
// trusted direct-detail result has a title that is identical to the requested
// title after the same Unicode/alphanumeric normalization used elsewhere.
//
// This is intentionally much stricter than general web scoring. It exists for
// the case where a direct metadata source (currently JavDB) has re-opened real
// detail pages but Google corroboration is unavailable or rate-limited. If two
// different IDs share the same exact title, the result remains unresolved.
func chooseExactDirectTitleCandidate(query string, results []titleWebSearchResult) (string, bool) {
	queryKey := compactComparable(query)
	if queryKey == "" || len(results) == 0 {
		return "", false
	}

	matched := make(map[string]string)
	for _, result := range results {
		if trustedCatalogSource(result.URL) == "" {
			continue
		}
		if compactComparable(result.Title) != queryKey {
			continue
		}
		for key, id := range uniqueCatalogIDsForDirectRow(result) {
			matched[key] = id
		}
	}

	if len(matched) != 1 {
		return "", false
	}
	for _, id := range matched {
		return id, true
	}
	return "", false
}

// directTitleEvidenceIsAmbiguous detects the case where the user's normalized
// title is genuinely compatible with two or more verified detail pages from a
// trusted direct metadata source. In that situation a search-engine ranking is
// not evidence about which local file the user meant, so the resolver must fail
// closed instead of letting Google arbitrarily break the tie.
func directTitleEvidenceIsAmbiguous(query string, results []titleWebSearchResult) bool {
	queryKey := compactComparable(query)
	if queryKey == "" {
		return false
	}

	matched := make(map[string]struct{})
	for _, result := range results {
		if trustedCatalogSource(result.URL) == "" {
			continue
		}
		titleKey := compactComparable(result.Title)
		if titleKey == "" || !strings.Contains(titleKey, queryKey) {
			continue
		}
		rowIDs := uniqueCatalogIDsForDirectRow(result)
		if len(rowIDs) != 1 {
			continue
		}
		for key := range rowIDs {
			matched[key] = struct{}{}
		}
		if len(matched) > 1 {
			return true
		}
	}
	return false
}

func uniqueCatalogIDsForDirectRow(result titleWebSearchResult) map[string]string {
	ids := append([]string{}, extractCatalogCandidates(result.Title+" "+result.Snippet)...)
	ids = append(ids, extractTrustedURLCatalogCandidates(result.URL)...)
	rowIDs := make(map[string]string)
	for _, id := range ids {
		id = normalizeWebCatalogCandidate(id)
		key := catalogComparable(id)
		if id == "" || key == "" {
			continue
		}
		rowIDs[key] = id
	}
	return rowIDs
}
