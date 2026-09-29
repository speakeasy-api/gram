package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestProjectEligibilityDurableLifecycleHintsCreateRollsBack(t *testing.T) {
	t.Parallel()
	testProjectEligibilityDurableLifecycleHints(t, "create")
}

func TestProjectEligibilityDurableLifecycleHintsDeleteRollsBack(t *testing.T) {
	t.Parallel()
	testProjectEligibilityDurableLifecycleHints(t, "delete")
}

func testProjectEligibilityDurableLifecycleHints(t *testing.T, operation string) {
	t.Helper()
	ctx, ti := newTestProjectsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{PrincipalUrn: "", Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, ac.ActiveOrganizationID)})
	projectID := uuid.Nil
	if operation == "delete" {
		projectID = createProjectForDeletion(t, ctx, ti, "lifecycle-"+uuid.NewString()[:8]).ID
	}
	beforeRows, readErr := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, readErr)
	var before int
	for _, row := range beforeRows {
		if row.Topic == "gram.plugins.v1.RoleProvisioningRequested" {
			before++
		}
	}
	_, err := ti.conn.Exec(ctx, `CREATE FUNCTION reject_lifecycle_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic='gram.plugins.v1.RoleProvisioningRequested' THEN RAISE EXCEPTION 'injected hint failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_lifecycle_hint BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION reject_lifecycle_hint()`) //nolint:glint // notestingrawsql: failure injection
	require.NoError(t, err)
	name := "created-" + uuid.NewString()[:8]
	if operation == "create" {
		_, err = ti.service.CreateProject(ctx, &gen.CreateProjectPayload{ApikeyToken: nil, SessionToken: nil, OrganizationID: ac.ActiveOrganizationID, Name: name})
	} else {
		err = ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{ID: projectID.String(), ApikeyToken: nil, SessionToken: nil})
	}
	require.Error(t, err)
	afterRows, readErr := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, readErr)
	var after int
	for _, row := range afterRows {
		if row.Topic == "gram.plugins.v1.RoleProvisioningRequested" {
			after++
		}
	}
	require.Equal(t, before, after)
	if operation == "create" {
		projects, readErr := projectrepo.New(ti.conn).ListProjectsByOrganization(ctx, ac.ActiveOrganizationID)
		require.NoError(t, readErr)
		for _, candidate := range projects {
			require.NotEqual(t, name, candidate.Name)
		}
	} else {
		retained, readErr := projectrepo.New(ti.conn).GetProjectByID(ctx, projectID)
		require.NoError(t, readErr)
		require.Equal(t, projectID, retained.ID)
	}
}
