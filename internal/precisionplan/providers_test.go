package precisionplan

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	name    string
	hit     func(string) (*SearchHit, error)
	queried []string
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Search(_ context.Context, sku string) (*SearchHit, error) {
	f.queried = append(f.queried, sku)
	return f.hit(sku)
}
func TestSearchCandidatesExactJapaneseOnlyAndNoConfirm(t *testing.T) {
	p := &fakeProvider{name: "libredmm", hit: func(s string) (*SearchHit, error) {
		if s == "MIUM-1423" {
			return &SearchHit{ExactSKU: s, WorkID: "product001", JapaneseOriginal: "日本語の正式原題"}, nil
		}
		return nil, nil
	}}
	q := []QueryGroup{{SKU: "MIUM-1423"}, {SKU: "MIUM-1423"}}
	got := SearchCandidates(context.Background(), q, []SearchProvider{p}, LookupPolicy{MaxRequests: 6})
	if got.Requests != 1 || len(got.Candidates) != 1 || got.Candidates[0].State != Review || got.Candidates[0].JapaneseOriginal != "日本語の正式原題" || !got.ReadOnly {
		t.Fatalf("must only produce read-only review candidate, got %+v", got)
	}
}
func TestSearchCandidatesSuffixMismatchNeverBecomesConfirmed(t *testing.T) {
	p := &fakeProvider{name: "libredmm", hit: func(s string) (*SearchHit, error) {
		return &SearchHit{ExactSKU: "START-487", WorkID: "base", JapaneseOriginal: "日本語の正式原題"}, nil
	}}
	got := SearchCandidates(context.Background(), []QueryGroup{{SKU: "START-487-EC"}}, []SearchProvider{p}, LookupPolicy{MaxRequests: 1})
	if len(got.Candidates) != 1 || got.Candidates[0].State != Review || got.Candidates[0].Reason != "provider_edition_or_sku_conflict" || got.Candidates[0].JapaneseOriginal != "" {
		t.Fatalf("suffix collision incorrectly accepted: %+v", got)
	}
}
func TestSearchCandidates429CircuitBreaker(t *testing.T) {
	throttled := &fakeProvider{name: "blocked", hit: func(string) (*SearchHit, error) { return nil, ErrRateLimited }}
	backup := &fakeProvider{name: "backup", hit: func(s string) (*SearchHit, error) {
		return &SearchHit{ExactSKU: s, WorkID: "id-" + s, JapaneseOriginal: "日本語原題"}, nil
	}}
	got := SearchCandidates(context.Background(), []QueryGroup{{SKU: "DCV-093"}, {SKU: "MIUM-1423"}}, []SearchProvider{throttled, backup}, LookupPolicy{MaxRequests: 10})
	if got.RateLimited != 1 || len(throttled.queried) != 1 || len(backup.queried) != 2 || got.Requests != 3 {
		t.Fatalf("429 provider repeated or fallback missing: %+v queries=%v", got, throttled.queried)
	}
}
func TestSearchCandidatesBudgetAndEnglishBlocking(t *testing.T) {
	p := &fakeProvider{name: "provider", hit: func(s string) (*SearchHit, error) {
		return &SearchHit{ExactSKU: s, WorkID: "id", JapaneseOriginal: "Only English"}, nil
	}}
	got := SearchCandidates(context.Background(), []QueryGroup{{SKU: "DCV-093"}, {SKU: "MIUM-1423"}}, []SearchProvider{p}, LookupPolicy{MaxRequests: 1})
	if got.Requests != 1 || got.Deferred != 1 || len(got.Candidates) != 2 || got.Candidates[0].JapaneseOriginal != "" || got.Candidates[0].State != Review || got.Candidates[1].State != Unknown {
		t.Fatalf("budget or English title guard broken: %+v", got)
	}
}
func TestSearchCandidatesRegionalBlockAndTransportError(t *testing.T) {
	restricted := &fakeProvider{name: "regional", hit: func(string) (*SearchHit, error) { return nil, ErrRegionBlocked }}
	offline := &fakeProvider{name: "offline", hit: func(string) (*SearchHit, error) { return nil, errors.New("timeout") }}
	got := SearchCandidates(context.Background(), []QueryGroup{{SKU: "MIUM-1423"}, {SKU: "DCV-093"}}, []SearchProvider{restricted, offline}, LookupPolicy{MaxRequests: 8})
	if got.RegionBlocked != 1 || len(restricted.queried) != 1 || len(offline.queried) != 2 || len(got.Candidates) != 2 {
		t.Fatalf("region block repeated: %+v", got)
	}
}
