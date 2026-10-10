//go:build sqlite_fts5

package r18devdump

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestExactTitleMatchesReturnsAllDistinctCatalogs(t *testing.T) {
	path := t.TempDir() + "/r18dev_dump.db"
	dump := strings.Join([]string{
		"COPY public.derived_video (content_id, dvd_id, title_ja) FROM stdin;",
		"abc00001\tABC-001\t同一 タイトル",
		"abc00002\tABC-002\t同一タイトル",
		"abc00003\tABC-003\t別タイトル",
		"\\.",
		"",
	}, "\n")
	if _, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	got, err := store.ExactTitleMatches(context.Background(), "同一タイトル")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exact matches=%+v, want two distinct catalogs", got)
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.DVDID] = true
	}
	if !ids["ABC-001"] || !ids["ABC-002"] {
		t.Fatalf("exact ids=%v, want ABC-001 and ABC-002", ids)
	}
}

func TestExactTitleMatchesRejectsMerelySimilarTitle(t *testing.T) {
	path := t.TempDir() + "/r18dev_dump.db"
	dump := strings.Join([]string{
		"COPY public.derived_video (content_id, dvd_id, title_ja) FROM stdin;",
		"abc00001\tABC-001\t完全一致タイトル",
		"\\.",
		"",
	}, "\n")
	if _, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	got, err := store.ExactTitleMatches(context.Background(), "完全一致タイトル別作品")
	if !errors.Is(err, models.ErrDumpMiss) {
		t.Fatalf("got=%+v err=%v, want exact-title miss", got, err)
	}
}
