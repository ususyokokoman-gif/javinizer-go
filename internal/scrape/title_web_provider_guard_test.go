package scrape

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTitleSearchProviderGuardTripsAfterRepeatedTimeouts(t *testing.T) {
	g := newTitleSearchProviderGuard()
	g.failure("bing", errors.New("Bing request failed: context deadline exceeded"))
	if err := g.wait(context.Background(), "bing", 0); err != nil {
		t.Fatalf("first timeout must not trip provider: %v", err)
	}
	g.failure("bing", errors.New("Bing request failed: context deadline exceeded"))
	if err := g.wait(context.Background(), "bing", 0); err == nil || !strings.Contains(err.Error(), "temporarily disabled") {
		t.Fatalf("second timeout must trip provider, err=%v", err)
	}
}

func TestTitleSearchProviderGuard403TripsImmediately(t *testing.T) {
	g := newTitleSearchProviderGuard()
	g.failure("yahoojp", errors.New("Yahoo Japan returned HTTP 403"))
	if err := g.wait(context.Background(), "yahoojp", 0); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("403 must immediately disable provider, err=%v", err)
	}
}

func TestTitleSearchProviderGuardSuccessResetsFailureCount(t *testing.T) {
	g := newTitleSearchProviderGuard()
	g.failure("duckduckgo", errors.New("DuckDuckGo request failed: timeout"))
	g.success("duckduckgo")
	g.failure("duckduckgo", errors.New("DuckDuckGo request failed: timeout"))
	if err := g.wait(context.Background(), "duckduckgo", 0); err != nil {
		t.Fatalf("success must reset consecutive failure count: %v", err)
	}
}

func TestTitleSearchProviderGuard429UsesCooldown(t *testing.T) {
	g := newTitleSearchProviderGuard()
	g.rateLimited("google", "1", time.Minute)
	if err := g.wait(context.Background(), "google", 0); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("429 cooldown missing, err=%v", err)
	}
}

func TestTitleSearchProviderGuardSerializesInFlightRequests(t *testing.T) {
	g := newTitleSearchProviderGuard()
	if err := g.wait(context.Background(), "bing", 0); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() {
		acquired <- g.wait(ctx, "bing", 0)
	}()

	select {
	case err := <-acquired:
		t.Fatalf("second request passed while first was in flight: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	g.done("bing")
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("second request did not acquire after release: %v", err)
		}
		g.done("bing")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("second request remained blocked after provider release")
	}
}

func TestTitleSearchProviderGuardWaitingRequestSeesCooldownAfterFailure(t *testing.T) {
	g := newTitleSearchProviderGuard()
	if err := g.wait(context.Background(), "google", 0); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- g.wait(context.Background(), "google", 0)
	}()
	time.Sleep(30 * time.Millisecond)

	g.failure("google", errors.New("Google request failed: context deadline exceeded"))
	g.failure("google", errors.New("Google request failed: context deadline exceeded"))
	g.done("google")

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "temporarily disabled") {
			t.Fatalf("waiting request ignored provider cooldown: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("waiting request did not fail fast after provider cooldown")
	}
}
