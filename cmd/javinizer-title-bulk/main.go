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
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	Title     string `json:"title"`
	CatalogID string `json:"catalog_id,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Attempts  int    `json:"attempts,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
}

type titleWork struct {
	Title   string
	Indices []int
}

type titleWorkResult struct {
	TaskIndex int
	CatalogID string
	Status    string
	Error     string
	ElapsedMS int64
	Attempts  int
}

func main() {
	var (
		root       = flag.String("root", "", "Root directory containing media files")
		outDir     = flag.String("out", "bulk-title-jev-output", "Output directory")
		workers    = flag.Int("workers", 4, "Concurrent unique-title -> Web -> Jev workers")
		timeout    = flag.Duration("timeout", 45*time.Second, "Per-title timeout")
		quickBytes  = flag.Int64("quick-hash-bytes", 64<<10, "Bytes sampled from head and tail for duplicate prefilter")
		skipDup     = flag.Bool("skip-duplicates", false, "Skip duplicate detection")
		resume      = flag.Bool("resume", true, "Resume from durable title state and reuse terminal cached results")
		statePath   = flag.String("state", "", "State file path (default: <out>/bulk-state.json)")
		maxAttempts = flag.Int("max-attempts", 3, "Maximum attempts for transient title-resolution failures")
		retryBase   = flag.Duration("retry-base-delay", 2*time.Second, "Base delay for transient retry backoff")
		r18Dump     = flag.String("r18-dump", "", "Local r18.dev dump path (default: JAVINIZER user config directory)")
		noLocalDB   = flag.Bool("no-local-title-db", false, "Disable local r18.dev title lookup and use web fallback only")
		prepareDB   = flag.Bool("prepare-title-db", false, "Prepare/update the local r18.dev title database and exit")
	)
	flag.Parse()

	if *prepareDB {
		db, err := prepareLocalTitleLookup(context.Background(), *r18Dump)
		if err != nil {
			fatalf("prepare local title database: %v", err)
		}
		if db != nil {
			_ = db.Close()
		}
		fmt.Println("TITLE_DB_PREPARE=PASS")
		return
	}

	if strings.TrimSpace(*root) == "" {
		// Double-click / no-argument startup is the normal end-user path.
		// CLI behavior remains available when -root is supplied.
		if len(os.Args) == 1 {
			if err := runGUI(); err != nil {
				fatalf("start GUI: %v", err)
			}
			return
		}
		fatalf("-root is required")
	}
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

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fatalf("resolve root: %v", err)
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
	store, err := newStateStore(resolvedStatePath, *resume)
	if err != nil {
		fatalf("load state: %v", err)
	}
	fmt.Printf("STATE_FILE=%s\n", resolvedStatePath)
	fmt.Printf("RESUME=%t\n", *resume)

	scanStarted := time.Now()
	files, err := scanMedia(absRoot)
	if err != nil {
		fatalf("scan media: %v", err)
	}
	if len(files) == 0 {
		fatalf("no media files found under %s", absRoot)
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

	cfg := &scrape.Config{
		JevCatalogEnabled:   true,
		JevCatalogThreshold: 0.80,
		JevCatalogModel:     "jev-latest",
	}

	var resolver *scrape.TitleCatalogResolver
	var titleDBCloser interface{ Close() error }
	if !*noLocalDB {
		titleDB, dbErr := prepareLocalTitleLookup(context.Background(), *r18Dump)
		if dbErr != nil {
			fmt.Printf("TITLE_DB=UNAVAILABLE error=%q; using web fallback\n", dbErr.Error())
			resolver = scrape.NewTitleCatalogResolver(cfg)
		} else {
			titleDBCloser = titleDB
			resolver = scrape.NewTitleCatalogResolverWithLookup(cfg, titleDB)
			fmt.Println("TITLE_RESOLUTION=LOCAL_R18_THEN_JEV_THEN_WEB")
		}
	} else {
		resolver = scrape.NewTitleCatalogResolver(cfg)
		fmt.Println("TITLE_RESOLUTION=WEB_ONLY")
	}
	if titleDBCloser != nil {
		defer func() { _ = titleDBCloser.Close() }()
	}

	started := time.Now()
	tasks := buildTitleWork(unique)
	fmt.Printf("TITLES_UNIQUE=%d\n", len(tasks))

	rows := make([]resultRecord, len(unique))
	jobs := make(chan int)
	results := make(chan titleWorkResult, *workers*2)
	var accepted atomic.Int64
	var rejected atomic.Int64
	var failed atomic.Int64
	var cachedFiles atomic.Int64

	pending := make([]int, 0, len(tasks))
	cachedTitles := 0
	for taskIndex, task := range tasks {
		cached, ok := store.cachedTitle(task.Title)
		if !ok {
			pending = append(pending, taskIndex)
			continue
		}
		cachedTitles++
		groupSize := len(task.Indices)
		for _, fileIndex := range task.Indices {
			item := unique[fileIndex]
			rows[fileIndex] = resultRecord{
				Path:      item.Path,
				Filename:  filepath.Base(item.Path),
				Title:     task.Title,
				CatalogID: cached.CatalogID,
				Status:    cached.Status,
				Error:     cached.Error,
				Attempts:  cached.Attempts,
				Cached:    true,
			}
		}
		switch cached.Status {
		case "accepted":
			accepted.Add(int64(groupSize))
		case "rejected":
			rejected.Add(int64(groupSize))
		}
		cachedFiles.Add(int64(groupSize))
	}
	fmt.Printf("TITLES_CACHED=%d\n", cachedTitles)
	fmt.Printf("FILES_CACHED=%d\n", cachedFiles.Load())

	workerCount := *workers
	if workerCount > len(pending) {
		workerCount = len(pending)
	}

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for taskIndex := range jobs {
				task := tasks[taskIndex]
				resolved := resolveTitleWithRetry(resolver, task.Title, *timeout, *maxAttempts, *retryBase, nil)
				result := titleWorkResult{
					TaskIndex: taskIndex,
					CatalogID: resolved.CatalogID,
					Status:    resolutionStatus(resolved.Err),
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
	lastCheckpoint := time.Now()
	if doneFiles > 0 {
		fmt.Printf("PROGRESS=%d/%d ACCEPTED=%d REJECTED=%d ERRORS=%d CACHED=%d\n",
			doneFiles, len(unique), accepted.Load(), rejected.Load(), failed.Load(), cachedFiles.Load())
	}
	for res := range results {
		task := tasks[res.TaskIndex]
		groupSize := len(task.Indices)
		for _, fileIndex := range task.Indices {
			item := unique[fileIndex]
			row := resultRecord{
				Path:      item.Path,
				Filename:  filepath.Base(item.Path),
				Title:     task.Title,
				CatalogID: res.CatalogID,
				Status:    res.Status,
				Error:     res.Error,
				ElapsedMS: res.ElapsedMS,
				Attempts:  res.Attempts,
			}
			rows[fileIndex] = row
		}

		switch res.Status {
		case "accepted":
			accepted.Add(int64(groupSize))
		case "rejected":
			rejected.Add(int64(groupSize))
		default:
			failed.Add(int64(groupSize))
		}
		if err := store.recordTask(task, unique, res.Status, res.CatalogID, res.Error, res.Attempts); err != nil {
			fatalf("record state: %v", err)
		}
		pendingCheckpoint++
		if pendingCheckpoint >= checkpointEvery || time.Since(lastCheckpoint) >= 2*time.Second {
			if err := store.checkpoint(); err != nil {
				fatalf("checkpoint state: %v", err)
			}
			pendingCheckpoint = 0
			lastCheckpoint = time.Now()
		}

		doneFiles += groupSize
		if doneFiles-lastReported >= 25 || doneFiles == len(unique) {
			elapsed := time.Since(started).Seconds()
			rate := float64(doneFiles) / elapsed
			fmt.Printf("PROGRESS=%d/%d ACCEPTED=%d REJECTED=%d ERRORS=%d CACHED=%d RATE=%.2f_files_per_sec\n",
				doneFiles, len(unique), accepted.Load(), rejected.Load(), failed.Load(), cachedFiles.Load(), rate)
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
	summary := fmt.Sprintf(
		"status=PASS\ninput_root=%s\nstate_file=%s\nresume=%t\nfiles_total=%d\nduplicates_confirmed=%d\nfiles_judged=%d\ntitles_unique=%d\ntitles_cached=%d\ntitles_resolved_live=%d\naccepted=%d\nrejected=%d\nerrors=%d\ncached_files=%d\nworkers=%d\nworkers_used=%d\nmax_attempts=%d\nretry_base_delay=%s\nstate_checkpoint_every=%d\nelapsed_seconds=%.2f\nfiles_per_second=%.3f\n",
		absRoot, resolvedStatePath, *resume, len(files), len(duplicates), len(rows), len(tasks), cachedTitles, len(pending), accepted.Load(), rejected.Load(), failed.Load(), cachedFiles.Load(), *workers, workerCount, *maxAttempts, retryBase.String(), checkpointEvery, elapsed.Seconds(), rate,
	)
	if err := os.WriteFile(filepath.Join(absOut, "summary.txt"), []byte(summary), 0o644); err != nil {
		fatalf("write summary: %v", err)
	}
	fmt.Print(summary)
}

func scanMedia(root string) ([]fileItem, error) {
	var out []fileItem
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if _, ok := mediaExt[strings.ToLower(filepath.Ext(info.Name()))]; !ok {
			return nil
		}
		out = append(out, fileItem{Path: path, Size: info.Size(), ModTimeNS: info.ModTime().UnixNano()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSpace(strings.TrimSuffix(base, ext))
}

func buildTitleWork(files []fileItem) []titleWork {
	indexByTitle := make(map[string]int, len(files))
	tasks := make([]titleWork, 0, len(files))
	for i, file := range files {
		title := titleFromPath(file.Path)
		if taskIndex, ok := indexByTitle[title]; ok {
			tasks[taskIndex].Indices = append(tasks[taskIndex].Indices, i)
			continue
		}
		indexByTitle[title] = len(tasks)
		tasks = append(tasks, titleWork{
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
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"path", "filename", "title", "catalog_id", "status", "error", "elapsed_ms", "attempts", "cached"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{r.Path, r.Filename, r.Title, r.CatalogID, r.Status, r.Error, fmt.Sprintf("%d", r.ElapsedMS), fmt.Sprintf("%d", r.Attempts), fmt.Sprintf("%t", r.Cached)}); err != nil {
			return err
		}
	}
	return w.Error()
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
