//go:build keepwords

package r18devdump

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func seedTitleDump(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "r18dev_dump.db")
	dump := strings.Join([]string{
		"COPY public.derived_video (content_id, dvd_id, title_en, title_ja, runtime_mins) FROM stdin;",
		"118abc00123\tABC-123\tProduct Number Movie\t品番優先作品\t120",
		"118xyz00999\tXYZ-999\tDifferent Movie\tABC-123\t99",
		"118jpn00001\tJPN-001\tJapanese Title English\t完全な日本語タイトル\t101",
		"118eng00001\tENG-001\tEnglish Search Title\t英語検索作品\t88",
		"\\.",
		"",
	}, "\n")

	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	require.NoError(t, err)
	return path
}

func TestLookupMovieTitleFallback(t *testing.T) {
	store, err := Open(seedTitleDump(t))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	t.Run("dvd id keeps priority over matching title", func(t *testing.T) {
		movie, err := store.LookupMovie(ctx, "ABC-123")
		require.NoError(t, err)
		require.Equal(t, "118abc00123", movie.ContentID)
		require.Equal(t, "ABC-123", movie.DVDID)
		require.Equal(t, 120, movie.Runtime)
	})

	t.Run("japanese title exact match", func(t *testing.T) {
		movie, err := store.LookupMovie(ctx, "完全な日本語タイトル")
		require.NoError(t, err)
		require.Equal(t, "118jpn00001", movie.ContentID)
		require.Equal(t, "JPN-001", movie.DVDID)
		require.Equal(t, "完全な日本語タイトル", movie.TitleJa)
	})

	t.Run("english title is case insensitive and input is trimmed", func(t *testing.T) {
		movie, err := store.LookupMovie(ctx, "  english search title  ")
		require.NoError(t, err)
		require.Equal(t, "118eng00001", movie.ContentID)
		require.Equal(t, "ENG-001", movie.DVDID)
		require.Equal(t, "English Search Title", movie.TitleEn)
	})

	t.Run("unknown title remains a dump miss", func(t *testing.T) {
		_, err := store.LookupMovie(ctx, "title that does not exist")
		require.True(t, errors.Is(err, models.ErrDumpMiss), "err=%v", err)
	})
}
