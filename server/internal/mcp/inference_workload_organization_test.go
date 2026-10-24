package mcp_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestWorkloadInference_OrganizationIssuerBindsTargetProject(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"organization", "project"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			ctx := f.fixtureContext(t)
			if tier == "project" {
				// The same provider URL at project tier must be selected using the requested
				// inference project, not the organization issuer's null project.
				projectIssuer, err := testrepo.New(f.ti.conn).CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
					OrganizationID: f.fx.orgID, ProjectID: uuid.NullUUID{UUID: f.fx.target.ProjectID, Valid: true},
					Name: "project-inference", Issuer: f.issuer.OAuth21URL, JwksUri: oauthtest.DiscoverWorkloadJWKSURI(t, f.issuer),
				})
				require.NoError(t, err)
				f.issuerID = projectIssuer
				require.NoError(t, testrepo.New(f.ti.conn).CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
					OrganizationID: f.fx.orgID, ProjectID: uuid.NullUUID{UUID: f.fx.target.ProjectID, Valid: true}, WorkloadIssuerID: f.issuerID, Subject: f.subject,
				}))
				f.assignmentID = assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, f.issuerID, f.subject, f.agentID)
			}
			enableWorkloadInference(t, f)
			issuer, err := usersessionsrepo.New(f.ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
				OrganizationID: conv.ToPGText(f.fx.orgID), Slug: "inference-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive",
				SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
			})
			require.NoError(t, err)
			setIssuerMode(t, ctx, f.ti, issuer.ID, "shared", "")
			issuerURL := f.ti.serverURL.String() + "/oauth/usi/" + issuer.ID.String()
			w := sharedToken(t, t.Context(), f.ti, issuer.ID.String(), url.Values{
				"grant_type": {workloadGrantJWTBearer}, "assertion": {f.assertion(t, issuerURL)}, "resource": {mcp.InferenceResource}, "project_id": {f.fx.target.ProjectID.String()},
			})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			token := decodeSharedTokenResponse(t, w).AccessToken
			authorized, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
			require.NoError(t, err)
			require.True(t, handled)
			authCtx, ok := contextvalues.GetAuthContext(authorized)
			require.True(t, ok)
			require.Equal(t, f.fx.target.ProjectID, *authCtx.ProjectID)
			// Organization issuer rows have no project_id; the immutable inference
			// resource and ceiling must retain the exact target rather than losing it.
			claims := unverifiedClaims(t, token)
			jti, ok := claims["jti"].(string)
			require.True(t, ok)
			row, err := usersessionsrepo.New(f.ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: jti})
			require.NoError(t, err)
			require.False(t, row.ProjectID.Valid)
			require.Equal(t, mcp.InferenceResource+":"+f.fx.target.ProjectID.String(), row.Resource.String)
			require.False(t, row.ResourceIdentity.Valid, "inference sessions carry no gateway identity")
			if tier == "project" {
				affected, err := testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(ctx, testrepo.SoftDeleteWorkloadAdmissionFixtureParams{OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject})
				require.NoError(t, err)
				require.Equal(t, int64(1), affected)
				_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
				require.True(t, handled)
				require.Error(t, err, "project-tier admission revocation must revoke an existing inference credential")
				retry := sharedToken(t, t.Context(), f.ti, issuer.ID.String(), url.Values{
					"grant_type": {workloadGrantJWTBearer}, "assertion": {f.assertion(t, issuerURL)}, "resource": {mcp.InferenceResource}, "project_id": {f.fx.target.ProjectID.String()},
				})
				require.NotEqual(t, http.StatusOK, retry.Code, "revoked project admission must also reject a fresh exchange")
			}
		})
	}

}
