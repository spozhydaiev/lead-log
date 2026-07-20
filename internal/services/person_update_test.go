package services

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePersonProfileUpdateOmittedNullAndAliases(t *testing.T) {
	first := "  Adlet  "
	notes := " Prefers async\nupdates. "
	in := UpdatePersonProfileInput{FirstName: NullableString{Set: true, Value: &first}, LastName: NullableString{Set: true}, Notes: NullableString{Set: true, Value: &notes}, AliasesSet: true, Aliases: []string{" Adlet ", "adlet", "Адлет"}}
	out, err := validatePersonProfileUpdate(in)
	if err != nil {
		t.Fatal(err)
	}
	if !out.FirstName.Set || out.FirstName.Value == nil || *out.FirstName.Value != "Adlet" {
		t.Fatalf("first name not trimmed: %#v", out.FirstName)
	}
	if !out.LastName.Set || out.LastName.Value != nil {
		t.Fatalf("last name null not preserved: %#v", out.LastName)
	}
	if !out.Notes.Set || out.Notes.Value == nil || *out.Notes.Value != "Prefers async\nupdates." {
		t.Fatalf("notes not preserved: %#v", out.Notes)
	}
	if len(out.Aliases) != 2 {
		t.Fatalf("aliases not deduped by normalization: %#v", out.Aliases)
	}
}

func TestValidatePersonProfileUpdateRejectsInvalidValues(t *testing.T) {
	empty := "   "
	if _, err := validatePersonProfileUpdate(UpdatePersonProfileInput{DisplayName: NullableString{Set: true, Value: &empty}}); !errors.Is(err, ErrPersonValidation) {
		t.Fatalf("empty display name err=%v", err)
	}
	bad := "Adlet\x00"
	if _, err := validatePersonProfileUpdate(UpdatePersonProfileInput{FirstName: NullableString{Set: true, Value: &bad}}); !errors.Is(err, ErrPersonValidation) {
		t.Fatalf("control char err=%v", err)
	}
	long := strings.Repeat("a", personNameMax+1)
	if _, err := validatePersonProfileUpdate(UpdatePersonProfileInput{DisplayName: NullableString{Set: true, Value: &long}}); !errors.Is(err, ErrPersonValidation) {
		t.Fatalf("long display name err=%v", err)
	}
	if _, err := validatePersonProfileUpdate(UpdatePersonProfileInput{AliasesSet: true, Aliases: []string{" "}}); !errors.Is(err, ErrPersonValidation) {
		t.Fatalf("empty alias err=%v", err)
	}
	tooMany := make([]string, personAliasCountMax+1)
	for i := range tooMany {
		tooMany[i] = "alias" + string(rune('a'+i))
	}
	if _, err := validatePersonProfileUpdate(UpdatePersonProfileInput{AliasesSet: true, Aliases: tooMany}); !errors.Is(err, ErrPersonValidation) {
		t.Fatalf("too many aliases err=%v", err)
	}
}
