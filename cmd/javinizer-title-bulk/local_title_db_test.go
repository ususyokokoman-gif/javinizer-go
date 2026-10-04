package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMissingTitleDumpFallsBackWithoutDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unconfigured", "dump.db")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store, err := prepareLocalTitleLookup(ctx, path)
	if store != nil || !errors.Is(err, models.ErrDumpTitleSearchUnavailable) {
		t.Fatalf("store=%v err=%v; want unavailable", store, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("normal title lookup created download directory: %v", err)
	}
}
