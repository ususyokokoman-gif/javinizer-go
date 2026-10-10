package scrape

import (
	"context"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func newNoWebResolver(t *testing.T, lookup models.R18DevTitleLookup) *TitleCatalogResolver {
	t.Helper()
	r := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	r.scraper.registry = nil
	r.scraper.httpClient = jevLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe/low-evidence input reached public Web search")
		return nil, nil
	})
	return r
}

func TestResolveDecisionGluedLeadingUnverifiedCatalogStopsBeforeWeb(t *testing.T) {
	inputs := []string{
		"300MIUM-1367俺たち結局ギャルが好き!屈託ない笑顔",
		"DANDYA-031誰もが憧れるスレンダーCA",
		"DASS-931神対応えぐいおもてなしこそ私の流儀です",
		"FNS-083妹の彼氏をゆるゆる部屋着で誘惑",
		"KOJA-044極上テクニックで精液全て絞り出しても終わらない",
		"ROYD-310妄想エロマンガを見てしまった隣のモデル級お姉さん",
		"LULU-1292人きりの社内でピタパン人妻女上司",
	}
	for _, input := range inputs {
		t.Run(input[:minInt(10, len(input))], func(t *testing.T) {
			r := newNoWebResolver(t, &richFakeTitleLookup{})
			decision, err := r.ResolveDecision(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Status != TitleDecisionReview || decision.CatalogID != "" || decision.AutoOrganizeEligible() {
				t.Fatalf("decision=%+v, want boundary-ambiguous review without catalog", decision)
			}
		})
	}
}

func TestResolveDecisionMultipleRawCatalogHintsUsesOnlyVerifiedLocalCandidate(t *testing.T) {
	r := newNoWebResolver(t, &richFakeTitleLookup{
		movie: &models.DumpMovie{DVDID: "AGMX-162"},
	})
	decision, err := r.ResolveDecision(context.Background(), "hhd800.com@AGMX-162")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.CatalogID != "AGMX-162" || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v, want locally verified AGMX-162 review", decision)
	}
}

func TestResolveDecisionLowEvidenceGarbageNeverStartsWebSearch(t *testing.T) {
	inputs := []string{
		"EarnVids",
		"a83d9d5b_sq_7-_v1-1_(_v1)",
		"KAORU_ADULT-1882382773811560487-01 - コピー",
		"unknown-1926025400838168644(1)",
		"Nipple Playと寸止め手コキ Vol 3 - Pornhub com",
		"ドs痴女性感メンズエステ 全身密着マッサージ なぎさのファンクラブの商品ファンティア[fantia]",
		"これも写メ日記に載せたけど再生できなくなってたからついったーへ",
	}
	for _, input := range inputs {
		t.Run(input[:minInt(10, len(input))], func(t *testing.T) {
			r := newNoWebResolver(t, &richFakeTitleLookup{})
			decision, err := r.ResolveDecision(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Status != TitleDecisionUnknown || decision.CatalogID != "" || decision.AutoOrganizeEligible() {
				t.Fatalf("decision=%+v, want unknown without public Web", decision)
			}
		})
	}
}

func TestResolveDecisionPlatformNoiseOverridesMisleadingLocalSimilarity(t *testing.T) {
	lookup := &splitExactTitleLookup{
		exactErr: models.ErrDumpMiss,
		searchMatches: []models.DumpTitleMatch{{
			DVDID:   "SIMM-474",
			TitleJa: "別作品",
			Score:   0.96,
		}},
	}
	r := newNoWebResolver(t, lookup)
	decision, err := r.ResolveDecision(context.Background(), "Nipple Playと寸止め手コキ Vol 3 - Pornhub com")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionUnknown || decision.CatalogID != "" || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v, want platform-noise unknown", decision)
	}
}
