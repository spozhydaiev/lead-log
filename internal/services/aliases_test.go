package services

import "testing"

func TestSplitEqualsArg(t *testing.T) {
	left, right, ok := splitEqualsArg(" Andrii = Андрій ")
	if !ok || left != "Andrii" || right != "Андрій" {
		t.Fatalf("splitEqualsArg returned (%q, %q, %v)", left, right, ok)
	}

	if _, _, ok := splitEqualsArg("Andrii Андрій"); ok {
		t.Fatal("splitEqualsArg should reject args without equals sign")
	}

	if _, _, ok := splitEqualsArg("Andrii = "); ok {
		t.Fatal("splitEqualsArg should reject empty target")
	}
}
