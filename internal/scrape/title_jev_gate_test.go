package scrape

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJevCatalogGateAcceptsAtThreshold(t *testing.T) {
	var gotAuth string
	var gotRequest jevSystemOneRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":0.80}},"usage":{"input_tokens":10,"output_tokens":1}}`))
	}))
	defer server.Close()

	s := &Scraper{
		httpClient: server.Client(),
		cfg: &Config{
			JevCatalogAPIKey:    "test-key",
			JevCatalogThreshold: 0.80,
			JevCatalogModel:     "jev-test",
			JevCatalogEndpoint:  server.URL,
		},
	}

	got, err := s.finalizeCatalogCandidate(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな", "IPX-072", []titleWebSearchResult{{
		Title:   "狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072",
		Snippet: "品番 IPX-072",
		URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00072/",
	}})
	if err != nil {
		t.Fatalf("finalizeCatalogCandidate returned error: %v", err)
	}
	if got != "IPX-072" {
		t.Fatalf("finalizeCatalogCandidate = %q, want IPX-072", got)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotRequest.Model != "jev-test" {
		t.Fatalf("model = %q", gotRequest.Model)
	}
	if gotRequest.State.CandidateCatalogID != "IPX-072" {
		t.Fatalf("candidate = %q", gotRequest.State.CandidateCatalogID)
	}
	q, ok := gotRequest.Questions["catalog_id_correct"]
	if !ok || q.Type != "noul" {
		t.Fatalf("catalog_id_correct question = %#v", q)
	}
}

func TestJevCatalogGateRejectsBelowThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":0.799}},"usage":{"input_tokens":10,"output_tokens":1}}`))
	}))
	defer server.Close()

	s := &Scraper{
		httpClient: server.Client(),
		cfg: &Config{
			JevCatalogAPIKey:    "test-key",
			JevCatalogThreshold: 0.80,
			JevCatalogModel:     "jev-test",
			JevCatalogEndpoint:  server.URL,
		},
	}

	_, err := s.finalizeCatalogCandidate(context.Background(), "今日、あなたの上司に犯されました。 大橋未久", "MIDE-007", nil)
	if err == nil {
		t.Fatal("expected Jev rejection error")
	}
	if !strings.Contains(err.Error(), "below threshold 0.800") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJevCatalogGateFailsClosedOnAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	s := &Scraper{
		httpClient: server.Client(),
		cfg: &Config{
			JevCatalogAPIKey:    "test-key",
			JevCatalogThreshold: 0.80,
			JevCatalogModel:     "jev-test",
			JevCatalogEndpoint:  server.URL,
		},
	}

	_, err := s.finalizeCatalogCandidate(context.Background(), "作品タイトル", "SSIS-001", nil)
	if err == nil {
		t.Fatal("expected fail-closed error")
	}
	if !strings.Contains(err.Error(), "Jev catalog validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJevCatalogGateDisabledWithoutAPIKeyPreservesCandidate(t *testing.T) {
	s := &Scraper{cfg: &Config{}}
	got, err := s.finalizeCatalogCandidate(context.Background(), "作品タイトル", "ssis-001", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "SSIS-001" {
		t.Fatalf("candidate = %q, want SSIS-001", got)
	}
}

func TestApplyJevCatalogGateEnvDefaultsToPointEight(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "secret")
	t.Setenv("JAVINIZER_JEV_CATALOG_THRESHOLD", "")
	t.Setenv("JAVINIZER_JEV_MODEL", "")
	t.Setenv("JAVINIZER_JEV_ENDPOINT", "")

	cfg := &Config{}
	applyJevCatalogGateEnv(cfg)

	if cfg.JevCatalogAPIKey != "secret" {
		t.Fatalf("api key not loaded")
	}
	if cfg.JevCatalogThreshold != 0.80 {
		t.Fatalf("threshold = %.3f, want 0.80", cfg.JevCatalogThreshold)
	}
	if cfg.JevCatalogModel != "jev-latest" {
		t.Fatalf("model = %q, want jev-latest", cfg.JevCatalogModel)
	}
	if cfg.JevCatalogEndpoint != "https://api.typesafe.ai/v1/systemone" {
		t.Fatalf("endpoint = %q", cfg.JevCatalogEndpoint)
	}
}

func TestBuildJevCatalogEvidenceKeepsRelevantTrustedEvidence(t *testing.T) {
	results := []titleWebSearchResult{
		{
			Title:   "狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072",
			Snippet: "品番 IPX-072",
			URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00072/",
		},
		{
			Title:   "無関係な作品 ABC-999",
			Snippet: "別作品",
			URL:     "https://example.com/other",
		},
	}

	got := buildJevCatalogEvidence("狙われた通学路 共謀痴漢電車 桃乃木かな", "IPX-072", results)
	if len(got) != 1 {
		t.Fatalf("evidence count = %d, want 1: %#v", len(got), got)
	}
	if got[0].Source != "dmm" {
		t.Fatalf("source = %q, want dmm", got[0].Source)
	}
	if len(got[0].CatalogIDs) != 1 || got[0].CatalogIDs[0] != "IPX-072" {
		t.Fatalf("catalog IDs = %#v", got[0].CatalogIDs)
	}
}
