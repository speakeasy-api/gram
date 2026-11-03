package hooks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// claudeTranscriptFixture covers the entry shapes Claude Code writes: string
// and array user content, assistant lines split per content block, tool
// results with string and array content, and entries the intake must skip.
func claudeTranscriptFixture() []string {
	return []string{
		`{"type":"session-owner","sessionId":"s"}`,
		`{"type":"user","uuid":"u-1","timestamp":"2026-11-03T10:00:00Z","isSidechain":false,"message":{"role":"user","content":"list the files"}}`,
		`{"type":"user","uuid":"u-meta","timestamp":"2026-11-03T10:00:00.5Z","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>"}}`,
		`{"type":"assistant","uuid":"a-1","timestamp":"2026-11-03T10:00:01Z","message":{"id":"msg_1","model":"claude-opus-5-5","role":"assistant","content":[{"type":"thinking","thinking":"hidden"}]}}`,
		`{"type":"assistant","uuid":"a-2","timestamp":"2026-11-03T10:00:02Z","message":{"id":"msg_1","model":"claude-opus-5-5","role":"assistant","content":[{"type":"text","text":"Listing them now."}]}}`,
		`{"type":"assistant","uuid":"a-3","timestamp":"2026-11-03T10:00:03Z","message":{"id":"msg_1","model":"claude-opus-5-5","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","uuid":"u-2","timestamp":"2026-11-03T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a.txt\nb.txt"}]}}`,
		`{"type":"assistant","uuid":"a-side","timestamp":"2026-11-03T10:00:04.5Z","isSidechain":true,"message":{"id":"msg_s","role":"assistant","content":[{"type":"text","text":"subagent"}]}}`,
		`{"type":"user","uuid":"u-3","timestamp":"2026-11-03T10:00:05Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"line one"},{"type":"image","source":{}}]}]}}`,
		`{"type":"assistant","uuid":"a-4","timestamp":"2026-11-03T10:00:06Z","message":{"id":"msg_2","model":"claude-opus-5-5","role":"assistant","content":[{"type":"text","text":"Two files."}]}}`,
		`{"type":"user","uuid":"u-4","timestamp":"2026-11-03T10:00:07Z","isCompactSummary":true,"message":{"role":"user","content":"summary of earlier turns"}}`,
		`{"type":"summary","summary":"Listing files","leafUuid":"a-4"}`,
		`not json`,
	}
}

func rawEntries(lines []string) []json.RawMessage {
	out := make([]json.RawMessage, len(lines))
	for i, line := range lines {
		out[i] = json.RawMessage(line)
	}
	return out
}

func TestClaudeTranscriptRows(t *testing.T) {
	t.Parallel()
	rows := claudeTranscriptRows(rawEntries(claudeTranscriptFixture()), time.Now())

	type got struct{ externalID, role, content, toolCallID string }
	summary := make([]got, len(rows))
	for i, row := range rows {
		summary[i] = got{row.externalID, row.role, row.content, row.toolCallID}
	}
	require.Equal(t, []got{
		{"claude-transcript:u-1", "user", "list the files", ""},
		{"claude-transcript:a-2", "assistant", "Listing them now.", ""},
		{"claude-transcript:a-3", "assistant", "", ""},
		{"claude-transcript:u-2", "tool", "a.txt\nb.txt", "toolu_1"},
		{"claude-transcript:u-3", "tool", "line one", "toolu_2"},
		{"claude-transcript:a-4", "assistant", "Two files.", ""},
	}, summary)

	toolCall := rows[2]
	require.Equal(t, "tool_calls", toolCall.finishReason)
	require.Equal(t, "claude-opus-5-5", toolCall.model)
	require.Equal(t, "msg_1", toolCall.messageID)
	require.JSONEq(t, `[{"id":"toolu_1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}}]`, string(toolCall.toolCalls))
	require.Empty(t, rows[0].model, "user rows carry no model")
	require.Equal(t, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), rows[0].createdAt)
}

func TestClaudeTranscriptRowsSplitsMixedEntry(t *testing.T) {
	t.Parallel()
	rows := claudeTranscriptRows(rawEntries([]string{
		`{"type":"assistant","uuid":"mixed","timestamp":"2026-11-03T10:00:00Z","message":{"id":"m","role":"assistant","content":[{"type":"tool_use","id":"toolu_a","name":"Read","input":{}},{"type":"text","text":"Reading."}]}}`,
	}), time.Now())
	require.Len(t, rows, 2)
	require.Equal(t, "claude-transcript:mixed/block:0", rows[0].externalID)
	require.Equal(t, "Reading.", rows[0].content, "text precedes the tool call it introduces")
	require.Equal(t, "claude-transcript:mixed/block:1", rows[1].externalID)
	require.NotNil(t, rows[1].toolCalls)
	require.True(t, rows[1].createdAt.After(rows[0].createdAt))
}

func claudeTranscriptRequest(t *testing.T, h http.Handler, upload map[string]any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(upload)
	require.NoError(t, err)
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	_, err = gz.Write(b)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	r := httptest.NewRequest(http.MethodPost, ClaudeTranscriptPath, &body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Content-Encoding", "gzip")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func claudeTranscriptUploadBody(sessionID string, lines []string) map[string]any {
	// The binary sends only lines that are valid JSON; the envelope could not
	// carry anything else as a raw value.
	valid := make([]string, 0, len(lines))
	for _, line := range lines {
		if json.Valid([]byte(line)) {
			valid = append(valid, line)
		}
	}
	lines = valid
	return map[string]any{
		"schema_version": claudeTranscriptSchemaV1,
		"session_id":     sessionID,
		"adapter":        "cowork",
		"hostname":       "cowork-vm",
		"offset_start":   0,
		"offset_end":     100,
		"entries":        rawEntries(lines),
	}
}

func claudeTranscriptStored(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res claudeTranscriptResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	return res.Stored
}

func TestClaudeTranscriptEndpointStoresIdempotently(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	ti.service.productFeatures = alwaysEnabledFeatures{}
	h := ti.service.ahpAuthenticate(http.HandlerFunc(ti.service.serveClaudeTranscript))

	sessionID := uuid.NewString()
	lines := claudeTranscriptFixture()
	require.Equal(t, int64(6), claudeTranscriptStored(t, claudeTranscriptRequest(t, h, claudeTranscriptUploadBody(sessionID, lines), ahpAuthHeaders)))

	// A full re-send (a fresh VM starts from offset zero) stores nothing new.
	require.Equal(t, int64(0), claudeTranscriptStored(t, claudeTranscriptRequest(t, h, claudeTranscriptUploadBody(sessionID, lines), ahpAuthHeaders)))

	// An overlapping upload stores only the line it adds.
	next := slices.Concat(lines[len(lines)-3:], []string{`{"type":"user","uuid":"u-5","timestamp":"2026-11-03T10:01:00Z","message":{"role":"user","content":"thanks"}}`})
	require.Equal(t, int64(1), claudeTranscriptStored(t, claudeTranscriptRequest(t, h, claudeTranscriptUploadBody(sessionID, next), ahpAuthHeaders)))

	chatID := sessionIDToUUID(sessionID)
	messages, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 7)
	roles := make([]string, len(messages))
	for i, m := range messages {
		roles[i] = m.Role
		require.Equal(t, claudeTranscriptOrigin, m.Origin.String)
		require.Equal(t, agentVariantCowork, m.Source.String)
	}
	require.Equal(t, []string{"user", "assistant", "assistant", "tool", "tool", "assistant", "user"}, roles)
	require.Equal(t, "toolu_1", messages[3].ToolCallID.String)

	_, err = chatrepo.New(ti.conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err, "the chat is created in the authenticated project")
}

func TestClaudeTranscriptEndpointSkipsWhenCaptureDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	h := ti.service.ahpAuthenticate(http.HandlerFunc(ti.service.serveClaudeTranscript))

	sessionID := uuid.NewString()
	require.Equal(t, int64(0), claudeTranscriptStored(t, claudeTranscriptRequest(t, h, claudeTranscriptUploadBody(sessionID, claudeTranscriptFixture()), ahpAuthHeaders)))
	messages, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestClaudeTranscriptEndpointRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	s := &Service{
		logger: testenv.NewLogger(t),
		auth:   &ahpTestAuthorizer{authCtx: &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}},
	}
	h := s.ahpAuthenticate(http.HandlerFunc(s.serveClaudeTranscript))
	valid := claudeTranscriptUploadBody(uuid.NewString(), nil)

	require.Equal(t, http.StatusUnauthorized, claudeTranscriptRequest(t, h, valid, map[string]string{"Gram-Project": "chosen"}).Code)
	require.Equal(t, http.StatusUnauthorized, claudeTranscriptRequest(t, h, valid, map[string]string{"Gram-Key": "other", "Gram-Project": "chosen"}).Code)

	badSchema := claudeTranscriptUploadBody(uuid.NewString(), nil)
	badSchema["schema_version"] = "claude.transcript.v0"
	require.Equal(t, http.StatusBadRequest, claudeTranscriptRequest(t, h, badSchema, ahpAuthHeaders).Code)

	noSession := claudeTranscriptUploadBody("  ", nil)
	require.Equal(t, http.StatusBadRequest, claudeTranscriptRequest(t, h, noSession, ahpAuthHeaders).Code)

	tooMany := claudeTranscriptUploadBody(uuid.NewString(), make([]string, claudeTranscriptMaxEntries+1))
	entries := make([]json.RawMessage, claudeTranscriptMaxEntries+1)
	for i := range entries {
		entries[i] = json.RawMessage(`{}`)
	}
	tooMany["entries"] = entries
	require.Equal(t, http.StatusRequestEntityTooLarge, claudeTranscriptRequest(t, h, tooMany, ahpAuthHeaders).Code)
}
