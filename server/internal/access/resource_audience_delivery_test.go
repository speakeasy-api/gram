package access

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"

	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	plugingen "github.com/speakeasy-api/gram/server/gen/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func (f *roleDeliveryFixture) saveAudience(t *testing.T, ctx context.Context, entries ...*gen.SetResourceAudienceEntry) {
	t.Helper()
	_, err := f.ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp", ResourceID: f.toolsetID.String(), Entries: entries,
		ExpectedVersion: currentAudienceVersion(t, ctx, f.ti, f.toolsetID.String()),
	})
	require.NoError(t, err)
}

func TestService_SetResourceAudience_RoleDelivery(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	// A configured marketplace needs a publication request when contents change.
	_, err := pluginsrepo.New(f.ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *f.ac.ProjectID, InstallationID: 12345, RepoOwner: "test-org", RepoName: "audience-delivery",
	})
	require.NoError(t, err)
	role := "role:organization:" + f.roleID
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: role, Level: "use"})
	membership := f.read(t, ctx, 1).Servers[0]
	require.Equal(t, f.serverID.String(), *membership.McpServerID)
	events, err := testrepo.New(f.ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	published := false
	for _, event := range events {
		if event.Topic != "gram.plugins.v1.PublicationRequested" {
			continue
		}
		request := &publicationv1.PublicationRequested{}
		require.NoError(t, proto.Unmarshal(event.Message, request))
		if request.GetOrganizationId() == f.ac.ActiveOrganizationID && request.GetProjectId() == f.ac.ProjectID.String() {
			published = true
		}
	}
	require.True(t, published, "audience delivery requests marketplace publication")
	// Narrowing and blocking use the same effective-access semantics as role edits.
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: role, Level: "use", Tools: []string{"search"}})
	require.Equal(t, membership.ID, f.read(t, ctx, 1).Servers[0].ID)
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: role, Level: "blocked"})
	f.read(t, ctx, 0)
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: role, Level: "use"})
	f.read(t, ctx, 1)
	f.saveAudience(t, ctx)
	f.read(t, ctx, 0)
}

func TestService_SetResourceAudience_RoleDeliveryRetainsOtherRole(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	otherRoleID := seedRole(t, ctx, f.ti.conn, f.ac.ActiveOrganizationID, mockRole("role_backup", "Backup", "backup", ""))
	role := "role:organization:" + f.roleID
	other := "role:organization:" + otherRoleID
	_, err := f.pluginService.SetPluginAssignments(ctx, &plugingen.SetPluginAssignmentsPayload{PluginID: f.plugin.ID, PrincipalUrns: []string{role, other}})
	require.NoError(t, err)
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: role, Level: "use"}, &gen.SetResourceAudienceEntry{PrincipalUrn: other, Level: "use"})
	membership := f.read(t, ctx, 1).Servers[0].ID
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: other, Level: "use"})
	require.Equal(t, membership, f.read(t, ctx, 1).Servers[0].ID)
	f.saveAudience(t, ctx)
	f.read(t, ctx, 0)
}

func TestService_SetResourceAudience_RoleDeliveryPreservesAdminRemoval(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	grant := &gen.SetResourceAudienceEntry{PrincipalUrn: "role:organization:" + f.roleID, Level: "use"}
	f.saveAudience(t, ctx, grant)
	membership := f.read(t, ctx, 1).Servers[0].ID
	require.NoError(t, f.pluginService.RemovePluginServer(ctx, &plugingen.RemovePluginServerPayload{ID: membership, PluginID: f.plugin.ID}))
	f.saveAudience(t, ctx, grant)
	f.read(t, ctx, 0)
	f.saveAudience(t, ctx)
	f.saveAudience(t, ctx, grant)
	f.read(t, ctx, 1)
}

func TestResourceAudienceRoleDeliveryProjects_LockUnionInProjectOrder(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	otherRoleID := seedRole(t, ctx, f.ti.conn, f.ac.ActiveOrganizationID, mockRole("role_backup", "Backup", "backup", ""))
	projects := []uuid.UUID{*f.ac.ProjectID, seedProject(t, ctx, f.ti.conn, f.ac.ActiveOrganizationID)}
	if strings.Compare(projects[0].String(), projects[1].String()) > 0 {
		projects[0], projects[1] = projects[1], projects[0]
	}
	roles := []string{"role:organization:" + f.roleID, "role:organization:" + otherRoleID}
	q := pluginsrepo.New(f.ti.conn)
	_, err := f.pluginService.SetPluginAssignments(ctx, &plugingen.SetPluginAssignmentsPayload{PluginID: f.plugin.ID, PrincipalUrns: []string{}})
	require.NoError(t, err)
	// The first role targets the higher project. The second targets the lower
	// one, so per-role locking would invert the global project order.
	for i, role := range roles {
		plugin, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
			OrganizationID: f.ac.ActiveOrganizationID, ProjectID: projects[1-i], Name: "Lock order", Slug: "lock-order",
		})
		require.NoError(t, err)
		_, err = q.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
			OrganizationID: f.ac.ActiveOrganizationID, PluginID: plugin.ID, PrincipalUrn: role,
		})
		require.NoError(t, err)
	}
	tx, err := f.ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: observe the existing SQLc admission locks within a real transaction
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()
	recorded := &audienceProjectLockRecorder{Tx: tx, projectIDs: nil}
	require.NoError(t, lockAudienceRoleDeliveryProjects(ctx, recorded, f.ac.ActiveOrganizationID, append(roles, roles[0])))
	require.Equal(t, []string{projects[0].String(), projects[1].String()}, recorded.projectIDs,
		"lock each affected project once in global order, independent of role order")
}

type audienceProjectLockRecorder struct {
	pgx.Tx
	projectIDs []string
}

func (tx *audienceProjectLockRecorder) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "-- name: LockProjectEnforcementState") {
		if projectID, ok := arguments[0].(string); ok {
			tx.projectIDs = append(tx.projectIDs, projectID)
		}
	}
	tag, err := tx.Tx.Exec(ctx, sql, arguments...) //nolint:glint // notestingrawsql: forward the existing SQLc query unchanged while recording lock order
	if err != nil {
		return tag, fmt.Errorf("execute recorded project lock: %w", err)
	}
	return tag, nil
}
