package chatredact_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// wordScanner flags every occurrence of word, or fails when failing is set.
type wordScanner struct {
	word    string
	failing bool
}

func (w wordScanner) Scan(_ context.Context, content string) (scanners.Result, error) {
	if w.failing {
		return scanners.Result{Findings: nil, STokens: 0, Completed: false}, nil
	}
	var findings []scanners.Finding
	for offset := 0; ; {
		i := strings.Index(content[offset:], w.word)
		if i < 0 {
			break
		}
		start := offset + i
		findings = append(findings, scanners.Finding{RuleID: "test", Match: w.word, StartPos: start, EndPos: start + len(w.word)})
		offset = start + len(w.word)
	}
	return scanners.Result{Findings: findings, STokens: 0, Completed: true}, nil
}

func TestTextMasksFindings(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "hunter2"})

	safe, changed, err := r.Text(t.Context(), "pw is hunter2, again hunter2")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "pw is [REDACTED], again [REDACTED]", safe)

	clean, changed, err := r.Text(t.Context(), "nothing to see")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "nothing to see", clean)
}

func TestTextFailsClosedWhenScanIncomplete(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "", failing: true})

	_, _, err := r.Text(t.Context(), "anything")
	require.True(t, errors.Is(err, chatredact.ErrScanFailed))
}

func TestTextRedactsSensitiveJSONKeysAndKeepsTokenCounts(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "zzz-unused"})

	safe, changed, err := r.Text(t.Context(), `{"api_key":"abc","prompt_tokens":12,"nested":{"password":"p"},"note":"ok"}`)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, safe, `"api_key":"[REDACTED]"`)
	require.Contains(t, safe, `"password":"[REDACTED]"`)
	require.Contains(t, safe, `"prompt_tokens":12`)
	require.Contains(t, safe, `"note":"ok"`)
}

func TestTextWithholdsAttachments(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "zzz-unused"})

	safe, changed, err := r.Text(t.Context(), `[{"type":"image","source":{"data":"base64"}}]`)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, `["[WITHHELD_ATTACHMENT]"]`, safe)
}

func TestMessageRedactsToolCallsAndRaw(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "hunter2"})

	text, changed, err := r.Message(t.Context(), "hello", []byte(`[{"arguments":"hunter2"}]`), []byte("raw hunter2"))
	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, text, "hunter2")
	require.Contains(t, text, "tool_calls: ")
	require.Contains(t, text, "content_raw: raw [REDACTED]")
}

func TestNewUsesGitleaks(t *testing.T) {
	t.Parallel()
	r := chatredact.New()

	// Assembled at runtime so no literal credential lives in the repository.
	key := "ghp_" + strings.Repeat("aB3dE5fG7h", 3) + "123456"
	safe, changed, err := r.Text(t.Context(), "token "+key+" end")
	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, safe, key)
}
