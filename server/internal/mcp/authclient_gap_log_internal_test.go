package mcp

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A gap that means something broke warns once per subject, server and reason;
// an unmade choice stays at debug.
func TestNoteAuthClientGap(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := &Service{authClientGapLog: newMandatoryShadowLog()}
	member := metaMember{serverID: uuid.New(), slug: "db"}
	user := urn.NewUserSubject("user_1")

	stale := gapResolution(authClientReasonNoAuthStale)
	s.noteAuthClientGap(t.Context(), logger, user, member, stale)
	s.noteAuthClientGap(t.Context(), logger, user, member, stale)
	s.noteAuthClientGap(t.Context(), logger, urn.NewUserSubject("user_2"), member, stale)
	s.noteAuthClientGap(t.Context(), logger, user, member, gapResolution(authClientReasonMissingChoice))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	require.Contains(t, lines[0], `"level":"WARN"`)
	require.Contains(t, lines[0], `"gram.auth_client.reason":"no_auth_server_has_clients"`)
	require.Contains(t, lines[0], member.serverID.String())
	require.NotContains(t, lines[0], "gram.auth_client.id", "no client is named when there is none")
	require.Contains(t, lines[1], `"level":"WARN"`, "another subject warns on its own")
	require.Contains(t, lines[2], `"level":"DEBUG"`)
	require.Contains(t, lines[2], `"gram.auth_client.reason":"missing_choice"`)

	buf.Reset()
	s.noteAuthClientGap(t.Context(), logger, user, member, gapResolution(authClientReasonUnassociated))
	require.Contains(t, buf.String(), `"level":"WARN"`, "a client no longer offered on the server is broken setup")
	require.Contains(t, buf.String(), `"gram.auth_client.reason":"client_unassociated"`)

	withClient := gapResolution(authClientReasonClientDisabled)
	withClient.clientID = uuid.New()
	buf.Reset()
	s.noteAuthClientGap(t.Context(), logger, user, member, withClient)
	require.Contains(t, buf.String(), withClient.clientID.String())
}

// Only a check right after sign-in warns, naming why it failed.
func TestLogConnectVerificationFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	client := remotesessions.Client{IssuerURL: "https://auth.example.com"}

	logConnectVerificationFailure(t.Context(), logger, remotesessionmetrics.ValidationTriggerKeepalive, client, "member_rejected_new_grant")
	require.Empty(t, buf.String())

	logConnectVerificationFailure(t.Context(), logger, remotesessionmetrics.ValidationTriggerConnect, client, "member_rejected_new_grant", attr.SlogHTTPResponseStatusCode(401))
	line := buf.String()
	require.Contains(t, line, `"level":"WARN"`)
	require.Contains(t, line, `"gram.oauth.failure_reason":"member_rejected_new_grant"`)
	require.Contains(t, line, "https://auth.example.com")
	require.Contains(t, line, "401")
}
