package precisionplan

import "sort"

// QueryGroup describes ONE lookup per exact, suffix-preserving SKU.
// FilePaths references each distinct originating file, never the lexically
// first path for that SKU. Search results remain unverified candidates.
type QueryGroup struct {
	SKU       string
	FilePaths []string
	Reasons   []string
	Stages    []string
	ReadOnly  bool
}

var fallbackStages = []string{"local_db_all_matches", "selected_official_scrapers", "official_web_page"}

// Queries coalesces duplicate requests without conflating distinct edition
// suffixes. Providers must verify their own identity and Japanese source title.
func (p Plan) Queries() []QueryGroup {
	bySKU := make(map[string]*QueryGroup)
	for _, candidate := range p.Supplemental {
		sku := exactSKU(candidate.SKU)
		if !plausibleSKU(sku) {
			continue
		}
		group, ok := bySKU[sku]
		if !ok {
			group = &QueryGroup{SKU: sku, Stages: append([]string(nil), fallbackStages...), ReadOnly: true}
			bySKU[sku] = group
		}
		group.FilePaths = append(group.FilePaths, candidate.Path)
		found := false
		for _, reason := range group.Reasons {
			if reason == candidate.Reason {
				found = true
				break
			}
		}
		if !found {
			group.Reasons = append(group.Reasons, candidate.Reason)
		}
	}
	keys := make([]string, 0, len(bySKU))
	for sku := range bySKU {
		keys = append(keys, sku)
	}
	sort.Strings(keys)
	groups := make([]QueryGroup, 0, len(keys))
	for _, sku := range keys {
		group := *bySKU[sku]
		sort.Strings(group.FilePaths)
		sort.Strings(group.Reasons)
		groups = append(groups, group)
	}
	return groups
}
