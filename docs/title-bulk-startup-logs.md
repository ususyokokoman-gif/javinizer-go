# Mac title bulk startup and logs

Start immediately writes a Japanese status in WebKit and emits `bulk-progress`
from Go before settings validation or starting the CLI. `TIMING` records carry
UTC timestamps, phase duration and (GUI/CLI) elapsed time since their respective
entrypoints. WebKit uses a second animation frame to measure a paint opportunity;
this is a rendering proxy, not a hardware display measurement. Paint reporting
waits until native Start has reset its trace, preventing delayed bridge startup
from discarding early measurements.

The output folder's `run.log` contains CLI output, stderr (labelled `STDERR:`),
GUI phases and WebKit timings. Early events are buffered; the log stays open
until the next run or shutdown so fast cached runs retain late paint reports.
The UI shows up to 3,000 human-readable rows and 10,000 technical rows. Detailed
logs are collapsed initially. Pause affects log rendering; processing continues.
The status and completion summary remain live. Copy uses the currently selected
human/technical view. The expanded log hides settings and fills the window.

## Scan and cache behavior

Normal GUI processing keeps 8 workers on Mac, duplicate detection disabled,
Jev threshold 0.80, local R18 candidate lookup followed by Jev and Web fallback,
and the existing 25-task / 2-second batched state checkpoint policy.

`WalkDir` avoids explicit stat calls for non-media entries. With duplicate
checking disabled, `scanMediaForTitles` also avoids per-video stat calls because
title grouping/cache reuse depend solely on filename-derived titles. New file
state bookkeeping uses `size=-1, mtime_ns=0` to denote unknown metadata; these
values must not be used as a cache validation predicate. Duplicate-enabled CLI
processing collects full size/mtime metadata and keeps its prior hash behavior.
Directory enumeration can still block on slow mounted drives. Discovery counts
are emitted on the first video and at roughly 250ms intervals between walk
callbacks; callbacks cannot report new data while a filesystem call blocks.

Grouping and cache reuse are evaluated before DB preparation. A fully cached
run skips the DB entirely. Persistent JSON state remains compatible with its
existing migrations, backup recovery and batched checkpoint implementation.

## Verification

```sh
node --test cmd/javinizer-title-bulk/gui_frontend/status.test.cjs
go test -race -tags sqlite_fts5 ./cmd/javinizer-title-bulk ./internal/r18devdump
go test -race -tags sqlite_fts5 ./internal/scrape -run '^(TestLocalTitle|TestTitleCatalogResolver|TestChooseJevFastCandidate|TestJevCatalogGate|TestLookupCatalogIDOnWeb)' -count=1
CGO_LDFLAGS='-framework UniformTypeIdentifiers' go build -trimpath -tags 'desktop,production,sqlite_fts5' ./cmd/javinizer-title-bulk
```

Optional read-only benchmarks accept `JAVINIZER_BENCH_STATE` and
`JAVINIZER_BENCH_ROOT`. Use `-run '^$' -bench BenchmarkPersistentStateLoad`
or `-bench BenchmarkMediaScan -benchtime=1x`. Large mounted folders can take
minutes even with stat calls removed.
