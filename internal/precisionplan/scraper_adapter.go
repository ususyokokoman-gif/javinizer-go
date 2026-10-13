package precisionplan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ScraperLookup is the minimal native Javinizer scraper surface needed for a
// read-only supplemental title lookup.
type ScraperLookup interface {
	Name() string
	IsEnabled() bool
	Search(context.Context, string) (*models.ScraperResult, error)
}

// ScraperAdapter connects a configured native Javinizer scraper to the
// precision-first query planner. It deliberately does NOT write anything to
// the movie cache, batch-job results, filesystem, or confirmed cache.
type ScraperAdapter struct {
	Scraper ScraperLookup
}

func (a ScraperAdapter) Name() string {
	if a.Scraper == nil {
		return "unavailable"
	}
	return a.Scraper.Name()
}

func (a ScraperAdapter) Search(ctx context.Context, sku string) (*SearchHit, error) {
	if a.Scraper == nil {
		return nil, errors.New("missing scraper")
	}
	if !a.Scraper.IsEnabled() {
		return nil, fmt.Errorf("scraper %s not enabled", a.Scraper.Name())
	}
	result, err := a.Scraper.Search(ctx, sku)
	if err != nil {
		if typed, ok := models.AsScraperError(err); ok {
			switch {
			case typed.Kind == models.ScraperErrorKindRateLimited || typed.StatusCode == 429:
				return nil, fmt.Errorf("%w: %v", ErrRateLimited, err)
			case typed.Kind == models.ScraperErrorKindBlocked || typed.StatusCode == 451:
				return nil, fmt.Errorf("%w: %v", ErrRegionBlocked, err)
			}
		}
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if !strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(a.Scraper.Name())) {
		return nil, fmt.Errorf("scraper source mismatch: expected %s", a.Scraper.Name())
	}
	// A title written using Han characters is not proof of Japanese language.
	// Reject non-Japanese-labeled source results before considering their text.
	if strings.TrimSpace(strings.ToLower(result.Language)) != "ja" {
		return nil, fmt.Errorf("scraper original-title language not verified as Japanese")
	}
	// OriginalTitle is the only permitted field. Title can be an English
	// translation, a placeholder, or a guessed identifier.
	return &SearchHit{
		ExactSKU:         result.ID,
		WorkID:           result.ContentID,
		JapaneseOriginal: result.OriginalTitle,
		SourceURL:        result.SourceURL,
	}, nil
}
