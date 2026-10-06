package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

type sequenceResolver struct {
	results []struct {
		id  string
		err error
	}
	calls int
}

func (r *sequenceResolver) Resolve(_ context.Context, _ string) (string, error) {
	idx := r.calls
	r.calls++
	if idx >= len(r.results) {
		return "", errors.New("unexpected extra call")
	}
	return r.results[idx].id, r.results[idx].err
}

func TestResolveTitleWithRetryTransientThenSuccess(t *testing.T) {
	r := &sequenceResolver{results: []struct {
		id  string
		err error
	}{
		{"", errors.New("upstream returned HTTP 503")},
		{"IPX-072", nil},
	}}
	var slept []time.Duration
	got := resolveTitleWithRetry(r, "title", time.Second, 3, time.Millisecond, func(d time.Duration) {
		slept = append(slept, d)
	})
	if got.Err != nil {
		t.Fatalf("err=%v", got.Err)
	}
	if got.CatalogID != "IPX-072" || got.Attempts != 2 || r.calls != 2 {
		t.Fatalf("got=%+v calls=%d", got, r.calls)
	}
	if len(slept) != 1 {
		t.Fatalf("sleeps=%d, want 1", len(slept))
	}
}

func TestResolveTitleWithRetryPermanentRejectDoesNotRetry(t *testing.T) {
	r := &sequenceResolver{results: []struct {
		id  string
		err error
	}{
		{"", errors.New("Jev rejected catalog-ID candidate IPX-072: probability 0.700 below threshold 0.800")},
		{"IPX-072", nil},
	}}
	got := resolveTitleWithRetry(r, "title", time.Second, 3, 0, func(time.Duration) {})
	if got.Err == nil {
		t.Fatal("expected rejection")
	}
	if got.Attempts != 1 || r.calls != 1 {
		t.Fatalf("attempts=%d calls=%d, want 1/1", got.Attempts, r.calls)
	}
	if resolutionStatus(got.Err) != "review" {
		t.Fatalf("status=%q", resolutionStatus(got.Err))
	}
}

func TestTransientErrorsRemainRetryable(t *testing.T) {
	cases := []string{
		"context deadline exceeded",
		"Jev catalog request returned HTTP 503",
		"Google HTTP search returned HTTP 403",
		"connection reset by peer",
	}
	for _, tc := range cases {
		if !isTransientResolutionError(errors.New(tc)) {
			t.Fatalf("expected transient: %s", tc)
		}
	}
	if isTransientResolutionError(errors.New("Jev rejected catalog-ID candidate ABC-123")) {
		t.Fatal("Jev reject must not be transient")
	}
}

func TestStateCheckpointAndTerminalCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bulk-state.json")
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	files := []fileItem{{Path: "movie.mp4", Size: 123, ModTimeNS: 456}}
	task := titleWork{Kind: scrape.TitleInputTitle, Title: "作品タイトル", Indices: []int{0}}
	if err := store.recordTask(task, files, "confirmed", "ABC-123", "ローカルタイトル完全一致", "完全一致", "", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state should not be rewritten before checkpoint, stat err=%v", err)
	}
	if err := store.checkpoint(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	cached, ok := reloaded.cachedTitle(task.cacheKey())
	if !ok {
		t.Fatal("expected terminal cached title")
	}
	if cached.CatalogID != "ABC-123" || cached.Status != "confirmed" || cached.Attempts != 2 {
		t.Fatalf("cached=%+v", cached)
	}
}

func TestTransientErrorIsNotTerminalCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bulk-state.json")
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	files := []fileItem{{Path: "movie.mp4", Size: 123, ModTimeNS: 456}}
	task := titleWork{Kind: scrape.TitleInputTitle, Title: "作品タイトル", Indices: []int{0}}
	if err := store.recordTask(task, files, "error", "", "外部検索", "通信エラー", "HTTP 429", 3); err != nil {
		t.Fatal(err)
	}
	if err := store.checkpoint(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.cachedTitle(task.cacheKey()); ok {
		t.Fatal("transient error must be retried on next run")
	}
}

func TestLegacyLocalAcceptCacheIsRevalidated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"titles":{"作品":{"status":"accepted","catalog_id":"ABC-123"}},"files":{"movie.mp4":{"status":"accepted","catalog_id":"ABC-123"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.cachedTitle("作品"); ok || len(store.state.Files) != 0 {
		t.Fatal("old local accept cache bypasses mandatory Jev")
	}
}

func TestRateLimitErrorsAreNotImmediatelyRetried(t *testing.T) {
	for _, message := range []string{
		"Google search returned HTTP 429",
		"too many requests",
		"google search temporarily disabled after rate limit; retry in 5m",
	} {
		if isTransientResolutionError(errors.New(message)) {
			t.Fatalf("rate limit error must not trigger per-item retry: %q", message)
		}
	}
}

func TestStateCacheRejectsDifferentDecisionPolicyVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	task := titleWork{Kind: scrape.TitleInputTitle, Title: "作品タイトル", Indices: []int{0}}
	st := emptyPersistentState()
	st.Titles[task.cacheKey()] = titleCacheRecord{
		CatalogID:     "ABC-123",
		Status:        "confirmed",
		PolicyVersion: scrape.TitleDecisionPolicyVersion + 1,
	}
	if err := writePersistentState(path, st); err != nil {
		t.Fatal(err)
	}
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.cachedTitle(task.cacheKey()); ok {
		t.Fatal("異なる判定基準版の確定結果を再利用してはいけない")
	}
}
