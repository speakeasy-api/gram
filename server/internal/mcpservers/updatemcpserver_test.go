package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateMcpServer_FullReplace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverA := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	serverB := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "test mcp server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &serverA,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	// Full-record replace: swap backend to serverB, flip visibility, drop
	// any optional fields.
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &serverB,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.NotNil(t, updated.RemoteMcpServerID)
	require.Equal(t, serverB, *updated.RemoteMcpServerID)
	require.Equal(t, types.McpServerVisibility("public"), updated.Visibility)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)
}

func TestUpdateMcpServer_InvalidBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "test mcp server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	// Update with neither backend — should fail validation.
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    uuid.NewString(),
		EnvironmentID:         nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateMcpServer_LeavesNameWhenOmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "original name",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "original name", *updated.Name)
}

func TestUpdateMcpServer_RecomputesSlugOnNameChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "before name",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)
	originalSlug := *created.Slug

	newName := "after name"
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                &newName,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, newName, *updated.Name)
	require.NotNil(t, updated.Slug)
	require.NotEqual(t, originalSlug, *updated.Slug, "slug should recompute when name changes")
}

func TestUpdateMcpServer_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "original name",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	emptyName := "   "
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                &emptyName,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateMcpServer_SetUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "set issuer",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, created.UserSessionIssuerID)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: &issuerID,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID)
	require.Equal(t, issuerID, *updated.UserSessionIssuerID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Nil(t, beforeSnapshot["UserSessionIssuerID"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, issuerID, afterSnapshot["UserSessionIssuerID"])
}

func TestUpdateMcpServer_ClearUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "clear issuer",
		EnvironmentID:       nil,
		UserSessionIssuerID: &issuerID,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, updated.UserSessionIssuerID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, issuerID, beforeSnapshot["UserSessionIssuerID"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, afterSnapshot["UserSessionIssuerID"])
}

func TestUpdateMcpServer_InvalidUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "bogus issuer",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	bogusIssuerID := uuid.NewString()

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: &bogusIssuerID,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    uuid.NewString(),
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
