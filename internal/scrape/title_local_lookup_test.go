package scrape

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestLocalTitleRequiresJevAtThreshold(t *testing.T) {
	for _, tc := range []struct {
		name        string
		enabled     bool
		probability float64
		accepted    bool
	}{
		{"threshold", true, 0.80, true},
		{"below", true, 0.799, false},
		{"disabled", false, 0.96, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TYPESAFE_API_KEY", "")
			t.Setenv("JAVINIZER_JEV_CATALOG_THRESHOLD", "")
			calls := 0
			lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{{DVDID: "IPX-072", TitleJa: "目的の作品タイトル", Score: 0.99}}}
			resolver := NewTitleCatalogResolverWithLookup(&Config{JevCatalogEnabled: tc.enabled, JevCatalogAPIKey: "test-key", JevCatalogThreshold: 0.80, JevCatalogEndpoint: "http://127.0.0.1/v1/systemone"}, lookup)
			resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return jevLookupResponse(200, `{"answers":{"catalog_id_correct":{"type":"noul","noul":`+fmtProbability(tc.probability)+`}}}`), nil
			})
			id, ok := resolver.resolveFromLocalTitle(context.Background(), "目的の作品タイトル")
			if ok != tc.accepted {
				t.Fatalf("id=%s accepted=%v, want %v", id, ok, tc.accepted)
			}
			if tc.enabled && calls == 0 {
				t.Fatal("local title bypassed Jev")
			}
		})
	}
}

func TestLocalTitleFallbackToExistingWeb(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		matches  []models.DumpTitleMatch
		err      error
	}{
		{name: "miss", err: models.ErrDumpMiss},
		{name: "unavailable", err: models.ErrDumpTitleSearchUnavailable},
		{name: "malformed", response: "not-json", matches: []models.DumpTitleMatch{{DVDID: "ABC-123", TitleJa: "狙われた通学路 共謀痴漢電車", Score: 0.99}}},
		{name: "http_error", response: "http-error", matches: []models.DumpTitleMatch{{DVDID: "ABC-123", TitleJa: "狙われた通学路 共謀痴漢電車", Score: 0.99}}},
		{name: "rejected", matches: []models.DumpTitleMatch{{DVDID: "ABC-123", TitleJa: "狙われた通学路 共謀痴漢電車", Score: 0.99}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sawJev := false
			scraper := newWebToJevLookupScraper(t, 0.80, &sawJev)
			original := scraper.httpClient
			localCalls := 0
			scraper.httpClient = jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost {
					var payload jevSystemOneRequest
					body, _ := io.ReadAll(req.Body)
					_ = json.Unmarshal(body, &payload)
					req.Body = io.NopCloser(bytes.NewReader(body))
					if payload.State.CandidateCatalogID == "ABC-123" {
						localCalls++
						if tc.response == "http-error" {
							return jevLookupResponse(503, ""), nil
						}
						if tc.response != "" {
							return jevLookupResponse(200, tc.response), nil
						}
						return jevLookupResponse(200, `{"answers":{"catalog_id_correct":{"type":"noul","noul":0.1}}}`), nil
					}
				}
				return original.Do(req)
			})
			resolver := &TitleCatalogResolver{scraper: scraper, titleLookup: &fakeTitleLookup{matches: tc.matches, err: tc.err}}
			id, err := resolver.Resolve(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
			if err != nil || id != "IPX-072" || !sawJev {
				t.Fatalf("id=%s err=%v web Jev=%v", id, err, sawJev)
			}
			if len(tc.matches) > 0 && localCalls != 1 {
				t.Fatalf("local Jev calls=%d", localCalls)
			}
		})
	}
}

func TestLocalTitleCancellationPreventsWeb(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, &fakeTitleLookup{err: context.Canceled})
	resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled local search started Web")
		return nil, context.Canceled
	})
	id, err := resolver.Resolve(ctx, "目的の作品タイトル")
	if id != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("id=%s err=%v", id, err)
	}
}
