package scrape

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type jevLookupHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f jevLookupHTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jevLookupResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newWebToJevLookupScraper(t *testing.T, probability float64, sawJev *bool) *Scraper {
	t.Helper()
	title := "狙われた通学路 共謀痴漢電車 桃乃木かな"
	ddgHTML := `<html><body>
	<div class="result">
	  <a class="result__a" href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00072/">狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072</a>
	  <div class="result__snippet">狙われた通学路 共謀痴漢電車 桃乃木かな 品番 IPX-072</div>
	</div>
	</body></html>`

	client := jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.EqualFold(req.URL.Hostname(), "www.google.com"):
			return jevLookupResponse(http.StatusNotFound, ""), nil
		case strings.Contains(strings.ToLower(req.URL.Hostname()), "duckduckgo.com"):
			return jevLookupResponse(http.StatusOK, ddgHTML), nil
		case req.URL.Hostname() == "127.0.0.1" && req.URL.Path == "/v1/systemone":
			*sawJev = true
			var payload jevSystemOneRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode Jev request: %v", err)
			}
			if payload.State.Title != title {
				t.Fatalf("Jev title = %q, want %q", payload.State.Title, title)
			}
			if payload.State.CandidateCatalogID != "IPX-072" {
				t.Fatalf("Jev candidate = %q, want IPX-072", payload.State.CandidateCatalogID)
			}
			if len(payload.State.Evidence) == 0 {
				t.Fatal("Jev request did not include Web evidence")
			}
			body := `{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":` +
				strings.TrimRight(strings.TrimRight(fmtProbability(probability), "0"), ".") +
				`}},"usage":{"input_tokens":20,"output_tokens":1}}`
			return jevLookupResponse(http.StatusOK, body), nil
		default:
			return jevLookupResponse(http.StatusNotFound, ""), nil
		}
	})

	return &Scraper{
		httpClient: client,
		cfg: &Config{
			JevCatalogEnabled:   true,
			JevCatalogAPIKey:    "test-key",
			JevCatalogThreshold: 0.80,
			JevCatalogModel:     "jev-test",
			JevCatalogEndpoint:  "http://127.0.0.1:7777/v1/systemone",
		},
	}
}

func fmtProbability(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}

func TestLookupCatalogIDOnWebAcceptsOnlyAfterJevAtThreshold(t *testing.T) {
	sawJev := false
	s := newWebToJevLookupScraper(t, 0.80, &sawJev)

	got, err := s.lookupCatalogIDOnWeb(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
	if err != nil {
		t.Fatalf("lookupCatalogIDOnWeb returned error: %v", err)
	}
	if got != "IPX-072" {
		t.Fatalf("resolved catalog ID = %q, want IPX-072", got)
	}
	if !sawJev {
		t.Fatal("Web-selected candidate bypassed Jev validation")
	}
}

func TestLookupCatalogIDOnWebRejectsWhenJevBelowThreshold(t *testing.T) {
	sawJev := false
	s := newWebToJevLookupScraper(t, 0.799, &sawJev)

	got, err := s.lookupCatalogIDOnWeb(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
	if err == nil {
		t.Fatalf("lookupCatalogIDOnWeb returned %q without rejection", got)
	}
	if got != "" {
		t.Fatalf("rejected Web candidate leaked catalog ID %q", got)
	}
	if !strings.Contains(err.Error(), "below threshold 0.800") {
		t.Fatalf("unexpected rejection error: %v", err)
	}
	if !sawJev {
		t.Fatal("Web-selected candidate bypassed Jev validation")
	}
}

func TestLookupCatalogIDOnWebJevStopsAfterStrongRepeatedWebCandidate(t *testing.T) {
	title := "狙われた通学路 共謀痴漢電車 桃乃木かな"
	ddgHTML := `<html><body>
	<div class="result">
	  <a class="result__a" href="https://example.com/a">狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072</a>
	  <div class="result__snippet">狙われた通学路 共謀痴漢電車 桃乃木かな 品番 IPX-072</div>
	</div>
	<div class="result">
	  <a class="result__a" href="https://example.net/b">狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072</a>
	  <div class="result__snippet">同一作品 IPX-072 狙われた通学路 共謀痴漢電車 桃乃木かな</div>
	</div>
	</body></html>`

	sawJev := false
	ddgCalls := 0
	lateProviderCalls := 0
	client := jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		host := strings.ToLower(req.URL.Hostname())
		switch {
		case host == "www.google.com":
			return jevLookupResponse(http.StatusNotFound, ""), nil
		case strings.Contains(host, "duckduckgo.com"):
			ddgCalls++
			return jevLookupResponse(http.StatusOK, ddgHTML), nil
		case host == "search.yahoo.co.jp" || host == "www.bing.com":
			lateProviderCalls++
			return jevLookupResponse(http.StatusInternalServerError, ""), nil
		case host == "127.0.0.1" && req.URL.Path == "/v1/systemone":
			sawJev = true
			return jevLookupResponse(http.StatusOK, `{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":0.90}}}`), nil
		default:
			return jevLookupResponse(http.StatusNotFound, ""), nil
		}
	})

	s := &Scraper{
		httpClient: client,
		cfg: &Config{
			JevCatalogEnabled:   true,
			JevCatalogAPIKey:    "test-key",
			JevCatalogThreshold: 0.80,
			JevCatalogModel:     "jev-test",
			JevCatalogEndpoint:  "http://127.0.0.1:7777/v1/systemone",
		},
	}

	got, err := s.lookupCatalogIDOnWeb(context.Background(), title)
	if err != nil {
		t.Fatalf("lookupCatalogIDOnWeb returned error: %v", err)
	}
	if got != "IPX-072" {
		t.Fatalf("resolved catalog ID=%q, want IPX-072", got)
	}
	if !sawJev {
		t.Fatal("strong repeated Web candidate bypassed Jev")
	}
	if ddgCalls != 1 {
		t.Fatalf("DuckDuckGo calls=%d, want 1", ddgCalls)
	}
	if lateProviderCalls != 0 {
		t.Fatalf("late fallback provider calls=%d, want 0", lateProviderCalls)
	}
}

func TestBulkTrustedURLIdentityMayBypassJevButNormalModeMayNot(t *testing.T) {
	title := "狙われた通学路 共謀痴漢電車 桃乃木かな"
	ddgHTML := `<html><body>
	<div class="result">
	  <a class="result__a" href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00072/">狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072</a>
	  <div class="result__snippet">狙われた通学路 共謀痴漢電車 桃乃木かな 品番 IPX-072</div>
	</div>
	</body></html>`

	jevCalls := 0
	client := jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		host := strings.ToLower(req.URL.Hostname())
		switch {
		case strings.Contains(host, "duckduckgo.com"):
			return jevLookupResponse(http.StatusOK, ddgHTML), nil
		case host == "127.0.0.1" && req.URL.Path == "/v1/systemone":
			jevCalls++
			return jevLookupResponse(http.StatusOK, `{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":0.10}}}`), nil
		default:
			return jevLookupResponse(http.StatusNotFound, ""), nil
		}
	})

	s := &Scraper{
		httpClient: client,
		cfg: &Config{
			JevCatalogEnabled:          true,
			JevCatalogAPIKey:           "test-key",
			JevCatalogThreshold:        0.80,
			JevCatalogModel:            "jev-test",
			JevCatalogEndpoint:         "http://127.0.0.1:7777/v1/systemone",
			PreferNonGoogleTitleSearch: true,
		},
	}
	got, err := s.lookupCatalogIDOnWeb(context.Background(), title)
	if err != nil {
		t.Fatalf("bulk trusted identity returned error: %v", err)
	}
	if got != "IPX-072" {
		t.Fatalf("resolved=%q, want IPX-072", got)
	}
	if jevCalls != 0 {
		t.Fatalf("bulk deterministic trusted identity called Jev %d times", jevCalls)
	}
}

func TestDeterministicTrustedWebIdentityRejectsConflict(t *testing.T) {
	title := "狙われた通学路 共謀痴漢電車 桃乃木かな"
	results := []titleWebSearchResult{
		{
			Title:   title + " IPX-072",
			Snippet: "品番 IPX-072",
			URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00072/",
		},
		{
			Title:   title + " ABC-999",
			Snippet: "品番 ABC-999",
			URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc00999/",
		},
	}
	if candidateHasDeterministicTrustedWebIdentity(title, "IPX-072", results) {
		t.Fatal("conflicting trusted URL identity must not be deterministic")
	}
}

func TestDeterministicWebConsensusNeedsThreeIndependentHighCoverageHosts(t *testing.T) {
	title := "わたし、犯されにゆきます。～弟想いの美しき姉編～"
	results := []titleWebSearchResult{
		{Title: "SNIS-323 " + title, URL: "https://a.example/video/1"},
		{Title: "SNIS-323 " + title, URL: "https://b.example/video/2"},
		{Title: "SNIS-323 " + title, URL: "https://c.example/video/3"},
	}
	if !candidateHasDeterministicWebConsensus(title, "SNIS-323", results) {
		t.Fatal("three independent exact/high-coverage cards should form deterministic consensus")
	}
}

func TestDeterministicWebConsensusFailsOnHighCoverageConflict(t *testing.T) {
	title := "作品タイトル 女優名"
	results := []titleWebSearchResult{
		{Title: "ABC-123 " + title, URL: "https://a.example/1"},
		{Title: "ABC-123 " + title, URL: "https://b.example/2"},
		{Title: "ABC-123 " + title, URL: "https://c.example/3"},
		{Title: "XYZ-999 " + title, URL: "https://d.example/4"},
	}
	if candidateHasDeterministicWebConsensus(title, "ABC-123", results) {
		t.Fatal("high-coverage conflicting catalog ID must block deterministic consensus")
	}
}
