//go:build keepwords

package scrape

import (
	"strings"
	"testing"
)

func TestBuildTitleWebQueriesPreservesFastPathAndAddsTargetedQueries(t *testing.T) {
	queries := buildTitleWebQueries("完全な日本語タイトル")
	if len(queries) < 7 {
		t.Fatalf("buildTitleWebQueries returned too few queries: %v", queries)
	}
	if queries[0] != "完全な日本語タイトル" {
		t.Fatalf("first query = %q", queries[0])
	}
	if queries[1] != "完全な日本語タイトル 品番" {
		t.Fatalf("second query = %q", queries[1])
	}

	joined := strings.Join(queries, "\n")
	for _, want := range []string{
		`"完全な日本語タイトル" 品番`,
		"site:dmm.co.jp",
		"site:fanza.co.jp",
		"site:mgstage.com",
		"site:r18.dev",
		"site:javdb.com",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("query variants missing %q: %v", want, queries)
		}
	}
}

func TestTrustedDMMCompactContentIDNormalizesToCatalogID(t *testing.T) {
	results := []titleWebSearchResult{
		{
			Title:   "完全な日本語タイトル",
			Snippet: "完全な日本語タイトルの商品ページ",
			URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ssis00001/",
		},
	}
	got, ok := chooseCatalogCandidate("完全な日本語タイトル", results)
	if !ok {
		t.Fatal("trusted DMM URL candidate was not accepted")
	}
	if got != "SSIS-001" {
		t.Fatalf("candidate = %q, want SSIS-001", got)
	}
}

func TestTrustedURLCompactIDSupportsMakerPrefix(t *testing.T) {
	ids := extractTrustedURLCatalogCandidates("https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=h_086ssis001/")
	if len(ids) != 1 || ids[0] != "SSIS-001" {
		t.Fatalf("trusted URL IDs = %v, want [SSIS-001]", ids)
	}
}

func TestUntrustedCompactURLDoesNotBecomeCatalogID(t *testing.T) {
	ids := extractTrustedURLCatalogCandidates("https://example.com/detail/cid=ssis00001/")
	if len(ids) != 0 {
		t.Fatalf("untrusted URL unexpectedly yielded IDs: %v", ids)
	}
}

func TestWeakUnrelatedTrustedURLStillRejected(t *testing.T) {
	results := []titleWebSearchResult{
		{
			Title:   "まったく別の作品",
			Snippet: "無関係な検索結果",
			URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ssis00001/",
		},
	}
	if got, ok := chooseCatalogCandidate("完全な日本語タイトル", results); ok {
		t.Fatalf("weak unrelated result resolved to %q; want unresolved", got)
	}
}

func TestMergeTitleWebResultsDeduplicatesRepeatedCards(t *testing.T) {
	first := titleWebSearchResult{Title: "作品 ABC-123", Snippet: "説明", URL: "https://www.dmm.co.jp/example"}
	second := titleWebSearchResult{Title: "別作品 XYZ-999", Snippet: "説明", URL: "https://www.dmm.co.jp/other"}
	got := mergeTitleWebResults([]titleWebSearchResult{first}, []titleWebSearchResult{first, second})
	if len(got) != 2 {
		t.Fatalf("merged result count = %d, want 2: %#v", len(got), got)
	}
}

func TestCorroboratedCandidateBeatsSingleCompetitor(t *testing.T) {
	query := "学校帰りの完全な日本語タイトル"
	results := []titleWebSearchResult{
		{Title: query + " SSIS-001", Snippet: "品番 SSIS-001", URL: "https://www.dmm.co.jp/a"},
		{Title: query + " SSIS-001", Snippet: "作品 SSIS-001", URL: "https://www.javdb.com/b"},
		{Title: query + " SSIS-002", Snippet: "候補 SSIS-002", URL: "https://example.com/c"},
	}
	got, ok := chooseCatalogCandidate(query, results)
	if !ok {
		t.Fatal("corroborated candidate was rejected")
	}
	if got != "SSIS-001" {
		t.Fatalf("candidate = %q, want SSIS-001", got)
	}
}
