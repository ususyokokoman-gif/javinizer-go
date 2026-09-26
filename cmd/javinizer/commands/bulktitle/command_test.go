package bulktitle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkExactDuplicates(t *testing.T) {
	dir := t.TempDir()
	paths := []struct {
		name string
		data string
	}{
		{"a.mp4", "same"},
		{"b.mp4", "same"},
		{"c.mp4", "diff"},
	}
	for _, tc := range paths {
		if err := os.WriteFile(filepath.Join(dir, tc.name), []byte(tc.data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	files, err := scanFiles(dir, []string{".mp4"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := markExactDuplicates(files, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups=%d want 1", len(groups))
	}
	if groups[0].Keeper != filepath.Join(dir, "a.mp4") {
		t.Fatalf("keeper=%q", groups[0].Keeper)
	}
	if files[1].DuplicateOf != filepath.Join(dir, "a.mp4") {
		t.Fatalf("b duplicate_of=%q", files[1].DuplicateOf)
	}
	if files[2].DuplicateOf != "" {
		t.Fatalf("c should not be duplicate: %q", files[2].DuplicateOf)
	}
}

func TestTitleFromPath(t *testing.T) {
	got := titleFromPath(filepath.Join("x", "狙われた通学路 共謀痴漢電車 桃乃木かな.mp4"))
	want := "狙われた通学路 共謀痴漢電車 桃乃木かな"
	if got != want {
		t.Fatalf("title=%q want %q", got, want)
	}
}
