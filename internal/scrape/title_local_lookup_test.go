package scrape

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

type fakeTitleLookup struct {
	matches []models.DumpTitleMatch
	err     error
	calls   int
}

func (f *fakeTitleLookup) SearchByTitle(_ context.Context, _ string, _ int) ([]models.DumpTitleMatch, error) {
	f.calls++
	return f.matches, f.err
}

func (f *fakeTitleLookup) ExactTitleMatches(_ context.Context, _ string) ([]models.DumpTitleMatch, error) {
	return f.matches, f.err
}

type splitExactTitleLookup struct {
	searchMatches []models.DumpTitleMatch
	exactMatches  []models.DumpTitleMatch
	exactErr      error
	searchCalls   int
}

func (f *splitExactTitleLookup) SearchByTitle(_ context.Context, _ string, _ int) ([]models.DumpTitleMatch, error) {
	f.searchCalls++
	return f.searchMatches, nil
}

func (f *splitExactTitleLookup) ExactTitleMatches(_ context.Context, _ string) ([]models.DumpTitleMatch, error) {
	if f.exactErr != nil {
		return nil, f.exactErr
	}
	return f.exactMatches, nil
}

type richFakeTitleLookup struct {
	fakeTitleLookup
	movie *models.DumpMovie
}

func (f *richFakeTitleLookup) LookupByDVDID(context.Context, string) (string, error) {
	return "", models.ErrDumpMiss
}

func (f *richFakeTitleLookup) LookupByContentID(context.Context, string) (string, error) {
	return "", models.ErrDumpMiss
}

func (f *richFakeTitleLookup) MatchByDisplayID(context.Context, string) ([]models.DumpMatch, error) {
	return nil, models.ErrDumpMiss
}

func (f *richFakeTitleLookup) LookupMovie(context.Context, string) (*models.DumpMovie, error) {
	if f.movie == nil {
		return nil, models.ErrDumpMiss
	}
	return f.movie, nil
}

func (f *richFakeTitleLookup) Stats(context.Context) (models.DumpStats, error) {
	return models.DumpStats{}, nil
}

func TestTitleCatalogResolverUsesLocalTitleThenJevWithoutWeb(t *testing.T) {
	jevCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jevCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-test","answers":{"catalog_id_correct":{"type":"noul","noul":0.96}}}`))
	}))
	defer server.Close()

	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{
		{
			ContentID: "118ipx00072",
			DVDID:     "IPX-072",
			TitleJa:   "狙われた通学路 共謀痴漢電車",
			TitleEn:   "Targeted School Route",
			Score:     0.96,
		},
	}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{
		JevCatalogEnabled:   true,
		JevCatalogAPIKey:    "test-key",
		JevCatalogThreshold: 0.80,
		JevCatalogModel:     "jev-test",
		JevCatalogEndpoint:  server.URL,
	}, lookup)

	got, err := resolver.Resolve(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
	if err != nil {
		t.Fatal(err)
	}
	if got != "IPX-072" {
		t.Fatalf("resolved=%q, want IPX-072", got)
	}
	if lookup.calls != 1 {
		t.Fatalf("local lookup calls=%d, want 1", lookup.calls)
	}
	if jevCalls != 1 {
		t.Fatalf("Jev calls=%d, want 1", jevCalls)
	}
}

func TestTitleCatalogResolverSkipsWeakLocalCandidate(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{
		{DVDID: "ABC-123", TitleJa: "別作品", Score: 0.40},
	}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	if _, ok := resolver.resolveFromLocalTitle(context.Background(), "目的の作品タイトル"); ok {
		t.Fatal("weak local candidate must not be automatically accepted")
	}
}

func TestLocalTitleRequiresJevAtThreshold(t *testing.T) {
	for _, tc := range []struct {
		name        string
		enabled     bool
		probability float64
		accepted    bool
	}{
		{"threshold", true, 0.80, true},
		{"below", true, 0.799, false},
		{"disabled", false, 0.96, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TYPESAFE_API_KEY", "")
			t.Setenv("JAVINIZER_JEV_CATALOG_THRESHOLD", "")
			calls := 0
			lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{{DVDID: "IPX-072", TitleJa: "目的の作品タイトル", Score: 0.99}}}
			resolver := NewTitleCatalogResolverWithLookup(&Config{JevCatalogEnabled: tc.enabled, JevCatalogAPIKey: "test-key", JevCatalogThreshold: 0.80, JevCatalogEndpoint: "http://127.0.0.1/v1/systemone"}, lookup)
			resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return jevLookupResponse(200, `{"answers":{"catalog_id_correct":{"type":"noul","noul":`+fmtProbability(tc.probability)+`}}}`), nil
			})
			id, ok := resolver.resolveFromLocalTitle(context.Background(), "目的の作品タイトル")
			if ok != tc.accepted {
				t.Fatalf("id=%s accepted=%v, want %v", id, ok, tc.accepted)
			}
			if tc.enabled && calls == 0 {
				t.Fatal("local title bypassed Jev")
			}
		})
	}
}

func TestLocalTitleFallbackToExistingWeb(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		matches  []models.DumpTitleMatch
		err      error
	}{
		{name: "miss", err: models.ErrDumpMiss},
		{name: "unavailable", err: models.ErrDumpTitleSearchUnavailable},
		{name: "malformed", response: "not-json", matches: []models.DumpTitleMatch{{DVDID: "ABC-123", TitleJa: "狙われた通学路 共謀痴漢電車", Score: 0.99}}},
		{name: "http_error", response: "http-error", matches: []models.DumpTitleMatch{{DVDID: "ABC-123", TitleJa: "狙われた通学路 共謀痴漢電車", Score: 0.99}}},
		{name: "rejected", matches: []models.DumpTitleMatch{{DVDID: "ABC-123", TitleJa: "狙われた通学路 共謀痴漢電車", Score: 0.99}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sawJev := false
			scraper := newWebToJevLookupScraper(t, 0.80, &sawJev)
			original := scraper.httpClient
			localCalls := 0
			scraper.httpClient = jevLookupHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost {
					var payload jevSystemOneRequest
					body, _ := io.ReadAll(req.Body)
					_ = json.Unmarshal(body, &payload)
					req.Body = io.NopCloser(bytes.NewReader(body))
					if payload.State.CandidateCatalogID == "ABC-123" {
						localCalls++
						if tc.response == "http-error" {
							return jevLookupResponse(503, ""), nil
						}
						if tc.response != "" {
							return jevLookupResponse(200, tc.response), nil
						}
						return jevLookupResponse(200, `{"answers":{"catalog_id_correct":{"type":"noul","noul":0.1}}}`), nil
					}
				}
				return original.Do(req)
			})
			resolver := &TitleCatalogResolver{scraper: scraper, titleLookup: &fakeTitleLookup{matches: tc.matches, err: tc.err}}
			id, err := resolver.Resolve(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
			if err != nil || id != "IPX-072" || !sawJev {
				t.Fatalf("id=%s err=%v web Jev=%v", id, err, sawJev)
			}
			if len(tc.matches) > 0 && localCalls != 1 {
				t.Fatalf("local Jev calls=%d", localCalls)
			}
		})
	}
}

func TestLocalTitleCancellationPreventsWeb(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, &fakeTitleLookup{err: context.Canceled})
	resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled local search started Web")
		return nil, context.Canceled
	})
	id, err := resolver.Resolve(ctx, "目的の作品タイトル")
	if id != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("id=%s err=%v", id, err)
	}
}

func TestTitleCatalogResolverInitializesDirectJavDBSource(t *testing.T) {
	r := NewTitleCatalogResolver(&Config{})
	if r == nil || r.scraper == nil || r.scraper.registry == nil {
		t.Fatal("resolver direct-source registry is not initialized")
	}
	instance, ok := r.scraper.registry.GetInstance("javdb")
	if !ok || instance == nil {
		t.Fatal("javdb direct title source is not registered")
	}
	if !instance.IsEnabled() {
		t.Fatal("javdb direct title source is not enabled")
	}
	if !r.scraper.cfg.PreferNonGoogleTitleSearch {
		t.Fatal("bulk resolver must prefer non-Google public search")
	}
	if !r.scraper.cfg.DisableHeadlessTitleSearch {
		t.Fatal("bulk resolver must disable headless public-search retry")
	}
}

func TestLocalTitleEvidenceIncludesFullDumpMetadata(t *testing.T) {
	lookup := &richFakeTitleLookup{
		fakeTitleLookup: fakeTitleLookup{matches: []models.DumpTitleMatch{{
			ContentID: "118ipx00072",
			DVDID:     "IPX-072",
			TitleJa:   "狙われた通学路 共謀痴漢電車",
			TitleEn:   "Targeted School Route",
			Score:     1.0,
		}}},
		movie: &models.DumpMovie{
			ContentID:   "118ipx00072",
			DVDID:       "IPX-072",
			TitleJa:     "狙われた通学路 共謀痴漢電車",
			TitleEn:     "Targeted School Route",
			ReleaseDate: "2018-08-19",
			Runtime:     120,
			Maker:       &models.DumpNamedEntity{NameJa: "アイデアポケット"},
			Actresses:   []models.DumpActress{{NameKanji: "桃乃木かな"}},
		},
	}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	evidence := resolver.buildLocalTitleEvidence(context.Background(), lookup.matches)
	if len(evidence) < 2 {
		t.Fatalf("evidence=%#v, want full metadata plus title-index evidence", evidence)
	}
	full := evidence[0].Snippet
	for _, want := range []string{"catalog_id=IPX-072", "桃乃木かな", "アイデアポケット", "release_date=2018-08-19", "runtime_mins=120"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full metadata evidence missing %q: %s", want, full)
		}
	}
}

func TestUniqueExactLocalTitlePreservesEditionSuffixWithoutJev(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{
		{DVDID: "SONE-999BOD", TitleJa: "おもてなしするって言ったのに、大量肉棒でちんボコされドM本性丸出しでアヘイキ大乱交しちゃった 音無鈴 （BOD）", Score: 1.0},
		{DVDID: "SONE-999", TitleJa: "おもてなしするって言ったのに、大量肉棒でちんボコされドM本性丸出しでアヘイキ大乱交しちゃった 音無鈴", Score: 0.989},
	}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{JevCatalogEnabled: true, JevCatalogAPIKey: "unused"}, lookup)
	resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unique exact local title must not call Jev/Web")
		return nil, nil
	})
	id, ok := resolver.resolveFromLocalTitle(context.Background(), "おもてなしするって言ったのに、大量肉棒でちんボコされドM本性丸出しでアヘイキ大乱交しちゃった 音無鈴 （BOD）")
	if !ok || id != "SONE-999BOD" {
		t.Fatalf("id=%q ok=%v", id, ok)
	}
}

func TestResolveDecisionConfirmsVerifiedCatalogID(t *testing.T) {
	lookup := &richFakeTitleLookup{
		movie: &models.DumpMovie{DVDID: "IPX-072", TitleJa: "狙われた通学路 共謀痴漢電車 桃乃木かな"},
	}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	decision, err := resolver.ResolveDecision(context.Background(), "IPX-072.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionConfirmed || decision.CatalogID != "IPX-072" || !decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestResolveDecisionCatalogWithoutLocalProofStaysReview(t *testing.T) {
	resolver := NewTitleCatalogResolver(&Config{})
	decision, err := resolver.ResolveDecision(context.Background(), "IPX-072.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.CatalogID != "IPX-072" || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestResolveDecisionUniqueExactLocalTitleIsConfirmed(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{{
		DVDID:   "IPX-072",
		TitleJa: "狙われた通学路 共謀痴漢電車 桃乃木かな",
		Score:   1.0,
	}}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	decision, err := resolver.ResolveDecision(context.Background(), "狙われた通学路 共謀痴漢電車 桃乃木かな")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionConfirmed || decision.CatalogID != "IPX-072" || !decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestResolveDecisionShortUniqueExactLocalTitleRequiresReview(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{{
		DVDID:   "KKV-538",
		TitleJa: "me",
		Score:   1.0,
	}}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	decision, err := resolver.ResolveDecision(context.Background(), "me")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.CatalogID != "KKV-538" || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v, want short exact title to remain review", decision)
	}
}

func TestResolveDecisionDuplicateExactLocalTitlesRequireReview(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{
		{DVDID: "ABC-001", TitleJa: "同一タイトル", Score: 1.0},
		{DVDID: "ABC-002", TitleJa: "同一タイトル", Score: 1.0},
	}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	decision, err := resolver.ResolveDecision(context.Background(), "同一タイトル")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestAutoOrganizeEligibilityRequiresApprovedDecisiveEvidence(t *testing.T) {
	tests := []struct {
		name     string
		decision TitleResolutionDecision
		want     bool
	}{
		{"verified catalog", TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: "IPX-072", Method: "品番とローカルDBの完全一致"}, true},
		{"db-bound catalog cache", TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: "IPX-072", Method: "品番とローカルDBの完全一致キャッシュ"}, true},
		{"exact title", TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: "IPX-072", Method: "ローカルタイトル完全一致"}, true},
		{"empty catalog", TitleResolutionDecision{Status: TitleDecisionConfirmed, Method: "品番とローカルDBの完全一致"}, false},
		{"embedded title is file-specific", TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: "IPX-072", Method: "埋込タイトル→ローカルタイトル完全一致"}, false},
		{"unapproved method", TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: "IPX-072", Method: "外部検索・Jevによる補助判定"}, false},
		{"review", TitleResolutionDecision{Status: TitleDecisionReview, CatalogID: "IPX-072", Method: "品番とローカルDBの完全一致"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.decision.AutoOrganizeEligible(); got != tt.want {
				t.Fatalf("eligible=%v, want %v: %+v", got, tt.want, tt.decision)
			}
		})
	}
}

func TestResolveDecisionExactTitleWithConflictingCatalogTokenRequiresReview(t *testing.T) {
	lookup := &fakeTitleLookup{matches: []models.DumpTitleMatch{{
		DVDID:   "ABC-001",
		TitleJa: "作品タイトル XYZ-999",
		Score:   1.0,
	}}}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	decision, err := resolver.ResolveDecision(context.Background(), "作品タイトル XYZ-999")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v, want review due conflicting catalog evidence", decision)
	}
}

func TestResolveDecisionUnsafeCatalogHintUsesLocalDBForReviewWithoutWeb(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		id    string
	}{
		{name: "mid filename", input: "226655.xyz XVSR-688.mp4はPikPakで共有されています", id: "XVSR-688"},
		{name: "glued japanese", input: "NACR-442若くて美しい父の二番目の妻", id: "NACR-442"},
		{name: "legacy version marker", input: "ROYD-_v115【モザイク除去】タイトル", id: "ROYD-115"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &richFakeTitleLookup{movie: &models.DumpMovie{DVDID: tc.id}}
			resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
			decision, err := resolver.ResolveDecision(context.Background(), tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Status != TitleDecisionReview || decision.CatalogID != tc.id {
				t.Fatalf("decision=%+v, want review %s", decision, tc.id)
			}
			if decision.AutoOrganizeEligible() {
				t.Fatalf("unsafe catalog hint became auto-organize eligible: %+v", decision)
			}
			if decision.Method != "品番候補＋ローカルDB存在確認" {
				t.Fatalf("method=%q", decision.Method)
			}
		})
	}
}

func TestReviewOnlyCatalogHintPreservesEditionSuffix(t *testing.T) {
	got := extractReviewOnlyCatalogCandidates("foo START-487-EC bar")
	if len(got) != 1 || got[0] != "START-487-EC" {
		t.Fatalf("review hints=%v, want exact START-487-EC without base collapse", got)
	}
}

func TestResolveDecisionExactScoreLocalAmbiguityStopsAtReviewWithoutWeb(t *testing.T) {
	lookup := &splitExactTitleLookup{
		exactErr: models.ErrDumpMiss,
		searchMatches: []models.DumpTitleMatch{
			{DVDID: "DASD-864", Score: 1.0},
			{DVDID: "4DAS864", Score: 1.0},
		},
	}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("known local ambiguity must not call Web")
		return nil, nil
	})
	decision, err := resolver.ResolveDecision(context.Background(), "完全一致相当タイトル")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.CatalogID != "" || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v, want ambiguous review", decision)
	}
}

func TestResolveDecisionSingleExactScoreWithoutGlobalProofStopsAtReview(t *testing.T) {
	lookup := &splitExactTitleLookup{
		exactErr: models.ErrDumpMiss,
		searchMatches: []models.DumpTitleMatch{
			{DVDID: "NKKD-287", Score: 1.0},
		},
	}
	resolver := NewTitleCatalogResolverWithLookup(&Config{}, lookup)
	resolver.scraper.httpClient = jevLookupHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("strong local review candidate must not call Web")
		return nil, nil
	})
	decision, err := resolver.ResolveDecision(context.Background(), "完全一致相当タイトル")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != TitleDecisionReview || decision.CatalogID != "NKKD-287" || decision.AutoOrganizeEligible() {
		t.Fatalf("decision=%+v, want single-candidate review", decision)
	}
}
