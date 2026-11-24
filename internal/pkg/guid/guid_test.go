package guid

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	id1 := New()
	id2 := New()
	if len(id1) != 32 {
		t.Fatalf("expected length 32, got %d (%q)", len(id1), id1)
	}
	if strings.Contains(id1, "-") {
		t.Fatalf("expected no dashes in %q", id1)
	}
	if id1 != strings.ToLower(id1) {
		t.Fatalf("expected lowercase hex, got %q", id1)
	}
	if id1 == id2 {
		t.Fatalf("expected unique guids, got duplicate %q", id1)
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"  550E8400-E29B-41D4-A716-446655440000  ", "550e8400e29b41d4a716446655440000"},
		{"550e8400e29b41d4a716446655440000", "550e8400e29b41d4a716446655440000"},
		{"", ""},
		{"---", ""},
	}
	for _, tc := range tests {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatDashed(t *testing.T) {
	valid32 := "550e8400e29b41d4a716446655440000"
	wantDashed := "550e8400-e29b-41d4-a716-446655440000"

	if got := FormatDashed(valid32); got != wantDashed {
		t.Fatalf("FormatDashed(%q) = %q, want %q", valid32, got, wantDashed)
	}
	if got := FormatDashed("  550E8400-E29B-41D4-A716-446655440000 "); got != wantDashed {
		t.Fatalf("FormatDashed(dashed) = %q, want %q", got, wantDashed)
	}
	// Wrong length returns original
	if got := FormatDashed("short"); got != "short" {
		t.Fatalf("FormatDashed(short) = %q, want %q", got, "short")
	}
	// Length 32 but non-hex returns original
	badHex := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	if got := FormatDashed(badHex); got != badHex {
		t.Fatalf("FormatDashed(badHex) = %q, want %q", got, badHex)
	}
}
