package adapters

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDatabaseNameNormalizesSeparators(t *testing.T) {
	tests := []struct {
		name       string
		instance   string
		requested  string
		wantResult string
	}{
		{name: "derives from instance name", instance: "Orders Prod", wantResult: "orders_prod"},
		{name: "requested wins over instance", instance: "orders", requested: "Billing-DB", wantResult: "billing_db"},
		{name: "collapses repeated separators", instance: "  orders --  prod  ", wantResult: "orders_prod"},
		{name: "prefixes digit led names", instance: "2024orders", wantResult: "db_2024orders"},
		{name: "falls back when empty", instance: "", requested: "", wantResult: "db"},
		{name: "falls back when only separators", instance: "--- ___", wantResult: "db"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveDatabaseName(test.instance, test.requested); got != test.wantResult {
				t.Fatalf("ResolveDatabaseName(%q, %q) = %q, want %q", test.instance, test.requested, got, test.wantResult)
			}
		})
	}
}

func TestResolveDatabaseNameKeepsASCIIWithinLimit(t *testing.T) {
	long := strings.Repeat("a", maxDatabaseNameLength+10)
	name := ResolveDatabaseName(long, "")
	if len(name) != maxDatabaseNameLength {
		t.Fatalf("len(ResolveDatabaseName(long ASCII)) = %d, want %d", len(name), maxDatabaseNameLength)
	}
	if name != long[:maxDatabaseNameLength] {
		t.Fatalf("ResolveDatabaseName(long ASCII) = %q, want %q", name, long[:maxDatabaseNameLength])
	}

	// A separator that lands on the 63rd byte is trimmed instead of leaving a trailing underscore.
	withSeparator := ResolveDatabaseName(strings.Repeat("a", maxDatabaseNameLength-1)+"-b", "")
	if want := strings.Repeat("a", maxDatabaseNameLength-1); withSeparator != want {
		t.Fatalf("ResolveDatabaseName(separator at limit) = %q, want %q", withSeparator, want)
	}
}

func TestResolveDatabaseNameTruncatesOnRuneBoundary(t *testing.T) {
	tests := []struct {
		label  string
		inputs string
	}{
		{label: "two byte rune", inputs: strings.Repeat("é", 40)},
		{label: "three byte rune", inputs: strings.Repeat("語", 30)},
		{label: "four byte rune", inputs: strings.Repeat("𐐷", 20)},
		{label: "cyrillic", inputs: strings.Repeat("д", 40)},
		{label: "mixed scripts", inputs: strings.Repeat("ordersé", 12)},
		{label: "separator after multi byte runes", inputs: strings.Repeat("é", 31) + "-x"},
	}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			name := ResolveDatabaseName(test.inputs, "")
			if !utf8.ValidString(name) {
				t.Fatalf("ResolveDatabaseName(%q) = %q, not valid UTF-8", test.inputs, name)
			}
			if len(name) > maxDatabaseNameLength {
				t.Fatalf("len(ResolveDatabaseName(%q)) = %d, want <= %d", test.inputs, len(name), maxDatabaseNameLength)
			}
			if name == "" {
				t.Fatalf("ResolveDatabaseName(%q) returned an empty name", test.inputs)
			}
			// A rune boundary truncation must keep every retained rune intact: re-encoding
			// the result and comparing to the normalized input prefix proves no partial rune.
			if !strings.HasPrefix(test.inputs, name) {
				t.Fatalf("ResolveDatabaseName(%q) = %q, want a prefix of the normalized input", test.inputs, name)
			}
		})
	}
}
