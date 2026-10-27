package hooks

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// A headless workload's Claude OTEL export, authenticated by its device-agent
// token alone, is attributed to the agent its workload is assigned to and never
// to a human: the rows name the agent and the device, and keep none of the
// account identity the export reports, even an email that belongs to a member
// of the organization.
func TestLogs_WorkloadTokenAttributesRowsToItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	workload := newHooksWorkload(t, ctx, ti)

	admitted, err := authenticateWorkload(t, ti, "logs", workload.token, workload.projectSlug)
	require.NoError(t, err)

	sessionID := "claude-tag-otel-" + uuid.NewString()
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	// dev@example.com is a member of the test organization, so a human request
	// carrying it would be attributed to that member.
	require.NoError(t, ti.service.Logs(admitted, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{
			resourceStrAttr("service.name", "claude-tag"),
			resourceStrAttr("user.email", "dev@example.com"),
		},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.api_request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", "dev@example.com"),
				strAttr("user.account_uuid", "account-"+uuid.NewString()),
				strAttr("organization.id", "anthropic-org-"+uuid.NewString()),
				strAttr("event.name", "api_request"),
				strAttr("model", "claude-opus-4-8"),
			},
		},
	)))

	rows := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, timestamp, 1)
	row := rows[0]
	require.NotNil(t, row.GramChatID)
	require.Equal(t, agentSessionID(admitted, sessionID), *row.GramChatID, "the session belongs to the agent")
	require.NotContains(t, row.Attributes, "dev@example.com")
	require.NotContains(t, row.ResourceAttributes, "dev@example.com")

	var attrs map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.Attributes), &attrs))
	gram, ok := attrs["gram"].(map[string]any)
	require.True(t, ok, "attributes: %s", row.Attributes)
	authorization, ok := gram["authorization"].(map[string]any)
	require.True(t, ok, "attributes: %s", row.Attributes)
	actor, ok := authorization["actor"].(map[string]any)
	require.True(t, ok, "attributes: %s", row.Attributes)
	require.Equal(t, "agent", actor["type"])
	require.Equal(t, workload.actor.ID, actor["id"])
	require.NotContains(t, gram, "account_type", "an agent row is never classified as a human's account")
	require.NotContains(t, attrs, "user", "no user identity is stored on an agent row")

	// The account identity the export reported is not cached for the session
	// either, so no later hook or batch can inherit it.
	for _, cachedID := range []string{sessionID, agentSessionID(admitted, sessionID)} {
		_, err := ti.service.getSessionMetadata(admitted, cachedID)
		require.Error(t, err, "an agent session seeds no session metadata under %q", cachedID)
	}
}
