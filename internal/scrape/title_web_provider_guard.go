package scrape

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// titleSearchProviderGuard centralizes rate pacing and temporary provider
// cooldowns for the high-volume title resolver. CPU/file workers may run in
// parallel, but public-search traffic must not scale linearly with worker count.
type titleSearchProviderGuard struct {
	mu           sync.Mutex
	nextAllowed  map[string]time.Time
	blockedUntil map[string]time.Time
}

func newTitleSearchProviderGuard() *titleSearchProviderGuard {
	return &titleSearchProviderGuard{
		nextAllowed:  make(map[string]time.Time),
		blockedUntil: make(map[string]time.Time),
	}
}

func (g *titleSearchProviderGuard) wait(ctx context.Context, provider string, minInterval time.Duration) error {
	if g == nil {
		return nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	for {
		now := time.Now()
		g.mu.Lock()
		if until := g.blockedUntil[provider]; until.After(now) {
			remaining := time.Until(until)
			g.mu.Unlock()
			return fmt.Errorf("%s search temporarily disabled after rate limit; retry in %s", provider, remaining.Round(time.Second))
		}
		wait := time.Until(g.nextAllowed[provider])
		if wait <= 0 {
			g.nextAllowed[provider] = now.Add(minInterval)
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *titleSearchProviderGuard) rateLimited(provider, retryAfter string, fallback time.Duration) {
	if g == nil {
		return
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return
	}
	if fallback <= 0 {
		fallback = 2 * time.Minute
	}
	d := parseRetryAfter(retryAfter)
	if d <= 0 {
		d = fallback
	}
	g.mu.Lock()
	g.blockedUntil[provider] = time.Now().Add(d)
	g.mu.Unlock()
}

func (g *titleSearchProviderGuard) success(provider string) {
	if g == nil {
		return
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	g.mu.Lock()
	delete(g.blockedUntil, provider)
	g.mu.Unlock()
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := time.Parse(time.RFC1123, raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

func (s *Scraper) waitTitleSearchProvider(ctx context.Context, provider string) error {
	if s == nil || s.titleSearchGuard == nil {
		return nil
	}
	interval := 500 * time.Millisecond
	switch strings.ToLower(provider) {
	case "google":
		interval = 1500 * time.Millisecond
	case "duckduckgo":
		interval = 500 * time.Millisecond
	case "yahoojp":
		interval = 500 * time.Millisecond
	case "bing":
		interval = 750 * time.Millisecond
	}
	return s.titleSearchGuard.wait(ctx, provider, interval)
}

func (s *Scraper) markTitleSearchRateLimited(provider, retryAfter string) {
	if s == nil || s.titleSearchGuard == nil {
		return
	}
	fallback := 2 * time.Minute
	if strings.EqualFold(provider, "google") {
		fallback = 5 * time.Minute
	}
	s.titleSearchGuard.rateLimited(provider, retryAfter, fallback)
}

func (s *Scraper) markTitleSearchSuccess(provider string) {
	if s == nil || s.titleSearchGuard == nil {
		return
	}
	s.titleSearchGuard.success(provider)
}
