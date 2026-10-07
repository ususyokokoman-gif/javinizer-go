package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

var mediaExt = map[string]struct{}{
	".mp4": {}, ".mkv": {}, ".avi": {}, ".wmv": {}, ".flv": {},
	".mov": {}, ".m4v": {}, ".ts": {}, ".webm": {},
}

type fileItem struct {
	Path      string
	Size      int64
	ModTimeNS int64
}

type duplicateRecord struct {
	DuplicatePath string `json:"duplicate_path"`
	CanonicalPath string `json:"canonical_path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
}

type resultRecord struct {
	Path                  string `json:"path"`
	Filename              string `json:"filename"`
	Title                 string `json:"title"`
	CatalogID             string `json:"catalog_id,omitempty"`
	Status                string `json:"status"`
	StatusJapanese        string `json:"status_ja"`
	Method                string `json:"method,omitempty"`
	Reason                string `json:"reason,omitempty"`
	DecisionPolicyVersion int    `json:"decision_policy_version"`
	AutoOrganizeEligible  bool   `json:"auto_organize_eligible"`
	Error                 string `json:"error,omitempty"`
	ElapsedMS             int64  `json:"elapsed_ms"`
	Attempts              int    `json:"attempts,omitempty"`
	Cached                bool   `json:"cached,omitempty"`
}

type titleWork struct {
	Kind    scrape.TitleInputKind
	Title   string
	Indices []int
}

func (w titleWork) cacheKey() string {
	return string(w.Kind) + "\x00" + strings.ToLower(strings.TrimSpace(w.Title))
}

type titleWorkResult struct {
	TaskIndex int
	CatalogID string
	Status    string
	Method    string
	Reason    string
	Error     string
	ElapsedMS int64
	Attempts  int
}

func main() {
	var (
		root                = flag.String("root", "", "Root directory containing media files")
		filesManifest       = flag.String("files-manifest", "", "JSON file containing an explicit list of media files")
		outDir              = flag.String("out", "bulk-title-jev-output", "Output directory")
		workers             = flag.Int("workers", 4, "Concurrent local identification workers; web providers are paced separately")
		timeout             = flag.Duration("timeout", 20*time.Second, "Per-item identification timeout")
		quickBytes          = flag.Int64("quick-hash-bytes", 64<<10, "Bytes sampled from head and tail for duplicate prefilter")
		skipDup             = flag.Bool("skip-duplicates", true, "Skip duplicate detection (set -skip-duplicates=false to enable)")
		resume              = flag.Bool("resume", true, "Resume from durable title state and reuse terminal cached results")
		statePath           = flag.String("state", "", "State file path (default: <out>/bulk-state.json)")
		resolutionCachePath = flag.String("resolution-cache", "", "Shared accepted-resolution cache (default: JAVINIZER user config directory)")
		maxAttempts         = flag.Int("max-attempts", 1, "Maximum attempts per item; provider-wide cooldown handles rate limits")
		retryBase           = flag.Duration("retry-base-delay", 2*time.Second, "Base delay for transient retry backoff when max-attempts > 1")
		r18Dump             = flag.String("r18-dump", "", "Local r18.dev dump path (default: JAVINIZER user config directory)")
		noLocalDB           = flag.Bool("no-local-title-db", false, "Disable local r18.dev title lookup and use web fallback only")
		prepareDB           = flag.Bool("prepare-title-db", false, "Prepare/update the local r18.dev title database and exit")
	)
	flag.Parse()

	if *prepareDB {
		db, err := prepareLocalTitleLookupWithDownload(context.Background(), *r18Dump, true)
		if err != nil {
			fatalf("prepare local title database: %v", err)
		}
		if db != nil {
			_ = db.Close()
		}
		fmt.Println("TITLE_DB_PREPARE=PASS")
		return
	}

	rootValue := strings.TrimSpace(*root)
	manifestValue := strings.TrimSpace(*filesManifest)
	if rootValue == "" && manifestValue == "" {
		// Double-click / no-argument startup is the normal end-user path.
		if len(os.Args) == 1 {
			if err := runGUI(); err != nil {
				fatalf("start GUI: %v", err)
			}
			return
		}
		fatalf("-root or -files-manifest is required")
	}
	if rootValue != "" && manifestValue != "" {
		fatalf("-root and -files-manifest cannot be used together")
	}
	runStarted := time.Now()
	configDone := beginCLIPhase("config")
	if *workers < 1 {
		fatalf("-workers must be >= 1")
	}
	if *quickBytes < 64<<10 {
		fatalf("-quick-hash-bytes must be >= 65536")
	}
	if *maxAttempts < 1 {
		fatalf("-max-attempts must be >= 1")
	}
	if *retryBase < 0 {
		fatalf("-retry-base-delay must be >= 0")
	}
	if strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")) == "" {
		fatalf("TYPESAFE_API_KEY is required")
	}

	absRoot := ""
	absManifest := ""
	var err error
	if rootValue != "" {
		absRoot, err = filepath.Abs(rootValue)
		if err != nil {
			fatalf("resolve root: %v", err)
		}
	} else {
		absManifest, err = filepath.Abs(manifestValue)
		if err != nil {
			fatalf("resolve files manifest: %v", err)
		}
	}
	absOut, err := filepath.Abs(*outDir)
	if err != nil {
		fatalf("resolve output: %v", err)
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	resolvedStatePath := strings.TrimSpace(*statePath)
	if resolvedStatePath == "" {
		resolvedStatePath = filepath.Join(absOut, "bulk-state.json")
	} else {
		resolvedStatePath, err = filepath.Abs(resolvedStatePath)
		if err != nil {
			fatalf("resolve state path: %v", err)
		}
	}
	configDone(nil)
	stateDone := beginCLIPhase("state_load")
	store, err := newStateStore(resolvedStatePath, *resume)
	stateDone(err)
	if err != nil {
		fatalf("load state: %v", err)
	}
	fmt.Printf("STATE_FILE=%s\n", resolvedStatePath)
	fmt.Printf("RESUME=%t\n", *resume)

	resolutionCache, cacheErr := openResolutionCache(*resolutionCachePath)
	if cacheErr != nil {
		fatalf("open shared resolution cache: %v", cacheErr)
	}
	defer func() { _ = resolutionCache.Close() }()
	fmt.Printf("RESOLUTION_CACHE=%s\n", resolutionCache.Path())

	scanStarted := time.Now()
	scanDone := beginCLIPhase("media_scan")
	progress := func(found int) { fmt.Printf("MEDIA_SCAN_PROGRESS=%d\n", found) }
	var files []fileItem
	targetSummaryLabel := "対象フォルダ"
	targetSummary := ""
	if absManifest != "" {
		files, err = scanMediaManifest(absManifest, progress, !*skipDup)
		targetSummaryLabel = "対象ファイル"
		targetSummary = "個別選択"
		fmt.Println("INPUT_MODE=FILES")
	} else {
		scan := scanMediaWithProgress
		if *skipDup {
			scan = scanMediaForTitles
		}
		files, err = scan(absRoot, progress)
		targetSummary = absRoot
		fmt.Println("INPUT_MODE=FOLDER")
	}
	scanDone(err)
	if err != nil {
		fatalf("scan media: %v", err)
	}
	if len(files) == 0 {
		fatalf("no media files found in selected input")
	}
	if absManifest != "" {
		targetSummary = fmt.Sprintf("個別選択（%d本）", len(files))
	}
	fmt.Printf("FILES_TOTAL=%d\n", len(files))
	fmt.Printf("MEDIA_SCAN_SECONDS=%.2f\n", time.Since(scanStarted).Seconds())

	duplicates := []duplicateRecord{}
	unique := files
	if !*skipDup {
		dupStarted := time.Now()
		fmt.Println("DUPLICATE_SCAN=START")
		unique, duplicates, err = removeExactDuplicates(files, *quickBytes)
		if err != nil {
			fatalf("duplicate scan: %v", err)
		}
		fmt.Printf("DUPLICATES_CONFIRMED=%d\n", len(duplicates))
		fmt.Printf("FILES_TO_JUDGE=%d\n", len(unique))
		fmt.Printf("DUPLICATE_SCAN_SECONDS=%.2f\n", time.Since(dupStarted).Seconds())
		if err := writeDuplicates(absOut, duplicates); err != nil {
			fatalf("write duplicate report: %v", err)
		}
	}

	started := runStarted
	groupingDone := beginCLIPhase("title_grouping")
	tasks := buildTitleWork(unique)
	groupingDone(nil)
	fmt.Printf("TITLES_UNIQUE=%d\n", len(tasks))

	rows := make([]resultRecord, len(unique))
	jobs := make(chan int)
	results := make(chan titleWorkResult, *workers*2)
	var confirmed atomic.Int64
	var review atomic.Int64
	var unknown atomic.Int64
	var failed atomic.Int64
	var cachedFiles atomic.Int64

	pending := make([]int, 0, len(tasks))
	cachedTitles := 0
	for taskIndex, task := range tasks {
		cached, ok := store.cachedTitle(task.cacheKey())
		if !ok {
			if id, hit, cacheErr := resolutionCache.Get(task.Kind, task.Title); cacheErr != nil {
				fatalf("read shared resolution cache: %v", cacheErr)
			} else if hit {
				cached = titleCacheRecord{
					CatalogID: id,
					Status:    "confirmed",
					Method:    "安全確認済み結果の再利用",
					Reason:    "現行の安全判定方式で確定済みの結果を再利用しました。",
					UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
				}
				ok = true
				if err := store.recordTask(task, unique, "confirmed", id, cached.Method, cached.Reason, "", 0); err != nil {
					fatalf("record shared-cache hit in state: %v", err)
				}
			}
		}
		if !ok {
			pending = append(pending, taskIndex)
			continue
		}
		cachedTitles++
		groupSize := len(task.Indices)
		for _, fileIndex := range task.Indices {
			item := unique[fileIndex]
			rows[fileIndex] = resultRecord{
				Path:                  item.Path,
				Filename:              filepath.Base(item.Path),
				Title:                 task.Title,
				CatalogID:             cached.CatalogID,
				Status:                cached.Status,
				StatusJapanese:        decisionStatusJapanese(cached.Status),
				Method:                cached.Method,
				Reason:                cached.Reason,
				DecisionPolicyVersion: scrape.TitleDecisionPolicyVersion,
				AutoOrganizeEligible:  cached.Status == "confirmed",
				Error:                 cached.Error,
				Attempts:              cached.Attempts,
				Cached:                true,
			}
		}
		switch cached.Status {
		case "confirmed":
			confirmed.Add(int64(groupSize))
		case "review":
			review.Add(int64(groupSize))
		case "unknown":
			unknown.Add(int64(groupSize))
		default:
			failed.Add(int64(groupSize))
		}
		cachedFiles.Add(int64(groupSize))
	}
	fmt.Printf("TITLES_CACHED=%d\n", cachedTitles)
	fmt.Printf("FILES_CACHED=%d\n", cachedFiles.Load())

	cfg := &scrape.Config{
		JevCatalogEnabled:   true,
		JevCatalogThreshold: 0.80,
		JevCatalogModel:     "jev-latest",
	}

	var resolver *scrape.TitleCatalogResolver
	var titleDBCloser interface{ Close() error }
	dbDone := beginCLIPhase("local_db_open")
	if len(pending) == 0 {
		fmt.Println("TITLE_DB=SKIPPED_ALL_CACHED")
		dbDone(nil)
	} else if !*noLocalDB {
		titleDB, dbErr := prepareLocalTitleLookup(context.Background(), *r18Dump)
		dbDone(dbErr)
		if dbErr != nil {
			fmt.Printf("LOCAL_TITLE_SEARCH=UNAVAILABLE TITLE_DB=UNAVAILABLE error=%q; using web fallback\n", dbErr.Error())
			resolver = scrape.NewTitleCatalogResolver(cfg)
		} else {
			titleDBCloser = titleDB
			resolver = scrape.NewTitleCatalogResolverWithLookup(cfg, titleDB)
			fmt.Println("TITLE_RESOLUTION=LOCAL_R18_THEN_JEV_THEN_WEB")
		}
	} else {
		dbDone(nil)
		resolver = scrape.NewTitleCatalogResolver(cfg)
		fmt.Println("TITLE_RESOLUTION=WEB_ONLY")
	}
	if titleDBCloser != nil {
		defer func() { _ = titleDBCloser.Close() }()
	}

	workerCount := *workers
	if workerCount > len(pending) {
		workerCount = len(pending)
	}

	var wg sync.WaitGroup
	var firstSearch sync.Once
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for taskIndex := range jobs {
				task := tasks[taskIndex]
				var searchDone func(error)
				firstSearch.Do(func() { searchDone = beginCLIPhase("first_title_search") })
				mediaPath := ""
				if len(task.Indices) > 0 {
					mediaPath = unique[task.Indices[0]].Path
				}
				resolved := resolveWorkWithRetry(resolver, mediaPath, task.Title, *timeout, *maxAttempts, *retryBase, nil)
				if searchDone != nil {
					searchDone(resolved.Err)
				}
				result := titleWorkResult{
					TaskIndex: taskIndex,
					CatalogID: resolved.CatalogID,
					Status:    resolved.Status,
					Method:    resolved.Method,
					Reason:    resolved.Reason,
					ElapsedMS: resolved.ElapsedMS,
					Attempts:  resolved.Attempts,
				}
				if resolved.Err != nil {
					result.Error = resolved.Err.Error()
				}
				results <- result
			}
		}()
	}

	go func() {
		for _, taskIndex := range pending {
			jobs <- taskIndex
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	doneFiles := int(cachedFiles.Load())
	lastReported := doneFiles
	const checkpointEvery = 25
	pendingCheckpoint := 0
	fmt.Printf("PROGRESS=%d/%d CONFIRMED=%d REVIEW=%d UNKNOWN=%d ERRORS=%d CACHED=%d\n",
		doneFiles, len(unique), confirmed.Load(), review.Load(), unknown.Load(), failed.Load(), cachedFiles.Load())
	lastCheckpoint := time.Now()

	for res := range results {
		task := tasks[res.TaskIndex]
		fmt.Printf("DECISION_RESULT=%s candidate=%s elapsed_ms=%d\n", res.Status, res.CatalogID, res.ElapsedMS)
		groupSize := len(task.Indices)
		for _, fileIndex := range task.Indices {
			item := unique[fileIndex]
			row := resultRecord{
				Path:                  item.Path,
				Filename:              filepath.Base(item.Path),
				Title:                 task.Title,
				CatalogID:             res.CatalogID,
				Status:                res.Status,
				StatusJapanese:        decisionStatusJapanese(res.Status),
				Method:                res.Method,
				Reason:                res.Reason,
				DecisionPolicyVersion: scrape.TitleDecisionPolicyVersion,
				AutoOrganizeEligible:  res.Status == "confirmed",
				Error:                 res.Error,
				ElapsedMS:             res.ElapsedMS,
				Attempts:              res.Attempts,
			}
			rows[fileIndex] = row
		}

		switch res.Status {
		case "confirmed":
			confirmed.Add(int64(groupSize))
		case "review":
			review.Add(int64(groupSize))
		case "unknown":
			unknown.Add(int64(groupSize))
		default:
			failed.Add(int64(groupSize))
		}
		if err := store.recordTask(task, unique, res.Status, res.CatalogID, res.Method, res.Reason, res.Error, res.Attempts); err != nil {
			fatalf("record state: %v", err)
		}
		// 共有キャッシュへ保存するのは「確定」だけ。要確認・未特定は
		// 将来の判定改善や別ソース追加で再評価できるよう共有確定結果にしない。
		if res.Status == "confirmed" && strings.TrimSpace(res.CatalogID) != "" {
			if err := resolutionCache.Put(task.Kind, task.Title, res.CatalogID); err != nil {
				fatalf("write shared resolution cache: %v", err)
			}
		}
		pendingCheckpoint++
		if pendingCheckpoint >= checkpointEvery || time.Since(lastCheckpoint) >= 2*time.Second {
			checkpointDone := beginCLIPhase("state_checkpoint")
			checkpointErr := store.checkpoint()
			checkpointDone(checkpointErr)
			if checkpointErr != nil {
				fatalf("checkpoint state: %v", checkpointErr)
			}
			pendingCheckpoint = 0
			lastCheckpoint = time.Now()
		}

		doneFiles += groupSize
		if doneFiles-lastReported >= 25 || doneFiles == len(unique) {
			elapsed := time.Since(started).Seconds()
			rate := float64(doneFiles) / elapsed
			fmt.Printf("PROGRESS=%d/%d CONFIRMED=%d REVIEW=%d UNKNOWN=%d ERRORS=%d CACHED=%d RATE=%.2f_files_per_sec\n",
				doneFiles, len(unique), confirmed.Load(), review.Load(), unknown.Load(), failed.Load(), cachedFiles.Load(), rate)
			lastReported = doneFiles
		}
	}

	if err := store.checkpoint(); err != nil {
		fatalf("final checkpoint state: %v", err)
	}
	if err := writeResults(absOut, rows); err != nil {
		fatalf("write results: %v", err)
	}

	elapsed := time.Since(started)
	rate := float64(len(rows)) / elapsed.Seconds()
	p50MS, p95MS := elapsedPercentiles(rows)
	resolutionMetrics := scrape.TitleResolutionMetrics{}
	if resolver != nil {
		resolutionMetrics = resolver.Metrics()
	}
	summary := fmt.Sprintf(
		"処理結果=完了\n判定基準版=%d\n%s=%s\n状態ファイル=%s\n再開機能=%t\n総ファイル数=%d\n重複確認数=%d\n判定対象数=%d\n検索単位数=%d\n再利用した検索単位=%d\n今回判定した検索単位=%d\n確定=%d\n要確認=%d\n未特定=%d\nエラー=%d\n自動整理対象=%d\n再利用ファイル=%d\n並列数=%d\n実使用並列数=%d\n最大試行回数=%d\n再試行基本待機=%s\n保存間隔=%d\n処理時間秒=%.2f\n1秒あたり処理数=%.3f\nP50=%dms\nP95=%dms\nWeb検索数=%d\n429件数=%d\n",
		scrape.TitleDecisionPolicyVersion, targetSummaryLabel, targetSummary, resolvedStatePath, *resume, len(files), len(duplicates), len(rows), len(tasks), cachedTitles, len(pending),
		confirmed.Load(), review.Load(), unknown.Load(), failed.Load(), confirmed.Load(), cachedFiles.Load(),
		*workers, workerCount, *maxAttempts, retryBase.String(), checkpointEvery, elapsed.Seconds(), rate,
		p50MS, p95MS, resolutionMetrics.WebSearches, resolutionMetrics.HTTP429,
	)
	for _, name := range []string{"summary.txt", "処理概要.txt"} {
		if err := os.WriteFile(filepath.Join(absOut, name), []byte(summary), 0o644); err != nil {
			fatalf("write summary: %v", err)
		}
	}
	fmt.Printf("SUMMARY FILES_TOTAL=%d CONFIRMED=%d REVIEW=%d UNKNOWN=%d ERRORS=%d CACHED=%d ELAPSED_MS=%d P50_MS=%d P95_MS=%d WEB_SEARCHES=%d HTTP_429=%d\n",
		len(files), confirmed.Load(), review.Load(), unknown.Load(), failed.Load(), cachedFiles.Load(), elapsed.Milliseconds(),
		p50MS, p95MS, resolutionMetrics.WebSearches, resolutionMetrics.HTTP429)
	fmt.Print(summary)
}

func scanMediaManifest(manifestPath string, progress func(int), metadata bool) ([]fileItem, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read files manifest: %w", err)
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		return nil, fmt.Errorf("decode files manifest: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("files manifest is empty")
	}
	out := make([]fileItem, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, rawPath := range paths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			return nil, fmt.Errorf("files manifest contains an empty path")
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve selected media %q: %w", path, err)
		}
		if _, ok := mediaExt[strings.ToLower(filepath.Ext(abs))]; !ok {
			return nil, fmt.Errorf("unsupported selected media file: %s", abs)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("stat selected media %s: %w", abs, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("selected media is not a regular file: %s", abs)
		}
		seen[abs] = struct{}{}
		item := fileItem{Path: abs, Size: -1}
		if metadata {
			item.Size = info.Size()
			item.ModTimeNS = info.ModTime().UnixNano()
		}
		out = append(out, item)
		if progress != nil {
			progress(len(out))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func scanMedia(root string) ([]fileItem, error) { return scanMediaWithProgress(root, nil) }

func scanMediaWithProgress(root string, progress func(int)) ([]fileItem, error) {
	return scanMediaOptions(root, progress, true)
}

// Title caching/grouping only use the path/title. In skip-duplicate mode,
// avoid one filesystem stat per video (especially costly on mounted drives).
// Size=-1 and mtime=0 explicitly mark uncollected bookkeeping metadata.
func scanMediaForTitles(root string, progress func(int)) ([]fileItem, error) {
	return scanMediaOptions(root, progress, false)
}

func scanMediaOptions(root string, progress func(int), metadata bool) ([]fileItem, error) {
	var out []fileItem
	lastReport := time.Now()
	report := func() {
		if progress != nil {
			progress(len(out))
		}
		lastReport = time.Now()
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if progress != nil && time.Since(lastReport) >= 250*time.Millisecond {
			report()
		}
		if entry.IsDir() {
			return nil
		}
		if _, ok := mediaExt[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
			return nil
		}
		item := fileItem{Path: path, Size: -1}
		if metadata {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			item.Size = info.Size()
			item.ModTimeNS = info.ModTime().UnixNano()
		}
		out = append(out, item)
		if len(out) == 1 {
			report()
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if err == nil {
		report()
	}
	return out, err
}

func titleFromPath(path string) string {
	prepared := scrape.PrepareTitleResolutionInput(path)
	return prepared.Query
}

func buildTitleWork(files []fileItem) []titleWork {
	indexByTitle := make(map[string]int, len(files))
	tasks := make([]titleWork, 0, len(files))
	for i, file := range files {
		prepared := scrape.PrepareTitleResolutionInput(file.Path)
		title := prepared.Query
		cacheKey := string(prepared.Kind) + "\x00" + strings.ToLower(title)
		if taskIndex, ok := indexByTitle[cacheKey]; ok {
			tasks[taskIndex].Indices = append(tasks[taskIndex].Indices, i)
			continue
		}
		indexByTitle[cacheKey] = len(tasks)
		tasks = append(tasks, titleWork{
			Kind:    prepared.Kind,
			Title:   title,
			Indices: []int{i},
		})
	}
	return tasks
}

func removeExactDuplicates(files []fileItem, quickBytes int64) ([]fileItem, []duplicateRecord, error) {
	bySize := make(map[int64][]fileItem)
	for _, f := range files {
		bySize[f.Size] = append(bySize[f.Size], f)
	}

	duplicateSet := make(map[string]struct{})
	var duplicates []duplicateRecord

	for _, sizeGroup := range bySize {
		if len(sizeGroup) < 2 {
			continue
		}
		byQuick := make(map[string][]fileItem)
		for _, f := range sizeGroup {
			h, err := quickHash(f.Path, f.Size, quickBytes)
			if err != nil {
				return nil, nil, err
			}
			byQuick[h] = append(byQuick[h], f)
		}

		for _, quickGroup := range byQuick {
			if len(quickGroup) < 2 {
				continue
			}
			byFull := make(map[string][]fileItem)
			for _, f := range quickGroup {
				h, err := fullHash(f.Path)
				if err != nil {
					return nil, nil, err
				}
				byFull[h] = append(byFull[h], f)
			}
			for hash, fullGroup := range byFull {
				if len(fullGroup) < 2 {
					continue
				}
				sort.Slice(fullGroup, func(i, j int) bool {
					iName := strings.TrimSuffix(filepath.Base(fullGroup[i].Path), filepath.Ext(fullGroup[i].Path))
					jName := strings.TrimSuffix(filepath.Base(fullGroup[j].Path), filepath.Ext(fullGroup[j].Path))
					iRunes := len([]rune(iName))
					jRunes := len([]rune(jName))
					if iRunes != jRunes {
						return iRunes > jRunes
					}
					return fullGroup[i].Path < fullGroup[j].Path
				})
				canonical := fullGroup[0]
				for _, dup := range fullGroup[1:] {
					duplicateSet[dup.Path] = struct{}{}
					duplicates = append(duplicates, duplicateRecord{
						DuplicatePath: dup.Path,
						CanonicalPath: canonical.Path,
						Size:          dup.Size,
						SHA256:        hash,
					})
				}
			}
		}
	}

	unique := make([]fileItem, 0, len(files)-len(duplicates))
	for _, f := range files {
		if _, dup := duplicateSet[f.Path]; !dup {
			unique = append(unique, f)
		}
	}
	sort.Slice(duplicates, func(i, j int) bool { return duplicates[i].DuplicatePath < duplicates[j].DuplicatePath })
	return unique, duplicates, nil
}

func quickHash(path string, size, sample int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := fmt.Fprintf(h, "%d:", size); err != nil {
		return "", err
	}

	headN := min64(sample, size)
	if _, err := io.CopyN(h, f, headN); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if size > headN {
		tailN := min64(sample, size-headN)
		if _, err := f.Seek(size-tailN, io.SeekStart); err != nil {
			return "", err
		}
		if _, err := io.CopyN(h, f, tailN); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fullHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeDuplicates(outDir string, rows []duplicateRecord) error {
	j, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "duplicates.json"), j, 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(outDir, "duplicates.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"duplicate_path", "canonical_path", "bytes", "sha256"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{r.DuplicatePath, r.CanonicalPath, fmt.Sprintf("%d", r.Size), r.SHA256}); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeResults(outDir string, rows []resultRecord) error {
	j, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "bulk-results.json"), j, 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(outDir, "bulk-results.csv"))
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"path", "filename", "title", "catalog_id", "status", "status_ja", "method", "reason", "decision_policy_version", "auto_organize_eligible", "error", "elapsed_ms", "attempts", "cached"}); err != nil {
		_ = f.Close()
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.Path, r.Filename, r.Title, r.CatalogID, r.Status, r.StatusJapanese,
			r.Method, r.Reason, fmt.Sprintf("%d", r.DecisionPolicyVersion), fmt.Sprintf("%t", r.AutoOrganizeEligible), r.Error,
			fmt.Sprintf("%d", r.ElapsedMS), fmt.Sprintf("%d", r.Attempts), fmt.Sprintf("%t", r.Cached),
		}); err != nil {
			_ = f.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return writeJapaneseDecisionReports(outDir, rows)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
