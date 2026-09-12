package javdb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"golang.org/x/text/unicode/norm"
)

type titleCandidate struct {
	title string
	uid   string
	url   string
	score float64
}

// SearchTitleCandidates searches JavDB directly by title and returns verified
// detail-page results for the strongest title matches. It deliberately returns
// multiple candidates so the caller can combine JavDB evidence with independent
// sources instead of treating the first search hit as authoritative.
//
// Title lookup is an identification aid, not a final metadata scrape. It is
// therefore intentionally available even when JavDB is disabled as a metadata
// scraper. Search and ScrapeURL retain their enabled checks.
func (s *scraper) SearchTitleCandidates(ctx context.Context, title string, limit int) ([]*models.ScraperResult, error) {
	if s == nil {
		return nil, fmt.Errorf("JavDB scraper is unavailable")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("JavDB title cannot be empty")
	}
	if limit <= 0 || limit > 5 {
		limit = 3
	}

	searchURL, err := s.GetURL(ctx, title)
	if err != nil {
		return nil, err
	}
	html, err := s.fetchPageCtx(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("JavDB title search failed: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse JavDB title search: %w", err)
	}

	candidates := make([]titleCandidate, 0, 8)
	doc.Find(".movie-list .item").Each(func(_ int, item *goquery.Selection) {
		link := item.Find("a.box[href], a[href]").First()
		href, ok := link.Attr("href")
		if !ok || !strings.Contains(href, "/v/") {
			return
		}
		candidateTitle := strings.TrimSpace(item.Find(".video-title").First().Text())
		uid := strings.TrimSpace(item.Find(".uid").First().Text())
		if candidateTitle == "" {
			candidateTitle = strings.TrimSpace(link.Text())
		}
		score := javDBTitleSimilarity(title, candidateTitle)
		if score < 0.20 {
			return
		}
		candidates = append(candidates, titleCandidate{
			title: candidateTitle,
			uid:   uid,
			url:   scraperutil.ResolveURL(s.baseURL, href),
			score: score,
		})
	})

	if len(candidates) == 0 {
		return nil, models.NewScraperNotFoundError("JavDB", "no title candidates found")
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	results := make([]*models.ScraperResult, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		detailHTML, fetchErr := s.fetchPageCtx(ctx, candidate.url)
		if fetchErr != nil {
			continue
		}
		detailDoc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(detailHTML))
		if parseErr != nil {
			continue
		}
		result, parseErr := s.parseDetailPage(detailDoc, candidate.url, candidate.uid)
		if parseErr != nil || result == nil || strings.TrimSpace(result.ID) == "" {
			continue
		}
		// The detail page is the authoritative JavDB evidence. Keep a candidate
		// only when its detail title still resembles the requested title.
		detailTitle := strings.TrimSpace(result.Title)
		if detailTitle == "" {
			detailTitle = strings.TrimSpace(result.OriginalTitle)
		}
		if javDBTitleSimilarity(title, detailTitle) < 0.20 {
			continue
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, models.NewScraperNotFoundError("JavDB", "title candidates did not yield verified detail pages")
	}
	return results, nil
}

func javDBTitleSimilarity(a, b string) float64 {
	a = compactTitle(a)
	b = compactTitle(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		short, long := len([]rune(a)), len([]rune(b))
		if short > long {
			short, long = long, short
		}
		return 0.75 + 0.25*float64(short)/float64(long)
	}
	ar := []rune(a)
	br := []rune(b)
	gramsA := titleBigrams(ar)
	gramsB := titleBigrams(br)
	if len(gramsA) == 0 || len(gramsB) == 0 {
		return 0
	}
	hits := 0
	for gram := range gramsA {
		if _, ok := gramsB[gram]; ok {
			hits++
		}
	}
	return float64(2*hits) / float64(len(gramsA)+len(gramsB))
}

func compactTitle(v string) string {
	v = strings.ToLower(norm.NFKC.String(v))
	var b strings.Builder
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func titleBigrams(v []rune) map[string]struct{} {
	out := make(map[string]struct{})
	if len(v) == 1 {
		out[string(v)] = struct{}{}
		return out
	}
	for i := 0; i+1 < len(v); i++ {
		out[string(v[i:i+2])] = struct{}{}
	}
	return out
}
