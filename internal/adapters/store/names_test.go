package store

import "testing"

func TestNormalizePersonName(t *testing.T) {
	tests := map[string]string{
		"  Andrii  ":        "andrii",
		"Andrii   Petrenko": "andrii petrenko",
		"ANDRII":            "andrii",
		"О’Connor":          "о'connor",
		"О‘Connor":          "о'connor",
		"О`Connor":          "о'connor",
		"О´Connor":          "о'connor",
		"ОʼConnor":          "о'connor",
		"ОʹConnor":          "о'connor",
		"АНДРІЙ":            "андрій",
		"Марія  Іваненко":   "марія іваненко",
	}
	for input, want := range tests {
		if got := NormalizePersonName(input); got != want {
			t.Fatalf("NormalizePersonName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizePersonNameKeepsConservativeBoundaries(t *testing.T) {
	pairs := [][2]string{
		{"Adlet", "Адлет"},
		{"Alex Johnson", "Alex"},
		{"O'Connor", "O Connor"},
		{"Anne-Marie", "Anne Marie"},
	}
	for _, pair := range pairs {
		if NormalizePersonName(pair[0]) == NormalizePersonName(pair[1]) {
			t.Fatalf("NormalizePersonName(%q) and NormalizePersonName(%q) unexpectedly collapsed to %q", pair[0], pair[1], NormalizePersonName(pair[0]))
		}
	}
}
