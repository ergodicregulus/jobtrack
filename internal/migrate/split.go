package migrate

import "strings"

// splitStatements breaks a SQL file into individual statements.
//
// This exists for one specific reason. When several statements are sent in a
// single query string, PostgreSQL executes them inside an *implicit*
// transaction block — so `CREATE INDEX CONCURRENTLY` fails with
// "cannot run inside a transaction block" even when the client never began one.
// Non-transactional migrations must therefore send one statement per round trip.
//
// The parser has to understand quoting or it will split inside a string that
// happens to contain a semicolon. It handles:
//
//	'single quoted'   with '' escaping
//	"identifiers"     with "" escaping
//	$$ … $$           and $tag$ … $tag$ dollar quoting (DO blocks, functions)
//	-- line comments
//	/* block comments */  (non-nesting, which matches Postgres for our purposes)
func splitStatements(sql string) []string {
	var (
		out     []string
		current strings.Builder
	)

	runes := []rune(sql)
	i, n := 0, len(runes)

	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			out = append(out, s)
		}
		current.Reset()
	}

	for i < n {
		c := runes[i]

		switch {
		// -- line comment
		case c == '-' && i+1 < n && runes[i+1] == '-':
			for i < n && runes[i] != '\n' {
				current.WriteRune(runes[i])
				i++
			}

		// /* block comment */
		case c == '/' && i+1 < n && runes[i+1] == '*':
			current.WriteString("/*")
			i += 2
			for i < n {
				if runes[i] == '*' && i+1 < n && runes[i+1] == '/' {
					current.WriteString("*/")
					i += 2
					break
				}
				current.WriteRune(runes[i])
				i++
			}

		// 'string literal', with '' as an escaped quote
		case c == '\'':
			current.WriteRune(c)
			i++
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						current.WriteString("''")
						i += 2
						continue
					}
					current.WriteRune('\'')
					i++
					break
				}
				current.WriteRune(runes[i])
				i++
			}

		// "quoted identifier", with "" as an escaped quote
		case c == '"':
			current.WriteRune(c)
			i++
			for i < n {
				if runes[i] == '"' {
					if i+1 < n && runes[i+1] == '"' {
						current.WriteString(`""`)
						i += 2
						continue
					}
					current.WriteRune('"')
					i++
					break
				}
				current.WriteRune(runes[i])
				i++
			}

		// $$ … $$ or $tag$ … $tag$
		case c == '$':
			if tag, ok := dollarTag(runes, i); ok {
				current.WriteString(tag)
				i += len([]rune(tag))
				for i < n {
					if runes[i] == '$' {
						if closing, ok := dollarTag(runes, i); ok && closing == tag {
							current.WriteString(tag)
							i += len([]rune(tag))
							break
						}
					}
					current.WriteRune(runes[i])
					i++
				}
			} else {
				current.WriteRune(c)
				i++
			}

		case c == ';':
			current.WriteRune(c)
			flush()
			i++

		default:
			current.WriteRune(c)
			i++
		}
	}

	flush()
	return out
}

// dollarTag reads a dollar-quote delimiter starting at i, e.g. `$$` or `$fn$`.
// Returns the literal delimiter and whether one was found.
func dollarTag(runes []rune, i int) (string, bool) {
	if runes[i] != '$' {
		return "", false
	}
	j := i + 1
	for j < len(runes) {
		c := runes[j]
		if c == '$' {
			return string(runes[i : j+1]), true
		}
		// Tags are identifiers: letters, digits and underscore, not starting
		// with a digit. Anything else means this `$` is a parameter or operator.
		isIdent := c == '_' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')
		if !isIdent || (j == i+1 && c >= '0' && c <= '9') {
			return "", false
		}
		j++
	}
	return "", false
}
