package plugins_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestProcessRoleDistributionSetup_CreatesRoleOnlyPlugin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	role := createTestRolePrincipal(t, ctx, ti, "Sales Team")
	err := pluginsrepo.New(ti.conn).EnableRoleSetupFeatureFixture(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)

	err = processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{})
	require.NoError(t, err)
	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var pluginID string
	for _, p := range list.Plugins {
		if p.Name == "Sales Team" {
			require.Empty(t, pluginID)
			require.True(t, p.AutoCreated)
			pluginID = p.ID
		}
	}
	require.NotEmpty(t, pluginID)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: pluginID})
	require.NoError(t, err)
	require.True(t, got.AutoCreated)
	require.Equal(t, "Sales Team", got.Name)
	require.Equal(t, "sales-team", got.Slug)
	require.Len(t, got.Assignments, 1)
	require.Equal(t, role, got.Assignments[0].PrincipalUrn)
	require.Empty(t, got.Servers)
	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: pluginID, Name: "Curated sales tools", Slug: got.Slug})
	require.NoError(t, err)
	require.True(t, updated.AutoCreated)
	// A distinct role reuses the existing auto-created plugin.
	secondRole := roleSetupFixture(t, ctx, ti, "sALES---TEAM")
	require.NoError(t, processDirectRoleSetup(ctx, ti, secondRole, plugins.PublicationRequests{Enabled: true}))
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: pluginID})
	require.NoError(t, err)
	require.True(t, got.AutoCreated)
	require.Equal(t, "Curated sales tools", got.Name)
	require.Len(t, got.Assignments, 2)
}

// SQL here only arranges incoming role intents and fault injection; assertions
// use the public worker and plugin readback seams.
func roleSetupFixture(t *testing.T, ctx context.Context, ti *testInstance, name string) string {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	role := createTestRolePrincipal(t, ctx, ti, name)
	err := pluginsrepo.New(ti.conn).EnableRoleSetupFeatureFixture(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	return role
}

func TestProcessRoleDistributionSetup_MatchesSlugPreservesEdits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := roleSetupFixture(t, ctx, ti, "Sales Team")
	description := "Keep this content"
	slug := "sales-team"
	first, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Administrator curated tools", Slug: &slug, Description: &description})
	require.NoError(t, err)
	otherSlug := "different-sales-tools"
	second, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Sales Team", Slug: &otherSlug})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "preserved-toolset")
	toolsetID := toolset.ID.String()
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: first.ID, ToolsetID: &toolsetID, Policy: "required"})
	require.NoError(t, err)
	before, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: first.ID})
	require.NoError(t, err)
	require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: first.ID})
	require.NoError(t, err)
	require.False(t, first.AutoCreated)
	require.False(t, got.AutoCreated)
	require.Equal(t, before.Name, got.Name)
	require.Equal(t, before.Slug, got.Slug)
	require.Equal(t, before.Description, got.Description)
	require.Equal(t, before.Servers, got.Servers)
	require.Len(t, got.Assignments, len(before.Assignments)+1)
	principals := make([]string, 0, len(got.Assignments))
	for _, a := range got.Assignments {
		principals = append(principals, a.PrincipalUrn)
	}
	require.Contains(t, principals, role)
	for _, a := range before.Assignments {
		require.Contains(t, principals, a.PrincipalUrn)
	}
	other, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: second.ID})
	require.NoError(t, err)
	for _, a := range other.Assignments {
		require.NotEqual(t, role, a.PrincipalUrn)
	}
}

func TestProcessRoleDistributionSetup_SameNameDifferentSlugCreatesPlugin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := roleSetupFixture(t, ctx, ti, "Sales Team")
	slug := "custom-sales"
	existing, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Sales Team", Slug: &slug})
	require.NoError(t, err)
	before, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: existing.ID})
	require.NoError(t, err)
	require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
	after, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: existing.ID})
	require.NoError(t, err)
	require.Equal(t, before, after)
	listed, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var matching []string
	for _, p := range listed.Plugins {
		if p.Slug == "sales-team" {
			matching = append(matching, p.ID)
		}
	}
	require.Len(t, matching, 1)
	require.NotEqual(t, existing.ID, matching[0])
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: matching[0]})
	require.NoError(t, err)
	require.Equal(t, "Sales Team", got.Name)
	require.Len(t, got.Assignments, 1)
	require.Equal(t, role, got.Assignments[0].PrincipalUrn)
}

func TestProcessRoleDistributionSetup_InvalidNormalizedSlugDoesNotFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		roleName string
	}{
		{name: "empty", roleName: "!!!"},
		{name: "over60", roleName: strings.Repeat("a", 61)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			role := roleSetupFixture(t, ctx, ti, tc.roleName)
			before, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
			require.NoError(t, err)

			err = processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{})
			require.Error(t, err)

			after, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestProcessRoleDistributionSetup_GatingAndInactiveRoles(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"off", "deleted", "workos-deleted", "foreign-org", "invalid-urn", "no-project", "disabled-org"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			role := roleSetupFixture(t, ctx, ti, "Engineering")
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			var err error
			switch mode {
			case "disabled-org":
				err = pluginsrepo.New(ti.conn).DisableRoleSetupOrganizationFixture(ctx, ac.ActiveOrganizationID)
			case "off":
				err = pluginsrepo.New(ti.conn).DisableRoleSetupFeatureFixture(ctx, ac.ActiveOrganizationID)
			case "deleted":
				err = pluginsrepo.New(ti.conn).DeleteRoleSetupRoleFixture(ctx, pluginsrepo.DeleteRoleSetupRoleFixtureParams{RoleUrn: role, OrganizationID: ac.ActiveOrganizationID})
			case "workos-deleted":
				err = pluginsrepo.New(ti.conn).DeleteRoleSetupWorkOSRoleFixture(ctx, pluginsrepo.DeleteRoleSetupWorkOSRoleFixtureParams{RoleUrn: role, OrganizationID: ac.ActiveOrganizationID})
			case "foreign-org":
				_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: "org_other_setup", Name: "Other setup", Slug: "other-setup"})
				require.NoError(t, err)
				err = testrepo.New(ti.conn).SourceMoveOrganizationRole(ctx, testrepo.SourceMoveOrganizationRoleParams{OrganizationID: "org_other_setup", RoleUrn: role})
			case "invalid-urn":
				role = "role:organization:not-a-uuid"
			case "no-project":
				err = pluginsrepo.New(ti.conn).DeleteRoleSetupProjectFixture(ctx, *ac.ProjectID)
			}
			require.NoError(t, err)
			err = processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true})
			if mode == "no-project" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if mode == "no-project" {
				err = pluginsrepo.New(ti.conn).RestoreRoleSetupProjectFixture(ctx, *ac.ProjectID)
				require.NoError(t, err)
			}
			list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
			require.NoError(t, err)
			for _, p := range list.Plugins {
				require.NotEqual(t, "Engineering", p.Name)
			}
			if mode == "off" {
				err = pluginsrepo.New(ti.conn).RestoreRoleSetupFeatureFixture(ctx, ac.ActiveOrganizationID)
				require.NoError(t, err)
				require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
			}
		})
	}
}

func TestProcessRoleDistributionSetup_ConcurrentWorkers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := roleSetupFixture(t, ctx, ti, "Engineering")
	const workers = 8
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			<-start
			err := processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{})
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	matches := 0
	for _, p := range list.Plugins {
		if p.Name == "Engineering" {
			matches++
		}
	}
	require.Equal(t, 1, matches)
}

func TestProcessRoleDistributionSetup_OldestActiveProject(t *testing.T) {
	t.Parallel()
	for _, tie := range []bool{false, true} {
		t.Run(fmt.Sprintf("tie=%t", tie), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			role := roleSetupFixture(t, ctx, ti, "Engineering")
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			second, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Second", Slug: "second", OrganizationID: ac.ActiveOrganizationID})
			require.NoError(t, err)
			deleted, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Deleted", Slug: "deleted", OrganizationID: ac.ActiveOrganizationID})
			require.NoError(t, err)
			err = pluginsrepo.New(ti.conn).AgeDeletedRoleSetupProjectFixture(ctx, deleted.ID)
			require.NoError(t, err)
			err = pluginsrepo.New(ti.conn).AgeRoleSetupProjectFixture(ctx, second.ID)
			require.NoError(t, err)
			expected := second.ID
			if tie {
				err = pluginsrepo.New(ti.conn).AgeRoleSetupProjectFixture(ctx, *ac.ProjectID)
				require.NoError(t, err)
				if ac.ProjectID.String() < second.ID.String() {
					expected = *ac.ProjectID
				}
			}
			require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
			for _, projectID := range []uuid.UUID{*ac.ProjectID, second.ID} {
				scoped := *ac
				scoped.ProjectID = &projectID
				scoped.ProjectSlug = nil
				list, err := ti.service.ListPlugins(contextvalues.SetAuthContext(ctx, &scoped), &gen.ListPluginsPayload{})
				require.NoError(t, err)
				matches := 0
				for _, p := range list.Plugins {
					if p.Name == "Engineering" {
						matches++
					}
				}
				if projectID == expected {
					require.Equal(t, 1, matches)
				} else {
					require.Zero(t, matches)
				}
			}
		})
	}
}

func TestProcessRoleDistributionSetup_PublicationFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := roleSetupFixture(t, ctx, ti, "Engineering")
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *ac.ProjectID, InstallationID: 12345, RepoOwner: "test-org", RepoName: "test-plugins",
		MarketplaceToken: pgtype.Text{String: "test-token", Valid: true}, PublishedMcpFingerprints: []byte(`{}`),
	})
	require.NoError(t, err)
	// Fail the final publication write, after creation and assignment.
	err = pluginsrepo.New(ti.conn).CreateRoleSetupPublicationFailureFunctionFixture(ctx)
	require.NoError(t, err)
	err = pluginsrepo.New(ti.conn).CreateRoleSetupPublicationFailureTriggerFixture(ctx)
	require.NoError(t, err)

	err = processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true})
	require.Error(t, err)

	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	for _, p := range list.Plugins {
		require.NotEqual(t, "Engineering", p.Name)
	}
	err = pluginsrepo.New(ti.conn).DropRoleSetupPublicationFailureTriggerFixture(ctx)
	require.NoError(t, err)
	before := roleSetupPublicationCount(t, ctx, ti)
	require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
	after := roleSetupPublicationCount(t, ctx, ti)
	require.Equal(t, before+1, after)
}

func TestProcessRoleDistributionSetup_GlobalRole(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	err := pluginsrepo.New(ti.conn).EnableRoleSetupFeatureFixture(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	id := uuid.New()
	role := "role:global:" + id.String()
	err = pluginsrepo.New(ti.conn).CreateRoleSetupGlobalRoleFixture(ctx, id)
	require.NoError(t, err)
	require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var pluginID string
	for _, p := range list.Plugins {
		if p.Name == "Global Engineering" {
			pluginID = p.ID
		}
	}
	require.NotEmpty(t, pluginID)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: pluginID})
	require.NoError(t, err)
	require.Len(t, got.Assignments, 1)
	require.Equal(t, "role:global:"+id.String(), got.Assignments[0].PrincipalUrn)
}

func TestProcessRoleDistributionSetup_DisableWhileInFlight(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := roleSetupFixture(t, ctx, ti, "Engineering")
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	// The processor must wait for the disabling transaction, then recheck
	// the feature after its update lock is released.
	err := pluginsrepo.New(tx).DisableRoleSetupFeatureFixture(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	finished := make(chan error, 1)

	go func() {
		err := processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{})
		finished <- err
	}()
	waitRoleSetupBlocked(t, ctx, ti, int32(tx.Conn().PgConn().PID()))
	require.NoError(t, tx.Commit(ctx))
	require.Eventually(t, func() bool { return len(finished) > 0 }, 5*time.Second, 10*time.Millisecond)
	res := <-finished
	require.NoError(t, res)
	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	for _, p := range list.Plugins {
		require.NotEqual(t, "Engineering", p.Name)
	}
}

func roleSetupPublicationCount(t *testing.T, ctx context.Context, ti *testInstance) int {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	n := 0
	for _, row := range rows {
		if row.Topic == "gram.plugins.v1.PublicationRequested" {
			n++
		}
	}
	return n
}

func TestProcessRoleDistributionSetup_DirectRemoteFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := roleSetupFixture(t, ctx, ti, "Engineering")
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Engineering"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
	require.NoError(t, err)
	server := createTestMcpServer(t, ctx, ti.conn, "setup-direct-remote", "public")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: &server.idStr, Policy: "required"})
	require.NoError(t, err)
	// Mark this existing attachment as direct-remote provenance. With no
	// resolvable rollout, audience expansion must fail closed (not complete).
	err = pluginsrepo.New(ti.conn).CreateRoleSetupCatalogRegistrationFixture(ctx, pluginsrepo.CreateRoleSetupCatalogRegistrationFixtureParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: server.id, Valid: true}})
	require.NoError(t, err)
	err = processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true})
	require.Error(t, err)

	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Assignments)
	require.Len(t, got.Servers, 1)
	// Returning to a legacy target demonstrates the intent remains retryable.
	err = pluginsrepo.New(ti.conn).DeleteRoleSetupCatalogRegistrationFixture(ctx, pluginsrepo.DeleteRoleSetupCatalogRegistrationFixtureParams{McpServerID: uuid.NullUUID{UUID: server.id, Valid: true}, ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{Enabled: true}))
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Assignments, 1)
	require.Equal(t, role, got.Assignments[0].PrincipalUrn)
}

func TestProcessRoleDistributionSetup_ConcurrentOrganizationAndGlobalRole(t *testing.T) {
	t.Parallel()
	for _, between := range []bool{false, true} {
		for _, orgFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("organization-commits-first=%t/worker-between-commits=%t", orgFirst, between), func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestPluginsService(t)
				orgTx := testenv.BeginTx(t, ctx, ti.conn)
				roleTx := testenv.BeginTx(t, ctx, ti.conn)
				organizationID := "org_setup_concurrent"
				_, err := orgrepo.New(orgTx).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: organizationID, Name: "Concurrent setup", Slug: "concurrent-setup"})
				require.NoError(t, err)
				now := conv.ToPGTimestamptz(time.Now().UTC())
				require.NoError(t, accessrepo.New(roleTx).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "concurrent-setup", WorkosName: "Concurrent Role", WorkosCreatedAt: now, WorkosUpdatedAt: now}))
				globalRole, err := accessrepo.New(roleTx).GetGlobalRoleBySlug(ctx, "concurrent-setup")
				require.NoError(t, err)
				// Neither source statement can see the other transaction's uncommitted row.
				if orgFirst {
					require.NoError(t, orgTx.Commit(ctx))
					if between {
						require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, ti.conn, organizationID, ""))
					}
					require.NoError(t, roleTx.Commit(ctx))
				} else {
					require.NoError(t, roleTx.Commit(ctx))
					if between {
						require.NoError(t, roledistribution.ProcessGlobalFanout(ctx, ti.conn, globalRole.ID, ""))
					}
					require.NoError(t, orgTx.Commit(ctx))
				}
				project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Concurrent project", Slug: "concurrent-project", OrganizationID: organizationID})
				require.NoError(t, err)
				ac, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				scoped := *ac
				scoped.ActiveOrganizationID = organizationID
				scoped.OrganizationSlug = "concurrent-setup"
				scoped.ProjectID = &project.ID
				scoped.ProjectSlug = nil
				scopedCtx := contextvalues.SetAuthContext(ctx, &scoped)
				scopedCtx = withauthzGrants(t, scopedCtx, ti.conn, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, organizationID)}, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, organizationID)})
				require.NoError(t, roledistribution.ProcessGlobalFanout(ctx, ti.conn, globalRole.ID, ""))
				require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, ti.conn, organizationID, ""))
				err = pluginsrepo.New(ti.conn).EnableRoleSetupFeatureFixture(ctx, organizationID)
				require.NoError(t, err)
				require.NoError(t, processDirectRoleSetup(scopedCtx, ti, "role:global:"+globalRole.ID.String(), plugins.PublicationRequests{Enabled: true}))
				list, err := ti.service.ListPlugins(scopedCtx, &gen.ListPluginsPayload{})
				require.NoError(t, err)
				var pluginID string
				for _, p := range list.Plugins {
					if p.Name == "Concurrent Role" {
						require.Empty(t, pluginID)
						pluginID = p.ID
					}
				}
				require.NotEmpty(t, pluginID)
				plugin, err := ti.service.GetPlugin(scopedCtx, &gen.GetPluginPayload{ID: pluginID})
				require.NoError(t, err)
				require.Len(t, plugin.Assignments, 1)
				require.Contains(t, plugin.Assignments[0].PrincipalUrn, "role:global:")
				require.Empty(t, plugin.Servers)
			})
		}
	}
}

func processDirectRoleSetup(ctx context.Context, ti *testInstance, role string, publication plugins.PublicationRequests) error {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return fmt.Errorf("missing fixture auth context")
	}
	_, err := roledistribution.ProcessRoleDistributionSetup(ctx, ti.conn, publication, ti.guard, role, ac.ActiveOrganizationID)
	if err != nil {
		return fmt.Errorf("process fixture role distribution: %w", err)
	}
	return nil
}
