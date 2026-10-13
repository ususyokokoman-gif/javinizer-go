//go:build precision_preview

package history

import (
	"strings"
	"testing"
)

// A nil command and absent batch ID are deliberate: the guard must execute
// before config loading, history lookup, cleanup sweep, or filesystem access.
func TestPreviewHistoryRevertFailsBeforeAnyDependencies(t *testing.T) {
	err := runHistoryRevert(nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "PRECISION_PREVIEW_READ_ONLY") {
		t.Fatalf("history revert was not rejected immediately: %v", err)
	}
}
