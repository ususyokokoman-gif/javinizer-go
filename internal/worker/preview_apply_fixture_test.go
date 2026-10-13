//go:build precision_preview

package worker

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrecisionPreviewControllerFailsClosedWithMediaFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "START-487-EC.mp4")
	before := []byte("fixture video bytes; never modify")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(before)
	var c jobController // Must reject even without workflow/dependencies.
	for i := 0; i < 3; i++ {
		err := c.StartApply(context.Background(), ApplyPhaseConfig{})
		if err == nil || !strings.Contains(err.Error(), "PRECISION_PREVIEW_READ_ONLY") {
			t.Fatalf("expected fail-closed gate at attempt %d, got %v", i, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != want {
		t.Fatal("fixture was modified despite preview gate")
	}
}
