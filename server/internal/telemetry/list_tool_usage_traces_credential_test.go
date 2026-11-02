package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// An agent's gateway tool call names the auth client that signed it in: a
// person's connection with its owner, or a shared credential with none. A
// call recorded before that was tracked names nothing.
func TestListToolUsageTraces_NamesTheCredentialAnAgentCallUsed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	now := time.Now().UTC().Add(-time.Minute)
	agentID := uuid.NewString()
	ownerID, ownerEmail := seedConnectedOrgUser(t, ctx, ti, "Connection Owner")
	personalClient := uuid.NewString()
	sharedClient := uuid.NewString()
	personalTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	sharedTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	untrackedTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")

	for _, row := range []struct {
		traceID    string
		clientType string
		clientID   string
		ownerID    string
	}{
		{personalTraceID, "remote_session", personalClient, ownerID},
		{sharedTraceID, "service_account", sharedClient, ""},
		{untrackedTraceID, "", "", ""},
	} {
		attrs := map[string]any{
			"gram.event.source":                     "tool_call",
			"gram.toolset.slug":                     "payments",
			"gen_ai.tool.name":                      "charge",
			"http.response.status_code":             200,
			"gram.authorization.actor.type":         "agent",
			"gram.authorization.actor.id":           agentID,
			"gram.authorization.authorizer_user_id": authCtx.UserID,
		}
		if row.clientType != "" {
			attrs["gram.auth_client.type"] = row.clientType
			attrs["gram.auth_client.id"] = row.clientID
		}
		if row.ownerID != "" {
			attrs["gram.auth_client.connection_owner_user_id"] = row.ownerID
		}
		encoded, err := json.Marshal(attrs)
		require.NoError(t, err)
		err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
			ID: uuid.NewString(), GramProjectID: ti.projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &row.traceID, GramURN: "tools:http:gram:charge",
			Body: "hosted tool event", Attributes: string(encoded), ResourceAttributes: "{}",
		}})
		require.NoError(t, err)
	}

	res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: now.Add(-time.Hour).Format(time.RFC3339), To: now.Add(time.Hour).Format(time.RFC3339), Limit: 10,
	})
	require.NoError(t, err)
	got := make(map[string]*gen.ToolUsageTraceSummary, len(res.Traces))
	for _, trace := range res.Traces {
		require.NotNil(t, trace.TraceID)
		got[*trace.TraceID] = trace
	}
	require.Len(t, got, 3)

	personal := got[personalTraceID]
	require.Equal(t, new("remote_session"), personal.AuthClientType)
	require.Equal(t, new(personalClient), personal.AuthClientID)
	require.Equal(t, new(ownerID), personal.ConnectionOwnerUserID)
	require.Equal(t, new("Connection Owner"), personal.ConnectionOwnerLabel)
	require.NotEqual(t, ownerEmail, *personal.ConnectionOwnerLabel, "the display name is preferred over the email")

	shared := got[sharedTraceID]
	require.Equal(t, new("service_account"), shared.AuthClientType)
	require.Equal(t, new(sharedClient), shared.AuthClientID)
	require.Nil(t, shared.ConnectionOwnerUserID)
	require.Nil(t, shared.ConnectionOwnerLabel)

	untracked := got[untrackedTraceID]
	require.Nil(t, untracked.AuthClientType)
	require.Nil(t, untracked.AuthClientID)
	require.Nil(t, untracked.ConnectionOwnerUserID)
	// The key creator stays reachable on every row.
	require.Equal(t, new(authCtx.UserID), untracked.AuthorizerUserID)
}
