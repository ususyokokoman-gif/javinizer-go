//go:build precision_preview

package worker

import "errors"

var errPrecisionPreviewApplyDisabled = errors.New("PRECISION_PREVIEW_READ_ONLY: file changes and apply are disabled in the Windows preview")

func precisionPreviewApplyGuard() error { return errPrecisionPreviewApplyDisabled }
