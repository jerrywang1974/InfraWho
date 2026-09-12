package search

import (
	"strings"
	"unicode"
)

// BuildFTSQuery turns a user q into an FTS5 MATCH expression.
// Tokens are AND-ed with prefix matching. Returns ok=false when nothing searchable remains.
func BuildFTSQuery(q string) (fts string, ok bool) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", false
	}
	// Split on any non-alnum so hostnames / kebab names become safe MATCH tokens.
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return r
		}
		return ' '
	}, q)

	var terms []string
	for _, tok := range strings.Fields(normalized) {
		if isFTSKeyword(tok) {
			continue
		}
		terms = append(terms, tok+"*")
	}
	if len(terms) == 0 {
		return "", false
	}
	return strings.Join(terms, " "), true
}

func isFTSKeyword(tok string) bool {
	switch strings.ToUpper(tok) {
	case "AND", "OR", "NOT", "NEAR":
		return true
	default:
		return false
	}
}
