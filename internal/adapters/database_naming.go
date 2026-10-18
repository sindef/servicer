package adapters

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// DatabaseNameParameter is the normalized parameter name exposed by relational products.
	DatabaseNameParameter = "databaseName"
	maxDatabaseNameLength = 63
)

// ResolveDatabaseName returns the normalized database name for a relational service.
// Explicit requests are honored after normalization; empty requests derive from the instance name.
func ResolveDatabaseName(instanceName, requested string) string {
	base := strings.TrimSpace(requested)
	if base == "" {
		base = instanceName
	}

	var b strings.Builder
	b.Grow(len(base))
	lastUnderscore := false

	for _, r := range strings.ToLower(base) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "db"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "db_" + name
	}
	if len(name) > maxDatabaseNameLength {
		// Truncate on a rune boundary: a byte slice at 63 can split a multi-byte
		// rune and emit invalid UTF-8, which must never reach a database or
		// Kubernetes identifier.
		var truncated strings.Builder
		truncated.Grow(maxDatabaseNameLength)
		size := 0
		for _, r := range name {
			if size+utf8.RuneLen(r) > maxDatabaseNameLength {
				break
			}
			truncated.WriteRune(r)
			size += utf8.RuneLen(r)
		}
		name = strings.TrimRight(truncated.String(), "_")
	}
	if name == "" {
		return "db"
	}
	return name
}
