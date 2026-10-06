package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
)

func decisionStatusJapanese(status string) string {
	switch status {
	case "confirmed":
		return "確定"
	case "review":
		return "要確認"
	case "unknown":
		return "未特定"
	case "error":
		return "エラー"
	default:
		return "不明"
	}
}

func writeJapaneseDecisionReports(outDir string, rows []resultRecord) error {
	if err := writeJapaneseDecisionCSV(filepath.Join(outDir, "判定結果.csv"), rows, func(resultRecord) bool { return true }); err != nil {
		return err
	}
	if err := writeJapaneseDecisionCSV(filepath.Join(outDir, "自動整理対象.csv"), rows, func(r resultRecord) bool {
		return r.Status == "confirmed" && r.AutoOrganizeEligible
	}); err != nil {
		return err
	}
	if err := writeJapaneseDecisionCSV(filepath.Join(outDir, "要確認一覧.csv"), rows, func(r resultRecord) bool {
		return r.Status != "confirmed"
	}); err != nil {
		return err
	}
	return nil
}

func writeJapaneseDecisionCSV(path string, rows []resultRecord, include func(resultRecord) bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Excel で文字化けしにくい UTF-8 BOM を付ける。
	if _, err := f.WriteString("\ufeff"); err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"パス", "ファイル名", "検索文字列", "品番候補", "判定",
		"判定方法", "判定理由", "判定基準版", "自動整理可", "エラー",
		"処理時間（ミリ秒）", "試行回数", "再利用",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if include != nil && !include(r) {
			continue
		}
		auto := "いいえ"
		if r.AutoOrganizeEligible {
			auto = "はい"
		}
		reused := "いいえ"
		if r.Cached {
			reused = "はい"
		}
		if err := w.Write([]string{
			r.Path,
			r.Filename,
			r.Title,
			r.CatalogID,
			decisionStatusJapanese(r.Status),
			r.Method,
			r.Reason,
			fmt.Sprintf("%d", r.DecisionPolicyVersion),
			auto,
			r.Error,
			fmt.Sprintf("%d", r.ElapsedMS),
			fmt.Sprintf("%d", r.Attempts),
			reused,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Sync()
}
