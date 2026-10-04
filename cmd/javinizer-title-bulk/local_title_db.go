package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

func defaultR18DumpPath() string {
	if env := strings.TrimSpace(os.Getenv("JAVINIZER_R18DEV_DUMP_PATH")); env != "" {
		return env
	}
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = filepath.Dir(os.Args[0])
	}
	return filepath.Join(base, "JAVINIZER", "r18dev", "r18dev_dump.db")
}

// prepareLocalTitleLookup makes the local title fast path self-contained for
// the GUI. Existing databases are upgraded with the FTS5 title index once.
// Missing dumps are unavailable during normal processing. Download/import is
// opt-in via -prepare-title-db so first-run GUI processing can fall back promptly.
func prepareLocalTitleLookup(ctx context.Context, path string) (*r18devdump.Store, error) {
	return prepareLocalTitleLookupWithDownload(ctx, path, false)
}

func prepareLocalTitleLookupWithDownload(ctx context.Context, path string, download bool) (*r18devdump.Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultR18DumpPath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve local title database path: %w", err)
	}
	path = abs

	if _, err := os.Stat(path); err == nil {
		fmt.Printf("TITLE_DB=FOUND path=%s\n", path)
		started := time.Now()
		if err := r18devdump.EnsureTitleSearchIndex(ctx, path); err != nil {
			return nil, fmt.Errorf("prepare title index: %w", err)
		}
		fmt.Printf("TITLE_DB_INDEX_READY seconds=%.2f\n", time.Since(started).Seconds())
		store, err := r18devdump.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open local title database: %w", err)
		}
		return store, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat local title database: %w", err)
	}

	if !download {
		return nil, fmt.Errorf("%w: dump not found at %s; run -prepare-title-db to download/import it", models.ErrDumpTitleSearchUnavailable, path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create local title database directory: %w", err)
	}
	fmt.Printf("TITLE_DB=FIRST_RUN_DOWNLOAD path=%s\n", path)

	client := &http.Client{Timeout: 30 * time.Minute}
	var lastPrinted int64
	var lastPrint time.Time
	progress := func(done, total int64) {
		now := time.Now()
		if done-lastPrinted < 32<<20 && !lastPrint.IsZero() && now.Sub(lastPrint) < 3*time.Second {
			return
		}
		lastPrinted = done
		lastPrint = now
		if total > 0 {
			fmt.Printf("TITLE_DB_DOWNLOAD=%d/%d %.1f%%\n", done, total, 100*float64(done)/float64(total))
		} else {
			fmt.Printf("TITLE_DB_DOWNLOAD_BYTES=%d\n", done)
		}
	}

	started := time.Now()
	res, err := r18devdump.Download(ctx, client, "", progress, func(r io.Reader, d r18devdump.DownloadResult) error {
		fmt.Printf("TITLE_DB_IMPORT=START source_date=%s\n", d.SourceDate)
		_, err := r18devdump.Import(ctx, r, path, r18devdump.ImportOptions{
			SourceURL:  d.FinalURL,
			SourceDate: d.SourceDate,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("download/import local title database: %w", err)
	}
	fmt.Printf("TITLE_DB=READY source_date=%s seconds=%.2f\n", res.SourceDate, time.Since(started).Seconds())

	store, err := r18devdump.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open downloaded local title database: %w", err)
	}
	return store, nil
}
