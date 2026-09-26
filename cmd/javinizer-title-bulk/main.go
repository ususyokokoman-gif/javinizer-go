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
	Path string
	Size int64
}

type duplicateRecord struct {
	DuplicatePath string `json:"duplicate_path"`
	CanonicalPath string `json:"canonical_path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
}

type resultRecord struct {
	Path       string  `json:"path"`
	Filename   string  `json:"filename"`
	Title      string  `json:"title"`
	CatalogID  string  `json:"catalog_id,omitempty"`
	Status     string  `json:"status"`
	Error      string  `json:"error,omitempty"`
	ElapsedMS  int64   `json:"elapsed_ms"`
}

type workResult struct {
	Index int
	Row   resultRecord
}

func main() {
	var (
		root       = flag.String("root", "", "Root directory containing media files")
		outDir     = flag.String("out", "bulk-title-jev-output", "Output directory")
		workers    = flag.Int("workers", 16, "Concurrent title->Web->Jev workers")
		timeout    = flag.Duration("timeout", 45*time.Second, "Per-title timeout")
		quickBytes = flag.Int64("quick-hash-bytes", 1<<20, "Bytes sampled from head and tail for duplicate prefilter")
		skipDup    = flag.Bool("skip-duplicates", false, "Skip duplicate detection")
	)
	flag.Parse()

	if strings.TrimSpace(*root) == "" {
		fatalf("-root is required")
	}
	if *workers < 1 {
		fatalf("-workers must be >= 1")
	}
	if *quickBytes < 64<<10 {
		fatalf("-quick-hash-bytes must be >= 65536")
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

	files, err := scanMedia(absRoot)
	if err != nil {
		fatalf("scan media: %v", err)
	}
	if len(files) == 0 {
		fatalf("no media files found under %s", absRoot)
	}
	fmt.Printf("FILES_TOTAL=%d\n", len(files))

	duplicates := []duplicateRecord{}
	unique := files
	if !*skipDup {
		fmt.Println("DUPLICATE_SCAN=START")
		unique, duplicates, err = removeExactDuplicates(files, *quickBytes)
		if err != nil {
			fatalf("duplicate scan: %v", err)
		}
		fmt.Printf("DUPLICATES_CONFIRMED=%d\n", len(duplicates))
		fmt.Printf("FILES_TO_JUDGE=%d\n", len(unique))
		if err := writeDuplicates(absOut, duplicates); err != nil {
			fatalf("write duplicate report: %v", err)
		}
	}

	cfg := &scrape.Config{
		JevCatalogEnabled:   true,
		JevCatalogThreshold: 0.80,
		JevCatalogModel:     "jev-latest",
	}
	resolver := scrape.NewTitleCatalogResolver(cfg)

	started := time.Now()
	rows := make([]resultRecord, len(unique))
	jobs := make(chan int)
	results := make(chan workResult, *workers*2)
	var accepted atomic.Int64
	var failed atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				item := unique[idx]
				title := titleFromPath(item.Path)
				began := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), *timeout)
				id, err := resolver.Resolve(ctx, title)
				cancel()

				row := resultRecord{
					Path:      item.Path,
					Filename:  filepath.Base(item.Path),
					Title:     title,
					ElapsedMS: time.Since(began).Milliseconds(),
				}
				if err != nil {
					row.Status = "error"
					row.Error = err.Error()
					failed.Add(1)
				} else {
					row.Status = "accepted"
					row.CatalogID = id
					accepted.Add(1)
				}
				results <- workResult{Index: idx, Row: row}
			}
		}()
	}

	go func() {
		for i := range unique {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	done := 0
	for res := range results {
		rows[res.Index] = res.Row
		done++
		if done%25 == 0 || done == len(unique) {
			elapsed := time.Since(started).Seconds()
			rate := float64(done) / elapsed
			fmt.Printf("PROGRESS=%d/%d ACCEPTED=%d ERRORS=%d RATE=%.2f_files_per_sec\n",
				done, len(unique), accepted.Load(), failed.Load(), rate)
		}
	}

	if err := writeResults(absOut, rows); err != nil {
		fatalf("write results: %v", err)
	}

	elapsed := time.Since(started)
	rate := float64(len(rows)) / elapsed.Seconds()
	summary := fmt.Sprintf(
		"status=PASS\ninput_root=%s\nfiles_total=%d\nduplicates_confirmed=%d\nfiles_judged=%d\naccepted=%d\nerrors=%d\nworkers=%d\nelapsed_seconds=%.2f\nfiles_per_second=%.3f\n",
		absRoot, len(files), len(duplicates), len(rows), accepted.Load(), failed.Load(), *workers, elapsed.Seconds(), rate,
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
		out = append(out, fileItem{Path: path, Size: info.Size()})
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
				sort.Slice(fullGroup, func(i, j int) bool { return fullGroup[i].Path < fullGroup[j].Path })
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
	if err := w.Write([]string{"path", "filename", "title", "catalog_id", "status", "error", "elapsed_ms"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{r.Path, r.Filename, r.Title, r.CatalogID, r.Status, r.Error, fmt.Sprintf("%d", r.ElapsedMS)}); err != nil {
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
