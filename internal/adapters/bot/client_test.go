package bot

import "testing"

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
		{name: "lowercases command", text: "/PERSON Jane", wantCmd: "/person", wantArg: "Jane"},
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
