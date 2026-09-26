package scrape

import (
	"context"
	"strconv"
	"encoding/json"
	"io"
	"net/http"
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
