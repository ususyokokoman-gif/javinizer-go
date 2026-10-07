package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

const bulkStateVersion = 8

type titleResolver interface {
	Resolve(context.Context, string) (string, error)
}

type titleDecisionResolver interface {
	ResolveDecision(context.Context, string) (scrape.TitleResolutionDecision, error)
}

type fileStateRecord struct {
	Size          int64  `json:"size"`
	ModTimeNS     int64  `json:"mtime_ns"`
	Title         string `json:"title"`
	CatalogID     string `json:"catalog_id,omitempty"`
	Status        string `json:"status"`
	Method        string `json:"method,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PolicyVersion int    `json:"decision_policy_version"`
	Error         string `json:"error,omitempty"`
	Attempts      int    `json:"attempts,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

type titleCacheRecord struct {
	CatalogID     string `json:"catalog_id,omitempty"`
	Status        string `json:"status"`
	Method        string `json:"method,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PolicyVersion int    `json:"decision_policy_version"`
	Error         string `json:"error,omitempty"`
	Attempts      int    `json:"attempts,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

type persistentState struct {
	Version int                         `json:"version"`
	Files   map[string]fileStateRecord  `json:"files"`
	Titles  map[string]titleCacheRecord `json:"titles"`
}

type stateStore struct {
	mu    sync.Mutex
	path  string
	state persistentState
	dirty bool
}

func emptyPersistentState() persistentState {
	return persistentState{
		Version: bulkStateVersion,
		Files:   make(map[string]fileStateRecord),
		Titles:  make(map[string]titleCacheRecord),
	}
}

func newStateStore(path string, resume bool) (*stateStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("state path is empty")
	}
	st := emptyPersistentState()
	if resume {
		loaded, err := loadPersistentState(path)
		if err != nil {
			return nil, err
		}
		st = loaded
	}
	return &stateStore{path: path, state: st}, nil
}

func loadPersistentState(path string) (persistentState, error) {
	for _, candidate := range []string{path, path + ".bak"} {
		raw, err := os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return persistentState{}, fmt.Errorf("read state %s: %w", candidate, err)
		}
		var st persistentState
		if err := json.Unmarshal(raw, &st); err != nil {
			continue
		}
		if st.Version != bulkStateVersion {
			// v8 では、ファイル名先頭の明確な品番をタイトルより先に分類する。
			// task/cache key の意味が変わるため、旧stateの確定・要確認・未特定を
			// 混在させず、現在の入力分類と判定基準で全件を再評価する。
			st = emptyPersistentState()
		}
		if st.Files == nil {
			st.Files = make(map[string]fileStateRecord)
		}
		if st.Titles == nil {
			st.Titles = make(map[string]titleCacheRecord)
		}
		return st, nil
	}
	return emptyPersistentState(), nil
}

func (s *stateStore) cachedTitle(title string) (titleCacheRecord, bool) {
	if s == nil {
		return titleCacheRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.state.Titles[title]
	if !ok || !isTerminalStatus(r.Status) || r.PolicyVersion != scrape.TitleDecisionPolicyVersion {
		return titleCacheRecord{}, false
	}
	return r, true
}

func (s *stateStore) recordTask(task titleWork, files []fileItem, status, catalogID, method, reason, errorText string, attempts int) error {
	if s == nil {
		return fmt.Errorf("state store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Files == nil {
		s.state.Files = make(map[string]fileStateRecord)
	}
	if s.state.Titles == nil {
		s.state.Titles = make(map[string]titleCacheRecord)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.state.Titles[task.cacheKey()] = titleCacheRecord{
		CatalogID:     catalogID,
		Status:        status,
		Method:        method,
		Reason:        reason,
		PolicyVersion: scrape.TitleDecisionPolicyVersion,
		Error:         errorText,
		Attempts:      attempts,
		UpdatedAt:     now,
	}
	for _, index := range task.Indices {
		if index < 0 || index >= len(files) {
			return fmt.Errorf("state file index %d out of range", index)
		}
		f := files[index]
		s.state.Files[f.Path] = fileStateRecord{
			Size:          f.Size,
			ModTimeNS:     f.ModTimeNS,
			Title:         task.Title,
			CatalogID:     catalogID,
			Status:        status,
			Method:        method,
			Reason:        reason,
			PolicyVersion: scrape.TitleDecisionPolicyVersion,
			Error:         errorText,
			Attempts:      attempts,
			UpdatedAt:     now,
		}
	}
	s.dirty = true
	return nil
}

func (s *stateStore) checkpoint() error {
	if s == nil {
		return fmt.Errorf("state store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	if err := writePersistentState(s.path, s.state); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func writePersistentState(path string, st persistentState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := path + ".tmp"
	bak := path + ".bak"
	if err := writeSyncedFile(tmp, raw, 0o644); err != nil {
		return err
	}

	_ = os.Remove(bak)
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, bak); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("backup previous state: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return fmt.Errorf("stat previous state: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Rename(bak, path)
		_ = os.Remove(tmp)
		return fmt.Errorf("activate state checkpoint: %w", err)
	}
	_ = os.Remove(bak)
	return nil
}

func writeSyncedFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open state temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write state temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync state temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close state temp file: %w", err)
	}
	return nil
}

type retryResolution struct {
	CatalogID string
	Status    string
	Method    string
	Reason    string
	Err       error
	Attempts  int
	ElapsedMS int64
}

func resolveTitleWithRetry(resolver titleResolver, title string, timeout time.Duration, maxAttempts int, baseDelay time.Duration, sleep func(time.Duration)) retryResolution {
	began := time.Now()
	if sleep == nil {
		sleep = time.Sleep
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var id, status, method, reason string
	var err error
	attempts := 0
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attempts = attempt
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		if detailed, ok := resolver.(titleDecisionResolver); ok {
			decision, decisionErr := detailed.ResolveDecision(ctx, title)
			id = decision.CatalogID
			status = string(decision.Status)
			method = decision.Method
			reason = decision.Reason
			err = decisionErr
		} else {
			id, err = resolver.Resolve(ctx, title)
			status = resolutionStatus(err)
			if err == nil {
				method = "従来方式"
				reason = "従来方式の判定結果です。"
			}
		}
		cancel()
		if err == nil {
			break
		}
		if !isTransientResolutionError(err) || attempt == maxAttempts {
			break
		}
		delay := retryDelay(baseDelay, attempt, title)
		fmt.Printf("RETRY title=%q attempt=%d/%d delay=%s error=%v\n", title, attempt+1, maxAttempts, delay, err)
		sleep(delay)
	}
	if status == "" {
		status = resolutionStatus(err)
	}
	return retryResolution{
		CatalogID: id,
		Status:    status,
		Method:    method,
		Reason:    reason,
		Err:       err,
		Attempts:  attempts,
		ElapsedMS: time.Since(began).Milliseconds(),
	}
}

func retryDelay(base time.Duration, failedAttempt int, title string) time.Duration {
	if base <= 0 {
		return 0
	}
	delay := base
	for i := 1; i < failedAttempt && delay < 30*time.Second; i++ {
		delay *= 2
	}
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%d", title, failedAttempt)))
	jitterWindow := delay / 4
	if jitterWindow <= 0 {
		return delay
	}
	jitter := time.Duration(uint64(h.Sum32()) % uint64(jitterWindow+1))
	return delay + jitter
}

func isTransientResolutionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// A provider-wide 429 cooldown cannot be helped by immediately retrying the
	// same item. Let the item remain non-terminal/error and move on; a later run
	// can retry after the shared provider guard has recovered.
	if strings.Contains(msg, "http 429") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "temporarily disabled after rate limit") {
		return false
	}
	patterns := []string{
		"context deadline exceeded",
		"timeout",
		"timed out",
		"temporary",
		"connection reset",
		"connection refused",
		"connection aborted",
		"broken pipe",
		"unexpected eof",
		"server misbehaving",
		"no such host",
		"http 403",
		"http 408",
		"http 425",
		"http 500",
		"http 502",
		"http 503",
		"http 504",
	}
	for _, pattern := range patterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

func resolutionStatus(err error) string {
	if err == nil {
		return "confirmed"
	}
	if isTransientResolutionError(err) {
		return "error"
	}
	msg := strings.ToLower(err.Error())
	reviewPatterns := []string{
		"jev rejected catalog-id candidate",
		"ambiguous title matches multiple verified catalog ids",
		"conflicts with web evidence",
	}
	for _, pattern := range reviewPatterns {
		if strings.Contains(msg, pattern) {
			return "review"
		}
	}
	unknownPatterns := []string{
		"no sufficiently corroborated catalog-id candidate",
		"opaque filename key produced no sufficiently corroborated catalog-id candidate",
		"title is empty",
		"candidate is empty",
		"no title candidates found",
	}
	for _, pattern := range unknownPatterns {
		if strings.Contains(msg, pattern) {
			return "unknown"
		}
	}
	return "error"
}

func isTerminalStatus(status string) bool {
	return status == "confirmed" || status == "review" || status == "unknown"
}
