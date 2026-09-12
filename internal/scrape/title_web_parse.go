package scrape

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func parseTitleWebResults(provider string, doc *goquery.Document) []titleWebSearchResult {
	if provider != "google" || doc == nil { return nil }
	results := make([]titleWebSearchResult, 0, 10)
	seen := make(map[string]struct{})
	appendResult := func(title, snippet, href string) {
		title = strings.TrimSpace(spaceRE.ReplaceAllString(title, " "))
		snippet = strings.TrimSpace(spaceRE.ReplaceAllString(snippet, " "))
		href = normalizeGoogleResultURL(strings.TrimSpace(href))
		if title == "" && snippet == "" { return }
		key := title + "\x00" + href
		if _, ok := seen[key]; ok { return }
		seen[key] = struct{}{}
		results = append(results, titleWebSearchResult{Title:title, Snippet:snippet, URL:href})
	}
	doc.Find("div.MjjYud, div.tF2Cxc, div.Gx5Zad").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		var resultLink *goquery.Selection
		sel.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool { if a.Find("h3").Length() > 0 { resultLink = a; return false }; return true })
		if resultLink != nil {
			href, _ := resultLink.Attr("href")
			title := resultLink.Find("h3").First().Text()
			snippet := sel.Find("div.VwiC3b, div.yXK7lf, span.aCOpRe").First().Text()
			if strings.TrimSpace(snippet) == "" { snippet = sel.Text() }
			appendResult(title, snippet, href)
		}
		return len(results) < 10
	})
	if len(results) < 10 {
		doc.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
			h3 := a.Find("h3").First(); if h3.Length() == 0 { return true }
			href, _ := a.Attr("href"); appendResult(h3.Text(), a.Parent().Parent().Text(), href)
			return len(results) < 10
		})
	}
	if len(results) == 0 {
		doc.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href"); text := strings.TrimSpace(a.Text())
			if text != "" && href != "" { appendResult(text, "", href) }
			return len(results) < 20
		})
	}
	return results
}

func normalizeGoogleResultURL(raw string) string {
	if raw == "" { return raw }
	candidate := raw
	if strings.HasPrefix(candidate, "/url?") { candidate = "https://www.google.com" + candidate }
	parsed, err := url.Parse(candidate); if err != nil { return raw }
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if (host == "google.com" || strings.HasSuffix(host, ".google.com")) && parsed.Path == "/url" {
		for _, key := range []string{"q", "url"} { if target := strings.TrimSpace(parsed.Query().Get(key)); target != "" { return target } }
	}
	return raw
}
