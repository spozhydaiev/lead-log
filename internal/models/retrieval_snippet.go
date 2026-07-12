package models

import (
	"strings"
	"unicode/utf8"
)

const DefaultRetrievalSnippetRunes = 240

func RetrievalSnippet(text, needle string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		maxRunes = DefaultRetrievalSnippetRunes
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	lowerText, lowerNeedle := strings.ToLower(text), strings.ToLower(strings.TrimSpace(needle))
	idx := -1
	if lowerNeedle != "" {
		idx = strings.Index(lowerText, lowerNeedle)
	}
	runes := []rune(text)
	start := 0
	if idx >= 0 {
		matchRune := utf8.RuneCountInString(text[:idx])
		start = matchRune - maxRunes/3
		if start < 0 {
			start = 0
		}
	}
	if start+maxRunes > len(runes) {
		start = len(runes) - maxRunes
	}
	if start < 0 {
		start = 0
	}
	end := start + maxRunes
	if end > len(runes) {
		end = len(runes)
	}
	out := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
