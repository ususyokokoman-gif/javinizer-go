package scrape

import (
	"context"
	"reflect"
	"testing"

	appconfig "github.com/javinizer/javinizer-go/internal/config"
)

func TestExtractKeepWordsFromTemplate(t *testing.T) {
	template := `<ID><KEEPWORDS:4K|AI|【ずん】|-UC|AI;PREFIX= - ;DELIM= > - <TITLE>`
	got := extractKeepWordsFromTemplate(template)
	want := []string{"4K", "AI", "【ずん】", "-UC"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractKeepWordsFromTemplate() = %#v, want %#v", got, want)
	}
}

func TestStripConfiguredKeepWordsPreservesRealTitleText(t *testing.T) {
	input := `MAIDの物語_AI_CUSTOM_【ずん】_-UC.mp4`
	got := stripConfiguredKeepWords(input, []string{"AI", "CUSTOM", "【ずん】", "-UC"})

	// AI is a standalone annotation and must disappear, but the "AI" inside
	// MAID must remain intact.
	want := `MAIDの物語_ _ _ _.mp4`
	if got != want {
		t.Fatalf("stripConfiguredKeepWords() = %q, want %q", got, want)
	}
}

func TestConfigFromAppConfigCarriesKeepWords(t *testing.T) {
	cfg := &appconfig.Config{}
	cfg.Output.Template.FileFormat = `<ID><KEEPWORDS:FOO|BAR|中文字幕;PREFIX= - ;DELIM= >`

	got := ConfigFromAppConfig(cfg)
	want := []string{"FOO", "BAR", "中文字幕"}
	if got == nil || !reflect.DeepEqual(got.FilenameKeepWords, want) {
		t.Fatalf("FilenameKeepWords = %#v, want %#v", got.FilenameKeepWords, want)
	}
}

func TestResolveTitleViaWebWithConfiguredNoiseCleansMovieIDBeforeLookup(t *testing.T) {
	s := &Scraper{cfg: &Config{FilenameKeepWords: []string{"SPECIAL", "【配布】"}}}
	cmd := ScrapeCmd{MovieID: `本当の作品タイトル_SPECIAL_【配布】`}

	got := s.resolveTitleViaWebWithConfiguredNoise(context.Background(), cmd)
	if got.MovieID != `本当の作品タイトル_ _` {
		t.Fatalf("MovieID = %q, want cleaned title", got.MovieID)
	}
}

func TestResolveTitleViaWebWithConfiguredNoiseDoesNotTouchCatalogID(t *testing.T) {
	s := &Scraper{cfg: &Config{FilenameKeepWords: []string{"ABC"}}}
	cmd := ScrapeCmd{MovieID: "ABC-123"}

	got := s.resolveTitleViaWebWithConfiguredNoise(context.Background(), cmd)
	if got.MovieID != "ABC-123" {
		t.Fatalf("catalog ID changed: %q", got.MovieID)
	}
}
