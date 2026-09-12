//go:build keepwords

package scrape

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
var googleBrowserSearchMu sync.Mutex

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

	// Hosted runners have also returned only these navigation/control links.
	// Organic pages normally contain many h3 results, so keep this conservative.
	if len(results) <= 3 {
		controlLinks := 0
		for _, result := range results {
			title := strings.ToLower(strings.TrimSpace(result.Title))
			switch title {
			case "ここをクリック", "click here", "フィードバック", "feedback":
				controlLinks++
			}
		}
		if controlLinks > 0 {
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

func fetchGoogleSearchWithHeadlessBrowser(ctx context.Context, endpoint string) ([]titleWebSearchResult, error) {
	googleBrowserSearchMu.Lock()
	defer googleBrowserSearchMu.Unlock()

	browser, err := findHeadlessSearchBrowser()
	if err != nil {
		return nil, err
	}

	profile, err := os.MkdirTemp("", "javinizer-google-browser-")
	if err != nil {
		return nil, fmt.Errorf("create browser profile: %w", err)
	}
	defer os.RemoveAll(profile)

	browserCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
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
		"--virtual-time-budget=8000",
		"--dump-dom",
		endpoint,
	}
	cmd := exec.CommandContext(browserCtx, browser, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	body, err := cmd.Output()
	if err != nil {
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
		return nil, fmt.Errorf("headless Google search returned an empty document")
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(bytes.NewReader(body), maxWebSearchBody))
	if err != nil {
		return nil, fmt.Errorf("parse rendered Google search page: %w", err)
	}
	results := parseTitleWebResults("google", doc)
	if isGoogleSearchInterstitial(doc, results) {
		return nil, fmt.Errorf("rendered Google search was still blocked by an interstitial")
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("rendered Google search returned no organic results")
	}
	return results, nil
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
