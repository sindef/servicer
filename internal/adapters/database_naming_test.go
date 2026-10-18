package adapters

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDatabaseNameNormalizesInput(t *testing.T) {
	tests := []struct {
		name      string
		instance  string
		requested string
		want      string
	}{
		{name: "derives from instance name", instance: "Reporting DB", want: "reporting_db"},
		{name: "explicit request wins", instance: "orders", requested: "testdb", want: "testdb"},
		{name: "empty request falls back to instance", instance: "Customer 360", requested: "   ", want: "customer_360"},
		{name: "both empty yields default", want: "db"},
		{name: "leading digit gets prefix", instance: "42orders", want: "db_42orders"},
		{name: "separators collapse", instance: "or--ders  db", want: "or_ders_db"},
		{name: "only separators yields default", instance: " - - ", want: "db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveDatabaseName(tt.instance, tt.requested); got != tt.want {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", tt.instance, tt.requested, got, tt.want)
			}
		})
	}
}

func TestResolveDatabaseNameTruncatesOnRuneBoundary(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			// 40 two-byte runes: byte 63 falls inside the 32nd rune.
			name: "two-byte rune split by byte limit",
			base: strings.Repeat("é", 40),
			want: strings.Repeat("é", 31),
		},
		{
			// Two ASCII bytes plus 30 three-byte runes: byte 63 falls inside a rune.
			name: "three-byte rune split by byte limit",
			base: "ab" + strings.Repeat("漢", 30),
			want: "ab" + strings.Repeat("漢", 20),
		},
		{
			// Cyrillic letters survive normalization and cross the limit too.
			name: "cyrillic name truncated",
			base: strings.Repeat("ж", 35),
			want: strings.Repeat("ж", 31),
		},
		{
			// Exactly 63 ASCII bytes: untouched.
			name: "ascii name at limit kept",
			base: strings.Repeat("a", maxDatabaseNameLength),
			want: strings.Repeat("a", maxDatabaseNameLength),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDatabaseName(tt.base, "")
			if got != tt.want {
				t.Fatalf("ResolveDatabaseName(%q, \"\") = %q, want %q", tt.base, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("ResolveDatabaseName(%q, \"\") = %q: not valid UTF-8", tt.base, got)
			}
			if len(got) > maxDatabaseNameLength {
				t.Fatalf("ResolveDatabaseName(%q, \"\") = %q: %d bytes exceeds limit %d", tt.base, got, len(got), maxDatabaseNameLength)
			}
		})
	}
}

func TestResolveDatabaseNameAlwaysValidUTF8WithinLimit(t *testing.T) {
	alphabets := []string{
		"a", "7", "-", "_", " ",
		"é", "ü", "ñ", "ж", "漢", "𝔞",
	}
	separators := []string{"", "-", "_", " ", "--"}

	for _, alphabet := range alphabets {
		for _, separator := range separators {
			for length := 1; length <= 70; length++ {
				var b strings.Builder
				for i := 0; i < length; i++ {
					if i > 0 {
						b.WriteString(separator)
					}
					b.WriteString(alphabet)
				}
				input := b.String()
				got := ResolveDatabaseName(input, "")
				if !utf8.ValidString(got) {
					t.Fatalf("ResolveDatabaseName(%q, \"\") = %q: not valid UTF-8", input, got)
				}
				if len(got) > maxDatabaseNameLength {
					t.Fatalf("ResolveDatabaseName(%q, \"\") = %q: %d bytes exceeds limit %d", input, got, len(got), maxDatabaseNameLength)
				}
				if got == "" || strings.HasPrefix(got, "_") || strings.HasSuffix(got, "_") {
					t.Fatalf("ResolveDatabaseName(%q, \"\") = %q: empty or dangling separator", input, got)
				}
			}
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		in       string
		maxBytes int
		want     string
	}{
		{in: "", maxBytes: 63, want: ""},
		{in: "abc", maxBytes: 63, want: "abc"},
		{in: "aé", maxBytes: 2, want: "a"},
		{in: "aé", maxBytes: 3, want: "aé"},
		{in: "漢漢", maxBytes: 4, want: "漢"},
		{in: strings.Repeat("é", 40), maxBytes: maxDatabaseNameLength, want: strings.Repeat("é", 31)},
	}

	for _, tt := range tests {
		got := truncateRunes(tt.in, tt.maxBytes)
		if got != tt.want {
			t.Fatalf("truncateRunes(%q, %d) = %q, want %q", tt.in, tt.maxBytes, got, tt.want)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("truncateRunes(%q, %d) = %q: not valid UTF-8", tt.in, tt.maxBytes, got)
		}
		if len(got) > tt.maxBytes {
			t.Fatalf("truncateRunes(%q, %d) = %q: %d bytes exceeds limit", tt.in, tt.maxBytes, got, len(got))
		}
	}
}
