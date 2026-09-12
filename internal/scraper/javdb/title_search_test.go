package javdb

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

func TestSearchTitleCandidatesVerifiesDetailPageID(t *testing.T) {
	const title = "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"
	searchURL := "https://javdb.test/search?q=" + url.QueryEscape(title) + "&f=all"
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
		searchURL: `
			<html><body><div class="movie-list">
			  <div class="item"><a class="box" href="/v/good"><div class="uid">SSIS-001</div><div class="video-title">SSIS-001 ` + title + `</div></a></div>
			  <div class="item"><a class="box" href="/v/bad"><div class="uid">XYZ-999</div><div class="video-title">XYZ-999 まったく別の作品</div></a></div>
			</div></body></html>`,
		"https://javdb.test/v/good": `
			<html><body>
			  <h2 class="title is-4"><strong>SSIS-001</strong> ` + title + `</h2>
			  <div class="movie-panel-info"><div class="panel-block"><strong>Maker:</strong><div class="value"><a>S1</a></div></div></div>
			</body></html>`,
	}})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	results, err := s.SearchTitleCandidates(context.Background(), title, 3)
	if err != nil {
		t.Fatalf("SearchTitleCandidates() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchTitleCandidates() returned %d results, want 1: %#v", len(results), results)
	}
	if got := results[0].ID; got != "SSIS-001" {
		t.Fatalf("verified detail ID = %q, want SSIS-001", got)
	}
	if got := results[0].SourceURL; got != "https://javdb.test/v/good" {
		t.Fatalf("verified detail URL = %q", got)
	}
}

func TestSearchTitleCandidatesWorksWhenMetadataScraperDisabled(t *testing.T) {
	const title = "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"
	searchURL := "https://javdb.test/search?q=" + url.QueryEscape(title) + "&f=all"
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
		searchURL: `<html><body><div class="movie-list"><div class="item"><a class="box" href="/v/good"><div class="uid">SSIS-001</div><div class="video-title">SSIS-001 ` + title + `</div></a></div></div></body></html>`,
		"https://javdb.test/v/good": `<html><body><h2 class="title is-4"><strong>SSIS-001</strong> ` + title + `</h2><div class="movie-panel-info"><div class="panel-block"><strong>Maker:</strong><div class="value"><a>S1</a></div></div></div></body></html>`,
	}})

	s := &scraper{
		client:      client,
		enabled:     false,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}
	if s.IsEnabled() {
		t.Fatal("test setup error: JavDB metadata scraper must be disabled")
	}

	results, err := s.SearchTitleCandidates(context.Background(), title, 3)
	if err != nil {
		t.Fatalf("disabled metadata scraper must still provide title identification evidence: %v", err)
	}
	if len(results) != 1 || results[0].ID != "SSIS-001" {
		t.Fatalf("title identification results = %#v, want one SSIS-001 result", results)
	}

	if _, err := s.Search(context.Background(), "SSIS-001"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("ordinary metadata Search must remain disabled, got err=%v", err)
	}
}

func TestJavDBTitleSimilarityRejectsUnrelatedTitle(t *testing.T) {
	if got := javDBTitleSimilarity("完全な日本語タイトル", "まったく別の作品"); got >= 0.20 {
		t.Fatalf("unrelated title similarity = %.3f, want < 0.20", got)
	}
	if got := javDBTitleSimilarity("完全な日本語タイトル", "SSIS-001 完全な日本語タイトル"); got < 0.75 {
		t.Fatalf("matching title similarity = %.3f, want >= 0.75", got)
	}
}
