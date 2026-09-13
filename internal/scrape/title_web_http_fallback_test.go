//go:build keepwords

package scrape

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
