package killswitches

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var errLifecycleTransactionQueryRejected = errors.New("lifecycle transaction query rejected")

// lifecycleTransactionQueries is a correctness capability, not an adversarial SQL sandbox.
// Collaborators remain trusted to issue domain SQL, but cannot accidentally complete the lifecycle
// transaction or smuggle transaction control through a multi-statement query.
type lifecycleTransactionQueries struct {
	db LifecycleTransactionQueries
}

func (q lifecycleTransactionQueries) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if err := validateLifecycleTransactionCall(sql, arguments); err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := q.db.Exec(ctx, sql, arguments...)
	if err != nil {
		return tag, fmt.Errorf("execute lifecycle transaction query: %w", err)
	}
	return tag, nil
}

func (q lifecycleTransactionQueries) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := validateLifecycleTransactionCall(sql, args); err != nil {
		return nil, err
	}
	rows, err := q.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query lifecycle transaction: %w", err)
	}
	return rows, nil
}

func (q lifecycleTransactionQueries) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := validateLifecycleTransactionCall(sql, args); err != nil {
		return rejectedLifecycleQueryRow{err: err}
	}
	return lifecycleQueryRow{row: q.db.QueryRow(ctx, sql, args...)}
}

type lifecycleQueryRow struct {
	row pgx.Row
}

func (r lifecycleQueryRow) Scan(destinations ...any) error {
	if err := r.row.Scan(destinations...); err != nil {
		return fmt.Errorf("scan lifecycle transaction query row: %w", err)
	}
	return nil
}

type rejectedLifecycleQueryRow struct {
	err error
}

func (r rejectedLifecycleQueryRow) Scan(...any) error {
	return r.err
}

func validateLifecycleTransactionCall(sql string, args []any) error {
	for _, arg := range args {
		if _, ok := arg.(pgx.QueryRewriter); ok {
			return fmt.Errorf("%w: query rewriters are not allowed", errLifecycleTransactionQueryRejected)
		}
	}
	return validateLifecycleTransactionSQL(sql)
}

func validateLifecycleTransactionSQL(sql string) error {
	keywords, err := lifecycleSQLKeywords(sql)
	if err != nil {
		return err
	}
	first := keywords[0]
	second := keywords[1]
	switch first {
	case "ABORT", "BEGIN", "COMMIT", "END", "RELEASE", "ROLLBACK", "SAVEPOINT":
		return fmt.Errorf("%w: transaction-control statement %s is not allowed", errLifecycleTransactionQueryRejected, first)
	case "PREPARE", "SET":
		if second == "TRANSACTION" {
			return fmt.Errorf("%w: transaction-control statement %s TRANSACTION is not allowed", errLifecycleTransactionQueryRejected, first)
		}
	case "START":
		return fmt.Errorf("%w: transaction-control statement START is not allowed", errLifecycleTransactionQueryRejected)
	}
	return nil
}

func lifecycleSQLKeywords(sql string) ([2]string, error) {
	var keywords [2]string
	keywordCount := 0
	hasContent := false
	terminated := false

	for i := 0; i < len(sql); {
		switch {
		case isLifecycleSQLSpace(sql[i]):
			i++
		case i+1 < len(sql) && sql[i:i+2] == "--":
			i += 2
			for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
		case i+1 < len(sql) && sql[i:i+2] == "/*":
			next, ok := skipLifecycleSQLBlockComment(sql, i)
			if !ok {
				return keywords, fmt.Errorf("%w: unterminated block comment", errLifecycleTransactionQueryRejected)
			}
			i = next
		case sql[i] == ';':
			if !hasContent {
				return keywords, fmt.Errorf("%w: empty SQL statement", errLifecycleTransactionQueryRejected)
			}
			if terminated {
				return keywords, fmt.Errorf("%w: multiple SQL statements are not allowed", errLifecycleTransactionQueryRejected)
			}
			terminated = true
			i++
		default:
			if terminated {
				return keywords, fmt.Errorf("%w: multiple SQL statements are not allowed", errLifecycleTransactionQueryRejected)
			}
			hasContent = true
			switch {
			case sql[i] == '\'':
				escape := i > 0 && (sql[i-1] == 'e' || sql[i-1] == 'E') && (i < 2 || !isLifecycleSQLIdentifierPart(sql[i-2]))
				next, ok := skipLifecycleSQLQuoted(sql, i, '\'', escape)
				if !ok {
					return keywords, fmt.Errorf("%w: unterminated string literal", errLifecycleTransactionQueryRejected)
				}
				i = next
			case sql[i] == '"':
				next, ok := skipLifecycleSQLQuoted(sql, i, '"', false)
				if !ok {
					return keywords, fmt.Errorf("%w: unterminated quoted identifier", errLifecycleTransactionQueryRejected)
				}
				i = next
			case sql[i] == '$':
				delimiter, ok := lifecycleSQLDollarDelimiter(sql, i)
				if !ok {
					i++
					continue
				}
				end := strings.Index(sql[i+len(delimiter):], delimiter)
				if end < 0 {
					return keywords, fmt.Errorf("%w: unterminated dollar-quoted string", errLifecycleTransactionQueryRejected)
				}
				i += len(delimiter) + end + len(delimiter)
			case isLifecycleSQLIdentifierStart(sql[i]):
				start := i
				for i < len(sql) && isLifecycleSQLIdentifierPart(sql[i]) {
					i++
				}
				if keywordCount < len(keywords) {
					keywords[keywordCount] = strings.ToUpper(sql[start:i])
					keywordCount++
				}
			default:
				i++
			}
		}
	}
	if !hasContent {
		return keywords, fmt.Errorf("%w: empty SQL statement", errLifecycleTransactionQueryRejected)
	}
	return keywords, nil
}

func skipLifecycleSQLBlockComment(sql string, start int) (int, bool) {
	depth := 1
	for i := start + 2; i < len(sql); {
		switch {
		case i+1 < len(sql) && sql[i:i+2] == "/*":
			depth++
			i += 2
		case i+1 < len(sql) && sql[i:i+2] == "*/":
			depth--
			i += 2
			if depth == 0 {
				return i, true
			}
		default:
			i++
		}
	}
	return len(sql), false
}

func skipLifecycleSQLQuoted(sql string, start int, quote byte, backslashEscapes bool) (int, bool) {
	for i := start + 1; i < len(sql); {
		if backslashEscapes && sql[i] == '\\' {
			i += 2
			continue
		}
		if sql[i] != quote {
			i++
			continue
		}
		if i+1 < len(sql) && sql[i+1] == quote {
			i += 2
			continue
		}
		return i + 1, true
	}
	return len(sql), false
}

func lifecycleSQLDollarDelimiter(sql string, start int) (string, bool) {
	if start+1 >= len(sql) {
		return "", false
	}
	if sql[start+1] == '$' {
		return "$$", true
	}
	if !isLifecycleSQLIdentifierStart(sql[start+1]) {
		return "", false
	}
	for i := start + 2; i < len(sql); i++ {
		if sql[i] == '$' {
			return sql[start : i+1], true
		}
		if !isLifecycleSQLIdentifierPart(sql[i]) {
			return "", false
		}
	}
	return "", false
}

func isLifecycleSQLSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f' || value == '\v'
}

func isLifecycleSQLIdentifierStart(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value == '_' || value >= 0x80
}

func isLifecycleSQLIdentifierPart(value byte) bool {
	return isLifecycleSQLIdentifierStart(value) || value >= '0' && value <= '9' || value == '$'
}
