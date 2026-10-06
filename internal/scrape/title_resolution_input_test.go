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
