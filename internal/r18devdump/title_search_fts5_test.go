//go:build sqlite_fts5

package r18devdump

import (
	"context"
	"strings"
	"testing"
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
