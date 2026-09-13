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
	"github.com/javinizer/javinizer-go/internal/models"
	scraperpkg "github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

type liveWebRecorder struct {
	inner interface {
		Do(*http.Request) (*http.Response, error)
	}
	mu      sync.Mutex
	hosts   []string
	queries []string
}

func (r *liveWebRecorder) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	if req != nil && req.URL != nil {
		r.hosts = append(r.hosts, req.URL.Host)
		r.queries = append(r.queries, req.URL.Query().Get("q"))
	}
	r.mu.Unlock()
	return r.inner.Do(req)
}

func (r *liveWebRecorder) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hosts...), append([]string(nil), r.queries...)
}

func newSubmissionLiveRegistry(t *testing.T) *scraperutil.ScraperRegistry {
	t.Helper()
	reg := scraperutil.NewScraperRegistry()
	scraperpkg.RegisterAll(reg)
	javdbRegistration, ok := reg.Get("javdb")
	if !ok {
		t.Fatal("production scraper registry did not register javdb")
	}
	settings := javdbRegistration.Defaults
	settings.Enabled = true
	if settings.Timeout <= 0 {
		settings.Timeout = 20
	}
	settings.RetryCount = 0
	settings.RateLimit = 0

	initialized, err := scraperpkg.NewDefaultScraperRegistryFrom(reg, scraperpkg.ScraperRegistryConfig{
		Overrides: map[string]models.ScraperSettings{
			"javdb": settings,
		},
		TimeoutSeconds: 20,
	}, nil, nil)
	if err != nil {
		t.Fatalf("initialize production JavDB registry: %v", err)
	}
	instance, ok := initialized.GetInstance("javdb")
	if !ok || instance == nil || !instance.IsEnabled() {
		t.Fatal("production JavDB instance was not initialized and enabled")
	}
	return initialized
}

func supportedLiveSearchHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "www.google.com" || h == "html.duckduckgo.com" || h == "duckduckgo.com"
}

func TestSubmissionLiveTitleToCatalogID(t *testing.T) {
	productionClient, err := httpclient.NewHTTPClient(nil, 20*time.Second)
	if err != nil {
		t.Fatalf("construct production HTTP client: %v", err)
	}
	recorder := &liveWebRecorder{inner: productionClient}

	keepWords := []string{"SPECIAL", "4K", "8K", "VR", "AI", "字幕", "中文字幕", "-UC", "UNCENSORED"}
	cfg := &Config{FilenameKeepWords: keepWords}
	s := &Scraper{
		registry:   newSubmissionLiveRegistry(t),
		httpClient: recorder,
		cfg:        cfg,
	}

	// Independent from JavDB/direct metadata. A final catalog ID is not enough
	// proof: at least one production general-web provider must return real
	// organic results in the Windows runner environment.
	strictTitle := "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"
	strictCtx, strictCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer strictCancel()
	strictOrganicResults := 0
	strictQueries := 0
	strictProvider := ""
	var strictErrors []string
	for _, q := range buildTitleWebQueries(strictTitle) {
		strictQueries++
		results, provider, searchErr := s.fetchGeneralTitleWebSearch(strictCtx, q)
		if searchErr != nil {
			strictErrors = append(strictErrors, searchErr.Error())
			t.Logf("LIVE_WEB_STRICT query=%q error=%v", q, searchErr)
			continue
		}
		t.Logf("LIVE_WEB_STRICT provider=%s query=%q organic_results=%d", provider, q, len(results))
		if len(results) > 0 {
			strictOrganicResults += len(results)
			strictProvider = provider
			break
		}
	}
	if strictQueries == 0 {
		t.Fatal("LIVE_WEB_STRICT_FAIL: no web query was generated")
	}
	if strictOrganicResults == 0 {
		t.Fatalf("LIVE_WEB_STRICT_FAIL: no production web provider returned usable search results; errors=%v", strictErrors)
	}
	t.Logf("LIVE_WEB_STRICT_PASS provider=%s organic_results=%d", strictProvider, strictOrganicResults)

	cases := []struct {
		id    string
		title string
	}{
		{"SSIS-001", "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"},
		{"MIDE-007", "今日、あなたの上司に犯されました。 大橋未久"},
		{"IPZ-508", "背徳の檻 幸せな二組の夫婦を襲う禁断の監禁強制スワッピング凌襲 美波なみ 愛田奈々"},
		{"SNIS-323", "わたし、犯されにゆきます。～弟想いの美しき姉編～"},
		{"IPX-072", "狙われた通学路 共謀痴漢電車 桃乃木かな"},
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
					t.Fatalf("KEEPWORD %q remained before web lookup: raw=%q cleaned=%q", unwanted, raw, cleaned)
				}
			}
			normalized := normalizeTitleForWebSearch(cleaned)

			beforeHosts, beforeQueries := recorder.snapshot()
			got := s.resolveTitleViaWebWithConfiguredNoise(ctx, ScrapeCmd{MovieID: raw})
			afterHosts, afterQueries := recorder.snapshot()
			caseHosts := afterHosts[len(beforeHosts):]
			caseQueries := afterQueries[len(beforeQueries):]

			t.Logf("LIVE_CASE expected=%s raw=%q", tc.id, raw)
			t.Logf("LIVE_CLEANED=%q", cleaned)
			t.Logf("LIVE_NORMALIZED=%q", normalized)
			t.Logf("LIVE_WEB_REQUEST_COUNT=%d", len(caseQueries))
			if len(caseQueries) == 0 {
				t.Fatalf("LIVE_WEB_REQUEST_FAIL: catalog ID %s was resolved without issuing any general-web request", tc.id)
			}

			for i, q := range caseQueries {
				host := caseHosts[i]
				t.Logf("LIVE_WEB_REQUEST host=%s q=%q", host, q)
				lowerHost := strings.ToLower(host)
				if strings.Contains(lowerHost, "bing.com") {
					t.Fatalf("Bing was contacted during catalog-ID resolution: host=%q q=%q", host, q)
				}
				if !supportedLiveSearchHost(host) {
					t.Fatalf("unexpected search-engine provider used: host=%q q=%q", host, q)
				}
				for _, unwanted := range keepWords {
					if strings.Contains(strings.ToUpper(q), strings.ToUpper(unwanted)) {
						t.Fatalf("KEEPWORD %q leaked into web query=%q", unwanted, q)
					}
				if !strings.Contains(compactComparable(q), compactComparable(normalized)) {
					t.Fatalf("normalized real title missing from web query=%q", q)
				}
			}

			if got.MovieID != tc.id {
				t.Fatalf("live title resolution = %q, want %q", got.MovieID, tc.id)
			}
			t.Logf("LIVE_RESOLVED=%s", got.MovieID)
		})
		if ok {
			passed++
		}
	}

	if passed != len(cases) {
		t.Fatalf("LIVE_MATRIX_FAIL=%d/%d passed", passed, len(cases))
	}
	t.Logf("LIVE_MATRIX_PASS=%d/%d", passed, len(cases))
}
