package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTitleFromPath(t *testing.T) {
	got := titleFromPath(filepath.Join("x", "狙われた通学路 共謀痴漢電車 桃乃木かな.mp4"))
	if got != "狙われた通学路 共謀痴漢電車 桃乃木かな" {
		t.Fatalf("titleFromPath=%q", got)
	}
}

func TestRemoveExactDuplicates(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	b := filepath.Join(dir, "b.mp4")
	c := filepath.Join(dir, "c.mp4")

	if err := os.WriteFile(a, []byte("same-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("different!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := []fileItem{
		{Path: a, Size: 12},
		{Path: b, Size: 12},
		{Path: c, Size: 12},
	}
	unique, duplicates, err := removeExactDuplicates(files, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unique) != 2 {
		t.Fatalf("unique=%d, want 2", len(unique))
	}
	if len(duplicates) != 1 {
		t.Fatalf("duplicates=%d, want 1", len(duplicates))
	}
	if duplicates[0].CanonicalPath != a {
		t.Fatalf("canonical=%q, want %q", duplicates[0].CanonicalPath, a)
	}
	if duplicates[0].DuplicatePath != b {
		t.Fatalf("duplicate=%q, want %q", duplicates[0].DuplicatePath, b)
	}
}

func TestRemoveExactDuplicatesPreservesDescriptiveFilename(t *testing.T) {
	dir := t.TempDir()
	descriptive := filepath.Join(dir, "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間.mp4")
	weak := filepath.Join(dir, "duplicate-copy.mp4")
	data := []byte("identical-media-content")

	if err := os.WriteFile(descriptive, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(weak, data, 0o644); err != nil {
		t.Fatal(err)
	}

	files := []fileItem{
		{Path: weak, Size: int64(len(data))},
		{Path: descriptive, Size: int64(len(data))},
	}
	unique, duplicates, err := removeExactDuplicates(files, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unique) != 1 {
		t.Fatalf("unique=%d, want 1", len(unique))
	}
	if unique[0].Path != descriptive {
		t.Fatalf("kept=%q, want descriptive %q", unique[0].Path, descriptive)
	}
	if len(duplicates) != 1 {
		t.Fatalf("duplicates=%d, want 1", len(duplicates))
	}
	if duplicates[0].CanonicalPath != descriptive {
		t.Fatalf("canonical=%q, want %q", duplicates[0].CanonicalPath, descriptive)
	}
	if duplicates[0].DuplicatePath != weak {
		t.Fatalf("duplicate=%q, want %q", duplicates[0].DuplicatePath, weak)
	}
}
