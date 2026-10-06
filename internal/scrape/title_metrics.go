package scrape

// TitleResolutionMetrics are operational counters for the public-search
// portion of title resolution. WebSearches counts actual HTTP search-engine
// requests (DuckDuckGo, Yahoo Japan, Bing, Google). HTTP429 counts responses
// whose status is exactly 429. Direct metadata-source requests are not included.
type TitleResolutionMetrics struct {
	WebSearches int64
	HTTP429     int64
}

func (s *Scraper) recordTitleWebSearchRequest() {
	if s != nil {
		s.titleWebSearchRequests.Add(1)
	}
}

func (s *Scraper) recordTitleWebHTTP429() {
	if s != nil {
		s.titleWebHTTP429Responses.Add(1)
	}
}

func (s *Scraper) titleResolutionMetrics() TitleResolutionMetrics {
	if s == nil {
		return TitleResolutionMetrics{}
	}
	return TitleResolutionMetrics{
		WebSearches: s.titleWebSearchRequests.Load(),
		HTTP429:     s.titleWebHTTP429Responses.Load(),
	}
}

// Metrics returns a snapshot of operational title-resolution counters.
func (r *TitleCatalogResolver) Metrics() TitleResolutionMetrics {
	if r == nil {
		return TitleResolutionMetrics{}
	}
	return r.scraper.titleResolutionMetrics()
}
