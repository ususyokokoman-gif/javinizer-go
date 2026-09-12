//go:build keepwords

package worker

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBuildScrapeCmd_WebFirstKeepsUnmatchedFilenameTitle(t *testing.T) {
	file := filepath.Join("videos", "本当の作品タイトル_SPECIAL_【配布】_4K.mp4")
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "FAKE-999"}}

	cmd, fromMatcher := buildScrapeCmd(file, models.FileMatchInfo{}, inputs, ScrapePhaseConfig{})

	require.Equal(t, "本当の作品タイトル_SPECIAL_【配布】_4K", cmd.MovieID)
	require.False(t, fromMatcher, "unmatched filename must not be pre-empted by MatchString before web-first lookup")
}

func TestBuildScrapeCmd_WebFirstPreservesValidatedScanMatchID(t *testing.T) {
	file := filepath.Join("videos", "anything_SPECIAL.mp4")
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "FAKE-999"}}

	cmd, fromMatcher := buildScrapeCmd(file, models.FileMatchInfo{MovieID: "ABW-123"}, inputs, ScrapePhaseConfig{})

	require.Equal(t, "ABW-123", cmd.MovieID)
	require.True(t, fromMatcher, "validated scan/match IDs must keep the existing direct-ID path")
}
