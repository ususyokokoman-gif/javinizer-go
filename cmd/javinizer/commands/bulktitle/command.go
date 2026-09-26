package bulktitle

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	internalscrape "github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/spf13/cobra"
)

var defaultExtensions = []string{".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".ts", ".webm"}

type fileResult struct {
	Path        string `json:"path"`
	Filename    string `json:"filename"`
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256,omitempty"`
	DuplicateOf string `json:"duplicate_of,omitempty"`
	Status      string `json:"status"`
	CatalogID   string `json:"catalog_id,omitempty"`
	Error       string `json:"error,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
}

type duplicateGroup struct {
	SHA256 string   `json:"sha256"`
	Size   int64    `json:"size"`
	Keeper string   `json:"keeper"`
	Files  []string `json:"files"`
}

type report struct {
	Root            string           `json:"root"`
	StartedAt       string           `json:"started_at"`
	CompletedAt     string           `json:"completed_at"`
	ElapsedMS       int64            `json:"elapsed_ms"`
	Workers         int              `json:"workers"`
	HashWorkers     int              `json:"hash_workers"`
	JevThreshold    float64          `json:"jev_threshold"`
	Scanned         int              `json:"scanned"`
	Unique          int              `json:"unique"`
	DuplicateFiles  int              `json:"duplicate_files"`
	DuplicateGroups int              `json:"duplicate_groups"`
	Accepted        int              `json:"accepted"`
	Errors          int              `json:"errors"`
	Duplicates      []duplicateGroup `json:"duplicates"`
	Files           []fileResult     `json:"files"`
}

func NewCommand() *cobra.Command {
	var (
		workers       int
		hashWorkers   int
		maxFiles      int
		timeout       time.Duration
		threshold     float64
		outputJSON    string
		outputCSV     string
		duplicatesCSV string
		scanOnly      bool
		extensions    []string
	)

	cmd := &cobra.Command{
		Use:   "bulk-title <root>",
		Short: "Resolve thousands of filenames to catalog IDs with Jev and detect exact duplicates",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			return run(cmd, args[0], configPath, workers, hashWorkers, maxFiles, timeout, threshold, extensions, scanOnly, outputJSON, outputCSV, duplicatesCSV)
		},
	}

	cmd.Flags().IntVarP(&workers, "workers", "w", 20, "Concurrent title/Web/Jev workers")
	cmd.Flags().IntVar(&hashWorkers, "hash-workers", 4, "Concurrent duplicate-hash workers")
	cmd.Flags().IntVar(&maxFiles, "max-files", 0, "Limit files for a trial run (0 = all)")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Timeout per title")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.80, "Jev acceptance threshold")
	cmd.Flags().StringSliceVar(&extensions, "extensions", defaultExtensions, "Media file extensions to scan")
	cmd.Flags().BoolVar(&scanOnly, "scan-only", false, "Only scan and detect duplicates; skip Web/Jev")
	cmd.Flags().StringVar(&outputJSON, "output", "bulk-title-results.json", "JSON report path")
	cmd.Flags().StringVar(&outputCSV, "csv", "bulk-title-results.csv", "CSV result path")
	cmd.Flags().StringVar(&duplicatesCSV, "duplicates", "bulk-title-duplicates.csv", "Exact duplicate CSV path")
	return cmd
}

func run(cmd *cobra.Command, root, configPath string, workers, hashWorkers, maxFiles int, timeout time.Duration, threshold float64, extensions []string, scanOnly bool, outputJSON, outputCSV, duplicatesCSV string) error {
	if workers < 1 {
		return fmt.Errorf("workers must be >= 1")
	}
	if hashWorkers < 1 {
		return fmt.Errorf("hash-workers must be >= 1")
	}
	if threshold <= 0 || threshold > 1 {
		return fmt.Errorf("threshold must be > 0 and <= 1")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("root is not a directory: %s", absRoot)
	}

	started := time.Now()
	files, err := scanFiles(absRoot, extensions, maxFiles)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no media files found under %s", absRoot)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Scanned %d media files\n", len(files))

	duplicates, err := markExactDuplicates(files, hashWorkers)
	if err != nil {
		return fmt.Errorf("duplicate scan failed: %w", err)
	}
	dupCount := 0
	for i := range files {
		if files[i].DuplicateOf != "" {
			dupCount++
			files[i].Status = "duplicate"
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Exact duplicates: %d files in %d groups\n", dupCount, len(duplicates))

	var resolver *internalscrape.TitleCatalogResolver
	if !scanOnly {
		cfg, err := config.LoadOrCreate(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		config.ApplyEnvironmentOverrides(cfg)
		if _, err := config.Prepare(cfg); err != nil {
			return fmt.Errorf("prepare config: %w", err)
		}
		scrapeCfg := internalscrape.ConfigFromAppConfig(cfg)
		if scrapeCfg == nil {
			return fmt.Errorf("failed to build scrape config")
		}
		if strings.TrimSpace(scrapeCfg.JevCatalogAPIKey) == "" {
			return fmt.Errorf("TYPESAFE_API_KEY is required for bulk-title Jev validation")
		}
		scrapeCfg.JevCatalogEnabled = true
		scrapeCfg.JevCatalogThreshold = threshold
		resolver = internalscrape.NewTitleCatalogResolver(scrapeCfg)

		if err := resolveAll(cmd.Context(), cmd, resolver, files, workers, timeout); err != nil {
			return err
		}
	} else {
		for i := range files {
			if files[i].DuplicateOf == "" {
				files[i].Status = "unique"
			}
		}
	}

	rep := buildReport(absRoot, started, workers, hashWorkers, threshold, files, duplicates)
	if err := writeJSON(outputJSON, rep); err != nil {
		return err
	}
	if err := writeResultsCSV(outputCSV, files); err != nil {
		return err
	}
	if err := writeDuplicatesCSV(duplicatesCSV, duplicates); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"scanned=%d unique=%d duplicates=%d groups=%d accepted=%d errors=%d elapsed=%s\njson=%s\ncsv=%s\nduplicates=%s\n",
		rep.Scanned, rep.Unique, rep.DuplicateFiles, rep.DuplicateGroups, rep.Accepted, rep.Errors,
		time.Duration(rep.ElapsedMS)*time.Millisecond, outputJSON, outputCSV, duplicatesCSV)
	return nil
}

func scanFiles(root string, extensions []string, maxFiles int) ([]fileResult, error) {
	allowed := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		allowed[ext] = struct{}{}
	}

	out := make([]fileResult, 0, 1024)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := allowed[strings.ToLower(filepath.Ext(d.Name()))]; !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fileResult{
			Path:     path,
			Filename: d.Name(),
			Title:    titleFromPath(path),
			Size:     info.Size(),
			Status:   "pending",
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	if maxFiles > 0 && len(out) > maxFiles {
		out = out[:maxFiles]
	}
	return out, nil
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	title := strings.TrimSpace(strings.TrimSuffix(base, ext))
	title = strings.Join(strings.Fields(title), " ")
	return title
}

func markExactDuplicates(files []fileResult, workers int) ([]duplicateGroup, error) {
	bySize := make(map[int64][]int)
	for i := range files {
		bySize[files[i].Size] = append(bySize[files[i].Size], i)
	}

	var quickCandidates []int
	for _, idxs := range bySize {
		if len(idxs) > 1 {
			quickCandidates = append(quickCandidates, idxs...)
		}
	}
	if len(quickCandidates) == 0 {
		return nil, nil
	}

	quick, err := hashMany(files, quickCandidates, workers, true)
	if err != nil {
		return nil, err
	}
	byQuick := make(map[string][]int)
	for _, idx := range quickCandidates {
		key := strconv.FormatInt(files[idx].Size, 10) + ":" + quick[idx]
		byQuick[key] = append(byQuick[key], idx)
	}

	var fullCandidates []int
	for _, idxs := range byQuick {
		if len(idxs) > 1 {
			fullCandidates = append(fullCandidates, idxs...)
		}
	}
	if len(fullCandidates) == 0 {
		return nil, nil
	}

	full, err := hashMany(files, fullCandidates, workers, false)
	if err != nil {
		return nil, err
	}
	byFull := make(map[string][]int)
	for _, idx := range fullCandidates {
		files[idx].SHA256 = full[idx]
		key := strconv.FormatInt(files[idx].Size, 10) + ":" + full[idx]
		byFull[key] = append(byFull[key], idx)
	}

	groups := make([]duplicateGroup, 0)
	for _, idxs := range byFull {
		if len(idxs) < 2 {
			continue
		}
		sort.Slice(idxs, func(i, j int) bool {
			return strings.ToLower(files[idxs[i]].Path) < strings.ToLower(files[idxs[j]].Path)
		})
		keeper := idxs[0]
		group := duplicateGroup{
			SHA256: files[keeper].SHA256,
			Size:   files[keeper].Size,
			Keeper: files[keeper].Path,
			Files:  make([]string, 0, len(idxs)),
		}
		for pos, idx := range idxs {
			group.Files = append(group.Files, files[idx].Path)
			if pos > 0 {
				files[idx].DuplicateOf = files[keeper].Path
			}
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Keeper) < strings.ToLower(groups[j].Keeper)
	})
	return groups, nil
}

func hashMany(files []fileResult, indexes []int, workers int, quick bool) (map[int]string, error) {
	type item struct {
		idx  int
		hash string
		err  error
	}
	jobs := make(chan int)
	results := make(chan item, len(indexes))
	var wg sync.WaitGroup
	if workers > len(indexes) {
		workers = len(indexes)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				var h string
				var err error
				if quick {
					h, err = quickFingerprint(files[idx].Path, files[idx].Size)
				} else {
					h, err = fullSHA256(files[idx].Path)
				}
				results <- item{idx: idx, hash: h, err: err}
			}
		}()
	}
	go func() {
		for _, idx := range indexes {
			jobs <- idx
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	out := make(map[int]string, len(indexes))
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		out[r.idx] = r.hash
	}
	return out, nil
}

func quickFingerprint(path string, size int64) (string, error) {
	const sampleSize int64 = 1 << 20
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	var sizeBuf [8]byte
	binary.LittleEndian.PutUint64(sizeBuf[:], uint64(size))
	_, _ = h.Write(sizeBuf[:])

	readChunk := func(offset, length int64) error {
		if length <= 0 {
			return nil
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		buf := make([]byte, length)
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return err
		}
		_, _ = h.Write(buf[:n])
		return nil
	}

	first := size
	if first > sampleSize {
		first = sampleSize
	}
	if err := readChunk(0, first); err != nil {
		return "", err
	}
	if size > sampleSize {
		last := sampleSize
		if size < 2*sampleSize {
			last = size - sampleSize
		}
		if err := readChunk(size-last, last); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fullSHA256(path string) (string, error) {
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

func resolveAll(parent context.Context, cmd *cobra.Command, resolver *internalscrape.TitleCatalogResolver, files []fileResult, workers int, timeout time.Duration) error {
	jobs := make(chan int)
	var wg sync.WaitGroup
	var completed atomic.Int64
	var accepted atomic.Int64
	var failed atomic.Int64

	if workers > len(files) {
		workers = len(files)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if files[idx].DuplicateOf != "" {
					continue
				}
				start := time.Now()
				ctx, cancel := context.WithTimeout(parent, timeout)
				id, err := resolver.Resolve(ctx, files[idx].Title)
				cancel()
				files[idx].DurationMS = time.Since(start).Milliseconds()
				if err != nil {
					files[idx].Status = "error"
					files[idx].Error = err.Error()
					failed.Add(1)
				} else {
					files[idx].Status = "accepted"
					files[idx].CatalogID = id
					accepted.Add(1)
				}
				n := completed.Add(1)
				if n%25 == 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "Resolved %d files (accepted=%d errors=%d)\n", n, accepted.Load(), failed.Load())
				}
			}
		}()
	}

	for idx := range files {
		if files[idx].DuplicateOf == "" {
			select {
			case jobs <- idx:
			case <-parent.Done():
				close(jobs)
				wg.Wait()
				return parent.Err()
			}
		}
	}
	close(jobs)
	wg.Wait()
	return nil
}

func buildReport(root string, started time.Time, workers, hashWorkers int, threshold float64, files []fileResult, duplicates []duplicateGroup) report {
	rep := report{
		Root:            root,
		StartedAt:       started.UTC().Format(time.RFC3339),
		CompletedAt:     time.Now().UTC().Format(time.RFC3339),
		ElapsedMS:       time.Since(started).Milliseconds(),
		Workers:         workers,
		HashWorkers:     hashWorkers,
		JevThreshold:    threshold,
		Scanned:         len(files),
		DuplicateGroups: len(duplicates),
		Duplicates:      duplicates,
		Files:           files,
	}
	for i := range files {
		if files[i].DuplicateOf != "" {
			rep.DuplicateFiles++
			continue
		}
		rep.Unique++
		if files[i].Status == "accepted" {
			rep.Accepted++
		}
		if files[i].Status == "error" {
			rep.Errors++
		}
	}
	return rep
}

func writeJSON(path string, rep report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func writeResultsCSV(path string, files []fileResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"path", "filename", "title", "size", "sha256", "duplicate_of", "status", "catalog_id", "error", "duration_ms"})
	for _, r := range files {
		_ = w.Write([]string{r.Path, r.Filename, r.Title, strconv.FormatInt(r.Size, 10), r.SHA256, r.DuplicateOf, r.Status, r.CatalogID, r.Error, strconv.FormatInt(r.DurationMS, 10)})
	}
	w.Flush()
	return w.Error()
}

func writeDuplicatesCSV(path string, groups []duplicateGroup) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"sha256", "size", "keeper", "duplicate"})
	for _, group := range groups {
		for _, path := range group.Files {
			if path == group.Keeper {
				continue
			}
			_ = w.Write([]string{group.SHA256, strconv.FormatInt(group.Size, 10), group.Keeper, path})
		}
	}
	w.Flush()
	return w.Error()
}
