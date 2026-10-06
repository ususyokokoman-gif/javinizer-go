package scrape

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// TitleDecisionStatus は、大量処理でファイルを自動整理してよいかを表す。
// 「確定」以外は、候補があってもファイル変更を許可しない。
type TitleDecisionStatus string

// TitleDecisionPolicyVersion は、自動整理を許可する判定基準の版。
// 判定条件を緩める・強める変更をした場合は必ず上げ、旧確定結果を再評価する。
const TitleDecisionPolicyVersion = 1

const (
	TitleDecisionConfirmed TitleDecisionStatus = "confirmed"
	TitleDecisionReview    TitleDecisionStatus = "review"
	TitleDecisionUnknown   TitleDecisionStatus = "unknown"
	TitleDecisionError     TitleDecisionStatus = "error"
)

// TitleResolutionDecision は作品同定の安全判定結果。
// CatalogID は「要確認」でも候補として保持できるが、自動整理可否は Status だけで決める。
type TitleResolutionDecision struct {
	Status    TitleDecisionStatus
	CatalogID string
	Method    string
	Reason    string
}

// AutoOrganizeEligible は、ファイル名変更などの破壊的操作へ進める判定かを返す。
// Jev のスコアや候補数ではなく「確定」のみを許可する。
func (d TitleResolutionDecision) AutoOrganizeEligible() bool {
	return d.Status == TitleDecisionConfirmed && strings.TrimSpace(d.CatalogID) != ""
}

// ResolveDecision は「疑わしきは変更せず」を強制する大量処理用の判定入口。
// 決定的なローカル証拠だけを自動確定し、それ以外の成功候補は要確認に留める。
func (r *TitleCatalogResolver) ResolveDecision(ctx context.Context, input string) (TitleResolutionDecision, error) {
	if r == nil || r.scraper == nil {
		err := fmt.Errorf("title catalog resolver is not initialized")
		return TitleResolutionDecision{
			Status: TitleDecisionError,
			Method: "作品判定",
			Reason: "作品判定機能を初期化できませんでした。",
		}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	prepared := PrepareTitleResolutionInput(input)
	if prepared.Query == "" {
		return TitleResolutionDecision{
			Status: TitleDecisionUnknown,
			Method: "入力確認",
			Reason: "作品を特定できる文字情報がありません。",
		}, nil
	}

	switch prepared.Kind {
	case TitleInputCatalog:
		ids := uniqueNormalizedCatalogIDs(extractStandaloneCatalogCandidates(prepared.Query))
		if len(ids) != 1 {
			return TitleResolutionDecision{
				Status: TitleDecisionReview,
				Method: "品番判定",
				Reason: "品番らしい文字はありますが、一意に確定できません。",
			}, nil
		}
		id := ids[0]
		verified, err := r.verifyCatalogIDInLocalDump(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return TitleResolutionDecision{Status: TitleDecisionError, CatalogID: id, Method: "ローカルDB照合", Reason: "品番の実在確認中に処理が中断されました。"}, err
			}
			return TitleResolutionDecision{Status: TitleDecisionError, CatalogID: id, Method: "ローカルDB照合", Reason: "品番の実在確認でエラーが発生しました。"}, err
		}
		if verified {
			return TitleResolutionDecision{
				Status:    TitleDecisionConfirmed,
				CatalogID: id,
				Method:    "品番とローカルDBの完全一致",
				Reason:    "ファイル名の品番がローカル作品DBに実在し、同じ品番として確認できました。",
			}, nil
		}
		return TitleResolutionDecision{
			Status:    TitleDecisionReview,
			CatalogID: id,
			Method:    "品番候補",
			Reason:    "品番形式は明確ですが、ローカル作品DBで実在確認できないため自動整理しません。",
		}, nil

	case TitleInputOpaque:
		id, err := r.scraper.lookupCatalogIDByOpaqueKey(ctx, prepared.Query)
		if err == nil && strings.TrimSpace(id) != "" {
			return TitleResolutionDecision{
				Status:    TitleDecisionReview,
				CatalogID: id,
				Method:    "不明IDの外部逆引き",
				Reason:    "外部検索から有力な品番候補を得ましたが、不明IDとの対応だけでは自動整理に十分な証拠ではありません。",
			}, nil
		}
		return r.decisionFromResolutionError("", "不明IDの外部逆引き", err)
	}

	if decision, ok, err := r.exactLocalTitleDecision(ctx, prepared.Query); err != nil {
		if ctx.Err() != nil {
			return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカルタイトル照合", Reason: "ローカル作品DBの照合中に処理が中断されました。"}, err
		}
		// ローカルDBが壊れている等の実エラーは、Webで無理に埋めずエラーとして残す。
		if !errors.Is(err, models.ErrDumpMiss) && !errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
			return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカルタイトル照合", Reason: "ローカル作品DBの照合でエラーが発生しました。"}, err
		}
	} else if ok {
		return decision, nil
	}

	// ここから先は検索・Jev等による補助推定。候補が得られても自動確定には昇格させない。
	// まず「誤リネームしない」を優先し、Web自動確定の昇格条件は実データの正解集合で
	// 十分に検証できたものだけ将来追加する。
	id, err := r.Resolve(ctx, prepared.Query)
	if err == nil && strings.TrimSpace(id) != "" {
		return TitleResolutionDecision{
			Status:    TitleDecisionReview,
			CatalogID: id,
			Method:    "外部検索・Jevによる補助判定",
			Reason:    "有力な品番候補は得られましたが、決定的なローカル証拠がないため自動整理せず要確認とします。",
		}, nil
	}
	return r.decisionFromResolutionError("", "外部検索・Jevによる補助判定", err)
}

func (r *TitleCatalogResolver) verifyCatalogIDInLocalDump(ctx context.Context, id string) (bool, error) {
	dump, ok := r.titleLookup.(models.R18DevDumpLookup)
	if !ok || dump == nil {
		return false, nil
	}
	movie, err := dump.LookupMovie(ctx, id)
	if err != nil {
		if errors.Is(err, models.ErrDumpMiss) {
			return false, nil
		}
		return false, err
	}
	if movie == nil || strings.TrimSpace(movie.DVDID) == "" {
		return false, nil
	}
	return catalogComparable(movie.DVDID) == catalogComparable(id), nil
}

func (r *TitleCatalogResolver) exactLocalTitleDecision(ctx context.Context, title string) (TitleResolutionDecision, bool, error) {
	if r.titleLookup == nil {
		return TitleResolutionDecision{}, false, nil
	}
	matches, err := r.titleLookup.SearchByTitle(ctx, title, 5)
	if err != nil {
		return TitleResolutionDecision{}, false, err
	}
	if len(matches) == 0 {
		return TitleResolutionDecision{}, false, nil
	}
	top := matches[0]
	if strings.TrimSpace(top.DVDID) == "" || top.Score < 0.999999 {
		return TitleResolutionDecision{}, false, nil
	}
	if len(matches) > 1 {
		second := matches[1]
		if second.Score >= 0.999999 &&
			!strings.EqualFold(strings.TrimSpace(top.DVDID), strings.TrimSpace(second.DVDID)) {
			return TitleResolutionDecision{
				Status: TitleDecisionReview,
				Method: "ローカルタイトル照合",
				Reason: "同じタイトルに複数の品番候補が完全一致したため、自動整理しません。",
			}, true, nil
		}
	}
	return TitleResolutionDecision{
		Status:    TitleDecisionConfirmed,
		CatalogID: strings.TrimSpace(top.DVDID),
		Method:    "ローカルタイトル完全一致",
		Reason:    "正規化したタイトルがローカル作品DBの一意な作品に完全一致しました。",
	}, true, nil
}

func (r *TitleCatalogResolver) decisionFromResolutionError(candidateID, method string, err error) (TitleResolutionDecision, error) {
	if err == nil {
		return TitleResolutionDecision{
			Status: TitleDecisionUnknown,
			Method: method,
			Reason: "作品を確定できる根拠が見つかりませんでした。",
		}, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return TitleResolutionDecision{
			Status:    TitleDecisionError,
			CatalogID: strings.TrimSpace(candidateID),
			Method:    method,
			Reason:    "時間内に判定処理を完了できませんでした。",
		}, err
	}
	msg := strings.ToLower(err.Error())
	reviewPatterns := []string{
		"jev rejected catalog-id candidate",
		"ambiguous title matches multiple verified catalog ids",
		"conflicts with web evidence",
	}
	for _, pattern := range reviewPatterns {
		if strings.Contains(msg, pattern) {
			return TitleResolutionDecision{
				Status:    TitleDecisionReview,
				CatalogID: strings.TrimSpace(candidateID),
				Method:    method,
				Reason:    "有力な候補または複数候補がありますが、矛盾や不確実性が残るため自動整理しません。",
			}, nil
		}
	}
	unknownPatterns := []string{
		"no sufficiently corroborated catalog-id candidate",
		"opaque filename key produced no sufficiently corroborated catalog-id candidate",
		"no title candidates found",
		"title is empty",
		"candidate is empty",
	}
	for _, pattern := range unknownPatterns {
		if strings.Contains(msg, pattern) {
			return TitleResolutionDecision{
				Status: TitleDecisionUnknown,
				Method: method,
				Reason: "安全に確定できる作品候補が見つかりませんでした。ファイルは変更しません。",
			}, nil
		}
	}
	return TitleResolutionDecision{
		Status:    TitleDecisionError,
		CatalogID: strings.TrimSpace(candidateID),
		Method:    method,
		Reason:    "判定処理で通信または解析エラーが発生しました。ファイルは変更しません。",
	}, err
}
