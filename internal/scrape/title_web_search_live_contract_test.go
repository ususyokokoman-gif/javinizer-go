//go:build submissionlive

package scrape

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	httpclient "github.com/javinizer/javinizer-go/internal/httpclient"
)

type liveGoogleRecorder struct {
	inner interface {
		Do(*http.Request) (*http.Response, error)
	}
	mu      sync.Mutex
	hosts   []string
	queries []string
}

func (r *liveGoogleRecorder) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	if req != nil && req.URL != nil {
		r.hosts = append(r.hosts, req.URL.Host)
		r.queries = append(r.queries, req.URL.Query().Get("q"))
	}
	r.mu.Unlock()
	return r.inner.Do(req)
}

func (r *liveGoogleRecorder) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hosts...), append([]string(nil), r.queries...)
}

func TestSubmissionLiveTitleToCatalogID(t *testing.T) {
	productionClient, err := httpclient.NewHTTPClient(nil, 20*time.Second)
	if err != nil {
		t.Fatalf("construct production HTTP client: %v", err)
	}
	recorder := &liveGoogleRecorder{inner: productionClient}

	keepWords := []string{"SPECIAL", "4K", "8K", "VR", "AI", "字幕", "中文字幕", "-UC", "UNCENSORED"}
	cfg := &Config{FilenameKeepWords: keepWords}
	s := &Scraper{httpClient: recorder, cfg: cfg}

	cases := []struct {
		id    string
		title string
	}{
		{"SSIS-001", "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"},
		{"MIDE-007", "今日、あなたの上司に犯されました。"},
		{"IPZ-508", "背徳の檻 幸せな二組の夫婦を襲う禁断の監禁強制スワッピング凌襲 美波なみ 愛田奈々"},
		{"SNIS-323", "わたし、犯されにゆきます。～弟想いの美しき姉編～"},
		{"IPX-072", "狙われた通学路共謀痴漢電車"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	passed := 0

	for _, tc := range cases {
		ok := t.Run(tc.id, func(t *testing.T) {
			raw := tc.title + "_SPECIAL_4K_8K_VR_AI_字幕_中文字幕_-UC_UNCENSORED"
			cleaned := stripConfiguredKeepWords(raw, cfg.FilenameKeepWords)
			upperCleaned := strings.ToUpper(cleaned)
			for _, unwanted := range keepWords {
				if strings.Contains(upperCleaned, strings.ToUpper(unwanted)) {
					t.Fatalf("KEEPWORD %q remained before Google lookup: raw=%q cleaned=%q", unwanted, raw, cleaned)
				}
			}
			normalized := normalizeTitleForWebSearch(cleaned)

			// Probe one real Google response and log exactly what our HTML parser can
			// see. This evidence distinguishes a bad query from a Google markup/parser
			// problem and exposes the candidate IDs that the scorer receives.
			probeResults, probeErr := s.fetchTitleWebSearch(ctx, "google", normalized)
			t.Logf("LIVE_PROBE query=%q results=%d err=%v", normalized, len(probeResults), probeErr)
			for i, result := range probeResults {
				ids := extractCatalogCandidates(result.Title + " " + result.Snippet + " " + result.URL)
				t.Logf("LIVE_PARSED_RESULT rank=%d title=%q snippet=%q url=%q candidate_ids=%v coverage=%.3f", i+1, truncateRunes(result.Title, 160), truncateRunes(result.Snippet, 220), truncateRunes(result.URL, 220), ids, queryCoverage(normalized, result.Title+" "+result.Snippet))
			}

			beforeHosts, beforeQueries := recorder.snapshot()
			got := s.resolveTitleViaWebWithConfiguredNoise(ctx, ScrapeCmd{MovieID: raw})
			afterHosts, afterQueries := recorder.snapshot()
			caseHosts := afterHosts[len(beforeHosts):]
			caseQueries := afterQueries[len(beforeQueries):]

			t.Logf("LIVE_CASE expected=%s raw=%q", tc.id, raw)
			t.Logf("LIVE_CLEANED=%q", cleaned)
			t.Logf("LIVE_NORMALIZED=%q", normalized)
			t.Logf("LIVE_REQUEST_COUNT=%d", len(caseQueries))

			if len(caseQueries) == 0 {
				t.Fatal("no live Google request was made")
			}
			for i, q := range caseQueries {
				t.Logf("LIVE_GOOGLE_REQUEST host=%s q=%q", caseHosts[i], q)
				if caseHosts[i] != "www.google.com" {
					t.Fatalf("non-Google provider used: host=%q q=%q", caseHosts[i], q)
				}
				for _, unwanted := range keepWords {
					if strings.Contains(strings.ToUpper(q), strings.ToUpper(unwanted)) {
						t.Fatalf("KEEPWORD %q leaked into Google q=%q", unwanted, q)
					}
				}
				if !strings.Contains(compactComparable(q), compactComparable(normalized)) {
					t.Fatalf("normalized real title missing from Google q=%q", q)
				}
			}

			if got.MovieID != tc.id {
				t.Fatalf("Google title resolution = %q, want %q", got.MovieID, tc.id)
			}
			t.Logf("LIVE_RESOLVED=%s", got.MovieID)
		})
		if ok {
			passed++
		}
	}

	hosts, _ := recorder.snapshot()
	for _, host := range hosts {
		if host != "www.google.com" {
			t.Fatalf("submission live test contacted non-Google host: %q", host)
		}
	}
	if passed != len(cases) {
		t.Fatalf("LIVE_MATRIX_FAIL=%d/%d passed", passed, len(cases))
	}
	t.Logf("LIVE_MATRIX_PASS=%d/%d", passed, len(cases))
}
