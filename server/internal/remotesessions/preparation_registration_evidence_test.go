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

func TestPreparationEvidence_CIMDUnknownRequiresConfirmation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuer := createCIMDIssuer(t, ctx, ti, "review-cimd", "https://idp.example.com/authorize", "https://idp.example.com/token")
	preparationAdvertise(t, ctx, ti, issuer)
	user := createUserSessionIssuer(t, ctx, ti.conn, "review-human")
	client := createCimdClient(t, ctx, ti, issuer.String(), user.String(), []string{"openid"})
	in := remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: uuid.MustParse(client.ID), Resource: "https://resource.example.com/", Mechanism: "cimd", Scopes: []string{"openid"}}
	preparationRecordGrants(t, ctx, ti, in.ClientID, nil)
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unknown_grants", result.State)
	require.Nil(t, result.GrantTypes)
	auth, _ := contextvalues.GetAuthContext(ctx)
	grants, err := testrepo.New(ti.conn).GetPreparationFixtureClientGrants(ctx, testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	require.Nil(t, grants)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	in.ExpectedGeneration = result.Generation
	before, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	confirmed, err := ti.service.PrepareIdentityChaining(ctx, in)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Nil(t, confirmed)
	grants, err = testrepo.New(ti.conn).GetPreparationFixtureClientGrants(ctx, testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	require.Nil(t, grants, "rejecting removal of legacy interactive publication must not infer recorded grants")
	unchanged, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "rejected confirmation must not change binding evidence")
	in.ConfirmGrants = []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken, oauthwire.GrantTypeJWTBearer}
	confirmed, err = ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "published_acceptance_unverified", confirmed.State)
	require.Equal(t, in.ConfirmGrants, confirmed.GrantTypes, "record only the explicitly confirmed safe grant set")
	require.Equal(t, "cimd_published", confirmed.GrantSource)
}

func TestPreparationEvidence_ManualRecordsEvidenceWithoutDiscoveryReadiness(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"unsupported_profile", "transient_failure"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			issuer := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "review-manual", ""))
			user := createUserSessionIssuer(t, ctx, ti.conn, "review-human")
			auth, _ := contextvalues.GetAuthContext(ctx)
			client := preparationManualClient(t, ctx, ti, *auth.ProjectID, issuer, "review-client")
			if state == "transient_failure" {
				preparationAdvertise(t, ctx, ti, issuer)
				require.NoError(t, testrepo.New(ti.conn).FailPreparationFixtureIssuerMetadata(ctx, testrepo.FailPreparationFixtureIssuerMetadataParams{ID: issuer, ProjectID: conv.ToNullUUID(*auth.ProjectID)}))
			}
			in := remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: client, Resource: "https://resource.example.com/", Mechanism: "manual", ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer}, Scopes: []string{"openid"}}
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, state, result.State, "manual configuration must not override readiness eligibility")
			require.Equal(t, client, result.ClientID)
			require.Equal(t, "administrator_declared", result.GrantSource)
			require.Equal(t, []string{oauthwire.GrantTypeJWTBearer}, result.GrantTypes)
			current, err := ti.service.ReadIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, result, current)
			if state == "unsupported_profile" {
				preparationAdvertise(t, ctx, ti, issuer)
				current, err = ti.service.ReadIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, "ready", current.State)
				require.Equal(t, result.Generation, current.Generation)
			}
		})
	}
}
