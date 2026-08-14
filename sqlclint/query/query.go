// Package query splits sqlc query files into individual named queries and
// parses each one with libpg_query.
package query

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	pganalyze "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// nameRe matches a sqlc query header, e.g. "-- name: GetToolset :one".
var nameRe = regexp.MustCompile(`(?m)^-- name: (\S+) :(\S+)[ \t]*\r?$`)

// Query is one named query from a sqlc query file.
type Query struct {
	// File is the path the query was read from, as supplied by the caller.
	File string

	// Name is the sqlc query name, e.g. "GetToolsetByID".
	Name string

	// Kind is the sqlc command, e.g. "one", "many", "exec", "execrows".
	Kind string

	// Comments are the "--" lines between the name header and the SQL, with the
	// leading "--" and one space stripped. sqlclint annotations live here, which
	// is also where sqlc picks up the generated method's doc comment.
	Comments []string

	// SQL is the query body with the comment block removed.
	SQL string

	// Line is the 1-indexed line of the "-- name:" header.
	Line int

	// SQLLine is the 1-indexed line where SQL begins, used to anchor diagnostics
	// at the statement rather than at its documentation.
	SQLLine int
}

// Split extracts every named query from a sqlc query file.
func Split(path string, src []byte) []Query {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	locs := nameRe.FindAllStringSubmatchIndex(text, -1)

	out := make([]Query, 0, len(locs))
	for i, loc := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}

		headerLine := strings.Count(text[:loc[0]], "\n") + 1
		comments, sql, sqlOffset := splitComments(text[loc[1]:end])

		out = append(out, Query{
			File:     path,
			Name:     text[loc[2]:loc[3]],
			Kind:     text[loc[4]:loc[5]],
			Comments: comments,
			SQL:      strings.TrimSpace(sql),
			Line:     headerLine,
			SQLLine:  headerLine + sqlOffset,
		})
	}
	return out
}

// splitComments peels the leading "--" block off a query body. It returns the
// comment text, the remaining SQL, and how many lines were consumed including
// the newline that ended the header.
func splitComments(rest string) (comments []string, sql string, lineOffset int) {
	lines := strings.Split(rest, "\n")

	i := 0
	// lines[0] is whatever followed the header on its own line, always empty.
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" && i == 0 {
			i++
			continue
		}
		if !strings.HasPrefix(trimmed, "--") {
			break
		}
		comments = append(comments, strings.TrimSpace(strings.TrimPrefix(trimmed, "--")))
		i++
	}

	return comments, strings.Join(lines[i:], "\n"), i
}

// Parse parses the query's SQL with the same Postgres grammar sqlc uses, so
// sqlc's own syntax needs no preprocessing: @name arrives as a unary "@"
// expression, sqlc.arg and sqlc.narg as schema-qualified function calls, and $1
// as a parameter reference.
func (q Query) Parse() (*pganalyze.ParseResult, error) {
	tree, err := pgquery.Parse(q.SQL)
	if err != nil {
		return nil, fmt.Errorf("parse query %s: %w", q.Name, err)
	}
	return tree, nil
}

// Hash fingerprints the query body so an ignore-file entry stays pinned to the
// SQL it was recorded against. Whitespace is normalized so reformatting alone
// does not invalidate an entry, while any change to the statement does.
func (q Query) Hash() string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(q.SQL), " ")))
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}

// Ref is the stable identity of a query within the repository, used as the key
// in the ignore file.
func (q Query) Ref() string { return q.File + "::" + q.Name }
