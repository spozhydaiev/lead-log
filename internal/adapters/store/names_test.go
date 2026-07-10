package store

import "testing"

func TestNormalizePersonName(t *testing.T) {
	tests := map[string]string{
		"  Andrii  ":        "andrii",
		"Andrii   Petrenko": "andrii petrenko",
		"О’Connor":          "о'connor",
		"О`Connor":          "о'connor",
		"АНДРІЙ":            "андрій",
	}
	for input, want := range tests {
		if got := NormalizePersonName(input); got != want {
			t.Fatalf("NormalizePersonName(%q) = %q, want %q", input, got, want)
		}
	}
}
