package scrape

import "testing"

func TestAutoOrganizeEligibleRequiresApprovedEvidence(t *testing.T) {
	for _, method := range []string{
		"品番とローカルDBの完全一致",
		"品番とローカルDBの完全一致キャッシュ",
		"ローカルタイトル完全一致",
	} {
		d := TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: "ABC-123", Method: method}
		if !d.AutoOrganizeEligible() {
			t.Errorf("approved method %q was not eligible", method)
		}
	}

	for _, d := range []TitleResolutionDecision{
		{Status: TitleDecisionConfirmed, CatalogID: "", Method: "品番とローカルDBの完全一致"},
		{Status: TitleDecisionConfirmed, CatalogID: "ABC-123", Method: "外部検索・Jevによる補助判定"},
		{Status: TitleDecisionConfirmed, CatalogID: "ABC-123", Method: "埋込タイトル→ローカルタイトル完全一致"},
		{Status: TitleDecisionReview, CatalogID: "ABC-123", Method: "品番とローカルDBの完全一致"},
		{Status: TitleDecisionUnknown, CatalogID: "ABC-123", Method: "ローカルタイトル完全一致"},
	} {
		if d.AutoOrganizeEligible() {
			t.Errorf("unsafe decision became auto eligible: %+v", d)
		}
	}
}
