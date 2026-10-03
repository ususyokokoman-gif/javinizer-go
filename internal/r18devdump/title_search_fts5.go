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
)

func buildTitleSearchIndex(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE VIRTUAL TABLE video_titles_fts USING fts5(
			content_id UNINDEXED,
			dvd_id UNINDEXED,
			title_ja,
			title_en,
			tokenize='trigram'
		);
		INSERT INTO video_titles_fts(content_id, dvd_id, title_ja, title_en)
		SELECT content_id, COALESCE(dvd_id,''), COALESCE(title_ja,''), COALESCE(title_en,'')
		FROM videos
		WHERE dvd_id IS NOT NULL
		  AND dvd_id <> ''
		  AND (COALESCE(title_ja,'') <> '' OR COALESCE(title_en,'') <> '');
	`); err != nil {
		return fmt.Errorf("build title FTS index: %w", err)
	}
	return nil
}

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
			if strings.Contains(strings.ToLower(err.Error()), "no such table") {
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
			m.DVDID = dvd.String
			m.TitleJa = ja.String
			m.TitleEn = en.String
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

		// A longer probe is much more selective. Once it yields a useful pool,
		// ranking in Go is cheaper than issuing progressively broader queries.
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

func titleSearchProbes(query string) []string {
	r := []rune(strings.TrimSpace(query))
	if len(r) < 3 {
		return nil
	}
	widths := []int{40, 32, 24, 18, 12, 8, 5}
	out := make([]string, 0, len(widths))
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
	if len(out) == 0 {
		out = append(out, string(r))
	}
	return out
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
	q := normalizeTitleSearch(query)
	c := normalizeTitleSearch(candidate)
	if q == "" || c == "" {
		return 0
	}
	if q == c {
		return 1
	}
	if strings.Contains(q, c) {
		coverage := float64(utf8.RuneCountInString(c)) / float64(utf8.RuneCountInString(q))
		return math.Min(0.99, 0.90+0.09*coverage)
	}
	if strings.Contains(c, q) {
		coverage := float64(utf8.RuneCountInString(q)) / float64(utf8.RuneCountInString(c))
		return math.Min(0.94, 0.82+0.12*coverage)
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

var _ = errors.Is
