//go:build keepwords

package scrape

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type titleWebFakeResponse struct {
	status int
	body   string
}

type titleWebFakeHTTPClient struct {
	mu        sync.Mutex
	responses []titleWebFakeResponse
	calls     int
	queries   []string
	hosts     []string
	paths     []string
}

func (f *titleWebFakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if req != nil && req.URL != nil {
		f.queries = append(f.queries, req.URL.Query().Get("q"))
		f.hosts = append(f.hosts, req.URL.Host)
		f.paths = append(f.paths, req.URL.Path)
	}
	idx := f.calls - 1
	resp := titleWebFakeResponse{status: http.StatusOK}
	if idx < len(f.responses) {
		resp = f.responses[idx]
	}
	return &http.Response{
		StatusCode: resp.status,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Header:     make(http.Header),
	}, nil
}

func googleResultHTML(id, title string) string {
	return `<html><body><div class="MjjYud"><div class="tF2Cxc"><a href="https://www.dmm.co.jp/example/` + id + `"><h3>` + id + ` ` + title + `</h3></a><div class="VwiC3b">` + title + ` 品番 ` + id + `</div></div></div></body></html>`
}

func TestNormalizeTitleForWebSearchRemovesFilenameNoise(t *testing.T) {
	got := normalizeTitleForWebSearch("【中文字幕】美しい人妻が夫に内緒で濃密SEX_4K_UNCENSORED.mp4")
	require.Equal(t, "美しい人妻が夫に内緒で濃密SEX", got)
}

func TestResolveTitleViaWebFindsCatalogIDFromGoogleResult(t *testing.T) {
	client := &titleWebFakeHTTPClient{responses: []titleWebFakeResponse{{
		status: http.StatusOK,
		body:   googleResultHTML("ABW-123", "美しい人妻が夫に内緒で濃密SEX"),
	}}}
	s := &Scraper{httpClient: client, cfg: &Config{UserAgent: "test-agent"}}

	cmd := ScrapeCmd{MovieID: "【中文字幕】美しい人妻が夫に内緒で濃密SEX_4K_UNCENSORED"}
	got := s.resolveTitleViaWeb(context.Background(), cmd)

	require.Equal(t, "ABW-123", got.MovieID)
	require.Equal(t, 1, client.calls)
	require.Equal(t, []string{"www.google.com"}, client.hosts)
}

func TestConfiguredKeepWordsAreAbsentFromActualGoogleQuery(t *testing.T) {
	keepWords := []string{"SPECIAL", "【配布】", "CUSTOMTAG", "字幕", "-UC"}
	client := &titleWebFakeHTTPClient{responses: []titleWebFakeResponse{{
		status: http.StatusOK,
		body:   googleResultHTML("ABW-123", "本当の作品タイトル"),
	}}}
	s := &Scraper{
		httpClient: client,
		cfg:        &Config{FilenameKeepWords: keepWords},
	}

	cmd := ScrapeCmd{MovieID: "本当の作品タイトル_SPECIAL_【配布】_CUSTOMTAG_字幕_-UC_4K"}
	got := s.resolveTitleViaWebWithConfiguredNoise(context.Background(), cmd)

	require.Equal(t, "ABW-123", got.MovieID)
	require.NotEmpty(t, client.queries)
	for i, query := range client.queries {
		require.Equal(t, "www.google.com", client.hosts[i])
		require.Equal(t, "/search", client.paths[i])
		for _, keepWord := range keepWords {
			require.NotContains(t, strings.ToUpper(query), strings.ToUpper(keepWord), "configured KEEPWORD leaked to Google q=")
		}
		require.NotContains(t, strings.ToLower(query), "4k")
		require.Contains(t, query, "本当の作品タイトル")
	}
}

func TestGoogleIsTheOnlySupportedWebProvider(t *testing.T) {
	client := &titleWebFakeHTTPClient{}
	s := &Scraper{httpClient: client, cfg: &Config{}}

	for _, provider := range []string{"duckduckgo", "bing"} {
		_, err := s.fetchTitleWebSearch(context.Background(), provider, "作品タイトル")
		require.Error(t, err)
		require.Contains(t, err.Error(), "Google is the only provider")
	}
	require.Zero(t, client.calls)
}

func TestResolveTitleViaWebRetriesOnlyGoogleQueryVariants(t *testing.T) {
	client := &titleWebFakeHTTPClient{responses: []titleWebFakeResponse{
		{status: http.StatusServiceUnavailable, body: ""},
		{status: http.StatusOK, body: googleResultHTML("SSIS-999", "完全な日本語タイトル")},
	}}
	s := &Scraper{httpClient: client, cfg: &Config{}}

	got := s.resolveTitleViaWeb(context.Background(), ScrapeCmd{MovieID: "完全な日本語タイトル"})

	require.Equal(t, "SSIS-999", got.MovieID)
	require.Equal(t, 2, client.calls)
	require.Equal(t, []string{"www.google.com", "www.google.com"}, client.hosts)
	require.Equal(t, "完全な日本語タイトル", client.queries[0])
	require.Equal(t, "完全な日本語タイトル 品番", client.queries[1])
}

func TestResolveTitleViaWebSkipsNormalCatalogID(t *testing.T) {
	client := &titleWebFakeHTTPClient{}
	s := &Scraper{httpClient: client, cfg: &Config{}}

	got := s.resolveTitleViaWeb(context.Background(), ScrapeCmd{MovieID: "ABW-123"})

	require.Equal(t, "ABW-123", got.MovieID)
	require.Zero(t, client.calls)
}

func TestChooseCatalogCandidateRejectsAmbiguousNearTie(t *testing.T) {
	results := []titleWebSearchResult{
		{Title: "ABC-123 同じ作品タイトル", Snippet: "同じ作品タイトル ABC-123"},
		{Title: "ABC-124 同じ作品タイトル", Snippet: "同じ作品タイトル ABC-124"},
	}

	_, ok := chooseCatalogCandidate("同じ作品タイトル", results)
	require.False(t, ok)
}

func TestExtractCatalogCandidatesRejectsCodecNoise(t *testing.T) {
	got := extractCatalogCandidates("H264-1080 ABW-123 x265-2160")
	require.Equal(t, []string{"ABW-123"}, got)
}
