package adapters

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestResolveDatabaseNameTruncatesOnRuneBoundary covers a name whose 63rd byte
// falls inside a multi-byte rune: the returned value must stay valid UTF-8 and
// within Postgres' 63-byte identifier limit.
func TestResolveDatabaseNameTruncatesOnRuneBoundary(t *testing.T) {
	// 62 ASCII letters followed by accented runes: byte index 62 and 63 are the
	// two bytes of the first "é", so a naive name[:63] slice cuts it in half.
	in := strings.Repeat("a", 62) + "ééé"

	got := ResolveDatabaseName(in, "")

	if !utf8.ValidString(got) {
		t.Fatalf("ResolveDatabaseName(%q) = %q: invalid UTF-8, bytes % x", in, got, got)
	}
	if len(got) > 63 {
		t.Fatalf("ResolveDatabaseName(%q) = %q: %d bytes exceeds 63-byte identifier limit", in, got, len(got))
	}
	if strings.ContainsRune(got, '�') {
		t.Fatalf("ResolveDatabaseName(%q) = %q: replacement character in result", in, got)
	}
}

// TestResolveDatabaseNameLengthBound checks that long inputs of both ASCII and
// multi-byte runes always produce valid UTF-8 names within the byte limit.
func TestResolveDatabaseNameLengthBound(t *testing.T) {
	cases := []string{
		strings.Repeat("z", 100),
		strings.Repeat("é", 100),
		strings.Repeat("名", 100),
		strings.Repeat("a", 61) + "é",
		strings.Repeat("a", 62) + "é",
		strings.Repeat("a", 63) + "é",
		strings.Repeat("я", 40) + strings.Repeat("b", 40),
	}
	for _, in := range cases {
		got := ResolveDatabaseName(in, "")
		if !utf8.ValidString(got) {
			t.Errorf("ResolveDatabaseName(%q) = %q: invalid UTF-8", in, got)
		}
		if len(got) > 63 {
			t.Errorf("ResolveDatabaseName(%q) = %q: %d bytes exceeds 63", in, got, len(got))
		}
		if got == "" {
			t.Errorf("ResolveDatabaseName(%q) returned empty name", in)
		}
	}
}
