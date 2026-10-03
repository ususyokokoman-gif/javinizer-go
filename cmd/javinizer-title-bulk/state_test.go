package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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
		{"", errors.New("Google search returned HTTP 429")},
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
	if resolutionStatus(got.Err) != "rejected" {
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
	task := titleWork{Title: "作品タイトル", Indices: []int{0}}
	if err := store.recordTask(task, files, "accepted", "ABC-123", "", 2); err != nil {
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
	cached, ok := reloaded.cachedTitle("作品タイトル")
	if !ok {
		t.Fatal("expected terminal cached title")
	}
	if cached.CatalogID != "ABC-123" || cached.Status != "accepted" || cached.Attempts != 2 {
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
	task := titleWork{Title: "作品タイトル", Indices: []int{0}}
	if err := store.recordTask(task, files, "error", "", "HTTP 429", 3); err != nil {
		t.Fatal(err)
	}
	if err := store.checkpoint(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.cachedTitle("作品タイトル"); ok {
		t.Fatal("transient error must be retried on next run")
	}
}
