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
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestWorkloadInference_OrganizationIssuerBindsTargetProject(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := f.fixtureContext(t)
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
}
