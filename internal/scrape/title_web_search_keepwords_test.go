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
}

func (f *titleWebFakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if req != nil && req.URL != nil {
		f.queries = append(f.queries, req.URL.Query().Get("q"))
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

func TestNormalizeTitleForWebSearchRemovesFilenameNoise(t *testing.T) {
	got := normalizeTitleForWebSearch("【中文字幕】美しい人妻が夫に内緒で濃密SEX_4K_UNCENSORED.mp4")
	require.Equal(t, "美しい人妻が夫に内緒で濃密SEX", got)
}

func TestResolveTitleViaWebFindsCatalogIDFromSearchResult(t *testing.T) {
	client := &titleWebFakeHTTPClient{responses: []titleWebFakeResponse{{
		status: http.StatusOK,
		body: `<html><body><div class="result">
			<a class="result__a" href="https://javdb.com/v/example">ABW-123 美しい人妻が夫に内緒で濃密SEX</a>
			<a class="result__snippet">美しい人妻が夫に内緒で濃密SEX - ABW-123</a>
		</div></body></html>`,
	}}}
	s := &Scraper{httpClient: client, cfg: &Config{UserAgent: "test-agent"}}

	cmd := ScrapeCmd{MovieID: "【中文字幕】美しい人妻が夫に内緒で濃密SEX_4K_UNCENSORED"}
	got := s.resolveTitleViaWeb(context.Background(), cmd)

	require.Equal(t, "ABW-123", got.MovieID)
	require.Equal(t, 1, client.calls)
}

func TestConfiguredKeepWordsAreAbsentFromActualWebQuery(t *testing.T) {
	client := &titleWebFakeHTTPClient{responses: []titleWebFakeResponse{{
		status: http.StatusOK,
		body: `<html><body><div class="result">
			<a class="result__a" href="https://javdb.com/v/example">ABW-123 本当の作品タイトル</a>
			<a class="result__snippet">本当の作品タイトル ABW-123</a>
		</div></body></html>`,
	}}}
	s := &Scraper{
		httpClient: client,
		cfg: &Config{FilenameKeepWords: []string{"SPECIAL", "【配布】", "CUSTOMTAG"}},
	}

	cmd := ScrapeCmd{MovieID: "本当の作品タイトル_SPECIAL_【配布】_CUSTOMTAG_4K"}
	got := s.resolveTitleViaWebWithConfiguredNoise(context.Background(), cmd)

	require.Equal(t, "ABW-123", got.MovieID)
	require.NotEmpty(t, client.queries)
	for _, query := range client.queries {
		require.NotContains(t, query, "SPECIAL")
		require.NotContains(t, query, "配布")
		require.NotContains(t, query, "CUSTOMTAG")
		require.NotContains(t, strings.ToLower(query), "4k")
		require.Contains(t, query, "本当の作品タイトル")
	}
}

func TestResolveTitleViaWebSkipsNormalCatalogID(t *testing.T) {
	client := &titleWebFakeHTTPClient{}
	s := &Scraper{httpClient: client, cfg: &Config{}}

	got := s.resolveTitleViaWeb(context.Background(), ScrapeCmd{MovieID: "ABW-123"})

	require.Equal(t, "ABW-123", got.MovieID)
	require.Zero(t, client.calls)
}

func TestResolveTitleViaWebFallsBackFromDuckDuckGoToBing(t *testing.T) {
	client := &titleWebFakeHTTPClient{responses: []titleWebFakeResponse{
		{status: http.StatusServiceUnavailable, body: ""},
		{status: http.StatusOK, body: `<html><body><ol><li class="b_algo">
			<h2><a href="https://www.dmm.co.jp/example">SSIS-999 完全な日本語タイトル</a></h2>
			<p>完全な日本語タイトル 品番 SSIS-999</p>
		</li></ol></body></html>`},
	}}
	s := &Scraper{httpClient: client, cfg: &Config{}}

	got := s.resolveTitleViaWeb(context.Background(), ScrapeCmd{MovieID: "完全な日本語タイトル"})

	require.Equal(t, "SSIS-999", got.MovieID)
	require.Equal(t, 2, client.calls)
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
