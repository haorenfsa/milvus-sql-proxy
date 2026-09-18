package pkg

import (
	"fmt"
	"strconv"
	"strings"
)

func sqlLiteral(v interface{}) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case string:
		return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'", "\x00", "\\0").Replace(x) + "'", nil
	case []byte:
		return sqlLiteral(string(x))
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.Itoa(x), nil
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(x), nil
	default:
		return "", fmt.Errorf("unsupported parameter type %T", v)
	}
}

// bindSQL scans lexical tokens, never replacing placeholders in literals or
// comments. PostgreSQL quoted identifiers are normalized for the shared parser.
func bindSQL(q, dialect string, args []interface{}, describe bool) (string, int, error) {
	var b strings.Builder
	count := 0
	for i := 0; i < len(q); {
		ch := q[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			quote := ch
			j := i + 1
			var value strings.Builder
			closed := false
			for j < len(q) {
				if q[j] == quote {
					if j+1 < len(q) && q[j+1] == quote {
						value.WriteByte(quote)
						j += 2
						continue
					}
					j++
					closed = true
					break
				}
				if q[j] == '\\' && dialect == "mysql" && j+1 < len(q) {
					j += 2
					continue
				}
				value.WriteByte(q[j])
				j++
			}
			if !closed {
				return "", 0, fmt.Errorf("unterminated quoted token")
			}
			if dialect == "postgres" {
				if quote == '"' {
					b.WriteByte('`')
					b.WriteString(strings.ReplaceAll(value.String(), "`", "``"))
					b.WriteByte('`')
				} else if quote == '\'' {
					lit, _ := sqlLiteral(value.String())
					b.WriteString(lit)
				} else {
					return "", 0, fmt.Errorf("use double quotes for PostgreSQL identifiers")
				}
			} else {
				b.WriteString(q[i:j])
			}
			i = j
			continue
		}
		if i+1 < len(q) && q[i:i+2] == "--" {
			j := strings.IndexByte(q[i:], '\n')
			if j < 0 {
				b.WriteString(q[i:])
				break
			}
			b.WriteString(q[i : i+j+1])
			i += j + 1
			continue
		}
		if i+1 < len(q) && q[i:i+2] == "/*" {
			j := strings.Index(q[i+2:], "*/")
			if j < 0 {
				return "", 0, fmt.Errorf("unterminated comment")
			}
			j += i + 4
			b.WriteString(q[i:j])
			i = j
			continue
		}
		idx := 0
		end := i + 1
		if dialect == "mysql" && ch == '?' {
			count++
			idx = count
		}
		if dialect == "postgres" && ch == '$' {
			for end < len(q) && q[end] >= '0' && q[end] <= '9' {
				end++
			}
			if end == i+1 {
				return "", 0, fmt.Errorf("dollar-quoted strings are not supported")
			}
			n, e := strconv.Atoi(q[i+1 : end])
			if e != nil || n < 1 || n > 65535 {
				return "", 0, fmt.Errorf("invalid parameter number")
			}
			idx = n
			if n > count {
				count = n
			}
		}
		if idx > 0 {
			if describe {
				b.WriteString("0")
			} else {
				if idx > len(args) {
					return "", 0, fmt.Errorf("missing parameter %d", idx)
				}
				v, e := sqlLiteral(args[idx-1])
				if e != nil {
					return "", 0, e
				}
				b.WriteString(v)
			}
			i = end
			continue
		}
		b.WriteByte(ch)
		i++
	}
	if !describe && count != len(args) {
		return "", 0, fmt.Errorf("expected %d parameters, got %d", count, len(args))
	}
	return b.String(), count, nil
}

// The legacy grammar recognizes BOOL as a keyword but not a column type.
// Rewrite type aliases only outside quoted tokens; VARCHAR contents are intact.
func normalizeTypes(q string) string {
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(q)), "CREATE TABLE") {
		return q
	}
	var b strings.Builder
	for i := 0; i < len(q); {
		if q[i] == '\'' || q[i] == '"' || q[i] == '`' {
			quote := q[i]
			j := i + 1
			for j < len(q) {
				if q[j] == '\\' && j+1 < len(q) {
					j += 2
					continue
				}
				if q[j] == quote {
					j++
					if j < len(q) && q[j] == quote {
						j++
						continue
					}
					break
				}
				j++
			}
			b.WriteString(q[i:j])
			i = j
			continue
		}
		if q[i] >= 'a' && q[i] <= 'z' || q[i] >= 'A' && q[i] <= 'Z' || q[i] == '_' {
			j := i + 1
			for j < len(q) && (q[j] >= 'a' && q[j] <= 'z' || q[j] >= 'A' && q[j] <= 'Z' || q[j] >= '0' && q[j] <= '9' || q[j] == '_') {
				j++
			}
			token := q[i:j]
			switch strings.ToLower(token) {
			case "bool", "boolean":
				token = "tinyint(1)"
			}
			b.WriteString(token)
			i = j
			continue
		}
		b.WriteByte(q[i])
		i++
	}
	return b.String()
}
