package precisionplan

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrRateLimited   = errors.New("provider rate limited")
	ErrRegionBlocked = errors.New("provider region blocked")
)

type SearchHit struct {
	ExactSKU         string
	WorkID           string
	JapaneseOriginal string
	SourceURL        string
}

type SearchProvider interface {
	Name() string
	Search(context.Context, string) (*SearchHit, error)
}

type LookupPolicy struct {
	MaxRequests int
	Timeout     time.Duration
}

type Candidate struct {
	SKU              string
	Source           string
	WorkID           string
	JapaneseOriginal string
	SourceURL        string
	State            string // "review" even when an exact-match title is found
	Reason           string
}

type SearchReport struct {
	Candidates    []Candidate
	Requests      int
	RateLimited   int
	RegionBlocked int
	Deferred      int
	ReadOnly      bool
}

// SearchCandidates discovers original Japanese metadata for already-planned
// queries. It never updates job data or returns a "confirmed" verdict.
// Providers which hit a global 429 or regional block are disabled for this run.
func SearchCandidates(ctx context.Context, groups []QueryGroup, providers []SearchProvider, policy LookupPolicy) SearchReport {
	report := SearchReport{ReadOnly: true, Candidates: make([]Candidate, 0)}
	if policy.MaxRequests <= 0 {
		policy.MaxRequests = 1
	}
	if policy.Timeout <= 0 {
		policy.Timeout = 8 * time.Second
	}
	blocked := map[string]bool{}
	seen := map[string]bool{}
	for _, group := range groups {
		sku := exactSKU(group.SKU)
		if !plausibleSKU(sku) || seen[sku] {
			continue
		}
		seen[sku] = true
		candidate := Candidate{SKU: sku, State: Unknown, Reason: "no_verified_japanese_candidate"}
		for _, provider := range providers {
			if provider == nil || blocked[provider.Name()] {
				continue
			}
			if ctx.Err() != nil {
				candidate.Reason = "cancelled"
				candidate.State = Error
				break
			}
			if report.Requests >= policy.MaxRequests {
				candidate.Reason = "request_budget_exhausted"
				report.Deferred++
				break
			}
			reqCtx, cancel := context.WithTimeout(ctx, policy.Timeout)
			hit, err := provider.Search(reqCtx, sku)
			cancel()
			report.Requests++
			if errors.Is(err, ErrRateLimited) {
				blocked[provider.Name()] = true
				report.RateLimited++
				candidate.Reason = "provider_rate_limited"
				continue
			}
			if errors.Is(err, ErrRegionBlocked) {
				blocked[provider.Name()] = true
				report.RegionBlocked++
				candidate.Reason = "provider_region_blocked"
				continue
			}
			if err != nil {
				candidate.Reason = "provider_unavailable"
				continue
			}
			if hit == nil {
				continue
			}
			if exactSKU(hit.ExactSKU) != sku {
				candidate.State = Review
				candidate.Reason = "provider_edition_or_sku_conflict"
				continue
			}
			if !japanese.MatchString(strings.TrimSpace(hit.JapaneseOriginal)) {
				candidate.State = Review
				candidate.Reason = "japanese_original_not_verified"
				continue
			}
			if strings.TrimSpace(hit.WorkID) == "" {
				candidate.State = Review
				candidate.Reason = "missing_work_identity"
				continue
			}
			candidate = Candidate{SKU: sku, Source: provider.Name(), WorkID: hit.WorkID,
				JapaneseOriginal: strings.TrimSpace(hit.JapaneseOriginal),
				SourceURL:        hit.SourceURL, State: Review, Reason: "candidate_only_requires_authoritative_identity_check"}
			break
		}
		report.Candidates = append(report.Candidates, candidate)
	}
	return report
}
