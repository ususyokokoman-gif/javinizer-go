package template

import (
	"path/filepath"
	"strings"
)

// resolveKeepWordsTag returns only user-configured words that are present in
// the original filename. No words are configured by default; the modifier is
// the complete allow-list for each template invocation.
//
// Syntax:
//   <KEEPWORDS:word1|word2|word3>
//   <KEEPWORDS:word1|word2;PREFIX= - ;DELIM= ;SUFFIX=>
//
// PREFIX and SUFFIX are emitted only when at least one configured word matches.
func resolveKeepWordsTag(modifier string, ctx *Context) string {
	if ctx == nil || ctx.OriginalFilename == "" || modifier == "" {
		return ""
	}

	parts := strings.Split(modifier, ";")
	if len(parts) == 0 {
		return ""
	}

	wordSpec := strings.TrimSpace(parts[0])
	if wordSpec == "" {
		return ""
	}

	delim := " "
	prefix := ""
	suffix := ""
	for _, option := range parts[1:] {
		key, value, ok := strings.Cut(option, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "DELIM":
			delim = value
		case "PREFIX":
			prefix = value
		case "SUFFIX":
			suffix = value
		}
	}

	// Never match against the extension itself. For example, configuring MP4
	// must not preserve MP4 merely because the file ends in .mp4.
	original := strings.TrimSuffix(ctx.OriginalFilename, filepath.Ext(ctx.OriginalFilename))
	if original == "" {
		return ""
	}

	seen := make(map[string]struct{})
	matches := make([]string, 0)
	for _, configured := range strings.Split(wordSpec, "|") {
		configured = strings.TrimSpace(configured)
		if configured == "" {
			continue
		}

		key := strings.ToLower(configured)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		if containsKeepWord(original, configured) {
			matches = append(matches, configured)
		}
	}

	if len(matches) == 0 {
		return ""
	}
	return prefix + strings.Join(matches, delim) + suffix
}

func containsKeepWord(filename, configured string) bool {
	if configured == "" {
		return false
	}

	haystack := strings.ToLower(filename)
	needle := strings.ToLower(configured)

	// Pure ASCII alphanumeric tokens use boundaries so short tokens such as AI
	// do not match TRAILER and 4K does not match 14K. Tokens containing
	// punctuation (for example -UC) and non-ASCII tokens (for example 字幕) are
	// matched literally as substrings.
	if !isASCIIAlnum(configured) {
		return strings.Contains(haystack, needle)
	}

	searchFrom := 0
	for searchFrom <= len(haystack)-len(needle) {
		rel := strings.Index(haystack[searchFrom:], needle)
		if rel < 0 {
			return false
		}
		start := searchFrom + rel
		end := start + len(needle)
		leftOK := start == 0 || !isASCIIAlnumByte(haystack[start-1])
		rightOK := end == len(haystack) || !isASCIIAlnumByte(haystack[end])
		if leftOK && rightOK {
			return true
		}
		searchFrom = start + 1
	}
	return false
}

func isASCIIAlnum(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isASCIIAlnumByte(s[i]) {
			return false
		}
	}
	return true
}

func isASCIIAlnumByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}
