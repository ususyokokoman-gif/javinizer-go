//go:build keepwords

package scrape

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery")

type titleLookupHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f titleLookupHTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestShouldUseHeadlessGoogleFallback(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden} {
		if !shouldUseHeadlessGoogleFallback(status) {
			t.Fatalf("status %d should use headless Google fallback", status)
		}
	}

	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		if shouldUseHeadlessGoogleFallback(status) {
			t.Fatalf("status %d should not use headless Google fallback", status)
		}
	}
}

func TestFetchTitleWebSearchFallsBackToBrowserOnHTTPRequestFailure(t *testing.T) {
	original := googleBrowserFallback
	defer func() { googleBrowserFallback = original }()

	called := false
	googleBrowserFallback = func(_ context.Context, endpoint string) ([]titleWebSearchResult, error) {
		called = true
		if !strings.Contains(endpoint, "google.com/search") {
			t.Fatalf("unexpected browser endpoint %q", endpoint)
		}
		return []titleWebSearchResult{{
			Title:   "完全な日本語タイトル SSIS-001",
			Snippet: "品番 SSIS-001",
			URL:     "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ssis00001/",
		}}, nil
	}

	s := &Scraper{httpClient: titleLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("simulated transport failure")
	})}
	results, err := s.fetchTitleWebSearch(context.Background(), "google", "完全な日本語タイトル")
	if err != nil {
		t.Fatalf("fetchTitleWebSearch returned error after browser fallback: %v", err)
	}
	if !called {
		t.Fatal("browser fallback was not called after HTTP request failure")
	}
	if len(results) != 1 || results[0].URL == "" {
		t.Fatalf("browser fallback results = %#v", results)
	}
}

func TestFetchTitleWebSearchFallsBackToBrowserWhenHTTP200HasNoOrganicResults(t *testing.T) {
	original := googleBrowserFallback
	defer func() { googleBrowserFallback = original }()

	called := false
	googleBrowserFallback = func(_ context.Context, _ string) ([]titleWebSearchResult, error) {
		called = true
		return []titleWebSearchResult{{
			Title:   "完全な日本語タイトル SSIS-001",
			Snippet: "品番 SSIS-001",
			URL:     "https://www.javdb.com/v/example",
		}}, nil
	}

	s := &Scraper{httpClient: titleLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("<html><body><main></main></body></html>")),
			Header:     make(http.Header),
		}, nil
	})}
	results, err := s.fetchTitleWebSearch(context.Background(), "google", "完全な日本語タイトル")
	if err != nil {
		t.Fatalf("fetchTitleWebSearch returned error after empty-result browser fallback: %v", err)
	}
	if !called {
		t.Fatal("browser fallback was not called after HTTP 200 with no organic results")
	}
	if len(results) != 1 {
		t.Fatalf("browser fallback results = %#v", results)
	}
}


func TestNormalizeBingResultURLDecodesTrackedTarget(t *testing.T) {
	raw := "https://www.bing.com/ck/a?!&&p=abc&u=a1aHR0cHM6Ly93d3cuZG1tLmNvLmpwL2RpZ2l0YWwvdmlkZW9hLy0vZGV0YWlsLz0vY2lkPWlwejAwNTA4Lw&ntb=1"
	want := "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipz00508/"
	got := normalizeBingResultURL(raw)
	if got != want {
		t.Fatalf("normalizeBingResultURL() = %q, want %q", got, want)
	}
	if source := trustedCatalogSource(got); source != "dmm" {
		t.Fatalf("decoded Bing result trusted source = %q, want dmm", source)
	}
	ids := extractTrustedURLCatalogCandidates(got)
	if len(ids) != 1 || ids[0] != "IPZ-508" {
		t.Fatalf("decoded Bing result catalog IDs = %#v, want [IPZ-508]", ids)
	}
}


func TestParseBingResultsNormalizesTrackedTarget(t *testing.T) {
	html := `<html><body><ol id="b_results"><li class="b_algo"><h2><a href="https://www.bing.com/ck/a?!&&p=abc&u=a1aHR0cHM6Ly93d3cuZG1tLmNvLmpwL2RpZ2l0YWwvdmlkZW9hLy0vZGV0YWlsLz0vY2lkPWlweDAwMDcyLw&ntb=1">狙われた通学路 共謀痴漢電車 桃乃木かな IPX-072</a></h2><div class="b_caption"><p>品番 IPX-072</p></div></li></ol></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	results := parseBingResults(doc)
	if len(results) != 1 {
		t.Fatalf("parseBingResults() returned %d results, want 1", len(results))
	}
	wantURL := "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00072/"
	if results[0].URL != wantURL {
		t.Fatalf("Bing parsed URL = %q, want %q", results[0].URL, wantURL)
	}
	if source := trustedCatalogSource(results[0].URL); source != "dmm" {
		t.Fatalf("Bing parsed trusted source = %q, want dmm", source)
	}
	ids := extractTrustedURLCatalogCandidates(results[0].URL)
	if len(ids) != 1 || ids[0] != "IPX-072" {
		t.Fatalf("Bing parsed catalog IDs = %#v, want [IPX-072]", ids)
	}
}
