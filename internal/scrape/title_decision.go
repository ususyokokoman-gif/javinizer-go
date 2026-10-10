package scrape

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/models"
)

// TitleDecisionStatus は、大量処理でファイルを自動整理してよいかを表す。
// 「確定」以外は、候補があってもファイル変更を許可しない。
type TitleDecisionStatus string

// TitleDecisionPolicyVersion は、自動整理を許可する判定基準の版。
// 判定条件を緩める・強める変更をした場合は必ず上げ、旧確定結果を再評価する。
const TitleDecisionPolicyVersion = 4

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
	if d.Status != TitleDecisionConfirmed || strings.TrimSpace(d.CatalogID) == "" {
		return false
	}
	method := strings.TrimSpace(d.Method)
	// Embedded metadata is file-specific supporting evidence. Even when it
	// resolves to a locally exact title, it does not by itself authorize a
	// destructive rename/move.
	if strings.HasPrefix(method, "埋込タイトル→") {
		return false
	}
	switch method {
	case "品番とローカルDBの完全一致",
		"品番とローカルDBの完全一致キャッシュ",
		"品番とローカルDBの完全一致（DB同一性確認済みキャッシュ）",
		"ローカルタイトル完全一致":
		return true
	default:
		return false
	}
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
		if looksGeneratedCatalogLikeKey(id) {
			return TitleResolutionDecision{
				Status: TitleDecisionUnknown,
				Method: "不明ID",
				Reason: "品番のような形ですがローカル作品DBに存在せず、生成・配信IDの特徴が強いため品番候補として扱いません。",
			}, nil
		}
		return TitleResolutionDecision{
			Status:    TitleDecisionReview,
			CatalogID: id,
			Method:    "品番候補",
			Reason:    "品番形式は明確ですが、ローカル作品DBで実在確認できないため自動整理しません。",
		}, nil

	case TitleInputOpaque:
		// Bulk precision-first policy: generated/download/content IDs are not
		// human titles and are not sent to broad web search. File-specific
		// embedded metadata is probed by the bulk caller before this point.
		return TitleResolutionDecision{
			Status: TitleDecisionUnknown,
			Method: "不明ID",
			Reason: "不明IDだけでは安全に作品を特定できないため、Webタイトル検索を行わず未特定とします。",
		}, nil
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

	// A similarity-index score of 1.0 is strong local evidence but is not
	// sufficient for confirmation unless the DB-global exact-title index above
	// proved uniqueness. If one or more exact-score candidates remain here,
	// stop locally at review instead of spending Web calls or turning a known
	// local ambiguity into a communication error.
	if decision, ok, err := r.localExactScoreReviewDecision(ctx, prepared.Query); err != nil {
		if ctx.Err() != nil {
			return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカルタイトル照合", Reason: "ローカル作品DBの候補確認中に処理が中断されました。"}, err
		}
		if !errors.Is(err, models.ErrDumpMiss) && !errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
			return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカルタイトル照合", Reason: "ローカル作品DBの候補確認でエラーが発生しました。"}, err
		}
	} else if ok {
		return decision, nil
	}

	// A catalog-looking token that is not in a safe leading position can still
	// avoid an unnecessary web lookup. Exact local DB existence makes it a
	// useful review hint, but never sufficient proof for automatic organization.
	if candidates := extractReviewOnlyCatalogCandidates(prepared.Query); len(candidates) == 1 {
		candidate := candidates[0]
		verified, verifyErr := r.verifyCatalogIDInLocalDump(ctx, candidate)
		if verifyErr != nil {
			return TitleResolutionDecision{
				Status:    TitleDecisionError,
				CatalogID: candidate,
				Method:    "品番候補＋ローカルDB存在確認",
				Reason:    "品番候補のローカル作品DB照合でエラーが発生しました。",
			}, verifyErr
		}
		if verified {
			return TitleResolutionDecision{
				Status:    TitleDecisionReview,
				CatalogID: candidate,
				Method:    "品番候補＋ローカルDB存在確認",
				Reason:    "品番候補はローカル作品DBに実在しますが、ファイル名上の位置または境界が自動確定条件を満たさないため要確認とします。",
			}, nil
		}
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

func (r *TitleCatalogResolver) ResolveLocalOnlyDecision(ctx context.Context, input string) (TitleResolutionDecision, error) {
	if r == nil || r.scraper == nil {
		err := fmt.Errorf("title catalog resolver is not initialized")
		return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカル作品判定", Reason: "作品判定機能を初期化できませんでした。"}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	prepared := PrepareTitleResolutionInput(input)
	if prepared.Query == "" {
		return TitleResolutionDecision{Status: TitleDecisionUnknown, Method: "ローカル作品判定", Reason: "作品を特定できる文字情報がありません。"}, nil
	}

	if prepared.Kind == TitleInputCatalog {
		ids := uniqueNormalizedCatalogIDs(extractStandaloneCatalogCandidates(prepared.Query))
		if len(ids) != 1 {
			return TitleResolutionDecision{Status: TitleDecisionReview, Method: "品番判定", Reason: "品番らしい文字はありますが、一意に確定できません。"}, nil
		}
		id := ids[0]
		verified, err := r.verifyCatalogIDInLocalDump(ctx, id)
		if err != nil {
			return TitleResolutionDecision{Status: TitleDecisionError, CatalogID: id, Method: "ローカルDB照合", Reason: "品番の実在確認でエラーが発生しました。"}, err
		}
		if verified {
			return TitleResolutionDecision{Status: TitleDecisionConfirmed, CatalogID: id, Method: "品番とローカルDBの完全一致", Reason: "品番がローカル作品DBに実在し、同じ品番として確認できました。"}, nil
		}
		if looksGeneratedCatalogLikeKey(id) {
			return TitleResolutionDecision{Status: TitleDecisionUnknown, Method: "不明ID", Reason: "品番のような形ですがローカル作品DBに存在せず、生成・配信IDの特徴が強いため品番候補として扱いません。"}, nil
		}
		return TitleResolutionDecision{Status: TitleDecisionReview, CatalogID: id, Method: "品番候補", Reason: "品番形式は明確ですが、ローカル作品DBで実在確認できません。"}, nil
	}
	if prepared.Kind == TitleInputOpaque {
		return TitleResolutionDecision{Status: TitleDecisionUnknown, Method: "不明ID", Reason: "不明IDだけではローカル証拠で作品を特定できません。"}, nil
	}

	if decision, ok, err := r.exactLocalTitleDecision(ctx, prepared.Query); err != nil {
		if !errors.Is(err, models.ErrDumpMiss) && !errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
			return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカルタイトル照合", Reason: "ローカル作品DBの照合でエラーが発生しました。"}, err
		}
	} else if ok {
		return decision, nil
	}
	if decision, ok, err := r.localExactScoreReviewDecision(ctx, prepared.Query); err != nil {
		if !errors.Is(err, models.ErrDumpMiss) && !errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
			return TitleResolutionDecision{Status: TitleDecisionError, Method: "ローカルタイトル照合", Reason: "ローカル作品DBの候補確認でエラーが発生しました。"}, err
		}
	} else if ok {
		return decision, nil
	}
	if candidates := extractReviewOnlyCatalogCandidates(prepared.Query); len(candidates) == 1 {
		candidate := candidates[0]
		verified, err := r.verifyCatalogIDInLocalDump(ctx, candidate)
		if err != nil {
			return TitleResolutionDecision{Status: TitleDecisionError, CatalogID: candidate, Method: "品番候補＋ローカルDB存在確認", Reason: "品番候補のローカル作品DB照合でエラーが発生しました。"}, err
		}
		if verified {
			return TitleResolutionDecision{Status: TitleDecisionReview, CatalogID: candidate, Method: "品番候補＋ローカルDB存在確認", Reason: "品番候補はローカル作品DBに実在しますが、自動確定条件を満たさないため要確認とします。"}, nil
		}
	}
	return TitleResolutionDecision{
		Status: TitleDecisionUnknown,
		Method: "ローカル証拠のみ",
		Reason: "ローカルDBと埋込メタデータだけでは安全に作品を特定できませんでした。Web検索は行いません。",
	}, nil
}

func looksGeneratedCatalogLikeKey(id string) bool {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" || strings.HasPrefix(id, "FC2-PPV-") {
		return false
	}
	prefix := id
	if i := strings.IndexRune(id, '-'); i >= 0 {
		prefix = id[:i]
	}
	runes := []rune(prefix)
	if len(runes) > 8 {
		return true
	}
	if len(runes) < 7 {
		return false
	}
	seenLetter := false
	for _, r := range runes {
		switch {
		case unicode.IsLetter(r):
			seenLetter = true
		case unicode.IsDigit(r):
			if seenLetter {
				return true
			}
		}
	}
	return false
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
	exactLookup, ok := r.titleLookup.(models.R18DevExactTitleLookup)
	if !ok {
		// Without a DB-global exact-match capability we cannot prove
		// uniqueness, so similarity search must never auto-confirm.
		return TitleResolutionDecision{}, false, nil
	}
	matches, err := exactLookup.ExactTitleMatches(ctx, title)
	if err != nil {
		return TitleResolutionDecision{}, false, err
	}
	if len(matches) == 0 {
		return TitleResolutionDecision{}, false, nil
	}
	byID := make(map[string]models.DumpTitleMatch, len(matches))
	for _, match := range matches {
		id := strings.TrimSpace(match.DVDID)
		if id == "" {
			continue
		}
		byID[strings.ToUpper(id)] = match
	}
	if len(byID) == 0 {
		return TitleResolutionDecision{}, false, nil
	}
	if len(byID) != 1 {
		return TitleResolutionDecision{
			Status: TitleDecisionReview,
			Method: "ローカルタイトル照合",
			Reason: "同じタイトルに複数の品番候補がDB全体で完全一致したため、自動整理しません。",
		}, true, nil
	}
	var only models.DumpTitleMatch
	for _, match := range byID {
		only = match
	}
	// A DB-wide exact title is decisive only when the input does not contain
	// conflicting catalog-like evidence. Mid-filename catalog tokens are never
	// allowed to silently lose to a title match.
	for _, candidate := range uniqueNormalizedCatalogIDs(extractStandaloneCatalogCandidates(title)) {
		if catalogComparable(candidate) != catalogComparable(only.DVDID) {
			return TitleResolutionDecision{
				Status: TitleDecisionReview,
				Method: "ローカルタイトル照合",
				Reason: "タイトル完全一致候補と入力中の品番候補が一致しないため、自動整理せず要確認とします。",
			}, true, nil
		}
	}
	// Very short exact titles are too collision-prone to authorize automatic
	// organization even when the current DB contains only one exact match.
	// Keep the candidate for review rather than turning a weak token such as
	// "me" into a destructive decision.
	const minExactTitleEvidenceRunes = 12
	meaningfulTitle := compactComparable(normalizeTitleForWebSearch(title))
	if len([]rune(meaningfulTitle)) < minExactTitleEvidenceRunes {
		return TitleResolutionDecision{
			Status:    TitleDecisionReview,
			CatalogID: strings.TrimSpace(only.DVDID),
			Method:    "ローカルタイトル照合",
			Reason:    "タイトルはローカル作品DB全体で一意に完全一致しますが、文字列が短く誤一致リスクが高いため自動整理せず要確認とします。",
		}, true, nil
	}
	return TitleResolutionDecision{
		Status:    TitleDecisionConfirmed,
		CatalogID: strings.TrimSpace(only.DVDID),
		Method:    "ローカルタイトル完全一致",
		Reason:    "タイトル文字列がローカル作品DB全体で一意な作品に完全一致し、矛盾する品番候補もありません。",
	}, true, nil
}

func (r *TitleCatalogResolver) localExactScoreReviewDecision(ctx context.Context, title string) (TitleResolutionDecision, bool, error) {
	if r == nil || r.titleLookup == nil {
		return TitleResolutionDecision{}, false, nil
	}
	matches, err := r.titleLookup.SearchByTitle(ctx, title, 5)
	if err != nil {
		return TitleResolutionDecision{}, false, err
	}
	byID := make(map[string]models.DumpTitleMatch)
	for _, match := range matches {
		id := strings.TrimSpace(match.DVDID)
		if id == "" || match.Score < 0.999999 {
			continue
		}
		byID[strings.ToUpper(id)] = match
	}
	if len(byID) == 0 {
		return TitleResolutionDecision{}, false, nil
	}
	if len(byID) > 1 {
		return TitleResolutionDecision{
			Status: TitleDecisionReview,
			Method: "ローカルタイトル照合",
			Reason: "ローカル作品DBで完全一致相当の品番候補が複数あるため、Web検索で多数決せず要確認とします。",
		}, true, nil
	}
	var only models.DumpTitleMatch
	for _, match := range byID {
		only = match
	}
	return TitleResolutionDecision{
		Status:    TitleDecisionReview,
		CatalogID: strings.TrimSpace(only.DVDID),
		Method:    "ローカルタイトル照合",
		Reason:    "ローカル作品DBに完全一致相当の有力候補がありますが、DB全体での一意な完全一致を証明できないため要確認とします。",
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
