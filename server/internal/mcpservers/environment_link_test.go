package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedEnvironment creates an environment in projectID. The tests only ever
// use its id; no entries are needed to exercise link authorization.
func seedEnvironment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) uuid.UUID {
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

func seedOtherProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return project.ID
}

// projectEnvironmentGrant is the project-wide grant the environments service
// requires to link an environment to a source or toolset.
func projectEnvironmentGrant(scope authz.Scope, projectID uuid.UUID) authz.Grant {
	return authz.NewGrantWithSelector(scope, authz.Selector{
		"resource_kind": "environment",
		"resource_id":   authz.WildcardResource,
		"project_id":    projectID.String(),
	})
}

func projectMCPWriteGrant(projectID uuid.UUID) authz.Grant {
	return authz.NewGrantWithSelector(authz.ScopeMCPWrite, authz.Selector{
		"resource_kind": "mcp",
		"resource_id":   authz.WildcardResource,
		"project_id":    projectID.String(),
	})
}

func createPayload(name string, environmentID *string, remoteID *string, toolsetID *string) *gen.CreateMcpServerPayload {
	return &gen.CreateMcpServerPayload{
		Name:              name,
		EnvironmentID:     environmentID,
		RemoteMcpServerID: remoteID,
		ToolsetID:         toolsetID,
		Visibility:        types.McpServerVisibility("private"),
	}
}

func updatePayload(server *types.McpServer, environmentID *string) *gen.UpdateMcpServerPayload {
	return &gen.UpdateMcpServerPayload{
		ID:                  server.ID,
		EnvironmentID:       environmentID,
		RemoteMcpServerID:   server.RemoteMcpServerID,
		TunneledMcpServerID: server.TunneledMcpServerID,
		ToolsetID:           server.ToolsetID,
		Visibility:          server.Visibility,
	}
}

func storedEnvironmentID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, serverID string) uuid.NullUUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(conn).GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
		ID:        uuid.MustParse(serverID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	return row.EnvironmentID
}

func auditCount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, action audit.Action) int64 {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(ctx, conn, action)
	require.NoError(t, err)
	return count
}

type linkFixture struct {
	ctx       context.Context //nolint:containedctx // the fixture's authenticated request context, shared by every call it makes
	ti        *testInstance
	orgID     string
	projectID uuid.UUID
	envID     string
	envID2    string
}

func newLinkFixture(t *testing.T) linkFixture {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return linkFixture{
		ctx:       ctx,
		ti:        ti,
		orgID:     authCtx.ActiveOrganizationID,
		projectID: *authCtx.ProjectID,
		envID:     seedEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).String(),
		envID2:    seedEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).String(),
	}
}

// mcpWriteOnly is a principal that can write every MCP server in the project
// but holds no environment grant.
func (f linkFixture) mcpWriteOnly(t *testing.T) context.Context {
	t.Helper()
	return withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID))
}

func (f linkFixture) withEnvironmentAuthority(t *testing.T, scope authz.Scope) context.Context {
	t.Helper()
	return withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID), projectEnvironmentGrant(scope, f.projectID))
}

// remoteServer creates a remote-backed server as the fixture's unrestricted
// caller, optionally already linked to environmentID.
func (f linkFixture) remoteServer(t *testing.T, environmentID *string) *types.McpServer {
	t.Helper()

	remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	created, err := f.ti.service.CreateMcpServer(f.ctx, createPayload("linked "+uuid.NewString()[:8], environmentID, &remoteID, nil))
	require.NoError(t, err)
	return created
}

func TestCreateMcpServer_EnvironmentLink_RequiresEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T, f linkFixture) context.Context{
		"mcp write only": func(t *testing.T, f linkFixture) context.Context {
			t.Helper()
			return f.mcpWriteOnly(t)
		},
		// The shape a role editor produces for one environment, project_id
		// included, still does not cover the whole project.
		"single environment read grant": func(t *testing.T, f linkFixture) context.Context {
			t.Helper()
			return withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID),
				authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{"resource_kind": "environment", "resource_id": f.envID, "project_id": f.projectID.String()}))
		},
		"single environment read grant without project": func(t *testing.T, f linkFixture) context.Context {
			t.Helper()
			return withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID),
				authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{"resource_kind": "environment", "resource_id": f.envID}))
		},
		"environment read in another project": func(t *testing.T, f linkFixture) context.Context {
			t.Helper()
			return withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID),
				projectEnvironmentGrant(authz.ScopeEnvironmentRead, seedOtherProject(t, f.ctx, f.ti.conn, f.orgID)))
		},
		// Environment authority without MCP write is not enough either.
		"environment write without mcp write": func(t *testing.T, f linkFixture) context.Context {
			t.Helper()
			return withExactAuthzGrants(t, f.ctx, f.ti.conn, projectEnvironmentGrant(authz.ScopeEnvironmentWrite, f.projectID))
		},
	}
	for name, caller := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newLinkFixture(t)
			remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
			beforeCreates := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerCreate)

			_, err := f.ti.service.CreateMcpServer(caller(t, f), createPayload("denied", &f.envID, &remoteID, nil))
			requireOopsCode(t, err, oops.CodeForbidden)
			require.Equal(t, beforeCreates, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerCreate))
		})
	}
}

// The rule does not depend on what the server fronts.
func TestCreateMcpServer_EnvironmentLink_AppliesToEveryBackend(t *testing.T) {
	t.Parallel()

	backends := map[string]func(t *testing.T, f linkFixture, payload *gen.CreateMcpServerPayload){
		"tunneled": func(t *testing.T, f linkFixture, payload *gen.CreateMcpServerPayload) {
			t.Helper()
			id := seedTunneledMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
			payload.TunneledMcpServerID = &id
		},
		"toolset": func(t *testing.T, f linkFixture, payload *gen.CreateMcpServerPayload) {
			t.Helper()
			toolset, err := toolsetsrepo.New(f.ti.conn).CreateToolset(f.ctx, toolsetsrepo.CreateToolsetParams{
				OrganizationID:         f.orgID,
				ProjectID:              f.projectID,
				Name:                   "linked toolset",
				Slug:                   "linked-" + uuid.NewString()[:8],
				Description:            pgtype.Text{String: "", Valid: false},
				DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
				McpSlug:                pgtype.Text{String: "", Valid: false},
				McpEnabled:             false,
			})
			require.NoError(t, err)
			id := toolset.ID.String()
			payload.ToolsetID = &id
		},
	}
	for name, setBackend := range backends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newLinkFixture(t)
			payload := createPayload("linked "+name, &f.envID, nil, nil)
			setBackend(t, f, payload)

			_, err := f.ti.service.CreateMcpServer(f.mcpWriteOnly(t), payload)
			requireOopsCode(t, err, oops.CodeForbidden)

			before := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink)
			created, err := f.ti.service.CreateMcpServer(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentRead), payload)
			require.NoError(t, err)
			require.Equal(t, f.envID, conv.PtrValOr(created.EnvironmentID, ""))
			require.Equal(t, before+1, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink))
		})
	}
}

// Disabling a server goes through the visibility path, which must apply the
// same rule and audit the unlink.
func TestUpdateMcpServer_EnvironmentLink_UnlinkWhileDisabling(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, &f.envID)
	payload := updatePayload(server, nil)
	payload.Visibility = types.McpServerVisibility("disabled")

	_, err := f.ti.service.UpdateMcpServer(f.mcpWriteOnly(t), payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, f.envID, storedEnvironmentID(t, f.ctx, f.ti.conn, server.ID).UUID.String())

	before := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentUnlink)
	updated, err := f.ti.service.UpdateMcpServer(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentRead), payload)
	require.NoError(t, err)
	require.Nil(t, updated.EnvironmentID)
	require.Equal(t, types.McpServerVisibility("disabled"), updated.Visibility)
	require.Equal(t, before+1, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentUnlink))
}

func TestCreateMcpServer_EnvironmentLink_AllowedWithEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	for _, scope := range []authz.Scope{authz.ScopeEnvironmentRead, authz.ScopeEnvironmentWrite} {
		remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
		created, err := f.ti.service.CreateMcpServer(f.withEnvironmentAuthority(t, scope), createPayload("allowed "+string(scope), &f.envID, &remoteID, nil))
		require.NoError(t, err)
		require.Equal(t, f.envID, conv.PtrValOr(created.EnvironmentID, ""))
	}
}

func TestCreateMcpServer_NoEnvironment_NeedsNoEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	created, err := f.ti.service.CreateMcpServer(f.mcpWriteOnly(t), createPayload("unlinked", nil, &remoteID, nil))
	require.NoError(t, err)
	require.Nil(t, created.EnvironmentID)
}

func TestCreateMcpServer_EnvironmentLink_RejectsOtherProjectEnvironment(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	otherEnv := seedEnvironment(t, f.ctx, f.ti.conn, f.orgID, seedOtherProject(t, f.ctx, f.ti.conn, f.orgID)).String()
	remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()

	_, err := f.ti.service.CreateMcpServer(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentWrite), createPayload("cross project", &otherEnv, &remoteID, nil))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_EnvironmentLink_TransitionsRequireEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	// Each case picks its environments from its own fixture, which owns the
	// database its audit counts are taken from.
	cases := []struct {
		name    string
		initial func(f linkFixture) *string
		next    func(f linkFixture) *string
	}{
		{name: "link", initial: func(linkFixture) *string { return nil }, next: func(f linkFixture) *string { return &f.envID }},
		{name: "relink", initial: func(f linkFixture) *string { return &f.envID }, next: func(f linkFixture) *string { return &f.envID2 }},
		{name: "unlink", initial: func(f linkFixture) *string { return &f.envID }, next: func(linkFixture) *string { return nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLinkFixture(t)
			next := tc.next(f)
			server := f.remoteServer(t, tc.initial(f))
			before := storedEnvironmentID(t, f.ctx, f.ti.conn, server.ID)
			beforeUpdates := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerUpdate)

			_, err := f.ti.service.UpdateMcpServer(f.mcpWriteOnly(t), updatePayload(server, next))
			requireOopsCode(t, err, oops.CodeForbidden)
			require.Equal(t, before, storedEnvironmentID(t, f.ctx, f.ti.conn, server.ID))
			require.Equal(t, beforeUpdates, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerUpdate))

			updated, err := f.ti.service.UpdateMcpServer(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentRead), updatePayload(server, next))
			require.NoError(t, err)
			require.Equal(t, conv.PtrValOr(next, ""), conv.PtrValOr(updated.EnvironmentID, ""))
		})
	}
}

func TestUpdateMcpServer_EnvironmentLink_RetainedLinkEditNeedsNoEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, &f.envID)

	// A grant on this one server only, as a scoped MCP editor would hold.
	scoped := withExactAuthzGrants(t, f.ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPWrite, server.ID))
	payload := updatePayload(server, &f.envID)
	name := "renamed with retained link"
	payload.Name = &name
	payload.Visibility = types.McpServerVisibility("disabled")

	updated, err := f.ti.service.UpdateMcpServer(scoped, payload)
	require.NoError(t, err)
	require.Equal(t, name, conv.PtrValOr(updated.Name, ""))
	require.Equal(t, f.envID, conv.PtrValOr(updated.EnvironmentID, ""))
}

func TestUpdateMcpServer_EnvironmentLink_RetainedLinkBackendChangeRequiresEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)

	toolsetServer, _ := createToolsetBackedServerFixture(t, f.ctx, f.ti, "toolset backed")
	// Link it as the unrestricted caller so only the backend changes below.
	toolsetServer, err := f.ti.service.UpdateMcpServer(f.ctx, updatePayload(toolsetServer, &f.envID))
	require.NoError(t, err)

	cases := map[string]*types.McpServer{
		"remote to remote":  f.remoteServer(t, &f.envID),
		"toolset to remote": toolsetServer,
	}
	for name, server := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			newRemote := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
			payload := updatePayload(server, &f.envID)
			payload.RemoteMcpServerID = &newRemote
			payload.ToolsetID = nil

			_, err := f.ti.service.UpdateMcpServer(f.mcpWriteOnly(t), payload)
			requireOopsCode(t, err, oops.CodeForbidden)

			updated, err := f.ti.service.UpdateMcpServer(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentRead), payload)
			require.NoError(t, err)
			require.Equal(t, newRemote, conv.PtrValOr(updated.RemoteMcpServerID, ""))
			require.Equal(t, f.envID, conv.PtrValOr(updated.EnvironmentID, ""))
		})
	}
}

func TestUpdateMcpServer_EnvironmentLink_RejectsOtherProjectEnvironment(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, &f.envID)
	otherEnv := seedEnvironment(t, f.ctx, f.ti.conn, f.orgID, seedOtherProject(t, f.ctx, f.ti.conn, f.orgID)).String()
	beforeUpdates := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerUpdate)

	_, err := f.ti.service.UpdateMcpServer(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentWrite), updatePayload(server, &otherEnv))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Equal(t, f.envID, storedEnvironmentID(t, f.ctx, f.ti.conn, server.ID).UUID.String())
	require.Equal(t, beforeUpdates, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerUpdate))
}

func TestGetMcpServer_EnvironmentLink_ReadableWithoutEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, &f.envID)

	reader := withExactAuthzGrants(t, f.ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPRead, server.ID))
	got, err := getMcpServerByID(reader, f.ti, server.ID)
	require.NoError(t, err)
	require.Equal(t, f.envID, conv.PtrValOr(got.EnvironmentID, ""))
}

func requireLinkAudit(t *testing.T, ctx context.Context, conn *pgxpool.Pool, action audit.Action, serverID string, fields map[string]any) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	record, err := audittest.LatestAuditLogByAction(ctx, conn, action)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, record.OrganizationID)
	require.Equal(t, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, record.ProjectID)
	require.Equal(t, authCtx.UserID, record.ActorID)
	require.Equal(t, "mcp_server", record.SubjectType)
	require.Equal(t, serverID, record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, fields, metadata)
}

func TestMcpServer_EnvironmentLink_AuditsEachTransition(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	links := func() int64 { return auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink) }
	unlinks := func() int64 { return auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentUnlink) }

	// Create with a link.
	before := links()
	server := f.remoteServer(t, &f.envID)
	require.Equal(t, before+1, links())
	requireLinkAudit(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink, server.ID, map[string]any{"environment_id": f.envID})

	// Relink records both environments.
	server, err := f.ti.service.UpdateMcpServer(f.ctx, updatePayload(server, &f.envID2))
	require.NoError(t, err)
	require.Equal(t, before+2, links())
	requireLinkAudit(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink, server.ID, map[string]any{"environment_id": f.envID2, "previous_environment_id": f.envID})

	// Retained link: a rename or a backend repoint is an update, not a link.
	beforeUnlinks := unlinks()
	name := "retained"
	payload := updatePayload(server, &f.envID2)
	payload.Name = &name
	newRemote := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	payload.RemoteMcpServerID = &newRemote
	server, err = f.ti.service.UpdateMcpServer(f.ctx, payload)
	require.NoError(t, err)
	require.Equal(t, before+2, links())
	require.Equal(t, beforeUnlinks, unlinks())

	// Unlink names the removed environment.
	server, err = f.ti.service.UpdateMcpServer(f.ctx, updatePayload(server, nil))
	require.NoError(t, err)
	require.Nil(t, server.EnvironmentID)
	require.Equal(t, beforeUnlinks+1, unlinks())
	requireLinkAudit(t, f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentUnlink, server.ID, map[string]any{"environment_id": f.envID2})

	// Null to null records nothing.
	_, err = f.ti.service.UpdateMcpServer(f.ctx, updatePayload(server, nil))
	require.NoError(t, err)
	require.Equal(t, before+2, links())
	require.Equal(t, beforeUnlinks+1, unlinks())
}

func TestUpdateMcpServer_EnvironmentLink_AuditFailureRollsBackLink(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, nil)
	beforeUpdates := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, audittest.RejectAction(f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink))

	_, err := f.ti.service.UpdateMcpServer(f.ctx, updatePayload(server, &f.envID))
	require.Error(t, err)
	require.False(t, storedEnvironmentID(t, f.ctx, f.ti.conn, server.ID).Valid)
	require.Equal(t, beforeUpdates, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerUpdate))
}

func TestCreateMcpServer_EnvironmentLink_AuditFailureRollsBackCreate(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	beforeCreates := auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, audittest.RejectAction(f.ctx, f.ti.conn, audit.ActionMcpServerEnvironmentLink))

	remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	before, err := repo.New(f.ti.conn).ListMCPServersByProjectID(f.ctx, repo.ListMCPServersByProjectIDParams{ProjectID: f.projectID})
	require.NoError(t, err)

	_, err = f.ti.service.CreateMcpServer(f.ctx, createPayload("rolled back", &f.envID, &remoteID, nil))
	require.Error(t, err)
	require.Equal(t, beforeCreates, auditCount(t, f.ctx, f.ti.conn, audit.ActionMcpServerCreate))
	after, err := repo.New(f.ti.conn).ListMCPServersByProjectID(f.ctx, repo.ListMCPServersByProjectIDParams{ProjectID: f.projectID})
	require.NoError(t, err)
	require.Len(t, after, len(before))
}

// A linked create joins the project lock that destination changes take, so it
// cannot interleave with one that has already checked for linked servers.
func TestCreateMcpServer_EnvironmentLink_TakesProjectLock(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	remoteID := seedRemoteMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()

	tx, err := f.ti.conn.Begin(f.ctx) //nolint:glint // notestingrawsql: a held transaction stands in for a concurrent writer of the project lock
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(f.ctx)) })
	require.NoError(t, admission.LockProject(f.ctx, tx, f.projectID))

	withEnv := f.withEnvironmentAuthority(t, authz.ScopeEnvironmentRead)
	done := make(chan error, 1)
	go func() {
		_, err := f.ti.service.CreateMcpServer(withEnv, createPayload("locked create", &f.envID, &remoteID, nil))
		done <- err
	}()

	testenv.WaitForQueryBlockedBy(t, f.ctx, f.ti.conn, testenv.BackendPID(tx), "%LockProjectEnforcementState :exec%")
	require.Empty(t, done, "linked create must wait for the project lock")
	require.NoError(t, tx.Commit(f.ctx))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-f.ctx.Done():
		t.Fatal("create did not finish after the lock was released")
	}
}
