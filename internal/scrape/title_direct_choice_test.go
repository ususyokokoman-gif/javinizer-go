package scrape

import "testing"

func TestChooseExactDirectTitleCandidateSelectsUniqueExactDetailTitle(t *testing.T) {
	query := "狙われた通学路共謀痴漢電車"
	results := []titleWebSearchResult{
		{
			Title:   "狙われた通学路 共謀痴漢電車",
			Snippet: "狙われた通学路 共謀痴漢電車 品番 IPX-072",
			URL:     "https://javdb.com/v/exact",
		},
		{
			Title:   "狙われた通学路 共謀痴漢電車 完全版",
			Snippet: "関連作品 品番 IPX-073",
			URL:     "https://javdb.com/v/related",
		},
		{
			Title:   "別作品",
			Snippet: "品番 IPX-074",
			URL:     "https://javdb.com/v/other",
		},
	}

	got, ok := chooseExactDirectTitleCandidate(query, results)
	if !ok {
		t.Fatal("unique exact trusted detail title was not selected")
	}
	if got != "IPX-072" {
		t.Fatalf("candidate = %q, want IPX-072", got)
	}
}

func TestChooseExactDirectTitleCandidateRejectsDuplicateExactTitles(t *testing.T) {
	query := "今日、あなたの上司に犯されました。"
	results := []titleWebSearchResult{
		{Title: query, Snippet: "品番 MIDE-007", URL: "https://javdb.com/v/a"},
		{Title: query, Snippet: "品番 MIDE-008", URL: "https://javdb.com/v/b"},
	}

	if got, ok := chooseExactDirectTitleCandidate(query, results); ok {
		t.Fatalf("ambiguous duplicate exact titles resolved to %q", got)
	}
}

func TestChooseExactDirectTitleCandidateRejectsUntrustedExactTitle(t *testing.T) {
	query := "完全な日本語タイトル"
	results := []titleWebSearchResult{
		{Title: query, Snippet: "品番 SSIS-001", URL: "https://javdb.com.evil.example/v/a"},
	}

	if got, ok := chooseExactDirectTitleCandidate(query, results); ok {
		t.Fatalf("untrusted exact title resolved to %q", got)
	}
}

func TestChooseExactDirectTitleCandidateRejectsAmbiguousRowIDs(t *testing.T) {
	query := "完全な日本語タイトル"
	results := []titleWebSearchResult{
		{Title: query, Snippet: "品番 SSIS-001 関連 SSIS-002", URL: "https://javdb.com/v/a"},
	}

	if got, ok := chooseExactDirectTitleCandidate(query, results); ok {
		t.Fatalf("row containing multiple IDs resolved to %q", got)
	}
}
