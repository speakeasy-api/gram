package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestPreparationUserIssuerLookupPreservesNotFound(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"missing", "foreign project", "foreign organization"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			for _, operation := range []string{"prepare", "unlink", "read"} {
				t.Run(operation, func(t *testing.T) {
					t.Parallel()
					ctx, ti, in := preparationFixture(t)
					switch scope {
					case "missing":
						in.UserSessionIssuerID = uuid.New()
					case "foreign project":
						project := createProject(t, ctx, ti.conn, "lookup-other-project")
						in.UserSessionIssuerID = createUserSessionIssuerInProject(t, ctx, ti.conn, project, "lookup-other-user")
					case "foreign organization":
						org := createOrganization(t, ctx, ti.conn, "lookup-other-org")
						issuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, org, "lookup-other-issuer")
						in.UserSessionIssuerID = createTrustedOrganizationTierUserSessionIssuerForOrganization(t, ctx, ti.conn, org, "lookup-other-user", issuer)
					}
					recorded := []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}
					preparationRecordGrants(t, ctx, ti, in.ClientID, recorded)
					in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
					var result *remotesessions.PreparationResult
					var err error
					switch operation {
					case "prepare":
						result, err = ti.service.PrepareIdentityChaining(ctx, in)
					case "unlink":
						result, err = ti.service.UnlinkIdentityChaining(ctx, in)
					case "read":
						result, err = ti.service.ReadIdentityChaining(ctx, in)
					}
					requireOopsCode(t, err, oops.CodeNotFound)
					require.Nil(t, result)
					auth, _ := contextvalues.GetAuthContext(ctx)
					q := testrepo.New(ti.conn)
					count, err := q.CountPreparationFixtureBindings(ctx, *auth.ProjectID)
					require.NoError(t, err)
					require.Zero(t, count, "rejected issuer must not create a binding")
					grants, err := q.GetPreparationFixtureClientGrants(ctx, testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
					require.NoError(t, err)
					require.Equal(t, recorded, grants, "rejected issuer must not alter client grants")
				})
			}
		})
	}
}
