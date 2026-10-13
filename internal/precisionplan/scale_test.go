package precisionplan

import (
	"fmt"
	"testing"
)

func largeFixtures(n int) ([]File, map[string]Verdict) {
	files := make([]File, 0, n)
	verdicts := make(map[string]Verdict, n)
	for i := 0; i < n; i++ {
		// Ten recurring but intact SKUs ensure the query planner actually
		// coalesces duplicate lookups without losing file identity.
		sku := fmt.Sprintf("FSDSS-%03dTK", i%10+100)
		path := fmt.Sprintf("video/%05d.mp4", i)
		f := File{Path: path, ParsedSKU: sku, WorkID: fmt.Sprintf("work-%03d", i%10),
			CurrentStatus: "completed", OriginalTitle: "検証済み日本語原題"}
		var v Verdict
		switch i % 4 {
		case 0, 1:
			v = trusted(f)
		case 2:
			f.OriginalTitle = "English title"
			v = Verdict{State: Review, PolicyVersion: PolicyVersion, Reasons: []string{"japanese_original_missing"}}
		case 3:
			f.CurrentStatus = "failed"
			f.WorkID = ""
			f.OriginalTitle = ""
			v = Verdict{State: Unknown, PolicyVersion: PolicyVersion}
		}
		files = append(files, f)
		verdicts[path] = v
	}
	return files, verdicts
}

func TestPlanSynthetic7000NoUnverifiedReady(t *testing.T) {
	files, verdicts := largeFixtures(7000)
	p, err := Build(files, verdicts)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Ready) != 3500 || len(p.Held) != 3500 || len(p.Supplemental) != 3500 {
		t.Fatalf("bad 7000-item counts: ready=%d held=%d supplemental=%d",
			len(p.Ready), len(p.Held), len(p.Supplemental))
	}
	q := p.Queries()
	if len(q) != 10 {
		t.Fatalf("dedup should produce 10 SKU queries, got %d", len(q))
	}
	totalLinked := 0
	for _, g := range q {
		totalLinked += len(g.FilePaths)
	}
	if totalLinked != 3500 {
		t.Fatalf("query groups dropped file paths: %d", totalLinked)
	}
	for _, e := range p.Ready {
		if e.PolicyVersion != PolicyVersion || e.WorkID == "" || e.JapaneseTitle == "" {
			t.Fatalf("unsafe selection: %+v", e)
		}
	}
}

func BenchmarkPlan7000(b *testing.B) {
	files, verdicts := largeFixtures(7000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := Build(files, verdicts)
		if err != nil || len(result.Ready) != 3500 {
			b.Fatalf("bad plan: %v", err)
		}
	}
}
