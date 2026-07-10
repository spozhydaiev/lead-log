package utils

import "testing"

func TestHasRefreshFlag(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{name: "long flag", arg: "--refresh", want: true},
		{name: "word", arg: "refresh", want: true},
		{name: "short flag", arg: "-r", want: true},
		{name: "trims and lowercases", arg: "  --REFRESH  ", want: true},
		{name: "empty", arg: "", want: false},
		{name: "extra args are not accepted", arg: "today --refresh", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasRefreshFlag(tt.arg); got != tt.want {
				t.Fatalf("HasRefreshFlag(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestStripRefreshFlag(t *testing.T) {
	tests := []struct {
		name        string
		arg         string
		wantClean   string
		wantRefresh bool
	}{
		{name: "keeps name", arg: "Jane Doe", wantClean: "Jane Doe", wantRefresh: false},
		{name: "strips long flag", arg: "Jane Doe --refresh", wantClean: "Jane Doe", wantRefresh: true},
		{name: "strips word", arg: "refresh Jane Doe", wantClean: "Jane Doe", wantRefresh: true},
		{name: "strips short flag", arg: "Jane -r Doe", wantClean: "Jane Doe", wantRefresh: true},
		{name: "case insensitive", arg: "Jane --REFRESH", wantClean: "Jane", wantRefresh: true},
		{name: "only refresh", arg: "--refresh", wantClean: "", wantRefresh: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotRefresh := StripRefreshFlag(tt.arg)
			if gotClean != tt.wantClean || gotRefresh != tt.wantRefresh {
				t.Fatalf("StripRefreshFlag(%q) = (%q, %v), want (%q, %v)", tt.arg, gotClean, gotRefresh, tt.wantClean, tt.wantRefresh)
			}
		})
	}
}
