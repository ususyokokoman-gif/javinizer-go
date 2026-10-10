//go:build sqlite_fts5

package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestSearchByTitleFindsJapaneseTitleWithActressSuffix(t *testing.T) {
	path := t.TempDir() + "/r18dev_dump.db"
	dump := strings.Join([]string{
		"COPY public.derived_video (content_id, dvd_id, title_en, title_ja) FROM stdin;",
		"118ipx00072\tIPX-072\tTargeted School Route\t狙われた通学路 共謀痴漢電車",
		"118ipx00073\tIPX-073\tDifferent Work\tまったく別の作品タイトル",
		"\\.",
		"",
	}, "\n")
	if _, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{SourceDate: "2026-10-04"}); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	got, err := store.SearchByTitle(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no title candidates")
	}
	if got[0].DVDID != "IPX-072" {
		t.Fatalf("top DVDID=%q, want IPX-072; matches=%+v", got[0].DVDID, got)
	}
	if got[0].Score < 0.90 {
		t.Fatalf("top score=%.3f, want >= 0.90", got[0].Score)
	}
}

func TestSearchByTitleStripsCommonFilenamePrefix(t *testing.T) {
	path := t.TempDir() + "/r18dev_dump.db"
	dump := strings.Join([]string{
		"COPY public.derived_video (content_id, dvd_id, title_ja) FROM stdin;",
		"ssis00001\tSSIS-001\t一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間",
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
	defer func() { _ = store.Close() }()

	got, err := store.SearchByTitle(context.Background(), "【高画質】一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].DVDID != "SSIS-001" {
		t.Fatalf("matches=%+v, want SSIS-001 first", got)
	}
}

func TestTitleSimilarityTreatsCensorMarkerAsWildcard(t *testing.T) {
	got := titleSimilarity(
		"今日、あなたの上司に犯されました。 大橋未久",
		"今日、あなたの上司に犯●れました。 大橋未久",
	)
	if got < 0.95 {
		t.Fatalf("score=%.3f, want >= 0.95", got)
	}
}

func TestDedupeTitleMatchesPrefersStandardDisplayID(t *testing.T) {
	got := dedupeTitleMatchesByDisplayID([]models.DumpTitleMatch{
		{ContentID: "4ipz508", DVDID: "4IPZ508", TitleJa: "作品", Score: 0.90},
		{ContentID: "ipz00508", DVDID: "IPZ-508", TitleJa: "作品", Score: 0.90},
	})
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1: %+v", len(got), got)
	}
	if got[0].DVDID != "IPZ-508" {
		t.Fatalf("DVDID=%q, want IPZ-508", got[0].DVDID)
	}
}

func TestSearchByTitleEnglishAndIndexUpgrade(t *testing.T) {
	path := t.TempDir() + "/dump.db"
	dump := "COPY public.derived_video (content_id, dvd_id, title_en, title_ja) FROM stdin;\nabc00123\tABC-123\tTargeted School Route\t日本語の作品名\n\\.\n"
	if _, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TABLE video_titles_fts"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SearchByTitle(context.Background(), "Targeted School Route", 5); !errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
		t.Fatalf("missing index: %v", err)
	}
	_ = store.Close()
	for i := 0; i < 2; i++ {
		if err := EnsureTitleSearchIndex(context.Background(), path); err != nil {
			t.Fatal(err)
		}
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.SearchByTitle(context.Background(), "targeted school route", 5)
	if err != nil || len(got) != 1 || got[0].DVDID != "ABC-123" {
		t.Fatalf("english lookup: %+v %v", got, err)
	}
	if _, err := store.SearchByTitle(context.Background(), "unknown title", 5); !errors.Is(err, models.ErrDumpMiss) {
		t.Fatalf("miss: %v", err)
	}
}

func TestTitleSimilarityStripsCommonPrefixSymmetrically(t *testing.T) {
	title := "【数量限定】彼氏のじゃ満足出来なくて…帰省した実家でおじさんのデカチン沼にハマってしまいました。 山下紗和 パンティと写真付き"
	if got := titleSimilarity(title, title); got != 1 {
		t.Fatalf("score=%f, want 1", got)
	}
}

func TestDedupeTitleMatchesPrefersHyphenatedCanonicalDisplayID(t *testing.T) {
	got := dedupeTitleMatchesByDisplayID([]models.DumpTitleMatch{
		{ContentID: "118abw366r", DVDID: "ABW366", TitleJa: "作品", Score: 1.0},
		{ContentID: "118abw366", DVDID: "ABW-366", TitleJa: "作品", Score: 1.0},
	})
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1: %+v", len(got), got)
	}
	if got[0].DVDID != "ABW-366" {
		t.Fatalf("DVDID=%q, want ABW-366", got[0].DVDID)
	}
}

func TestExactTitleMatchesReturnsAllDuplicateCatalogs(t *testing.T) {
	path := t.TempDir() + "/exact-duplicates.db"
	dump := strings.Join([]string{
		"COPY public.derived_video (content_id, dvd_id, title_ja) FROM stdin;",
		"abc00001\tABC-001\t同一タイトル",
		"abc00002\tABC-002\t同一タイトル",
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
	if len(got) != 2 || got[0].DVDID != "ABC-001" || got[1].DVDID != "ABC-002" {
		t.Fatalf("matches=%+v, want both exact catalogs", got)
	}
}

func TestExactTitleMatchesDoesNotNormalizeNearMatchIntoConfirmation(t *testing.T) {
	path := t.TempDir() + "/strict-exact.db"
	dump := "COPY public.derived_video (content_id, dvd_id, title_ja) FROM stdin;\nabc00001\tABC-001\t作品タイトル！\n\\.\n"
	if _, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.ExactTitleMatches(context.Background(), "作品タイトル"); !errors.Is(err, models.ErrDumpMiss) {
		t.Fatalf("near match must not be exact proof: %v", err)
	}
}
