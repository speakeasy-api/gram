package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

func TestClaudeSessionStartUpsertsShadowMCPInventoryURLs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "claude-shadow-inventory@example.com"
	_, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "" +
				"speakeasy: https://mcp.speakeasy.com/mcp?token=secret (HTTP) - connected\n" +
				"local-tools: /usr/local/bin/local-tools (STDIO) - connected",
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "mcp.speakeasy.com", rows[0].URLHost)
	require.Equal(t, "speakeasy", rows[0].ServerName)
}

func TestClaudeConfigChangeUpsertsCoworkShadowMCPInventoryURLs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "cowork-shadow-inventory@example.com"
	_, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "ConfigChange",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_cowork": []any{
				map[string]any{
					"name":           "Linear",
					"url":            "https://mcp.linear.app/mcp?workspace=speakeasy",
					"transport":      "HTTP",
					"status":         "connected",
					"connector_uuid": "linear-connector",
				},
				map[string]any{
					"name":      "Local",
					"command":   "node local.js",
					"transport": "STDIO",
					"status":    "connected",
				},
			},
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://mcp.linear.app/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "mcp.linear.app", rows[0].URLHost)
	require.Equal(t, "Linear", rows[0].ServerName)
}

func TestClaudeSessionStartUpsertsShadowMCPInventoryURLsWhenOTELMetadataArrives(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "otel-shadow-inventory@example.com"
	noAuthCtx := t.Context()
	_, err := ti.service.Claude(noAuthCtx, &gen.ClaudePayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "" +
				"notion: https://mcp.notion.com/mcp?auth=secret (HTTP) - connected\n" +
				"local-tools: /usr/local/bin/local-tools (STDIO) - connected",
		},
	})
	require.NoError(t, err)

	cached, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, cached, 2)

	err = ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", userEmail),
				strAttr("organization.id", "claude-org-otel"),
			},
		},
	))
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://mcp.notion.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "mcp.notion.com", rows[0].URLHost)
	require.Equal(t, "notion", rows[0].ServerName)
}

func TestClaudeConfigChangeUpsertsShadowMCPInventoryURLsFromCachedSessionMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "metadata-shadow-inventory@example.com"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     userEmail,
		UserID:        "",
		ExternalOrgID: "claude-org-metadata",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}, 24*time.Hour))

	noAuthCtx := t.Context()
	_, err := ti.service.Claude(noAuthCtx, &gen.ClaudePayload{
		HookEventName: "ConfigChange",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "asana: https://mcp.asana.com/mcp?workspace=speakeasy (HTTP) - connected",
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://mcp.asana.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "mcp.asana.com", rows[0].URLHost)
	require.Equal(t, "asana", rows[0].ServerName)
}

func TestCodexSessionStartUpsertsShadowMCPInventoryURLs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "codex-shadow-inventory@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_codex": []any{
				map[string]any{
					"name":    "platform-logs",
					"enabled": true,
					"transport": map[string]any{
						"type": "streamable_http",
						"url":  "https://logs.example.com/mcp?auth=secret",
					},
				},
				map[string]any{
					"name":    "filesystem",
					"enabled": true,
					"transport": map[string]any{
						"type":    "stdio",
						"command": "filesystem",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://logs.example.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "logs.example.com", rows[0].URLHost)
	require.Equal(t, "platform-logs", rows[0].ServerName)
}

func requireShadowMCPInventoryURLsFromHooksEventually(
	ctx context.Context,
	t *testing.T,
	chClient *telemetryrepo.Queries,
	projectID string,
	wantLen int,
) []telemetryrepo.ShadowMCPInventoryURLRow {
	t.Helper()

	var rows []telemetryrepo.ShadowMCPInventoryURLRow
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		rows, err = chClient.ListShadowMCPInventoryURLs(ctx, telemetryrepo.ListShadowMCPInventoryURLsParams{
			GramProjectID: projectID,
			Limit:         50,
		})
		assert.NoError(c, err)
		assert.Len(c, rows, wantLen)
	}, 5*time.Second, 100*time.Millisecond)

	return rows
}
