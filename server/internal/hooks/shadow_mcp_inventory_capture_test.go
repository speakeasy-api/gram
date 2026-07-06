package hooks

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
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

func TestClaudeSessionStartSkipsHostedMCPInventoryURLs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	customDomain := "hooks-hosted-" + uuid.NewString()[:8] + ".example.com"

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          customDomain,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = customdomainsrepo.New(ti.conn).UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		ID:              domain.ID,
	})
	require.NoError(t, err)

	sessionID := uuid.NewString()
	userEmail := "claude-hosted-inventory@example.com"
	_, err = ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_claude_code": "" +
				"external: https://external.example.com/mcp?token=secret (HTTP) - connected\n" +
				"gram: https://app.getgram.ai/mcp/hosted (HTTP) - connected\n" +
				"custom: https://" + customDomain + "/mcp/custom (HTTP) - connected",
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://external.example.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "external.example.com", rows[0].URLHost)
	require.Equal(t, "external", rows[0].ServerName)
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

func TestCodexSessionStartUpsertsShadowMCPInventoryURLWithoutName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "codex-shadow-inventory-no-name@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_codex": []any{
				map[string]any{
					"enabled": true,
					"transport": map[string]any{
						"type": "streamable_http",
						"url":  "https://nameless.example.com/mcp?auth=secret",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://nameless.example.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "nameless.example.com", rows[0].URLHost)
	require.Empty(t, rows[0].ServerName)
}

func TestCodexSessionStartDedupesShadowMCPInventoryURLsByCanonicalURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	userEmail := "codex-shadow-inventory-dedupe@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_codex": []any{
				map[string]any{
					"name":    "first",
					"enabled": true,
					"transport": map[string]any{
						"type": "streamable_http",
						"url":  "https://dedupe.example.com/mcp?token=one",
					},
				},
				map[string]any{
					"name":    "second",
					"enabled": true,
					"transport": map[string]any{
						"type": "streamable_http",
						"url":  "https://dedupe.example.com/mcp?token=two",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	rows := requireShadowMCPInventoryURLsFromHooksEventually(ctx, t, chClient, authCtx.ProjectID.String(), 1)
	require.Equal(t, "https://dedupe.example.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "dedupe.example.com", rows[0].URLHost)
}

func TestCodexSessionStartSkipsShadowMCPInventoryWhenSnapshotCacheFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := uuid.NewString()
	ti.service.cache = mcpSetErrorCache{
		Cache:   ti.service.cache,
		failKey: sessionMCPListCacheKey(sessionID),
		err:     errors.New("redis: connection refused"),
	}

	userEmail := "codex-shadow-inventory-cache-fail@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		AdditionalData: map[string]any{
			"mcp_inventory_codex": []any{
				map[string]any{
					"name":    "cache-fail",
					"enabled": true,
					"transport": map[string]any{
						"type": "streamable_http",
						"url":  "https://cache-fail.example.com/mcp",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	require.Never(t, func() bool {
		rows, err := chClient.ListShadowMCPInventoryURLs(ctx, telemetryrepo.ListShadowMCPInventoryURLsParams{
			GramProjectID: authCtx.ProjectID.String(),
			Limit:         50,
		})
		require.NoError(t, err)
		return len(rows) > 0
	}, 500*time.Millisecond, 50*time.Millisecond)
}

func TestClaudeOTELLogsWarnsWhenMCPInventorySnapshotMissing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	var logBuffer bytes.Buffer
	ti.service.logger = slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	sessionID := uuid.NewString()
	userEmail := "missing-mcp-list@example.com"
	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", userEmail),
				strAttr("organization.id", "claude-org-missing-mcp-list"),
			},
		},
	))
	require.NoError(t, err)
	require.Contains(t, logBuffer.String(), "claude_otel_mcp_list_cache_miss")
	require.Contains(t, logBuffer.String(), sessionID)
	require.True(t, strings.Contains(logBuffer.String(), "cache miss") || strings.Contains(logBuffer.String(), "cache"))
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

type mcpSetErrorCache struct {
	cache.Cache
	failKey string
	err     error
}

func (c mcpSetErrorCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if key == c.failKey {
		return c.err
	}
	//nolint:wrapcheck // test pass-through to the embedded real cache
	return c.Cache.Set(ctx, key, value, ttl)
}
