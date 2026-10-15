package mcpservers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// These tests call the shared write commands directly, skipping the
// management API's ownership pre-check, so they exercise the same-project
// toolset guard in the CreateMCPServer and UpdateMCPServer statements that
// every writer goes through.

func createToolsetServerInTransaction(t *testing.T, ctx context.Context, tx pgx.Tx, toolsetID uuid.UUID) (mcpserversrepo.McpServer, error) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	server, err := mcpservers.CreateMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		ActorUserID:           authCtx.UserID,
		ActorEmail:            authCtx.Email,
		Name:                  "toolset guard",
		Visibility:            mcpservers.VisibilityDisabled,
		NetworkAccessMode:     "",
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{},
		RemoteMCPServerID:     uuid.NullUUID{},
		TunneledMCPServerID:   uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMCPServerID:  uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
	})
	if err != nil {
		return mcpserversrepo.McpServer{}, fmt.Errorf("create toolset-backed server: %w", err)
	}
	return server, nil
}

func TestCreateMCPServerInTransaction_RefusesOtherProjectToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherToolsetID := seedOtherProjectToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	err := pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		_, err := createToolsetServerInTransaction(t, ctx, tx, otherToolsetID)
		return err
	})
	require.ErrorIs(t, err, mcpservers.ErrServerReferenceOutsideProject)

	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: otherToolsetID, ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestCreateMCPServerInTransaction_AllowsSameProjectToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID

	var created mcpserversrepo.McpServer
	require.NoError(t, pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		var err error
		created, err = createToolsetServerInTransaction(t, ctx, tx, toolsetID)
		return err
	}))
	require.Equal(t, *authCtx.ProjectID, created.ProjectID)
	require.Equal(t, uuid.NullUUID{UUID: toolsetID, Valid: true}, created.ToolsetID)
}

// updateBackendToToolset swaps a remote-backed server's backend to toolsetID
// through the shared lifecycle update command.
func updateBackendToToolset(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID) (mcpserversrepo.McpServer, error) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, _ := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "toolset guard")

	var updated mcpserversrepo.McpServer
	err := pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		existing, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
			ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("lock server: %w", err)
		}
		updated, err = mcpservers.UpdateMCPServerLifecycleInTransaction(ctx, tx, audit.NewLogger(), existing, mcpservers.LifecycleUpdateInput{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
			ActorUserID: authCtx.UserID, ActorEmail: authCtx.Email,
			ServerID: existing.ID, Visibility: existing.Visibility,
			UserSessionIssuerID: existing.UserSessionIssuerID,
			ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("update server backend: %w", err)
		}
		return nil
	})
	if err != nil {
		return mcpserversrepo.McpServer{}, fmt.Errorf("update in transaction: %w", err)
	}
	return updated, nil
}

func TestUpdateMCPServerLifecycleInTransaction_RefusesOtherProjectToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherToolsetID := seedOtherProjectToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	_, err := updateBackendToToolset(t, ctx, ti, otherToolsetID)
	require.ErrorIs(t, err, mcpservers.ErrServerReferenceOutsideProject)

	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: otherToolsetID, ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestUpdateMCPServerLifecycleInTransaction_AllowsSameProjectToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID

	updated, err := updateBackendToToolset(t, ctx, ti, toolsetID)
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: toolsetID, Valid: true}, updated.ToolsetID)
	require.False(t, updated.RemoteMcpServerID.Valid)
}
