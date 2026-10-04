package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var processStarted = time.Now()

// phaseTiming deliberately excludes titles, paths and credentials.
func phaseTiming(scope, phase, event string, began time.Time, origin time.Time, err error) string {
	status := "ok"
	if err != nil {
		status = "error"
	}
	now := time.Now()
	return fmt.Sprintf("TIMING scope=%s phase=%s event=%s timestamp=%s elapsed_ms=%.3f since_start_ms=%.3f status=%s", scope, phase, event, now.UTC().Format(time.RFC3339Nano), float64(now.Sub(began).Microseconds())/1000, float64(now.Sub(origin).Microseconds())/1000, status)
}

func beginCLIPhase(phase string) func(error) {
	began := time.Now()
	fmt.Println(phaseTiming("cli", phase, "START", began, processStarted, nil))
	return func(err error) { fmt.Println(phaseTiming("cli", phase, "END", began, processStarted, err)) }
}

// runTrace buffers startup events before the output folder is available.
// Its file remains open until the next run/shutdown so late WebKit paint
// measurements are retained even when a cached run completes within one frame.
type runTrace struct {
	mu      sync.Mutex
	file    *os.File
	pending []string
	failed  bool
}

func (t *runTrace) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file != nil {
		_ = t.file.Close()
	}
	t.file = nil
	t.pending = nil
	t.failed = false
}
func (t *runTrace) open(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	for _, line := range t.pending {
		if _, err := fmt.Fprintln(f, line); err != nil {
			_ = f.Close()
			return err
		}
	}
	t.file = f
	t.pending = nil
	return nil
}
func (t *runTrace) write(line string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil {
		if len(t.pending) < 1000 {
			t.pending = append(t.pending, line)
		}
		return false
	}
	if _, err := fmt.Fprintln(t.file, line); err != nil && !t.failed {
		t.failed = true
		return true
	}
	return false
}
func (t *runTrace) close() { t.reset() }
