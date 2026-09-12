package scrape

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	googleBrowserSearchMu sync.Mutex
	googleBrowserBlockedUntil time.Time
)

const googleBrowserBlockCooldown = 10 * time.Minute

func isGoogleSearchInterstitial(doc *goquery.Document, results []titleWebSearchResult) bool {
	if doc == nil { return false }
	interstitial := false
	doc.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href"); lower := strings.ToLower(href)
		if strings.Contains(lower,"emsg=sg_rel") || strings.Contains(lower,"emsg=sg_srch") { interstitial=true; return false }
		return true
	})
	if interstitial { return true }
	if len(results) > 0 && len(results) <= 4 {
		controls := 0
		for _, result := range results { if isGoogleControlResult(result) { controls++ } }
		if controls == len(results) { return true }
	}
	pageText := strings.ToLower(strings.TrimSpace(doc.Text()))
	for _, marker := range []string{"unusual traffic","not a robot","異常なトラフィック","recaptcha"} { if strings.Contains(pageText,marker) { return true } }
	return false
}

func isGoogleControlResult(result titleWebSearchResult) bool {
	title := strings.ToLower(strings.TrimSpace(result.Title)); rawURL := strings.ToLower(strings.TrimSpace(result.URL))
	switch title { case "ここをクリック","click here","フィードバック","feedback": return true }
	if rawURL == "#" { switch title { case "このページが表示された理由","why this page","why this page is displayed": return true } }
	if strings.Contains(rawURL,"google.com/policies/terms") || strings.Contains(rawURL,"policies.google.com/terms") { switch title { case "利用規約","terms","terms of service": return true } }
	return strings.Contains(rawURL,"support.google.com/websearch/answer/86640")
}

func fetchGoogleSearchWithHeadlessBrowser(ctx context.Context, endpoint string) ([]titleWebSearchResult, error) {
	googleBrowserSearchMu.Lock(); defer googleBrowserSearchMu.Unlock()
	if time.Now().Before(googleBrowserBlockedUntil) { return nil, fmt.Errorf("Google browser fallback is cooling down after an interstitial") }
	browser, err := findHeadlessSearchBrowser()
	if err != nil { return nil, err }
	profile, err := os.MkdirTemp("", "javinizer-google-browser-")
	if err != nil { return nil, fmt.Errorf("create browser profile: %w", err) }
	defer os.RemoveAll(profile)
	browserCtx, cancel := context.WithTimeout(ctx,18*time.Second); defer cancel()
	args := []string{"--headless=new","--disable-gpu","--no-first-run","--disable-default-apps","--disable-extensions","--disable-background-networking","--disable-sync","--lang=ja-JP","--user-data-dir="+profile,"--virtual-time-budget=6000","--dump-dom",endpoint}
	cmd := exec.CommandContext(browserCtx,browser,args...)
	var stderr strings.Builder; cmd.Stderr=&stderr
	body, err := cmd.Output()
	if err != nil {
		if browserCtx.Err()!=nil { return nil, fmt.Errorf("headless Google search timed out: %w",browserCtx.Err()) }
		detail:=strings.TrimSpace(stderr.String()); if len(detail)>600 { detail=detail[:600] }
		if detail!="" { return nil, fmt.Errorf("headless Google search failed: %w: %s",err,detail) }
		return nil, fmt.Errorf("headless Google search failed: %w",err)
	}
	if len(body)==0 { return nil, fmt.Errorf("headless Google search returned an empty document") }
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(bytes.NewReader(body),maxWebSearchBody))
	if err != nil { return nil, fmt.Errorf("parse rendered Google search page: %w",err) }
	results := parseTitleWebResults("google",doc)
	if isGoogleSearchInterstitial(doc,results) { googleBrowserBlockedUntil=time.Now().Add(googleBrowserBlockCooldown); return nil, fmt.Errorf("headless Google search was blocked by an interstitial") }
	if len(results)==0 { return nil, fmt.Errorf("headless Google search returned no organic results") }
	return results,nil
}
