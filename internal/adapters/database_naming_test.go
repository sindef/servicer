package adapters

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDatabaseName(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		requested    string
		want         string
	}{
		{
			name:         "explicit name is normalized",
			instanceName: "orders-db",
			requested:    "Reporting DB",
			want:         "reporting_db",
		},
		{
			name:         "empty request derives from instance name",
			instanceName: "Orders DB",
			requested:    "",
			want:         "orders_db",
		},
		{
			name:         "leading digit gets a db prefix",
			instanceName: "orders-db",
			requested:    "1reporting",
			want:         "db_1reporting",
		},
		{
			name:         "separator-only request falls back to db",
			instanceName: "orders-db",
			requested:    " _ _ ",
			want:         "db",
		},
		{
			name:         "long ascii name truncates at 63 bytes",
			instanceName: "orders-db",
			requested:    "x" + strings.Repeat("y", 69),
			want:         "x" + strings.Repeat("y", 62),
		},
		{
			name:         "truncation drops a trailing separator",
			instanceName: "orders-db",
			requested:    "x" + strings.Repeat("y", 62) + " " + strings.Repeat("z", 10),
			want:         "x" + strings.Repeat("y", 62),
		},
		{
			name:         "63rd byte inside a multibyte rune truncates on the rune boundary",
			instanceName: strings.Repeat("é", 32),
			requested:    "",
			// 32 runes of 2 bytes = 64 bytes; the 63rd byte is the second byte of
			// the 32nd rune, so truncation must stop at 31 complete runes.
			want: strings.Repeat("é", 31),
		},
		{
			name:         "cyrillic letters truncate on rune boundaries",
			instanceName: "orders-db",
			requested:    strings.Repeat("я", 33),
			want:         strings.Repeat("я", 31),
		},
		{
			name:         "multibyte name that fits in 63 bytes is kept whole",
			instanceName: "orders-db",
			requested:    strings.Repeat("é", 30) + "abc",
			want:         strings.Repeat("é", 30) + "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDatabaseName(tt.instanceName, tt.requested)

			if got != tt.want {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", tt.instanceName, tt.requested, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("ResolveDatabaseName(%q, %q) returned invalid UTF-8: %q", tt.instanceName, tt.requested, got)
			}
			if len(got) > maxDatabaseNameLength {
				t.Fatalf("ResolveDatabaseName(%q, %q) returned %d bytes, want at most %d", tt.instanceName, tt.requested, len(got), maxDatabaseNameLength)
			}
		})
	}
}

func TestResolveDatabaseNameAlwaysReturnsValidUTF8(t *testing.T) {
	// Cover rune sizes from 1 to 4 bytes and lengths that cross the 63-byte
	// limit at every byte offset around it.
	inputs := []string{"a", "é", "я", "€", "🙂"}
	for _, r := range inputs {
		for count := 1; count <= 40; count++ {
			instanceName := strings.Repeat(r, count)
			for _, requested := range []string{"", instanceName, strings.Repeat("x", 60) + instanceName} {
				got := ResolveDatabaseName(instanceName, requested)

				if !utf8.ValidString(got) {
					t.Fatalf("ResolveDatabaseName(%q, %q) returned invalid UTF-8: %q", instanceName, requested, got)
				}
				if len(got) > maxDatabaseNameLength {
					t.Fatalf("ResolveDatabaseName(%q, %q) returned %d bytes, want at most %d", instanceName, requested, len(got), maxDatabaseNameLength)
				}
				if got == "" {
					t.Fatalf("ResolveDatabaseName(%q, %q) returned an empty name", instanceName, requested)
				}
			}
		}
	}
}
