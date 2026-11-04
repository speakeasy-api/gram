package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEvaluateGrantsAgreesAcrossModes answers questions about another
// principal's loaded grants the same way whichever evaluator decides, and
// never reads the request's own grants.
func TestEvaluateGrantsAgreesAcrossModes(t *testing.T) {
	t.Parallel()

	owner := "user:owner_1"
	project := Project(fixtureOrgID, "proj_1")
	cases := []struct {
		name   string
		grants []Grant
		op     Operation
		want   bool
	}{
		{name: "reader reads", grants: []Grant{grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_1")}, op: project.Read(), want: true},
		{name: "writer reads", grants: []Grant{grantOf(owner, string(ScopeProjectWrite), ResourceKindProject, "proj_1")}, op: project.Read(), want: true},
		{name: "other project", grants: []Grant{grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_2")}, op: project.Read(), want: false},
		{name: "blocked reader", grants: []Grant{
			grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_1"),
			grantOf(owner, string(ScopeProjectBlockedRead), ResourceKindProject, "proj_1"),
		}, op: project.Read(), want: false},
		{name: "no grants", grants: nil, op: project.Read(), want: false},
	}

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			// The caller's own grants allow everything the cases ask; only
			// the loaded grants may decide.
			ctx := humanClientContext(t, grantOf("role:organization:role_1", string(ScopeProjectWrite), ResourceKindProject, "*"))
			for _, tc := range cases {
				got, err := client.EvaluateGrants(ctx, tc.grants, tc.op)
				require.NoError(t, err, tc.name)
				require.Equal(t, tc.want, got, tc.name)
			}
			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
			if mode == MigrationModeShadow {
				require.Len(t, recorder.outcomes(), len(cases))
			}
		})
	}
}

func TestEvaluateGrantsRejectsAlternatives(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	ctx := humanClientContext(t)
	_, err := client.EvaluateGrants(ctx, nil, Project(fixtureOrgID, "proj_1").PublishPlugin())
	require.ErrorIs(t, err, ErrInvalidCheck)
	_, err = client.EvaluateGrants(ctx, nil, Operation{})
	require.Error(t, err)
}

// TestWriteOrAdministerFallsThroughBlocks mirrors the engine's RequireAny:
// a block on one alternative does not stop the other.
func TestWriteOrAdministerFallsThroughBlocks(t *testing.T) {
	t.Parallel()

	role := "role:organization:role_1"
	project := Project(fixtureOrgID, "proj_1")
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)

			admin := humanClientContext(t,
				grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID),
				grantOf(role, string(ScopeProjectBlockedWrite), ResourceKindProject, "proj_1"),
			)
			require.NoError(t, client.Require(admin, project.WriteOrAdminister()), "the administer alternative survives a write block")

			writer := humanClientContext(t, grantOf(role, string(ScopeProjectWrite), ResourceKindProject, "proj_1"))
			require.NoError(t, client.Require(writer, project.WriteOrAdminister()))

			reader := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
			require.Error(t, client.Require(reader, project.WriteOrAdminister()))

			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
		})
	}
}

// TestEvaluateGrantsInOrganizationRoutesOutsideAdmission follows the loaded
// organization's rollout when no request was admitted, where EvaluateGrants
// keeps the engine. Inside an admitted request both route the same way.
func TestEvaluateGrantsInOrganizationRoutesOutsideAdmission(t *testing.T) {
	t.Parallel()

	grants := []Grant{grantOf("user:owner_1", string(ScopeProjectRead), ResourceKindProject, "proj_1")}
	read := Project(fixtureOrgID, "proj_1").Read()

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			ctx := context.Background()

			allowed, err := client.EvaluateGrants(ctx, grants, read)
			require.NoError(t, err)
			require.True(t, allowed)
			require.Empty(t, recorder.outcomes(), "without admission EvaluateGrants keeps the engine")

			allowed, err = client.EvaluateGrantsInOrganization(ctx, fixtureOrgID, grants, read)
			require.NoError(t, err)
			require.True(t, allowed)
			if mode == MigrationModeShadow {
				require.Equal(t, []ShadowOutcome{ShadowMatch}, recorder.outcomes())
				require.Equal(t, fixtureOrgID, recorder.all()[0].OrganizationID)
			} else {
				require.Empty(t, recorder.outcomes())
			}

			allowed, err = client.EvaluateGrantsInOrganization(ctx, "", grants, read)
			require.NoError(t, err)
			require.True(t, allowed)
			if mode == MigrationModeShadow {
				require.Len(t, recorder.outcomes(), 1, "an empty organization keeps the engine")
			}
		})
	}
}

// TestEvaluateGrantsInOrganizationMatchesAcrossModes answers the same way in
// every mode, so moving a caller to the organization-routed form changes
// which evaluator decides and never the answer.
func TestEvaluateGrantsInOrganizationMatchesAcrossModes(t *testing.T) {
	t.Parallel()

	owner := "user:owner_1"
	read := Project(fixtureOrgID, "proj_1").Read()
	cases := []struct {
		name   string
		grants []Grant
		want   bool
	}{
		{name: "reader", grants: []Grant{grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_1")}, want: true},
		{name: "other project", grants: []Grant{grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_2")}, want: false},
		{name: "blocked", grants: []Grant{
			grantOf(owner, string(ScopeProjectRead), ResourceKindProject, "proj_1"),
			grantOf(owner, string(ScopeProjectBlockedRead), ResourceKindProject, "proj_1"),
		}, want: false},
		{name: "none", grants: nil, want: false},
	}
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			for _, tc := range cases {
				got, err := client.EvaluateGrantsInOrganization(context.Background(), fixtureOrgID, tc.grants, read)
				require.NoError(t, err, tc.name)
				require.Equal(t, tc.want, got, tc.name)
			}
			if mode == MigrationModeShadow {
				require.Len(t, recorder.outcomes(), len(cases))
			}
			for _, outcome := range recorder.outcomes() {
				require.Equal(t, ShadowMatch, outcome)
			}
		})
	}
}
