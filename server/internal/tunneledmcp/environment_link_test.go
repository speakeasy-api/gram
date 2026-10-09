package tunneledmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

func seedLinkEnvironment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	slug := "env-" + uuid.NewString()[:8]
	env, err := environmentsrepo.New(conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return env.ID
}

// seedTunnelWrapper inserts an MCP server on the tunnel, optionally linked to
// environmentID, through the generated repo.
func seedTunnelWrapper(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, tunnelID uuid.UUID, environmentID uuid.NullUUID, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	id := uuid.New()
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           projectID,
		Name:                conv.ToPGText("wrapper " + id.String()[:8]),
		Slug:                conv.ToPGText("wrapper-" + id.String()[:8]),
		EnvironmentID:       environmentID,
		TunneledMcpServerID: uuid.NullUUID{UUID: tunnelID, Valid: true},
		Visibility:          visibility,
		NetworkAccessMode:   conv.ToPGText("public_only"),
	})
	require.NoError(t, err)
	return server
}

func projectEnvironmentReadGrant(projectID uuid.UUID) authz.Grant {
	return authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
		authz.SelectorKeyResourceKind: "environment",
		authz.SelectorKeyResourceID:   authz.WildcardResource,
		authz.SelectorKeyProjectID:    projectID.String(),
	})
}

func rotatePayload(id uuid.UUID) *gen.RotateServerKeyPayload {
	return &gen.RotateServerKeyPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: id.String()}
}

func storedKeyHash(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, tunnelID uuid.UUID) string {
	t.Helper()

	row, err := repo.New(conn).GetServerByID(ctx, repo.GetServerByIDParams{ID: tunnelID, ProjectID: projectID})
	require.NoError(t, err)
	return row.KeyHash
}

func rotateAudits(t *testing.T, ctx context.Context, conn *pgxpool.Pool) int64 {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionTunneledMcpServerRotateKey)
	require.NoError(t, err)
	return count
}

func TestRotateServerKey_EnvironmentLinkedWrapperRequiresEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID, envID uuid.UUID){
		"linked private wrapper": func(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID, envID uuid.UUID) {
			t.Helper()
			seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnelID, uuid.NullUUID{UUID: envID, Valid: true}, "private")
		},
		"linked disabled wrapper": func(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID, envID uuid.UUID) {
			t.Helper()
			seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnelID, uuid.NullUUID{UUID: envID, Valid: true}, "disabled")
		},
		"one of two wrappers linked": func(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID, envID uuid.UUID) {
			t.Helper()
			seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnelID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "private")
			seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnelID, uuid.NullUUID{UUID: envID, Valid: true}, "private")
		},
	}
	for name, seed := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx := requireAuthContext(t, ctx)
			projectID := *authCtx.ProjectID
			tunnel := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
			envID := seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)
			seed(t, ctx, ti, projectID, tunnel.ID, envID)

			beforeAudits := rotateAudits(t, ctx, ti.conn)
			writeOnly := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, projectID))
			_, err := ti.service.RotateServerKey(writeOnly, rotatePayload(tunnel.ID))
			requireOopsCode(t, err, oops.CodeForbidden)
			require.Equal(t, tunnel.KeyHash, storedKeyHash(t, ctx, ti.conn, projectID, tunnel.ID))
			require.Equal(t, beforeAudits, rotateAudits(t, ctx, ti.conn))

			withEnv := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, projectID), projectEnvironmentReadGrant(projectID))
			rotated, err := ti.service.RotateServerKey(withEnv, rotatePayload(tunnel.ID))
			require.NoError(t, err)
			require.NotEmpty(t, rotated.TunnelKey)
			require.NotEqual(t, tunnel.KeyHash, storedKeyHash(t, ctx, ti.conn, projectID, tunnel.ID))
		})
	}
}

func TestRotateServerKey_UnlinkedOrDeletedWrappersNeedNoEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	projectID := *authCtx.ProjectID
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
	envID := seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)

	seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "private")
	deleted := seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: envID, Valid: true}, "private")
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: projectID})
	require.NoError(t, err)

	// A linked wrapper on a different tunnel does not count either.
	otherTunnel := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
	seedTunnelWrapper(t, ctx, ti.conn, projectID, otherTunnel.ID, uuid.NullUUID{UUID: envID, Valid: true}, "private")

	writeOnly := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, projectID))
	rotated, err := ti.service.RotateServerKey(writeOnly, rotatePayload(tunnel.ID))
	require.NoError(t, err)
	require.NotEmpty(t, rotated.TunnelKey)
}

// A rotation that waits behind an uncommitted link must see that link once it
// commits, so the destination cannot change hands without environment
// authority.
func TestRotateServerKey_WaitsForConcurrentEnvironmentLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	projectID := *authCtx.ProjectID
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
	envID := seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)
	wrapper := seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "private")

	// The linking writer, as UpdateMcpServer runs it: project lock, then row.
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: a held transaction stands in for a concurrent environment link
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(ctx)) })
	require.NoError(t, admission.LockProject(ctx, tx, projectID))
	_, err = mcpserversrepo.New(tx).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:                wrapper.Name,
		Slug:                wrapper.Slug,
		EnvironmentID:       uuid.NullUUID{UUID: envID, Valid: true},
		TunneledMcpServerID: wrapper.TunneledMcpServerID,
		Visibility:          wrapper.Visibility,
		ID:                  wrapper.ID,
		ProjectID:           projectID,
	})
	require.NoError(t, err)

	writeOnly := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, projectID))
	done := make(chan error, 1)
	go func() {
		_, err := ti.service.RotateServerKey(writeOnly, rotatePayload(tunnel.ID))
		done <- err
	}()

	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), "%LockProjectEnforcementState :exec%")
	require.Empty(t, done, "rotation must wait for the project lock")
	require.NoError(t, tx.Commit(ctx))

	select {
	case err := <-done:
		requireOopsCode(t, err, oops.CodeForbidden)
	case <-ctx.Done():
		t.Fatal("rotation did not finish after the link committed")
	}
	require.Equal(t, tunnel.KeyHash, storedKeyHash(t, ctx, ti.conn, projectID, tunnel.ID))
}

// getServer reports a linked server even when the caller cannot list it, so
// the dashboard never unlocks rotation from a filtered inventory.
func TestGetServer_EnvironmentLinkedCountsServersTheCallerCannotList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	projectID := *authCtx.ProjectID
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
	envID := seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)
	seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "private")
	hidden := seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: envID, Valid: true}, "disabled")

	caller := authztest.WithExactGrants(t, ctx,
		projectScopedMCPGrant(authz.ScopeMCPWrite, projectID),
		authz.NewGrantWithSelector(authz.ScopeMCPBlockedRead, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   hidden.ID.String(),
		}),
	)

	got, err := ti.service.GetServer(caller, &gen.GetServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: tunnel.ID.String()})
	require.NoError(t, err)
	require.True(t, conv.PtrValOr(got.EnvironmentLinked, false))
	_, err = ti.service.RotateServerKey(caller, rotatePayload(tunnel.ID))
	requireOopsCode(t, err, oops.CodeForbidden)

	// Another tunnel with no linked server reports false.
	other := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
	got, err = ti.service.GetServer(ctx, &gen.GetServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: other.ID.String()})
	require.NoError(t, err)
	require.NotNil(t, got.EnvironmentLinked)
	require.False(t, *got.EnvironmentLinked)
}

func TestRotateServerKey_RefusedWhenAnyLinkedEnvironmentIsExcluded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	projectID := *authCtx.ProjectID
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, projectID)
	excluded := seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)
	readable := seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)
	seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: readable, Valid: true}, "private")
	seedTunnelWrapper(t, ctx, ti.conn, projectID, tunnel.ID, uuid.NullUUID{UUID: excluded, Valid: true}, "disabled")

	excludedFrom := func(environmentID uuid.UUID) context.Context {
		return authztest.WithExactGrants(t, ctx,
			projectScopedMCPGrant(authz.ScopeMCPWrite, projectID),
			projectEnvironmentReadGrant(projectID),
			authz.NewGrantWithSelector(authz.ScopeEnvironmentBlockedRead, authz.Selector{
				authz.SelectorKeyResourceKind: "environment",
				authz.SelectorKeyResourceID:   environmentID.String(),
				authz.SelectorKeyProjectID:    projectID.String(),
			}),
		)
	}

	caller := excludedFrom(excluded)
	got, err := ti.service.GetServer(caller, &gen.GetServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: tunnel.ID.String()})
	require.NoError(t, err)
	require.True(t, conv.PtrValOr(got.EnvironmentLinked, false))
	require.False(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, true))

	beforeAudits := rotateAudits(t, ctx, ti.conn)
	_, err = ti.service.RotateServerKey(caller, rotatePayload(tunnel.ID))
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, tunnel.KeyHash, storedKeyHash(t, ctx, ti.conn, projectID, tunnel.ID))
	require.Equal(t, beforeAudits, rotateAudits(t, ctx, ti.conn))

	// Excluded only from an environment the tunnel does not use: allowed.
	other := excludedFrom(seedLinkEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID))
	got, err = ti.service.GetServer(other, &gen.GetServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: tunnel.ID.String()})
	require.NoError(t, err)
	require.True(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, false))
	_, err = ti.service.RotateServerKey(other, rotatePayload(tunnel.ID))
	require.NoError(t, err)
}
