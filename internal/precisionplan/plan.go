package precisionplan

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// PolicyVersion is the minimum accepted current decision policy.
// Only a server-computed verdict from this policy can enter Ready.
const PolicyVersion = "precision-first-jp-v1"

const (
	Confirmed = "confirmed"
	Review    = "review"
	Unknown   = "unknown"
	Error     = "error"
)

type File struct {
	Path          string
	ParsedSKU     string
	CurrentStatus string
	WorkID        string
	OriginalTitle string
	Excluded      bool
}

type Verdict struct {
	State         string
	PolicyVersion string
	SKU           string
	WorkID        string
	JapaneseTitle string
	Reasons       []string
	// Verified must be set by the server-side authority-checking engine,
	// never a web scraper, a score, or a client-provided field.
	Verified bool
}

type ReadyItem struct {
	Path          string
	SKU           string
	WorkID        string
	JapaneseTitle string
	PolicyVersion string
}

type SupplementalItem struct {
	Path   string
	SKU    string
	Reason string
}

type HeldItem struct {
	Path   string
	State  string
	Reason string
}

type Plan struct {
	PolicyVersion string
	Total         int
	Ready         []ReadyItem
	Supplemental  []SupplementalItem
	Held          []HeldItem
	Excluded      int
	ReadOnly      bool
	Errors        int
}

var skuPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,16}(?:-[A-Z0-9]{1,16}){1,4}$`)
var digits = regexp.MustCompile(`[0-9]{2,}`)
var overlongNumericID = regexp.MustCompile(`[0-9]{9,}`)
var japanese = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}]`)

func exactSKU(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func plausibleSKU(s string) bool {
	s = exactSKU(s)
	return skuPattern.MatchString(s) && digits.MatchString(s) && !overlongNumericID.MatchString(s) && !strings.HasPrefix(s, "MERGED-")
}

// Build constructs a READ-ONLY plan. It never authorizes an apply operation.
// Its input verdicts must come from a trusted in-process audit, not the client.
func Build(files []File, verdicts map[string]Verdict) (Plan, error) {
	plan := Plan{
		PolicyVersion: PolicyVersion,
		ReadOnly:      true,
		Ready:         make([]ReadyItem, 0),
		Supplemental:  make([]SupplementalItem, 0),
		Held:          make([]HeldItem, 0),
	}
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Path == "" {
			return Plan{}, errors.New("precision plan: empty file path")
		}
		if _, exists := seen[file.Path]; exists {
			return Plan{}, fmt.Errorf("precision plan: duplicate file path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		plan.Total++
		if file.Excluded {
			plan.Excluded++
			continue
		}
		verdict, ok := verdicts[file.Path]
		sku := exactSKU(file.ParsedSKU)
		missingJP := !japanese.MatchString(strings.TrimSpace(file.OriginalTitle))
		if file.CurrentStatus == "failed" && plausibleSKU(sku) {
			plan.Supplemental = append(plan.Supplemental, SupplementalItem{
				Path: file.Path, SKU: sku, Reason: "upstream_search_failed",
			})
		} else if missingJP && plausibleSKU(sku) {
			plan.Supplemental = append(plan.Supplemental, SupplementalItem{
				Path: file.Path, SKU: sku, Reason: "japanese_original_missing",
			})
		}
		if !ok {
			plan.Held = append(plan.Held, HeldItem{Path: file.Path, State: Review, Reason: "missing_authoritative_audit"})
			continue
		}
		switch verdict.State {
		case Confirmed:
			if verdict.PolicyVersion != PolicyVersion || !verdict.Verified ||
				!plausibleSKU(sku) || exactSKU(verdict.SKU) != sku ||
				strings.TrimSpace(verdict.WorkID) == "" ||
				verdict.WorkID != file.WorkID ||
				!japanese.MatchString(strings.TrimSpace(verdict.JapaneseTitle)) ||
				!japanese.MatchString(strings.TrimSpace(file.OriginalTitle)) ||
				strings.TrimSpace(file.OriginalTitle) != strings.TrimSpace(verdict.JapaneseTitle) ||
				file.CurrentStatus != "completed" {
				plan.Held = append(plan.Held, HeldItem{Path: file.Path, State: Review, Reason: "unsafe_or_stale_confirmation"})
				continue
			}
			plan.Ready = append(plan.Ready, ReadyItem{
				Path: file.Path, SKU: sku, WorkID: verdict.WorkID,
				JapaneseTitle: verdict.JapaneseTitle, PolicyVersion: PolicyVersion,
			})
		case Review, Unknown, Error:
			plan.Held = append(plan.Held, HeldItem{Path: file.Path, State: verdict.State, Reason: strings.Join(verdict.Reasons, ";")})
			if verdict.State == Error {
				plan.Errors++
			}
		default:
			plan.Held = append(plan.Held, HeldItem{Path: file.Path, State: Review, Reason: "invalid_audit_state"})
		}
	}
	for p := range verdicts {
		if _, present := seen[p]; !present {
			return Plan{}, fmt.Errorf("precision plan: verdict refers to file not in job: %q", p)
		}
	}
	sort.Slice(plan.Ready, func(i, j int) bool { return plan.Ready[i].Path < plan.Ready[j].Path })
	sort.Slice(plan.Supplemental, func(i, j int) bool { return plan.Supplemental[i].Path < plan.Supplemental[j].Path })
	sort.Slice(plan.Held, func(i, j int) bool { return plan.Held[i].Path < plan.Held[j].Path })
	return plan, nil
}
