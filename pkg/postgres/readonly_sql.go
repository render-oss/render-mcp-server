package postgres

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var disallowedReadOnlySQLTokens = map[string]struct{}{
	"alter": {}, "analyze": {}, "call": {}, "cluster": {}, "comment": {},
	"copy": {}, "create": {}, "deallocate": {}, "delete": {}, "discard": {}, "do": {},
	"drop": {}, "execute": {}, "for": {}, "grant": {}, "insert": {}, "into": {},
	"listen": {}, "lock": {}, "lo_export": {}, "lo_import": {}, "lo_unlink": {},
	"merge": {}, "nextval": {}, "notify": {}, "pg_advisory_lock": {},
	"pg_advisory_lock_shared": {}, "pg_advisory_xact_lock": {},
	"pg_advisory_xact_lock_shared": {}, "pg_cancel_backend": {}, "pg_create_restore_point": {},
	"pg_logical_emit_message": {}, "pg_notify": {}, "pg_reload_conf": {},
	"pg_rotate_logfile": {}, "pg_switch_wal": {}, "pg_terminate_backend": {},
	"pg_try_advisory_lock": {}, "pg_try_advisory_lock_shared": {},
	"pg_try_advisory_xact_lock": {}, "pg_try_advisory_xact_lock_shared": {},
	"prepare": {}, "refresh": {}, "reindex": {}, "reset": {}, "revoke": {},
	"security": {}, "set": {}, "set_config": {}, "setval": {}, "truncate": {},
	"unlisten": {}, "update": {}, "vacuum": {},
}

var allowedReadOnlySQLStarts = map[string]struct{}{
	"select": {}, "show": {}, "table": {}, "values": {}, "with": {},
}

// validateReadOnlySQL rejects write-shaped and multi-statement SQL before a
// database connection is opened. PostgreSQL's READ ONLY transaction remains
// the authoritative backstop for effects hidden behind functions or views.
func validateReadOnlySQL(query string) error {
	tokens, statements, err := scanSQL(query)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return errors.New("SQL query must not be empty")
	}
	if statements > 1 {
		return errors.New("SQL query must contain exactly one statement")
	}
	if _, ok := allowedReadOnlySQLStarts[tokens[0]]; !ok {
		return fmt.Errorf("SQL statement %q is not allowed; use a read-only SELECT, WITH, SHOW, TABLE, or VALUES query", tokens[0])
	}
	for _, token := range tokens {
		if _, disallowed := disallowedReadOnlySQLTokens[token]; disallowed {
			return fmt.Errorf("SQL token %q is not allowed in a read-only query", token)
		}
	}
	return nil
}

// scanSQL returns unquoted identifier tokens and the number of statements.
// It understands PostgreSQL comments, quoted identifiers, string literals,
// and dollar-quoted strings so their contents cannot masquerade as SQL.
func scanSQL(query string) ([]string, int, error) {
	var tokens []string
	statements := 1
	seenTerminator := false

	for i := 0; i < len(query); {
		ch := query[i]
		switch {
		case unicode.IsSpace(rune(ch)):
			i++
		case ch == '-' && i+1 < len(query) && query[i+1] == '-':
			i += 2
			for i < len(query) && query[i] != '\n' {
				i++
			}
		case ch == '/' && i+1 < len(query) && query[i+1] == '*':
			var err error
			i, err = skipBlockComment(query, i)
			if err != nil {
				return nil, 0, err
			}
		case ch == '\'':
			var err error
			i, err = skipQuoted(query, i, '\'')
			if err != nil {
				return nil, 0, err
			}
			if seenTerminator {
				statements = 2
			}
		case ch == '"':
			var err error
			i, err = skipQuoted(query, i, '"')
			if err != nil {
				return nil, 0, err
			}
			if seenTerminator {
				statements = 2
			}
		case ch == '$':
			end, ok, err := skipDollarQuoted(query, i)
			if err != nil {
				return nil, 0, err
			}
			if ok {
				i = end
				if seenTerminator {
					statements = 2
				}
				continue
			}
			i++
		case ch == ';':
			if seenTerminator {
				statements = 2
			}
			seenTerminator = true
			i++
		case isIdentifierStart(ch):
			start := i
			i++
			for i < len(query) && isIdentifierPart(query[i]) {
				i++
			}
			if seenTerminator {
				statements = 2
			}
			tokens = append(tokens, strings.ToLower(query[start:i]))
		default:
			if seenTerminator {
				statements = 2
			}
			i++
		}
	}
	return tokens, statements, nil
}

func skipQuoted(query string, start int, quote byte) (int, error) {
	for i := start + 1; i < len(query); i++ {
		if query[i] != quote {
			continue
		}
		if i+1 < len(query) && query[i+1] == quote {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, errors.New("SQL query contains an unterminated quoted value")
}

func skipBlockComment(query string, start int) (int, error) {
	depth := 1
	for i := start + 2; i < len(query); {
		switch {
		case i+1 < len(query) && query[i:i+2] == "/*":
			depth++
			i += 2
		case i+1 < len(query) && query[i:i+2] == "*/":
			depth--
			i += 2
			if depth == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	return 0, errors.New("SQL query contains an unterminated block comment")
}

func skipDollarQuoted(query string, start int) (int, bool, error) {
	endTag := start + 1
	for endTag < len(query) && (isIdentifierPart(query[endTag]) || query[endTag] == '$') {
		if query[endTag] == '$' {
			delimiter := query[start : endTag+1]
			closing := strings.Index(query[endTag+1:], delimiter)
			if closing < 0 {
				return 0, true, errors.New("SQL query contains an unterminated dollar-quoted value")
			}
			return endTag + 1 + closing + len(delimiter), true, nil
		}
		endTag++
	}
	return start, false, nil
}

func isIdentifierStart(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func isIdentifierPart(ch byte) bool {
	return isIdentifierStart(ch) || ch >= '0' && ch <= '9' || ch == '$'
}
