//go:build !precision_preview

package worker

func precisionPreviewApplyGuard() error { return nil }

func IsPrecisionPreviewBuild() bool { return false }
