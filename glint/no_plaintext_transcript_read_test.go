package glint

import (
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
			"(*" + reader + ".Service).Sanctioned": "test",
			"(*" + reader + ".Service).Stale":      "test",
			reader + ".Gone":                       "test",
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
	}
	for sql, want := range cases {
		require.Equal(t, want, sqlReadsPlaintextTranscript(sql), sql)
	}
}

func TestFuncPackagePath(t *testing.T) {
	t.Parallel()

	require.Equal(t, "github.com/x/y.z/p", funcPackagePath("(*github.com/x/y.z/p.T).M"))
	require.Equal(t, "github.com/x/y.z/p", funcPackagePath("(github.com/x/y.z/p.T).M"))
	require.Equal(t, "github.com/x/y.z/p", funcPackagePath("github.com/x/y.z/p.F"))
}
