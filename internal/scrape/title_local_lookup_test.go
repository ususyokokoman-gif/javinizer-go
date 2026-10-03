package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

type fakeTitleLookup struct {
	matches []models.DumpTitleMatch
	err     error
	calls   int
}

func (f *fakeTitleLookup) SearchByTitle(_ context.Context, _ string, _ int) ([]models.DumpTitleMatch, error) {
	f.calls++
	return f.matches, f.err
}

func TestTitleCatalogResolverUsesLocalTitleThenJevWithoutWeb(t *testing.T) {
	jevCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jevCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":0.96}}}`))
	}))
	defer server.Close()

	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{
		{
			ContentID: "118ipx00072",
			DVDID:     "IPX-072",
			TitleJa:   "狙われた通学路 共謀痴漢電車",
			TitleEn:   "Targeted School Route",
			Score:     0.96,
		},
	}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{
		JevCatalogEnabled:   true,
		JevCatalogAPIKey:    "test-key",
		JevCatalogThreshold: 0.80,
		JevCatalogModel:     "jev-test",
		JevCatalogEndpoint:  server.URL,
	}, lookup)

	got, err := resolver.Resolve(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
	if err != nil {
		t.Fatal(err)
	}
	if got != "IPX-072" {
		t.Fatalf("resolved=%q, want IPX-072", got)
	}
	if lookup.calls != 1 {
		t.Fatalf("local lookup calls=%d, want 1", lookup.calls)
	}
	if jevCalls != 1 {
		t.Fatalf("Jev calls=%d, want 1", jevCalls)
	}
}

func TestTitleCatalogResolverSkipsWeakLocalCandidate(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{
		{DVDID: "ABC-123", TitleJa: "別作品", Score: 0.40},
	}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	if _, ok := resolver.resolveFromLocalTitle(context.Background(), "目的の作品タイトル"); ok {
		t.Fatal("weak local candidate must not be automatically accepted")
	}
}
