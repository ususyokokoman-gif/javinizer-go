package scrape

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Google sometimes serves a lightweight redirect/interstitial to plain HTTP
// clients even though a real browser receives the organic result page. The
// desktop application already runs on machines with a modern browser, so use a
// headless browser only as a fallback. Serializing the fallback avoids launching
// many browser processes at once when a batch contains several title-only files.
var (
	googleBrowserSearchMu      sync.Mutex
	googleBrowserBlockedUntil time.Time
)

const googleBrowserBlockCooldown = 10 * time.Minute

func isGoogleSearchInterstitial(doc *goquery.Document, results []titleWebSearchResult) bool {
	if doc == nil {
		return false
	}

	interstitial := false
	doc.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		lower := strings.ToLower(href)
		if strings.Contains(lower, "emsg=sg_rel") || strings.Contains(lower, "emsg=sg_srch") {
			interstitial = true
			return false
		}
		return true
	})
	if interstitial {
		return true
	}

	// Hosted runners can receive a Google control page instead of organic
	// results. Keep generic labels such as "利用規約" conservative by requiring
	// the matching Google control URL; the stronger historical labels remain
	// sufficient on their own.
	if len(results) > 0 && len(results) <= 4 {
		controlLinks := 0
		for _, result := range results {
			if isGoogleControlResult(result) {
				controlLinks++
			}
		}
		if controlLinks == len(results) {
			return true
		}
	}

	pageText := strings.ToLower(strings.TrimSpace(doc.Text()))
	for _, marker := range []string{"unusual traffic", "not a robot", "異常なトラフィック", "recaptcha"} {
		if strings.Contains(pageText, marker) {
			return true
		}
	}
	return false
}

func isGoogleControlResult(result titleWebSearchResult) bool {
	title := strings.ToLower(strings.TrimSpace(result.Title))
	rawURL := strings.ToLower(strings.TrimSpace(result.URL))

	switch title {
	case "ここをクリック", "click here", "フィードバック", "feedback":
		return true
	}

	if rawURL == "#" {
		switch title {
		case "このページが表示された理由", "why this page", "why this page is displayed":
			return true
		}
	}
	if strings.Contains(rawURL, "google.com/policies/terms") || strings.Contains(rawURL, "policies.google.com/terms") {
		switch title {
		case "利用規約", "terms", "terms of service":
			return true
		}
	}
	if strings.Contains(rawURL, "support.google.com/websearch/answer/86640") {
		return true
	}
	return false
}

func fetchGoogleSearchWithHeadlessBrowser(ctx context.Context, endpoint string) ([]titleWebSearchResult, error) {
	googleBrowserSearchMu.Lock()
	defer googleBrowserSearchMu.Unlock()

	// Once Google has positively served a control/interstitial page, repeatedly
	// launching Edge for every query variant only wastes tens of seconds. During
	// a short cooldown, use an independent search engine directly instead.
	if time.Now().Before(googleBrowserBlockedUntil) {
		return fetchBingSearchForGoogleEndpoint(ctx, endpoint)
	}

	browser, err := findHeadlessSearchBrowser()
	if err != nil {
		return fetchBingSearchForGoogleEndpoint(ctx, endpoint)
	}

	profile, err := os.MkdirTemp("", "javinizer-google-browser-")
	if err != nil {
		return nil, fmt.Errorf("create browser profile: %w", err)
	}
	defer os.RemoveAll(profile)

	browserCtx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()

	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-sync",
		"--lang=ja-JP",
		"--user-data-dir=" + profile,
		"--virtual-time-budget=6000",
		"--dump-dom",
		endpoint,
	}
	cmd := exec.CommandContext(browserCtx, browser, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	body, err := cmd.Output()
	if err != nil {
		// A browser timeout/failure should not make title resolution depend on a
		// single provider. Bing is deliberately HTTP-only and therefore cheap.
		if fallback, fallbackErr := fetchBingSearchForGoogleEndpoint(ctx, endpoint); fallbackErr == nil && len(fallback) > 0 {
			return fallback, nil
		}
		if browserCtx.Err() != nil {
			return nil, fmt.Errorf("headless Google search timed out: %w", browserCtx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 600 {
			detail = detail[:600]
		}
		if detail != "" {
			return nil, fmt.Errorf("headless Google search failed: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("headless Google search failed: %w", err)
	}
	if len(body) == 0 {
		return fetchBingSearchForGoogleEndpoint(ctx, endpoint)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(bytes.NewReader(body), maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse rendered Google search page: %w", err)
	}
	results := parseTitleWebResults("google", doc)
	if isGoogleSearchInterstitial(doc, results) {
		googleBrowserBlockedUntil = time.Now().Add(googleBrowserBlockCooldown)
		return fetchBingSearchForGoogleEndpoint(ctx, endpoint)
	}
	if len(results) == 0 {
		return fetchBingSearchForGoogleEndpoint(ctx, endpoint)
	}
	return results, nil
}

func fetchBingSearchForGoogleEndpoint(ctx context.Context, googleEndpoint string) ([]titleWebSearchResult, error) {
	parsed, err := url.Parse(googleEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Google search endpoint for fallback: %w", err)
	}
	query := strings.TrimSpace(parsed.Query().Get("q"))
	if query == "" {
		return nil, fmt.Errorf("Google search endpoint had no query for fallback")
	}
	return fetchBingTitleWebSearch(ctx, query)
}

func fetchBingTitleWebSearch(ctx context.Context, query string) ([]titleWebSearchResult, error) {
	endpoint := "https://www.bing.com/search?setlang=ja-JP&count=10&form=QBLH&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en-US;q=0.7,en;q=0.5")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Bing fallback request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Bing fallback returned HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse Bing fallback page: %w", err)
	}
	results := parseBingTitleWebResults(doc)
	if len(results) == 0 {
		return nil, fmt.Errorf("Bing fallback returned no organic results")
	}
	return results, nil
}

func parseBingTitleWebResults(doc *goquery.Document) []titleWebSearchResult {
	if doc == nil {
		return nil
	}
	results := make([]titleWebSearchResult, 0, 10)
	seen := make(map[string]struct{})
	doc.Find("li.b_algo").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		link := sel.Find("h2 a").First()
		if link.Length() == 0 {
			return true
		}
		title := strings.TrimSpace(spaceRE.ReplaceAllString(link.Text(), " "))
		href, _ := link.Attr("href")
		snippet := strings.TrimSpace(spaceRE.ReplaceAllString(sel.Find("div.b_caption p").First().Text(), " "))
		if snippet == "" {
			snippet = strings.TrimSpace(spaceRE.ReplaceAllString(sel.Find("p").First().Text(), " "))
		}
		if title == "" {
			return true
		}
		key := title + "\x00" + strings.TrimSpace(href)
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
		results = append(results, titleWebSearchResult{Title: title, Snippet: snippet, URL: strings.TrimSpace(href)})
		return len(results) < 10
	})
	return results
}

func findHeadlessSearchBrowser() (string, error) {
	for _, name := range []string{
		"msedge", "msedge.exe", "google-chrome", "google-chrome-stable",
		"chrome", "chrome.exe", "chromium", "chromium-browser",
	} {
		if path, err := exec.LookPath(name); err == nil && strings.TrimSpace(path) != "" {
			return path, nil
		}
	}

	candidates := make([]string, 0, 16)
	if runtime.GOOS == "windows" {
		for _, root := range []string{
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("ProgramFiles"),
			os.Getenv("LOCALAPPDATA"),
		} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			)
		}
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
	}
	if runtime.GOOS == "linux" {
		candidates = append(candidates,
			"/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable",
			"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
			"/usr/bin/chromium", "/usr/bin/chromium-browser",
		)
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no supported headless browser found (Edge/Chrome/Chromium)")
}
