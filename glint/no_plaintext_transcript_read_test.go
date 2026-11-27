package glint

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoPlaintextTranscriptRead(t *testing.T) {
	t.Parallel()

	const reader = "github.com/speakeasy-api/gram/server/internal/plaintextread/reader"
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoPlaintextTranscriptReadAnalyzerWith(noPlaintextTranscriptReadSettings{}, noPlaintextTranscriptReadAllowlist{
		sanctioned: map[string]string{
			"(*" + reader + ".Service).Sanctioned":       "test",
			"(*" + reader + ".Service).Stale":            "test",
			"(*" + reader + ".Service).SanctionedInsert": "test",
			reader + ".Gone":                             "test",
		},
		pending: map[string]string{
			reader + ".Pending": "test",
		},
	}), reader)
}

func TestPendingPlaintextTranscriptReadersEmpty(t *testing.T) {
	t.Parallel()

	// The guard is complete only when every reader opens shadows. Entries
	// here are temporary; see pendingPlaintextTranscriptReaders.
	require.Empty(t, pendingPlaintextTranscriptReaders)
}

func TestSQLReadsPlaintextTranscript(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"SELECT m.content FROM chat_messages m":                                true,
		"SELECT id FROM chat_messages WHERE tool_calls IS NOT NULL":            true,
		"SELECT chat_messages.storage_error FROM chat_messages":                true,
		"SELECT id FROM chat_messages WHERE role = 'content'":                  false,
		"SELECT p.content FROM parts p JOIN chat_messages cm ON cm.id = p.mid": false,
		"SELECT cm.created_at AS content FROM chat_messages cm":                false,
		"INSERT INTO chat_messages (content, tool_calls) VALUES ($1, $2)":      false,
		"UPDATE chat_messages SET tool_call_summaries = $1 WHERE id = $2":      false,
		"UPDATE chat_messages SET x = 1 WHERE content_raw IS NULL":             true,
		"INSERT INTO chat_messages (content) VALUES ($1) RETURNING content":    true,
		"SELECT content FROM chat_summaries":                                   false,
		"-- name: X :one\nSELECT id FROM chat_messages -- content\n":           false,
		"SELECT sha256(convert_to(m.content, 'UTF8')) FROM chat_messages m":    true,

		// Quoted identifiers.
		`SELECT cm.content FROM "chat_messages" cm`:       true,
		`SELECT "cm"."content" FROM "chat_messages" "cm"`: true,
		`SELECT "content" FROM chat_messages`:             true,

		// Comments and strings are masked in one pass.
		"SELECT id FROM chat_messages WHERE role = '--' AND content = ''":                 true,
		"SELECT id FROM chat_messages -- it's\nWHERE content IS NULL":                     true,
		"SELECT cm.content FROM chat_messages /* recent */ cm":                            true,
		"SELECT id FROM chat_messages /* content */ WHERE id = $1":                        false,
		"SELECT id FROM chat_messages /* a /* nested */ content */ WHERE 1=1":             false,
		`SELECT id FROM chat_messages WHERE role = E'it\'s content'`:                      false,
		"SELECT id FROM chat_messages WHERE role LIKE '%x%' ESCAPE '\\' AND content = ''": true,

		// SET strips only each assignment's target.
		"UPDATE chat_messages SET role = CASE WHEN content = '' THEN 'x' ELSE role END WHERE id = $1": true,
		"UPDATE chat_messages SET x = (SELECT a FROM t), content = $1 WHERE id = $2":                  false,
		"UPDATE chat_messages SET x = (SELECT a FROM t WHERE b = content) WHERE id = $1":              true,
		"UPDATE chat_messages SET content = $1, tool_calls = $2 WHERE id = $3":                        false,
		"UPDATE chat_messages SET x = coalesce(y, 1), tool_calls = $2 WHERE id = $3":                  false,
		"UPDATE chat_messages SET x = left(content, 4) WHERE id = $1":                                 true,

		// Stars select every column.
		"SELECT * FROM chat_messages WHERE id = $1":                      true,
		"SELECT cm.* FROM chat_messages cm":                              true,
		"SELECT p.* FROM parts p JOIN chat_messages cm ON cm.id = p.mid": false,
		"SELECT count(*) FROM chat_messages WHERE id = $1":               false,
		"UPDATE chat_messages SET role = $1 WHERE id = $2 RETURNING *":   true,
	}
	for sql, want := range cases {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, want, sqlReadsPlaintextTranscript(sql), sql)
		})
	}
}

func TestFuncPackagePath(t *testing.T) {
	t.Parallel()

	require.Equal(t, "github.com/x/y.z/p", funcPackagePath("(*github.com/x/y.z/p.T).M"))
	require.Equal(t, "github.com/x/y.z/p", funcPackagePath("(github.com/x/y.z/p.T).M"))
	require.Equal(t, "github.com/x/y.z/p", funcPackagePath("github.com/x/y.z/p.F"))
}

func TestSQLUpdatesPlaintextTranscript(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"UPDATE chat_messages SET tool_call_summaries = $1 WHERE id = $2":                                                            true,
		"UPDATE chat_messages AS m SET content = $1, finish_reason = $2 WHERE m.id = $3":                                             true,
		"UPDATE chat_messages SET tool_outcome = $1, tool_outcome_notes = $2 WHERE id = $3":                                          true,
		`UPDATE "chat_messages" SET "storage_error" = $1 WHERE id = $2`:                                                              true,
		"INSERT INTO chat_messages (id, content) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET content = EXCLUDED.content":          true,
		"INSERT INTO chat_messages (id, content) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET source = EXCLUDED.source":            false,
		"INSERT INTO chat_messages (content, tool_calls) VALUES ($1, $2)":                                                            false,
		"UPDATE chat_messages SET risk_analyzed_at = now() WHERE content IS NULL":                                                    false,
		"UPDATE chat_summaries SET content = $1 WHERE id = $2":                                                                       false,
		"UPDATE chat_messages SET tool_call_summaries = jsonb_set(COALESCE(tool_call_summaries, '{}'), ARRAY[$1], $2) WHERE id = $3": true,
		"-- name: X :exec\n-- SET content = $1\nUPDATE chat_messages SET risk_analyzed_at = now() WHERE id = $1":                     false,
	}
	for sql, want := range cases {
		require.Equal(t, want, sqlUpdatesPlaintextTranscript(sql), sql)
	}
}

func TestSQLInsertsPlaintextTranscript(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"INSERT INTO chat_messages (chat_id, content, tool_calls) VALUES ($1, $2, $3)":  true,
		`INSERT INTO "chat_messages" ("id", "storage_error") VALUES ($1, $2)`:           true,
		"INSERT INTO chat_messages (chat_id, role) VALUES ($1, $2)":                     false,
		"INSERT INTO chat_message_transcripts (message_id, ciphertext) VALUES ($1, $2)": false,
		"UPDATE chat_messages SET content = $1 WHERE id = $2":                           false,
	}
	for sql, want := range cases {
		require.Equal(t, want, sqlInsertsPlaintextTranscript(sql), sql)
	}
}

// A dependency read from export data built with -trimpath reports its files
// by import path. golangci-lint reads one that way when its own results are
// cached, so the rule must still find the generated SQL on disk; otherwise a
// cached run sees no plaintext use and reports live allowlist entries as stale.
func TestTrimmedPathOnDisk(t *testing.T) {
	t.Parallel()

	const module = "github.com/speakeasy-api/gram/server"
	src, err := filepath.Abs(filepath.Join(analysistest.TestData(), "src"))
	require.NoError(t, err)
	readerDir := filepath.Join(src, filepath.FromSlash(module+"/internal/plaintextread/reader"))
	repoFile := filepath.Join(src, filepath.FromSlash(module+"/internal/plaintextread/repo/queries.sql.go"))
	trimmed := module + "/internal/plaintextread/repo/queries.sql.go"

	got := trimmedPathOnDisk(trimmed, module+"/internal/plaintextread/reader", readerDir)
	require.Equal(t, repoFile, got)
	require.Equal(t, plaintextTranscriptInsert, parseSqlcQueries(got)["CreateMessage"], "the mapped file is the generated SQL")

	require.Equal(t, repoFile, trimmedPathOnDisk(trimmed, module+"/internal/plaintextread/reader_test", readerDir), "an external test package lives in the same directory")
	require.Empty(t, trimmedPathOnDisk("example.com/other/repo/queries.sql.go", module+"/internal/plaintextread/reader", readerDir), "another module does not map")
	require.Empty(t, parseSqlcQueries(trimmed), "the trimmed name alone is not a file, which is how cached runs lost the SQL")
}
