package adapters

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDatabaseNameNormalizesASCII(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		requested    string
		want         string
	}{
		{name: "requested wins", instanceName: "svc-1", requested: "Reporting DB", want: "reporting_db"},
		{name: "instance fallback", instanceName: "Orders 360", requested: "", want: "orders_360"},
		{name: "leading digits prefixed", instanceName: "9lives", requested: "", want: "db_9lives"},
		{name: "unsupported characters dropped", instanceName: "team@svc!", requested: "", want: "teamsvc"},
		{name: "nothing left", instanceName: "@@@", requested: "", want: "db"},
		{name: "ascii max length kept", instanceName: strings.Repeat("a", maxDatabaseNameLength), requested: "", want: strings.Repeat("a", maxDatabaseNameLength)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveDatabaseName(tt.instanceName, tt.requested); got != tt.want {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", tt.instanceName, tt.requested, got, tt.want)
			}
		})
	}
}

// TestResolveDatabaseNameTruncatesOnRuneBoundary covers names whose 63rd byte falls
// inside a multi-byte rune: byte slicing used to cut the rune in half and return
// a value that was not valid UTF-8.
func TestResolveDatabaseNameTruncatesOnRuneBoundary(t *testing.T) {
	tests := []struct {
		name     string
		rune     string
		runeSize int
		prefix   int
		crosses  bool
		want     string
	}{
		{name: "two byte rune split", rune: "é", runeSize: 2, prefix: 62, crosses: true, want: strings.Repeat("a", 62)},
		{name: "two byte cyrillic split", rune: "с", runeSize: 2, prefix: 62, crosses: true, want: strings.Repeat("a", 62)},
		{name: "three byte rune split", rune: "漢", runeSize: 3, prefix: 61, crosses: true, want: strings.Repeat("a", 61)},
		{name: "long rune past limit", rune: "𝕒", runeSize: 4, prefix: 62, crosses: true, want: strings.Repeat("a", 62)},
		{name: "rune ends exactly at limit", rune: "é", runeSize: 2, prefix: 61, want: strings.Repeat("a", 61) + "é"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.Repeat("a", tt.prefix) + strings.Repeat(tt.rune, 10)
			boundary := tt.prefix + tt.runeSize
			if tt.crosses && boundary <= maxDatabaseNameLength {
				t.Fatalf("fixture must cross byte %d, prefix+one rune is %d bytes", maxDatabaseNameLength, boundary)
			}
			if !tt.crosses && boundary != maxDatabaseNameLength {
				t.Fatalf("fixture must end its rune on byte %d, prefix+one rune is %d bytes", maxDatabaseNameLength, boundary)
			}

			got := ResolveDatabaseName(input, "")
			if !utf8.ValidString(got) {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, not valid UTF-8", input, "", got)
			}
			if len(got) > maxDatabaseNameLength {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, %d bytes exceeds %d", input, "", got, len(got), maxDatabaseNameLength)
			}
			if got != tt.want {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", input, "", got, tt.want)
			}
		})
	}
}

func TestResolveDatabaseNameAlwaysValidUTF8WithinLimit(t *testing.T) {
	alphabets := []string{"a", "é", "ü", "с", "漢", "字", "𝕒"}

	for _, letter := range alphabets {
		for length := 1; length <= 80; length++ {
			input := strings.Repeat(letter, length)
			got := ResolveDatabaseName(input, "")
			if !utf8.ValidString(got) {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, not valid UTF-8", input, "", got)
			}
			if len(got) > maxDatabaseNameLength {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, %d bytes exceeds %d", input, "", got, len(got), maxDatabaseNameLength)
			}
			if got == "" {
				t.Fatalf("ResolveDatabaseName(%q, %q) returned empty name", input, "")
			}
		}
	}
}

func TestResolveDatabaseNameTrimsTrailingSeparatorAfterTruncation(t *testing.T) {
	input := strings.Repeat("a", 62) + "-tail"
	got := ResolveDatabaseName(input, "")
	if got != strings.Repeat("a", 62) {
		t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want 62 a's with separator removed", input, "", got)
	}
}
