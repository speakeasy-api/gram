package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func setSetupGuideCompleted(ctx context.Context, ti *testInstance, id string, completed bool) (*types.McpServer, error) {
	server, err := ti.service.SetSetupGuideCompleted(ctx, &gen.SetSetupGuideCompletedPayload{
		ID:               id,
		Completed:        completed,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	return server, err //nolint:wrapcheck // returned for oops-code assertions on the chain
}

func TestSetSetupGuideCompleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fixture := createRemoteServerFixture(t, ctx, ti, "setup guide")
	require.Nil(t, fixture.server.SetupGuideCompletedAt)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	marked, err := setSetupGuideCompleted(ctx, ti, fixture.server.ID, true)
	require.NoError(t, err)
	require.NotNil(t, marked.SetupGuideCompletedAt)
	firstCompletedAt := *marked.SetupGuideCompletedAt

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates+1, afterUpdates)

	fetched, err := getMcpServerByID(ctx, ti, fixture.server.ID)
	require.NoError(t, err)
	require.Equal(t, marked.SetupGuideCompletedAt, fetched.SetupGuideCompletedAt)

	// The view has second precision; compare the stored microsecond value so
	// a re-mark that overwrote the time would show.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rowParams := repo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(fixture.server.ID), ProjectID: *authCtx.ProjectID}
	firstRow, err := repo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, rowParams)
	require.NoError(t, err)
	require.True(t, firstRow.SetupGuideCompletedAt.Valid)

	remarked, err := setSetupGuideCompleted(ctx, ti, fixture.server.ID, true)
	require.NoError(t, err)
	require.Equal(t, firstCompletedAt, *remarked.SetupGuideCompletedAt)
	remarkedRow, err := repo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, rowParams)
	require.NoError(t, err)
	require.True(t, firstRow.SetupGuideCompletedAt.Time.Equal(remarkedRow.SetupGuideCompletedAt.Time), "marking done again keeps the original time")
	require.True(t, remarkedRow.UpdatedAt.Time.After(firstRow.UpdatedAt.Time), "the re-mark did write the row")

	cleared, err := setSetupGuideCompleted(ctx, ti, fixture.server.ID, false)
	require.NoError(t, err)
	require.Nil(t, cleared.SetupGuideCompletedAt)

	fetched, err = getMcpServerByID(ctx, ti, fixture.server.ID)
	require.NoError(t, err)
	require.Nil(t, fetched.SetupGuideCompletedAt)
}

func TestSetSetupGuideCompleted_OtherProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fixture := createRemoteServerFixture(t, ctx, ti, "setup guide other project")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherProject := uuid.New()
	otherAuth := *authCtx
	otherAuth.ProjectID = &otherProject
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)

	_, err := setSetupGuideCompleted(otherCtx, ti, fixture.server.ID, true)
	requireOopsCode(t, err, oops.CodeNotFound)

	fetched, err := getMcpServerByID(ctx, ti, fixture.server.ID)
	require.NoError(t, err)
	require.Nil(t, fetched.SetupGuideCompletedAt)
}

func TestSetSetupGuideCompleted_UnknownServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := setSetupGuideCompleted(ctx, ti, uuid.NewString(), true)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetSetupGuideCompleted_RBAC_ReadGrantDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fixture := createRemoteServerFixture(t, ctx, ti, "setup guide read-only")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, fixture.server.ID))
	_, err := setSetupGuideCompleted(scoped, ti, fixture.server.ID, true)
	requireOopsCode(t, err, oops.CodeForbidden)

	fetched, err := getMcpServerByID(ctx, ti, fixture.server.ID)
	require.NoError(t, err)
	require.Nil(t, fetched.SetupGuideCompletedAt)
}

func TestSetSetupGuideCompleted_RBAC_ServerScopedWriteAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fixture := createRemoteServerFixture(t, ctx, ti, "setup guide scoped write")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, fixture.server.ID))
	marked, err := setSetupGuideCompleted(scoped, ti, fixture.server.ID, true)
	require.NoError(t, err)
	require.NotNil(t, marked.SetupGuideCompletedAt)
}
