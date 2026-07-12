package models

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRetrievalSnippetUnicodeKeepsMatchAndUTF8(t *testing.T) {
	text := strings.Repeat("перед ", 30) + "Адлет працює над CH-1234 retry policy" + strings.Repeat(" після", 30)
	snippet := RetrievalSnippet(text, "Адлет", 80)
	if !utf8.ValidString(snippet) {
		t.Fatal("snippet must keep valid UTF-8")
	}
	if !strings.Contains(snippet, "Адлет") {
		t.Fatalf("snippet should keep matched Cyrillic term: %q", snippet)
	}
	if len([]rune(snippet)) > 82 { // allow ellipses
		t.Fatalf("snippet too long: %d", len([]rune(snippet)))
	}
}
