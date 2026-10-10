package main

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

type titleResolverFunc func(context.Context, string) (string, error)

func (f titleResolverFunc) Resolve(ctx context.Context, title string) (string, error) {
	return f(ctx, title)
}

type localOnlyResolverStub struct {
	resolveCalls  []string
	localCalls    []string
	localDecision scrape.TitleResolutionDecision
	localErr      error
}

func (r *localOnlyResolverStub) Resolve(_ context.Context, title string) (string, error) {
	r.resolveCalls = append(r.resolveCalls, title)
	return "", nil
}

func (r *localOnlyResolverStub) ResolveLocalOnlyDecision(_ context.Context, title string) (scrape.TitleResolutionDecision, error) {
	r.localCalls = append(r.localCalls, title)
	return r.localDecision, r.localErr
}

func TestResolveWorkUsesEmbeddedTitleLocalOnlyForOpaqueInput(t *testing.T) {
	original := embeddedTitleProbe
	defer func() { embeddedTitleProbe = original }()
	embeddedTitleProbe = func(context.Context, string) (string, error) {
		return "狙われた通学路 共謀痴漢電車 桃乃木かな", nil
	}

	resolver := &localOnlyResolverStub{localDecision: scrape.TitleResolutionDecision{
		Status:    scrape.TitleDecisionConfirmed,
		CatalogID: "IPX-072",
		Method:    "ローカルタイトル完全一致",
		Reason:    "test",
	}}
	got := resolveWorkWithRetry(resolver, "/tmp/a.mp4", "c9dqb3ybpvq80kae_1280p", time.Second, 1, 0, func(time.Duration) {})
	if got.Err != nil || got.CatalogID != "IPX-072" || got.Status != "review" {
		t.Fatalf("resolution=%+v", got)
	}
	if len(resolver.localCalls) != 1 || resolver.localCalls[0] != "狙われた通学路 共謀痴漢電車 桃乃木かな" {
		t.Fatalf("localCalls=%v", resolver.localCalls)
	}
	if len(resolver.resolveCalls) != 0 {
		t.Fatalf("opaque embedded path called general resolver/Web path: %v", resolver.resolveCalls)
	}
	if got.Method != "埋込タイトル→ローカルタイトル完全一致" {
		t.Fatalf("method=%q", got.Method)
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

func TestResolveWorkOpaqueEmbeddedLocalMissNeverCallsGeneralResolver(t *testing.T) {
	original := embeddedTitleProbe
	defer func() { embeddedTitleProbe = original }()
	embeddedTitleProbe = func(context.Context, string) (string, error) {
		return "ビッグマネー 浮き世の沙汰は株しだい STOCK3", nil
	}

	resolver := &localOnlyResolverStub{localDecision: scrape.TitleResolutionDecision{
		Status: scrape.TitleDecisionUnknown,
		Method: "ローカル証拠のみ",
		Reason: "no local proof",
	}}
	got := resolveWorkWithRetry(resolver, "/tmp/a.mov", "0125534.Id_e0000000e936afc", time.Second, 1, 0, func(time.Duration) {})
	if got.Status != "unknown" || got.CatalogID != "" || got.Err != nil {
		t.Fatalf("resolution=%+v", got)
	}
	if len(resolver.localCalls) != 1 {
		t.Fatalf("localCalls=%v", resolver.localCalls)
	}
	if len(resolver.resolveCalls) != 0 {
		t.Fatalf("opaque fallback called general resolver/Web path: %v", resolver.resolveCalls)
	}
}

func TestResolveWorkOpaqueWithoutEmbeddedMetadataNeverCallsResolver(t *testing.T) {
	original := embeddedTitleProbe
	defer func() { embeddedTitleProbe = original }()
	embeddedTitleProbe = func(context.Context, string) (string, error) {
		return "", nil
	}

	resolver := &localOnlyResolverStub{}
	got := resolveWorkWithRetry(resolver, "/tmp/a.mp4", "xcobuazsdivkokyx_720p", time.Second, 1, 0, func(time.Duration) {})
	if got.Status != "unknown" || got.Err != nil {
		t.Fatalf("resolution=%+v", got)
	}
	if len(resolver.localCalls) != 0 || len(resolver.resolveCalls) != 0 {
		t.Fatalf("opaque path unexpectedly called resolver: local=%v general=%v", resolver.localCalls, resolver.resolveCalls)
	}
}
