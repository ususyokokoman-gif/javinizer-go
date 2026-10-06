package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

var errEmbeddedTitleProbeUnavailable = errors.New("embedded title probe unavailable")

var (
	ffprobeOnce        sync.Once
	ffprobePath        string
	embeddedTitleProbe = probeEmbeddedTitle
)

type ffprobeFormatTags struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

func locateFFprobe() string {
	ffprobeOnce.Do(func() {
		if path, err := exec.LookPath("ffprobe"); err == nil {
			ffprobePath = path
			return
		}
		candidates := []string{
			"/opt/homebrew/bin/ffprobe",
			"/usr/local/bin/ffprobe",
		}
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			candidates = append([]string{filepath.Join(dir, "ffprobe")}, candidates...)
			if runtime.GOOS == "windows" {
				candidates = append([]string{filepath.Join(dir, "ffprobe.exe")}, candidates...)
			}
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				ffprobePath = candidate
				return
			}
		}
	})
	return ffprobePath
}

func probeEmbeddedTitle(ctx context.Context, mediaPath string) (string, error) {
	bin := locateFFprobe()
	if bin == "" {
		return "", errEmbeddedTitleProbeUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-show_entries", "format_tags=title",
		"-of", "json",
		mediaPath,
	)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("ffprobe embedded title: %w", err)
	}
	var decoded ffprobeFormatTags
	if err := json.Unmarshal(out, &decoded); err != nil {
		return "", fmt.Errorf("decode ffprobe title: %w", err)
	}
	for key, value := range decoded.Format.Tags {
		if strings.EqualFold(strings.TrimSpace(key), "title") {
			return strings.TrimSpace(value), nil
		}
	}
	return "", nil
}

func resolveWorkWithRetry(resolver titleResolver, mediaPath, input string, timeout time.Duration, maxAttempts int, baseDelay time.Duration, sleep func(time.Duration)) retryResolution {
	began := time.Now()
	prepared := scrape.PrepareTitleResolutionInput(input)
	var embeddedReview *retryResolution
	if prepared.Kind == scrape.TitleInputOpaque {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		embedded, probeErr := embeddedTitleProbe(ctx, mediaPath)
		cancel()
		if probeErr == nil && strings.TrimSpace(embedded) != "" {
			embeddedPrepared := scrape.PrepareTitleResolutionInput(embedded)
			if embeddedPrepared.Query != "" && embeddedPrepared.Kind != scrape.TitleInputOpaque && !strings.EqualFold(embeddedPrepared.Query, prepared.Query) {
				fmt.Printf("EMBEDDED_TITLE=HIT input=%q title=%q\n", prepared.Query, embeddedPrepared.Query)
				resolved := resolveTitleWithRetry(resolver, embeddedPrepared.Query, timeout, maxAttempts, baseDelay, sleep)
				if resolved.Status == "confirmed" {
					resolved.ElapsedMS = time.Since(began).Milliseconds()
					return resolved
				}
				if resolved.Status == "review" {
					copy := resolved
					embeddedReview = &copy
				}
				if resolved.Err != nil {
					fmt.Printf("EMBEDDED_TITLE=FALLBACK input=%q error=%v\n", prepared.Query, resolved.Err)
				} else {
					fmt.Printf("EMBEDDED_TITLE=FALLBACK input=%q status=%s\n", prepared.Query, resolved.Status)
				}
			}
		}
	}
	resolved := resolveTitleWithRetry(resolver, prepared.Query, timeout, maxAttempts, baseDelay, sleep)
	// 埋込タイトル側に要確認候補があり、元の不明ID側では何も分からなかった場合は
	// 候補だけ残す。ただし「確定」へ昇格はさせない。
	if embeddedReview != nil && (resolved.Status == "unknown" || resolved.Status == "error") {
		resolved = *embeddedReview
	}
	// 埋込タイトルと不明ID逆引きで別候補が出た場合は矛盾として候補を消し、
	// 要確認に固定する。多数決でどちらかを選ばない。
	if embeddedReview != nil && resolved.Status == "review" &&
		strings.TrimSpace(embeddedReview.CatalogID) != "" &&
		strings.TrimSpace(resolved.CatalogID) != "" &&
		!strings.EqualFold(embeddedReview.CatalogID, resolved.CatalogID) {
		resolved.CatalogID = ""
		resolved.Method = "埋込タイトルと不明IDの照合"
		resolved.Reason = "埋込タイトル由来候補と不明ID逆引き候補が一致しないため、自動整理せず要確認とします。"
	}
	resolved.ElapsedMS = time.Since(began).Milliseconds()
	return resolved
}
