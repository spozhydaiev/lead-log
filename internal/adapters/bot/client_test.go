package bot

import (
	"strings"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantCmd string
		wantArg string
	}{
		{name: "plain text", text: "remember this", wantCmd: "", wantArg: "remember this"},
		{name: "command without arg", text: "/daily", wantCmd: "/daily", wantArg: ""},
		{name: "command with arg", text: "/note follow up", wantCmd: "/note", wantArg: "follow up"},
		{name: "now command with arg", text: "/now follow up", wantCmd: "/now", wantArg: "follow up"},
		{name: "lowercases command", text: "/DAILY", wantCmd: "/daily", wantArg: ""},
		{name: "strips bot username", text: "/daily@LeadLogBot --refresh", wantCmd: "/daily", wantArg: "--refresh"},
		{name: "trims arg", text: "/done   123  ", wantCmd: "/done", wantArg: "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArg := splitCommand(tt.text)
			if gotCmd != tt.wantCmd || gotArg != tt.wantArg {
				t.Fatalf("splitCommand(%q) = (%q, %q), want (%q, %q)", tt.text, gotCmd, gotArg, tt.wantCmd, tt.wantArg)
			}
		})
	}
}

func TestHelpTextDocumentsMVPCommands(t *testing.T) {
	help := models.LanguageEnglish.CommonMessages().HelpText
	for _, want := range []string{
		"/note <text> — quickly save a raw note without AI processing",
		"/now <text> — save and immediately structure a note",
		"/daily --refresh — regenerate the daily digest",
		"/weekly --refresh — regenerate the weekly digest",
		"regular text without /note",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help text does not contain %q\n%s", want, help)
		}
	}

	for _, removed := range []string{"/person", "/agenda", "/review", "/alias", "/merge", "/ticket"} {
		if strings.Contains(help, removed) {
			t.Fatalf("help text still exposes %q\n%s", removed, help)
		}
	}
}
