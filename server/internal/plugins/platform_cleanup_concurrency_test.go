package plugins_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestPlatformCleanupPreservesConcurrentManualUpdate(t *testing.T) {
	t.Parallel()
	for _, wrapped := range []bool{false, true} {
		name := "direct"
		if wrapped {
			name = "wrapped"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			ac, _ := contextvalues.GetAuthContext(ctx)
			org, projectID := ac.ActiveOrganizationID, *ac.ProjectID
			_, toolsetID := platformDistributionServer(t, ctx, ti, wrapped)
			role := createTestRolePrincipal(t, ctx, ti, "concurrent-cleanup")
			principal, err := urn.ParsePrincipal(role)
			require.NoError(t, err)
			selectors, err := authz.NewSelector(authz.ScopeMCPConnect, "*").MarshalJSON()
			require.NoError(t, err)
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: org, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
			require.NoError(t, err)
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Concurrent cleanup"})
			require.NoError(t, err)
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
			require.NoError(t, err)
			before, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			require.Len(t, before.Servers, 1)
			membershipID := uuid.MustParse(before.Servers[0].ID)
			_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: toolsetID, Version: 1, ToolUrns: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")}, ResourceUrns: []urn.Resource{}})
			require.NoError(t, err)

			// Queue the real manual update first, then cleanup, behind the same row
			// lock. Observed PostgreSQL waiters make this ordering deterministic.
			blocker := testenv.BeginTx(t, ctx, ti.conn)
			_, err = pluginsrepo.New(blocker).LockPlatformCleanupMemberships(ctx, pluginsrepo.LockPlatformCleanupMembershipsParams{OrganizationID: org, ProjectID: projectID, MembershipIds: []uuid.UUID{membershipID}})
			require.NoError(t, err)
			updated := make(chan error, 1)
			go func() {
				_, err := ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{ID: membershipID.String(), PluginID: plugin.ID, DisplayName: "Manually curated", Policy: "optional", SortOrder: 7})
				updated <- err
			}()
			testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(blocker), "%UpdatePluginServer%")

			cleanupTx := testenv.BeginTx(t, ctx, ti.conn)
			type cleanupResult struct {
				removed []uuid.UUID
				err     error
			}
			cleaned := make(chan cleanupResult, 1)
			go func() {
				removed, err := roledelivery.ContentChanged(ctx, cleanupTx, org, projectID, toolsetID, nil)
				cleaned <- cleanupResult{removed: removed, err: err}
			}()
			testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(blocker), 2)
			require.NoError(t, blocker.Commit(ctx))
			require.NoError(t, <-updated)
			result := <-cleaned
			require.NoError(t, result.err)
			require.Empty(t, result.removed, "cleanup must re-read provenance after the manual update commits")
			require.NoError(t, cleanupTx.Commit(ctx))
			after, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			require.Len(t, after.Servers, 1)
			require.Equal(t, membershipID.String(), after.Servers[0].ID)
			require.Equal(t, "Manually curated", after.Servers[0].DisplayName)
			require.Equal(t, "optional", after.Servers[0].Policy)
			require.Equal(t, int32(7), after.Servers[0].SortOrder)
		})
	}
}
