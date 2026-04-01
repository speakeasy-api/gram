package toolsets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestToolsetsService_AddExternalOAuthServer_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Audit External OAuth Toolset")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)

	updated, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: types.Slug("audit-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.ExternalOauthServer)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetAttachExternalOAuth), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, updated.Name, record.SubjectDisplay)
	require.Equal(t, string(updated.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, updated.ExternalOauthServer.ID, metadata["external_oauth_server_id"])
	require.Equal(t, string(updated.ExternalOauthServer.Slug), metadata["external_oauth_server_slug"])
	require.InDelta(t, updated.ToolsetVersion, metadata["toolset_version_after"], 0)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_AddExternalOAuthServer_PrivateToolset_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Private External OAuth Toolset")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)

	_, err = ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: types.Slug("private-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "private MCP servers cannot have external OAuth servers")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}
