package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestRequireMemberOrganizationMatchesInEveryMode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-member-organization"
	seedOrganization(t, ctx, db, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, db, organizationID))
	seedAdminAudience(t, ctx, db, organizationID)
	org := Organization(organizationID)

	engine := NewEngine(testenv.NewLogger(t), db, staticChallengeLogging(false), workos.NewStubClient())
	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), recorder)
			client.engine = engine
			// The request's own policies grant nothing: only the named
			// user's live membership may decide.
			request := humanClientContext(t)

			require.NoError(t, client.RequireMemberOrganization(request, "admin-alpha", org.Administer()))
			require.NoError(t, client.RequireMemberOrganization(request, "ordinary-member", org.Read()))
			require.Error(t, client.RequireMemberOrganization(request, "ordinary-member", org.Administer()))
			require.Error(t, client.RequireMemberOrganization(request, "blocked-admin", org.Administer()), "the admin block holds")
			require.Error(t, client.RequireMemberOrganization(request, "not-a-member", org.Read()))
			require.Error(t, ignoreForbidden(client.RequireMemberOrganization(request, "admin-alpha", Project(organizationID, "proj_1").Read())), "only organization operations")

			for _, comparison := range recorder.all() {
				require.Equal(t, ShadowMatch, comparison.Outcome, comparison.Detail)
			}
			if mode == MigrationModeShadow {
				require.Len(t, recorder.all(), 5)
			}
		})
	}
}
