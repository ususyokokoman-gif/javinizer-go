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
	if prepared.Kind != scrape.TitleInputOpaque {
		resolved := resolveTitleWithRetry(resolver, prepared.Query, timeout, maxAttempts, baseDelay, sleep)
		resolved.ElapsedMS = time.Since(began).Milliseconds()
		return resolved
	}

	// Precision-first bulk policy for opaque/download IDs:
	// probe file-specific embedded metadata, but never turn that metadata into
	// a broad Web title search. Only deterministic local evidence may produce
	// a candidate, and even a local confirmation remains review because the
	// embedded title is file-specific supporting evidence.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	embedded, probeErr := embeddedTitleProbe(ctx, mediaPath)
	cancel()
	if probeErr == nil && strings.TrimSpace(embedded) != "" {
		embeddedPrepared := scrape.PrepareTitleResolutionInput(embedded)
		if embeddedPrepared.Query != "" && embeddedPrepared.Kind != scrape.TitleInputOpaque && !strings.EqualFold(embeddedPrepared.Query, prepared.Query) {
			fmt.Printf("EMBEDDED_TITLE=HIT input=%q title=%q\n", prepared.Query, embeddedPrepared.Query)
			if localResolver, ok := resolver.(localOnlyTitleDecisionResolver); ok {
				localCtx, localCancel := context.WithTimeout(context.Background(), timeout)
				decision, err := localResolver.ResolveLocalOnlyDecision(localCtx, embeddedPrepared.Query)
				localCancel()
				resolved := retryResolution{
					CatalogID: decision.CatalogID,
					Status:    string(decision.Status),
					Method:    decision.Method,
					Reason:    decision.Reason,
					Err:       err,
					Attempts:  1,
				}
				if resolved.Status == "confirmed" {
					resolved.Status = "review"
					resolved.Method = "埋込タイトル→" + resolved.Method
					resolved.Reason = "埋込タイトルからローカルDB上の一意候補を確認しましたが、埋込メタデータ単独では自動整理せず要確認とします。"
					resolved.ElapsedMS = time.Since(began).Milliseconds()
					return resolved
				}
				if resolved.Status == "review" {
					resolved.Method = "埋込タイトル→" + resolved.Method
					resolved.ElapsedMS = time.Since(began).Milliseconds()
					return resolved
				}
				if resolved.Status == "error" && resolved.Err != nil {
					resolved.Method = "埋込タイトル→" + resolved.Method
					resolved.ElapsedMS = time.Since(began).Milliseconds()
					return resolved
				}
				fmt.Printf("EMBEDDED_TITLE=LOCAL_MISS input=%q status=%s\n", prepared.Query, resolved.Status)
			} else {
				fmt.Printf("EMBEDDED_TITLE=LOCAL_RESOLVER_UNAVAILABLE input=%q\n", prepared.Query)
			}
		}
	}

	return retryResolution{
		Status:    "unknown",
		Method:    "不明ID＋埋込メタデータ確認",
		Reason:    "不明IDと埋込メタデータから決定的なローカル証拠を得られなかったため、一般Web検索を行わず未特定とします。",
		Attempts:  1,
		ElapsedMS: time.Since(began).Milliseconds(),
	}
}
