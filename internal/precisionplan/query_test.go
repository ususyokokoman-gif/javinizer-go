package precisionplan

import (
	"reflect"
	"testing"
)

func TestQueryGroupsDeduplicateExactSKUWithoutLosingFilePaths(t *testing.T) {
	p := Plan{Supplemental: []SupplementalItem{
		{Path: "z/file2.mp4", SKU: "START-487-EC", Reason: "upstream_search_failed"},
		{Path: "a/file1.mp4", SKU: "START-487-EC", Reason: "japanese_original_missing"},
		{Path: "b/base.mp4", SKU: "START-487", Reason: "japanese_original_missing"},
		{Path: "c/random.mp4", SKU: "123456", Reason: "unknown_id"},
	}}
	groups := p.Queries()
	if len(groups) != 2 {
		t.Fatalf("expected 2 distinct suffix-preserving queries: %+v", groups)
	}
	if groups[0].SKU != "START-487" || groups[1].SKU != "START-487-EC" {
		t.Fatalf("suffix collapse: %+v", groups)
	}
	want := []string{"a/file1.mp4", "z/file2.mp4"}
	if !reflect.DeepEqual(groups[1].FilePaths, want) {
		t.Fatalf("file identity loss: got=%v", groups[1].FilePaths)
	}
	if !groups[1].ReadOnly || len(groups[1].Stages) != 3 {
		t.Fatalf("unsafe provider query plan: %+v", groups[1])
	}
	if len(groups[1].Reasons) != 2 {
		t.Fatalf("lost candidate reasons: %+v", groups[1].Reasons)
	}
}
func TestQueryGroupsDeterminismAndNoWebForOpaque(t *testing.T) {
	a := Plan{Supplemental: []SupplementalItem{
		{Path: "p2", SKU: "DCV-093", Reason: "missing"},
		{Path: "p1", SKU: "DCV-093", Reason: "missing"},
		{Path: "p3", SKU: "CARIBBEANCOM-081625", Reason: "failed"},
		{Path: "p4", SKU: "Merged-1767081655227", Reason: "unknown"},
	}}
	b := Plan{Supplemental: []SupplementalItem{a.Supplemental[2], a.Supplemental[1], a.Supplemental[0], a.Supplemental[3]}}
	if !reflect.DeepEqual(a.Queries(), b.Queries()) {
		t.Fatal("nondeterministic batch grouping")
	}
	for _, g := range a.Queries() {
		if g.SKU == "MERGED-1767081655227" {
			t.Fatal("random ID queried")
		}
	}
}
