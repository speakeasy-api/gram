package query_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sqlclint/query"
)

const twoQueries = `-- Leading file comment, attached to nothing.

-- name: GetToolset :one
-- Fetches one toolset.
-- Second doc line.
SELECT * FROM toolsets
WHERE id = @id AND project_id = @project_id;

-- name: DeleteToolset :execrows
DELETE FROM toolsets WHERE id = @id AND project_id = @project_id;
`

func TestSplitReadsNameKindAndBody(t *testing.T) {
	t.Parallel()

	got := query.Split("queries.sql", []byte(twoQueries))
	require.Len(t, got, 2)

	require.Equal(t, "GetToolset", got[0].Name)
	require.Equal(t, "one", got[0].Kind)
	require.Equal(t, []string{"Fetches one toolset.", "Second doc line."}, got[0].Comments)
	require.Contains(t, got[0].SQL, "SELECT * FROM toolsets")
	require.NotContains(t, got[0].SQL, "Fetches one toolset")

	require.Equal(t, "DeleteToolset", got[1].Name)
	require.Equal(t, "execrows", got[1].Kind)
	require.Empty(t, got[1].Comments)
}

// Diagnostics are only useful if they point at the right line.
func TestSplitRecordsHeaderAndSQLLines(t *testing.T) {
	t.Parallel()

	got := query.Split("queries.sql", []byte(twoQueries))

	require.Equal(t, 3, got[0].Line)
	require.Equal(t, 6, got[0].SQLLine)
	require.Equal(t, 9, got[1].Line)
	require.Equal(t, 10, got[1].SQLLine)
}

func TestSplitHandlesCarriageReturns(t *testing.T) {
	t.Parallel()

	got := query.Split("queries.sql", []byte("-- name: GetToolset :one\r\nSELECT 1;\r\n"))
	require.Len(t, got, 1)
	require.Equal(t, "GetToolset", got[0].Name)
	require.Equal(t, "SELECT 1;", got[0].SQL)
}

func TestSplitIgnoresTextThatMerelyMentionsAName(t *testing.T) {
	t.Parallel()

	got := query.Split("queries.sql", []byte("-- see -- name: NotAQuery :one for details\nSELECT 1;\n"))
	require.Empty(t, got)
}

func TestRefCombinesFileAndName(t *testing.T) {
	t.Parallel()

	got := query.Split("server/internal/toolsets/queries.sql", []byte(twoQueries))
	require.Equal(t, "server/internal/toolsets/queries.sql::GetToolset", got[0].Ref())
}

// Reformatting must not invalidate an ignore entry, or every whitespace change
// would re-raise unrelated violations.
func TestHashIgnoresWhitespaceButNotContent(t *testing.T) {
	t.Parallel()

	base := query.Split("q.sql", []byte("-- name: A :one\nSELECT *\nFROM toolsets WHERE id = @id;\n"))[0]
	reformatted := query.Split("q.sql", []byte("-- name: A :one\nSELECT * FROM toolsets\n    WHERE id = @id;\n"))[0]
	changed := query.Split("q.sql", []byte("-- name: A :one\nSELECT * FROM toolsets WHERE id = @other;\n"))[0]

	require.Equal(t, base.Hash(), reformatted.Hash())
	require.NotEqual(t, base.Hash(), changed.Hash())
}

// A comment is not part of the SQL, so editing documentation must not look like
// editing the statement.
func TestHashIgnoresComments(t *testing.T) {
	t.Parallel()

	a := query.Split("q.sql", []byte("-- name: A :one\n-- one note\nSELECT 1;\n"))[0]
	b := query.Split("q.sql", []byte("-- name: A :one\n-- a different note\nSELECT 1;\n"))[0]

	require.Equal(t, a.Hash(), b.Hash())
}

func TestParseRejectsInvalidSQL(t *testing.T) {
	t.Parallel()

	got := query.Split("q.sql", []byte("-- name: A :one\nSELECT * FROM toolsets WHERE id = @id AND;\n"))[0]
	_, err := got.Parse()
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse query A")
}
