package adapters

import (
	"strings"
	"unicode"
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
		name = strings.TrimRight(name[:maxDatabaseNameLength], "_")
	}
	if name == "" {
		return "db"
	}
	return name
}
