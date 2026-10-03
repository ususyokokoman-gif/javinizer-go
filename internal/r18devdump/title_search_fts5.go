//go:build sqlite_fts5

package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

const titleSearchIndexVersion = "1"

// EnsureTitleSearchIndex upgrades an existing r18.dev dump in place with the
// FTS5 trigram title index. This is intentionally separate from Open because
// Store is read-only at runtime.
func EnsureTitleSearchIndex(ctx context.Context, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("title index path is empty")
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open dump for title index: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping dump for title index: %w", err)
	}
	return buildTitleSearchIndex(ctx, db)
}

// buildTitleSearchIndex is called both after a fresh dump import and when an
// older dump is upgraded in place. The FTS table stores the original titles so
// a prefix/sub-string from the filename can be matched locally with zero HTTP.
func buildTitleSearchIndex(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("title index database is nil")
	}

	var version string
	err := db.QueryRowContext(ctx,
		"SELECT value FROM dump_meta WHERE key = 'title_search_index_version'",
	).Scan(&version)
	switch {
	case err == nil && version == titleSearchIndexVersion:
		var count int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='video_titles_fts'",
		).Scan(&count); err == nil && count == 1 {
			return nil
		}
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		// Older sidecars can exist without dump_meta only if they are corrupt;
		// surface that instead of silently falling back to slow HTTP forever.
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return fmt.Errorf("read title index version: %w", err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin title index build: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS video_titles_fts"); err != nil {
		return fmt.Errorf("drop old title index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE VIRTUAL TABLE video_titles_fts USING fts5(
			content_id UNINDEXED,
			dvd_id UNINDEXED,
			title_ja,
			title_en,
			tokenize='trigram'
		)
	`); err != nil {
		return fmt.Errorf("create title FTS index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO video_titles_fts(content_id, dvd_id, title_ja, title_en)
		SELECT content_id, COALESCE(dvd_id,''), COALESCE(title_ja,''), COALESCE(title_en,'')
		FROM videos
		WHERE dvd_id IS NOT NULL
		  AND dvd_id <> ''
		  AND (COALESCE(title_ja,'') <> '' OR COALESCE(title_en,'') <> '')
	`); err != nil {
		return fmt.Errorf("populate title FTS index: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO dump_meta(key,value) VALUES('title_search_index_version', ?)",
		titleSearchIndexVersion,
	); err != nil {
		return fmt.Errorf("write title index version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit title index build: %w", err)
	}
	rollback = false
	return nil
}

// SearchByTitle returns ranked local title candidates. It intentionally returns
// only a small candidate set; final automatic adoption is handled by Jev.
func (s *Store) SearchByTitle(ctx context.Context, query string, limit int) ([]models.DumpTitleMatch, error) {
	if s == nil || s.db == nil {
		return nil, models.ErrDumpMiss
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < 3 {
		return nil, models.ErrDumpMiss
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	probes := titleSearchProbes(query)
	seen := make(map[string]models.DumpTitleMatch)
	for _, probe := range probes {
		rows, err := s.db.QueryContext(ctx, `
			SELECT content_id, dvd_id, title_ja, title_en
			FROM video_titles_fts
			WHERE video_titles_fts MATCH ?
			LIMIT 100
		`, quoteFTS5Phrase(probe))
		if err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "no such table") ||
				strings.Contains(lower, "no such module") {
				return nil, models.ErrDumpTitleSearchUnavailable
			}
			return nil, fmt.Errorf("dump title search %q: %w", probe, err)
		}
		for rows.Next() {
			var m models.DumpTitleMatch
			var dvd, ja, en sql.NullString
			if err := rows.Scan(&m.ContentID, &dvd, &ja, &en); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan dump title search: %w", err)
			}
			m.DVDID = strings.TrimSpace(dvd.String)
			m.TitleJa = strings.TrimSpace(ja.String)
			m.TitleEn = strings.TrimSpace(en.String)
			if m.DVDID == "" {
				continue
			}
			m.Score = bestTitleSimilarity(query, m.TitleJa, m.TitleEn)
			if old, ok := seen[m.ContentID]; !ok || m.Score > old.Score {
				seen[m.ContentID] = m
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate dump title search: %w", err)
		}
		_ = rows.Close()

		// The longest successful phrase is the most selective. Stop as soon as
		// enough candidates exist and rank them in Go.
		if len(seen) >= limit {
			break
		}
	}

	if len(seen) == 0 {
		return nil, models.ErrDumpMiss
	}
	out := make([]models.DumpTitleMatch, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].DVDID < out[j].DVDID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// titleSearchProbes starts with long filename prefixes and progressively
// broadens. Typical files use "official title + actress", so the official title
// remains at the beginning and is found without scanning the whole web.
func titleSearchProbes(query string) []string {
	query = stripCommonFilenamePrefix(strings.TrimSpace(query))
	r := []rune(query)
	if len(r) < 3 {
		return nil
	}
	widths := []int{64, 48, 40, 32, 24, 18, 12, 8, 5}
	out := make([]string, 0, len(widths)+1)
	seen := make(map[string]bool)
	for _, w := range widths {
		if w > len(r) {
			continue
		}
		p := strings.TrimSpace(string(r[:w]))
		if utf8.RuneCountInString(p) < 3 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 || !seen[query] {
		out = append([]string{query}, out...)
	}
	return out
}

func stripCommonFilenamePrefix(s string) string {
	for {
		s = strings.TrimSpace(s)
		if len(s) < 2 {
			return s
		}
		pairs := [][2]string{{"[", "]"}, {"【", "】"}, {"(", ")"}, {"（", "）"}}
		changed := false
		for _, p := range pairs {
			if strings.HasPrefix(s, p[0]) {
				if end := strings.Index(s, p[1]); end >= 0 && end < 40 {
					s = strings.TrimSpace(s[end+len(p[1]):])
					changed = true
					break
				}
			}
		}
		if !changed {
			return s
		}
	}
}

func quoteFTS5Phrase(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func bestTitleSimilarity(query, ja, en string) float64 {
	best := 0.0
	for _, candidate := range []string{ja, en} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if score := titleSimilarity(query, candidate); score > best {
			best = score
		}
	}
	return best
}

func titleSimilarity(query, candidate string) float64 {
	q := normalizeTitleSearch(stripCommonFilenamePrefix(query))
	c := normalizeTitleSearch(candidate)
	if q == "" || c == "" {
		return 0
	}
	if q == c {
		return 1
	}
	if strings.Contains(q, c) {
		coverage := float64(utf8.RuneCountInString(c)) / float64(utf8.RuneCountInString(q))
		return math.Min(0.995, 0.90+0.095*coverage)
	}
	if strings.Contains(c, q) {
		coverage := float64(utf8.RuneCountInString(q)) / float64(utf8.RuneCountInString(c))
		return math.Min(0.95, 0.82+0.13*coverage)
	}

	qgrams := runeTrigrams(q)
	cgrams := runeTrigrams(c)
	if len(qgrams) == 0 || len(cgrams) == 0 {
		return 0
	}
	inter := 0
	for g := range qgrams {
		if _, ok := cgrams[g]; ok {
			inter++
		}
	}
	union := len(qgrams) + len(cgrams) - inter
	if union == 0 {
		return 0
	}
	jaccard := float64(inter) / float64(union)
	prefix := commonPrefixRatio(q, c)
	return 0.75*jaccard + 0.25*prefix
}

func normalizeTitleSearch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func runeTrigrams(s string) map[string]struct{} {
	r := []rune(s)
	out := make(map[string]struct{})
	if len(r) < 3 {
		return out
	}
	for i := 0; i+3 <= len(r); i++ {
		out[string(r[i:i+3])] = struct{}{}
	}
	return out
}

func commonPrefixRatio(a, b string) float64 {
	ar := []rune(a)
	br := []rune(b)
	n := len(ar)
	if len(br) < n {
		n = len(br)
	}
	i := 0
	for i < n && ar[i] == br[i] {
		i++
	}
	den := len(ar)
	if len(br) > den {
		den = len(br)
	}
	if den == 0 {
		return 0
	}
	return float64(i) / float64(den)
}
