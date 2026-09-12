package javdb

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

func TestSearchTitleCandidatesVerifiesDetailPageID(t *testing.T) {
	const title = "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/search?q=%E4%B8%80%E3%83%B6%E6%9C%88%E9%96%93%E3%81%AE%E7%A6%81%E6%AC%B2%E3%81%AE%E6%9E%9C%E3%81%A6%E3%81%AB%E5%BD%BC%E5%A5%B3%E3%81%AE%E3%83%AB%E3%83%BC%E3%83%A0%E3%83%A1%E3%82%A4%E3%83%882%E4%BA%BA%E3%81%A8%E6%B5%AE%E6%B0%97SEX%E3%81%A0%E3%81%91%E3%81%AB%E6%B2%A1%E9%A0%AD%E3%81%97%E3%81%9F%E5%BD%BC%E5%A5%B3%E4%B8%8D%E5%9C%A8%E3%81%AE3%E6%97%A5%E9%96%93&f=all": `
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

func TestJavDBTitleSimilarityRejectsUnrelatedTitle(t *testing.T) {
	if got := javDBTitleSimilarity("完全な日本語タイトル", "まったく別の作品"); got >= 0.20 {
		t.Fatalf("unrelated title similarity = %.3f, want < 0.20", got)
	}
	if got := javDBTitleSimilarity("完全な日本語タイトル", "SSIS-001 完全な日本語タイトル"); got < 0.75 {
		t.Fatalf("matching title similarity = %.3f, want >= 0.75", got)
	}
}
