package adapters

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDatabaseNameTruncatesOnRuneBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		instance string
		request  string
		want     string
	}{
		{
			name:     "ascii name keeps full length",
			instance: strings.Repeat("a", 63),
			want:     strings.Repeat("a", 63),
		},
		{
			name:     "ascii name over limit is cut to 63 bytes",
			instance: strings.Repeat("a", 70),
			want:     strings.Repeat("a", 63),
		},
		{
			name:    "ascii truncation does not leave trailing separator",
			request: strings.Repeat("a", 60) + "-bcd-efg",
			want:    strings.Repeat("a", 60) + "_bc",
		},
		{
			// 40 two-byte runes are 80 bytes; byte 63 falls inside the rune
			// starting at byte 62, so the last rune must be dropped.
			name:     "accented letters stop before the split rune",
			instance: strings.Repeat("é", 40),
			want:     strings.Repeat("é", 31),
		},
		{
			name:     "cyrillic letters stop before the split rune",
			instance: strings.Repeat("ж", 40),
			want:     strings.Repeat("ж", 31),
		},
		{
			// 20 four-byte runes are 80 bytes; the rune starting at byte 60
			// would cross byte 64, so truncation stops at 60 bytes.
			name:     "four byte letters stop on the last whole rune",
			instance: strings.Repeat("𐐨", 20),
			want:     strings.Repeat("𐐨", 15),
		},
		{
			name:     "mixed ascii and multibyte runes fill the limit",
			instance: strings.Repeat("a", 61) + strings.Repeat("ü", 4),
			want:     strings.Repeat("a", 61) + "ü",
		},
		{
			name:     "short multibyte name is unchanged",
			instance: "café",
			want:     "café",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDatabaseName(tt.instance, tt.request)
			if got != tt.want {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", tt.instance, tt.request, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q is not valid UTF-8", tt.instance, tt.request, got)
			}
			if len(got) > maxDatabaseNameLength {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q is %d bytes, exceeds %d", tt.instance, tt.request, got, len(got), maxDatabaseNameLength)
			}
			if got == "" || strings.HasPrefix(got, "_") || strings.HasSuffix(got, "_") {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q is not a usable identifier", tt.instance, tt.request, got)
			}
		})
	}
}

func TestResolveDatabaseNameTruncationStaysValidForAnyMultiByteName(t *testing.T) {
	// Every result must be valid UTF-8 within the Postgres identifier byte
	// limit, no matter where the rune boundary falls.
	alphabets := []string{"é", "ж", "字", "𐐨"}
	for _, alphabet := range alphabets {
		for count := 1; count <= 80; count++ {
			input := strings.Repeat(alphabet, count)
			got := ResolveDatabaseName(input, "")
			if !utf8.ValidString(got) {
				t.Fatalf("ResolveDatabaseName(%q) = %q is not valid UTF-8", input, got)
			}
			if len(got) > maxDatabaseNameLength {
				t.Fatalf("ResolveDatabaseName(%q) = %q is %d bytes, exceeds %d", input, got, len(got), maxDatabaseNameLength)
			}
		}
	}
}

func TestResolveDatabaseNameDefaults(t *testing.T) {
	tests := []struct {
		instance string
		request  string
		want     string
	}{
		{instance: "", request: "", want: "db"},
		{instance: "---", request: "", want: "db"},
		{instance: "Orders", request: "", want: "orders"},
		{instance: "svc", request: "  Billing DB  ", want: "billing_db"},
		{instance: "svc", request: "123 main", want: "db_123_main"},
	}

	for _, tt := range tests {
		if got := ResolveDatabaseName(tt.instance, tt.request); got != tt.want {
			t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", tt.instance, tt.request, got, tt.want)
		}
	}
}
