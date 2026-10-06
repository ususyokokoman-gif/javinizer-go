package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type titleResolverFunc func(context.Context, string) (string, error)

func (f titleResolverFunc) Resolve(ctx context.Context, title string) (string, error) {
	return f(ctx, title)
}

func TestResolveWorkUsesEmbeddedHumanTitleBeforeOpaqueWebLookup(t *testing.T) {
	original := embeddedTitleProbe
	defer func() { embeddedTitleProbe = original }()
	embeddedTitleProbe = func(context.Context, string) (string, error) {
		return "狙われた通学路 共謀痴漢電車 桃乃木かな", nil
	}

	var calls []string
	resolver := titleResolverFunc(func(_ context.Context, title string) (string, error) {
		calls = append(calls, title)
		if title == "狙われた通学路 共謀痴漢電車 桃乃木かな" {
			return "IPX-072", nil
		}
		return "", fmt.Errorf("unexpected query %q", title)
	})

	got := resolveWorkWithRetry(resolver, "/tmp/a.mp4", "c9dqb3ybpvq80kae_1280p", time.Second, 1, 0, func(time.Duration) {})
	if got.Err != nil || got.CatalogID != "IPX-072" {
		t.Fatalf("resolution=%+v", got)
	}
	if len(calls) != 1 || calls[0] != "狙われた通学路 共謀痴漢電車 桃乃木かな" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestResolveWorkDoesNotProbeEmbeddedTitleForHumanTitle(t *testing.T) {
	original := embeddedTitleProbe
	defer func() { embeddedTitleProbe = original }()
	probeCalls := 0
	embeddedTitleProbe = func(context.Context, string) (string, error) {
		probeCalls++
		return "", nil
	}

	resolver := titleResolverFunc(func(_ context.Context, title string) (string, error) {
		if title != "今日、あなたの上司に犯されました。 大橋未久" {
			t.Fatalf("title=%q", title)
		}
		return "MIDE-007", nil
	})
	got := resolveWorkWithRetry(resolver, "/tmp/a.mp4", "今日、あなたの上司に犯されました。 大橋未久", time.Second, 1, 0, func(time.Duration) {})
	if got.Err != nil || got.CatalogID != "MIDE-007" {
		t.Fatalf("resolution=%+v", got)
	}
	if probeCalls != 0 {
		t.Fatalf("probeCalls=%d, want 0", probeCalls)
	}
}

func TestResolveWorkFallsBackToOpaqueKeyWhenEmbeddedTitleFails(t *testing.T) {
	original := embeddedTitleProbe
	defer func() { embeddedTitleProbe = original }()
	embeddedTitleProbe = func(context.Context, string) (string, error) {
		return "候補タイトル", nil
	}

	var calls []string
	resolver := titleResolverFunc(func(_ context.Context, title string) (string, error) {
		calls = append(calls, title)
		if title == "候補タイトル" {
			return "", fmt.Errorf("no candidate")
		}
		if title == "c9dqb3ybpvq80kae" {
			return "IPX-072", nil
		}
		return "", fmt.Errorf("unexpected query %q", title)
	})

	got := resolveWorkWithRetry(resolver, "/tmp/a.mp4", "c9dqb3ybpvq80kae_720p", time.Second, 1, 0, func(time.Duration) {})
	if got.Err != nil || got.CatalogID != "IPX-072" {
		t.Fatalf("resolution=%+v", got)
	}
	if len(calls) != 2 || calls[0] != "候補タイトル" || calls[1] != "c9dqb3ybpvq80kae" {
		t.Fatalf("calls=%v", calls)
	}
}
