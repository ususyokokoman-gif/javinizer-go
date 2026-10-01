package scrape

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraper/javdb"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// TitleCatalogResolver is a lightweight title -> catalog ID resolver.
// It intentionally skips metadata scraping and persistence. The resolver uses
// public web evidence and, when configured, the Jev catalog gate.
type TitleCatalogResolver struct {
	scraper *Scraper
}

// NewTitleCatalogResolver constructs a resolver suitable for high-volume title
// identification. The supplied Config is copied so callers may safely reuse or
// mutate their original config while this resolver is running.
func NewTitleCatalogResolver(cfg *Config) *TitleCatalogResolver {
	resolved := Config{}
	if cfg != nil {
		resolved = *cfg
	}
	applyJevCatalogGateEnv(&resolved)
	if resolved.JevCatalogThreshold <= 0 {
		resolved.JevCatalogThreshold = 0.80
	}
	if strings.TrimSpace(resolved.JevCatalogModel) == "" {
		resolved.JevCatalogModel = "jev-latest"
	}
	if strings.TrimSpace(resolved.JevCatalogEndpoint) == "" {
		resolved.JevCatalogEndpoint = "https://api.typesafe.ai/v1/systemone"
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// The bulk title resolver used to leave registry nil, which silently
	// disabled collectDirectTitleEvidence and forced every title through public
	// search engines. Register only JavDB here as the verified direct-title
	// source; final metadata scraping remains outside this lightweight resolver.
	registry := scraperutil.NewScraperRegistry()
	javdb.Register(registry)
	defaults := registry.GetAllDefaults()
	if settings, ok := defaults["javdb"]; ok {
		if settings.Timeout <= 0 {
			settings.Timeout = 20
		}
		if settings.RateLimit <= 0 {
			settings.RateLimit = 1000
		}
		if settings.RetryCount <= 0 {
			settings.RetryCount = 1
		}
		if err := registry.InitInstances(map[string]scraperutil.ScraperDeps{
			"javdb": {Settings: settings},
		}); err != nil {
			logging.Warnf("[scrape] initialize JavDB title resolver: %v", err)
		}
	}

	return &TitleCatalogResolver{
		scraper: &Scraper{
			registry:   registry,
			httpClient: client,
			cfg:        &resolved,
		},
	}
}

// Resolve returns the validated catalog ID for a free-form title.
// No metadata lookup is performed after the catalog ID is identified.
func (r *TitleCatalogResolver) Resolve(ctx context.Context, title string) (string, error) {
	if r == nil || r.scraper == nil {
		return "", fmt.Errorf("title catalog resolver is not initialized")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("title is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return r.scraper.lookupCatalogIDOnWeb(ctx, title)
}
