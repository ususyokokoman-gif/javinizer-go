//go:build precision_preview

package worker

import "testing"

func TestPrecisionPreviewMustBlockApply(t *testing.T) {
 if err := precisionPreviewApplyGuard(); err == nil {t.Fatal("preview must block apply")}
}
