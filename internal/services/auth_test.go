package services

import "testing"

func TestNormalizeEmailAndPasswordValidation(t *testing.T) {
	orig, norm, err := NormalizeEmail(" User@Example.COM ")
	if err != nil || orig != "User@Example.COM" || norm != "user@example.com" {
		t.Fatalf("orig=%q norm=%q err=%v", orig, norm, err)
	}
	if _, _, err := NormalizeEmail("bad"); err == nil {
		t.Fatal("expected invalid email")
	}
	if err := ValidatePassword("short", 12); err == nil {
		t.Fatal("expected short password rejection")
	}
	if err := ValidatePassword("a-long-secure-password", 12); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordHashAndTokenHash(t *testing.T) {
	h, err := hashPassword("a-long-secure-password")
	if err != nil {
		t.Fatal(err)
	}
	if h == "a-long-secure-password" || !verifyPassword(h, "a-long-secure-password") || verifyPassword(h, "wrong-password") {
		t.Fatalf("password hash verification failed: %q", h)
	}
	tok, th, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" || th == "" || tok == th || HashToken(tok) != th {
		t.Fatalf("bad token/hash")
	}
}

func TestRegisterProfileValidationHelpers(t *testing.T) {
	if got := ValidateResponseLanguage(""); got != "en" {
		t.Fatalf("default language=%q", got)
	}
	if got := ValidateResponseLanguage("en"); got != "en" {
		t.Fatalf("language en rejected: %q", got)
	}
	if got := ValidateResponseLanguage("uk"); got != "uk" {
		t.Fatalf("language uk rejected: %q", got)
	}
	if got := ValidateResponseLanguage("de"); got != "" {
		t.Fatalf("invalid language accepted: %q", got)
	}
	if MaxDisplayNameLength != 100 {
		t.Fatalf("unexpected display name max: %d", MaxDisplayNameLength)
	}
}
