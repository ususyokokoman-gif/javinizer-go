package scrape

import "testing"

func TestPrepareTitleResolutionInputClassifiesHumanTitle(t *testing.T) {
	got := PrepareTitleResolutionInput("狙われた通学路 共謀痴漢電車 桃乃木かな_1080p.mp4")
	if got.Kind != TitleInputTitle {
		t.Fatalf("kind=%s, want title", got.Kind)
	}
	if got.Query != "狙われた通学路 共謀痴漢電車 桃乃木かな" {
		t.Fatalf("query=%q", got.Query)
	}
}

func TestPrepareTitleResolutionInputClassifiesOpaqueKey(t *testing.T) {
	got := PrepareTitleResolutionInput("c9dqb3ybpvq80kae_1280p.mp4")
	if got.Kind != TitleInputOpaque {
		t.Fatalf("kind=%s, want opaque_id", got.Kind)
	}
	if got.Query != "c9dqb3ybpvq80kae" {
		t.Fatalf("query=%q", got.Query)
	}
}

func TestPrepareTitleResolutionInputClassifiesLongNumericKey(t *testing.T) {
	got := PrepareTitleResolutionInput("1099490057860206593_1080p.mp4")
	if got.Kind != TitleInputOpaque {
		t.Fatalf("kind=%s, want opaque_id", got.Kind)
	}
	if got.Query != "1099490057860206593" {
		t.Fatalf("query=%q", got.Query)
	}
}

func TestPrepareTitleResolutionInputPreservesCatalogID(t *testing.T) {
	got := PrepareTitleResolutionInput("IPX-072_1080p.mp4")
	if got.Kind != TitleInputCatalog {
		t.Fatalf("kind=%s, want catalog", got.Kind)
	}
	if got.Query != "IPX-072" {
		t.Fatalf("query=%q", got.Query)
	}
}

func TestPrepareTitleResolutionInputStripsOpaquePartSuffix(t *testing.T) {
	got := PrepareTitleResolutionInput("1099490057860206593_1.mp4")
	if got.Kind != TitleInputOpaque {
		t.Fatalf("kind=%s, want opaque_id", got.Kind)
	}
	if got.Query != "1099490057860206593" {
		t.Fatalf("query=%q", got.Query)
	}
}

func TestStandaloneCatalogExtractionRejectsSubstringInsideOpaqueKey(t *testing.T) {
	if ids := extractStandaloneCatalogCandidates("abcre-0275ixyz_720p"); len(ids) != 0 {
		t.Fatalf("ids=%v, opaque substring must not auto-resolve", ids)
	}
	ids := extractStandaloneCatalogCandidates("RE-0275I")
	if len(ids) != 1 || ids[0] != "RE-0275I" {
		t.Fatalf("standalone ids=%v, want [RE-0275I]", ids)
	}
}

func TestCatalogSuffixVariantsArePreserved(t *testing.T) {
	for _, want := range []string{"START-487-EC", "FSDSS-999TK", "SNIS-999BOD", "SONE-999BOD"} {
		got := PrepareTitleResolutionInput(want + ".mp4")
		if got.Kind != TitleInputCatalog {
			t.Fatalf("%s kind=%s, want catalog", want, got.Kind)
		}
		ids := extractStandaloneCatalogCandidates(got.Query)
		if len(ids) != 1 || ids[0] != want {
			t.Fatalf("%s ids=%v", want, ids)
		}
	}
}

func TestPrepareTitleResolutionInputClassifiesLeadingCatalogBeforeTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ADN-444_【モザイク除去】地味で無口な隣のお姉さん_黒川すみれ.mp4", "ADN-444"},
		{"dass-003_隣人に俺の妻が寝取られて。_黒川すみれ.mkv", "DASS-003"},
		{"CJOD-179_hdハメを外した女教師に誘惑されて_黒川すみれ.mp4", "CJOD-179"},
		{"START-487-EC_作品タイトル.mp4", "START-487-EC"},
		{"FSDSS-999TK_作品タイトル.mp4", "FSDSS-999TK"},
		{"SNIS-999BOD_作品タイトル.mp4", "SNIS-999BOD"},
	}
	for _, tt := range tests {
		got := PrepareTitleResolutionInput(tt.input)
		if got.Kind != TitleInputCatalog {
			t.Errorf("%q kind=%s, want catalog", tt.input, got.Kind)
			continue
		}
		if got.Query != tt.want {
			t.Errorf("%q query=%q, want %q", tt.input, got.Query, tt.want)
		}
	}
}

func TestPrepareTitleResolutionInputDoesNotPromoteEmbeddedOpaqueCatalogSubstring(t *testing.T) {
	got := PrepareTitleResolutionInput("abcre-0275ixyz_720p.mp4")
	if got.Kind == TitleInputCatalog {
		t.Fatalf("kind=%s query=%q; opaque substring must not become catalog", got.Kind, got.Query)
	}
}
