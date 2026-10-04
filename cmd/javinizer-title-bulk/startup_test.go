package main

import (
	"os"
	"testing"
)

func BenchmarkPersistentStateLoad(b *testing.B) {
	path := os.Getenv("JAVINIZER_BENCH_STATE")
	if path == "" {
		b.Skip("set JAVINIZER_BENCH_STATE")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadPersistentState(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMediaScan(b *testing.B) {
	root := os.Getenv("JAVINIZER_BENCH_ROOT")
	if root == "" {
		b.Skip("set JAVINIZER_BENCH_ROOT")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		files, err := scanMediaForTitles(root, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(files)), "files")
	}
}

func TestScanMediaProgressCountsOnlyMedia(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b.mp4", "a.MKV", "notes.txt"} {
		if err := os.WriteFile(root+"/"+name, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var counts []int
	files, err := scanMediaWithProgress(root, func(n int) { counts = append(counts, n) })
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || len(counts) == 0 || counts[len(counts)-1] != 2 {
		t.Fatalf("files=%d progress=%v", len(files), counts)
	}
	for i := 1; i < len(counts); i++ {
		if counts[i] < counts[i-1] {
			t.Fatal("scan progress went backwards")
		}
	}
	if files[0].Path > files[1].Path {
		t.Fatal("scan order is unstable")
	}
}

func TestRunTraceRetainsEarlyAndLateTimings(t *testing.T) {
	var trace runTrace
	trace.reset()
	trace.write("first_display")
	path := t.TempDir() + "/run.log"
	if err := trace.open(path); err != nil {
		t.Fatal(err)
	}
	trace.write("child_start")
	trace.write("cli_output")
	trace.close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "first_display\nchild_start\ncli_output\n" {
		t.Fatalf("trace=%q", raw)
	}
}

func TestTitleOnlyScanSkipsMetadataButKeepsTitles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/作品.mp4", []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	full, err := scanMedia(root)
	if err != nil {
		t.Fatal(err)
	}
	fast, err := scanMediaForTitles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fast) != 1 || len(full) != 1 || fast[0].Path != full[0].Path {
		t.Fatal("title scan changed media selection")
	}
	if fast[0].Size != -1 || fast[0].ModTimeNS != 0 {
		t.Fatal("title-only mode must not claim to have collected metadata")
	}
	if full[0].Size != 5 || full[0].ModTimeNS == 0 {
		t.Fatal("duplicate scan lost metadata")
	}
	if buildTitleWork(full)[0].Title != buildTitleWork(fast)[0].Title {
		t.Fatal("grouping changed")
	}
}
