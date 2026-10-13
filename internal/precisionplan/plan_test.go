package precisionplan

import (
	"reflect"
	"testing"
)

func file(path, sku, status, id, title string) File {
	return File{Path: path, ParsedSKU: sku, CurrentStatus: status, WorkID: id, OriginalTitle: title}
}
func trusted(f File) Verdict {
	return Verdict{State: Confirmed, PolicyVersion: PolicyVersion, SKU: f.ParsedSKU,
		WorkID: f.WorkID, JapaneseTitle: f.OriginalTitle, Verified: true}
}
func TestPlanSelectiveExtraction(t *testing.T) {
	safe := file("z/safe.mp4", "START-487-EC", "completed", "start487ec", "日本語の原題")
	old := file("a/old.mp4", "SNIS-999BOD", "completed", "snis999bod", "正式な日本語原題")
	eng := file("b/english.mp4", "FSDSS-999TK", "completed", "fsdss999tk", "English original")
	failed := file("c/failed.mp4", "SIRO-5622", "failed", "", "")
	random := file("d/random.mp4", "123456789", "failed", "", "")
	files := []File{safe, old, eng, failed, random}
	verdicts := map[string]Verdict{safe.Path: trusted(safe), old.Path: trusted(old),
		eng.Path:    {State: Review, Reasons: []string{"missing_japanese_original"}},
		failed.Path: {State: Unknown}, random.Path: {State: Unknown}}
	verdicts[old.Path] = Verdict{State: Confirmed, PolicyVersion: "legacy", SKU: old.ParsedSKU,
		WorkID: old.WorkID, JapaneseTitle: old.OriginalTitle, Verified: true}
	plan, err := Build(files, verdicts)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ReadOnly || plan.Total != 5 || len(plan.Ready) != 1 || len(plan.Held) != 4 || len(plan.Supplemental) != 2 {
		t.Fatalf("wrong plan summary: %+v", plan)
	}
	if plan.Ready[0].Path != safe.Path || plan.Ready[0].SKU != "START-487-EC" {
		t.Fatalf("unsafe ready item: %+v", plan.Ready)
	}
	wantSupp := []SupplementalItem{{Path: eng.Path, SKU: eng.ParsedSKU, Reason: "japanese_original_missing"},
		{Path: failed.Path, SKU: failed.ParsedSKU, Reason: "upstream_search_failed"}}
	if !reflect.DeepEqual(plan.Supplemental, wantSupp) {
		t.Fatalf("got supplemental=%+v want=%+v", plan.Supplemental, wantSupp)
	}
}
func TestPlanUnsafeConfirmedVariants(t *testing.T) {
	a := file("sample.mp4", "START-487-EC", "completed", "real-edition", "版違いの日本語原題")
	for _, tc := range []struct {
		name   string
		mutate func(*File, *Verdict)
	}{
		{"verdict from legacy policy", func(_ *File, v *Verdict) { v.PolicyVersion = "old" }},
		{"not verified", func(_ *File, v *Verdict) { v.Verified = false }},
		{"suffix stripped", func(_ *File, v *Verdict) { v.SKU = "START-487" }},
		{"work identity differs", func(_ *File, v *Verdict) { v.WorkID = "wrong" }},
		{"English verdict title", func(_ *File, v *Verdict) { v.JapaneseTitle = "English only" }},
		{"source original title English", func(f *File, _ *Verdict) { f.OriginalTitle = "English" }},
		{"source/title mismatch", func(f *File, _ *Verdict) { f.OriginalTitle = "別作品の原題" }},
		{"upstream failed", func(f *File, _ *Verdict) { f.CurrentStatus = "failed" }},
		{"bad code", func(f *File, _ *Verdict) { f.ParsedSKU = "invalid-foo" }},
		{"missing workID", func(f *File, _ *Verdict) { f.WorkID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := a
			v := trusted(f)
			tc.mutate(&f, &v)
			got, err := Build([]File{f}, map[string]Verdict{f.Path: v})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Ready) != 0 || len(got.Held) != 1 || got.Held[0].State != Review {
				t.Fatalf("accepted unsafe confirmation: %+v", got)
			}
		})
	}
}
func TestPlanExclusionAndInvalidInput(t *testing.T) {
	a := file("safe.mp4", "FSDSS-999TK", "completed", "w1", "正式な日本語原題")
	a.Excluded = true
	p, err := Build([]File{a}, map[string]Verdict{a.Path: trusted(a)})
	if err != nil || len(p.Ready) != 0 || p.Excluded != 1 || len(p.Supplemental) != 0 {
		t.Fatalf("exclusion failed: %+v %v", p, err)
	}
	if _, err = Build([]File{a, a}, nil); err == nil {
		t.Fatal("duplicate file path not rejected")
	}
	if _, err = Build([]File{a}, map[string]Verdict{"absent.mp4": trusted(a)}); err == nil {
		t.Fatal("orphan verdict not rejected")
	}
	if _, err = Build([]File{{Path: ""}}, nil); err == nil {
		t.Fatal("empty path not rejected")
	}
}
func TestPlanNeverEquatesUpstreamCompletedToConfirmed(t *testing.T) {
	a := file("unverified.mp4", "MIUM-1423", "completed", "w1", "日本語原題")
	p, err := Build([]File{a}, nil)
	if err != nil || len(p.Ready) != 0 || len(p.Held) != 1 {
		t.Fatalf("completed without audit granted permission: %+v %v", p, err)
	}
}
func TestPlanDeterminism(t *testing.T) {
	a := file("b.mp4", "SIRO-5622", "failed", "", "")
	b := file("a.mp4", "DCV-093", "failed", "", "")
	p1, e1 := Build([]File{a, b}, map[string]Verdict{a.Path: {State: Unknown}, b.Path: {State: Unknown}})
	p2, e2 := Build([]File{b, a}, map[string]Verdict{a.Path: {State: Unknown}, b.Path: {State: Unknown}})
	if e1 != nil || e2 != nil || !reflect.DeepEqual(p1, p2) {
		t.Fatalf("nondeterministic result %+v %+v %v %v", p1, p2, e1, e2)
	}
}
