//go:build keepwords

package template

import "testing"

func TestKeepWordsTag_SelectedWordsOnly(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123_4K_AI_字幕.mp4"}

	got, err := engine.Execute("<KEEPWORDS:4K|8K|AI|字幕;PREFIX= - ;DELIM= ;SUFFIX=>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := " - 4K AI 字幕"
	if got != want {
		t.Fatalf("Execute() = %q, want %q", got, want)
	}
}

func TestKeepWordsTag_NoMatchesNoDanglingSeparator(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123.mp4"}

	got, err := engine.Execute("<ID><KEEPWORDS:4K|AI;PREFIX= - > - <ORIGINALTITLE>", &Context{
		ID:               "ABC-123",
		OriginalTitle:    "日本語原題",
		OriginalFilename: ctx.OriginalFilename,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := "ABC-123 - 日本語原題"
	if got != want {
		t.Fatalf("Execute() = %q, want %q", got, want)
	}
}

func TestKeepWordsTag_ASCIIBoundaries(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123_TRAILER_14K.mp4"}

	got, err := engine.Execute("<KEEPWORDS:AI|4K>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "" {
		t.Fatalf("Execute() = %q, want empty string", got)
	}
}

func TestKeepWordsTag_CaseInsensitiveUsesConfiguredSpelling(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "abc-123_4k_uncensored.mp4"}

	got, err := engine.Execute("<KEEPWORDS:4K|UNCENSORED;DELIM=+>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := "4K+UNCENSORED"
	if got != want {
		t.Fatalf("Execute() = %q, want %q", got, want)
	}
}

func TestKeepWordsTag_DeduplicatesConfiguredWords(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123_4k.mp4"}

	got, err := engine.Execute("<KEEPWORDS:4K|4k|4K>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "4K" {
		t.Fatalf("Execute() = %q, want %q", got, "4K")
	}
}

func TestKeepWordsTag_DoesNotMatchExtension(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123.mp4"}

	got, err := engine.Execute("<KEEPWORDS:MP4>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "" {
		t.Fatalf("Execute() = %q, want empty string", got)
	}
}

func TestKeepWordsTag_NonASCIISubstring(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123_中文字幕.mp4"}

	got, err := engine.Execute("<KEEPWORDS:字幕>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "字幕" {
		t.Fatalf("Execute() = %q, want %q", got, "字幕")
	}
}

func TestKeepWordsTag_PunctuationToken(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123-UC.mp4"}

	got, err := engine.Execute("<KEEPWORDS:-UC>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "-UC" {
		t.Fatalf("Execute() = %q, want %q", got, "-UC")
	}
}

func TestKeepWordsTag_EmptyByDefault(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-123_4K_AI.mp4"}

	got, err := engine.Execute("<KEEPWORDS>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "" {
		t.Fatalf("Execute() = %q, want empty string", got)
	}
}

func TestKeepWordsTag_EarlierOverlappingKeywordWins(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-111 【ずん】物語.mp4"}

	got, err := engine.Execute("<KEEPWORDS:【ずん】|ずん>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "【ずん】" {
		t.Fatalf("Execute() = %q, want %q", got, "【ずん】")
	}
}

func TestKeepWordsTag_LaterKeywordUsedWhenPreferredKeywordAbsent(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-111 ずん物語.mp4"}

	got, err := engine.Execute("<KEEPWORDS:【ずん】|ずん>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "ずん" {
		t.Fatalf("Execute() = %q, want %q", got, "ずん")
	}
}

func TestKeepWordsTag_LaterKeywordKeptAtSeparateOccurrence(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-111 【ずん】物語 ずん特典.mp4"}

	got, err := engine.Execute("<KEEPWORDS:【ずん】|ずん>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "【ずん】 ずん" {
		t.Fatalf("Execute() = %q, want %q", got, "【ずん】 ずん")
	}
}

func TestKeepWordsTag_ConfigOrderDefinesPriority(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{OriginalFilename: "ABC-111 【ずん】物語.mp4"}

	got, err := engine.Execute("<KEEPWORDS:ずん|【ずん】>", ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "ずん" {
		t.Fatalf("Execute() = %q, want %q", got, "ずん")
	}
}
