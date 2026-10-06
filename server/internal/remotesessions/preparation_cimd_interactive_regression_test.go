package remotesessions_test

import (
	"encoding/json"
	"net/http/httptest"
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

func TestPreparationCIMDPreservesInteractiveGrants(t *testing.T) {
	t.Parallel()
	for _, legacy := range []bool{false, true} {
		for _, mechanism := range []string{"cimd", "manual"} {
			for _, tc := range []struct {
				name      string
				confirmed []string
			}{
				{"remove authorization code", []string{oauthwire.GrantTypeRefreshToken, oauthwire.GrantTypeJWTBearer}},
				{"remove refresh token", []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeJWTBearer}},
				{"replace with jwt", []string{oauthwire.GrantTypeJWTBearer}},
				{"clear grants", []string{}},
			} {
				name := mechanism + "/" + tc.name
				if legacy {
					name += "/legacy NULL"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					ctx, ti := newTestService(t)
					issuer := createCIMDIssuer(t, ctx, ti, "interactive-cimd", "https://idp.example.com/authorize", "https://idp.example.com/token")
					preparationAdvertise(t, ctx, ti, issuer)
					user := createUserSessionIssuer(t, ctx, ti.conn, "interactive-human")
					client := createCimdClient(t, ctx, ti, issuer.String(), user.String(), []string{"openid"})
					in := remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: uuid.MustParse(client.ID), Resource: "https://resource.example.com/", Mechanism: mechanism, Scopes: []string{"openid"}, ConfirmGrants: tc.confirmed}
					original := []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}
					if legacy {
						original = nil
					}
					preparationRecordGrants(t, ctx, ti, in.ClientID, original)
					auth, _ := contextvalues.GetAuthContext(ctx)
					q := testrepo.New(ti.conn)
					key := testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}
					_, err := ti.service.PrepareIdentityChaining(ctx, in)
					requireOopsCode(t, err, oops.CodeBadRequest)
					grants, err := q.GetPreparationFixtureClientGrants(ctx, key)
					require.NoError(t, err)
					require.Equal(t, original, grants)
					count, err := q.CountPreparationFixtureBindings(ctx, *auth.ProjectID)
					require.NoError(t, err)
					require.Zero(t, count, "rejected publication must roll back the binding")

					// Explicitly confirming the union adds JWT without guessing grants.
					combined := []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken, oauthwire.GrantTypeJWTBearer}
					in.ConfirmGrants = combined
					prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
					require.NoError(t, err)
					require.ElementsMatch(t, combined, prepared.GrantTypes)
					in.ExpectedGeneration = prepared.Generation
					before, err := ti.service.ReadIdentityChaining(ctx, in)
					require.NoError(t, err)
					in.ConfirmGrants = tc.confirmed
					_, err = ti.service.PrepareIdentityChaining(ctx, in)
					requireOopsCode(t, err, oops.CodeBadRequest)
					after, err := ti.service.ReadIdentityChaining(ctx, in)
					require.NoError(t, err)
					require.Equal(t, before, after, "rejected updates preserve generation and provenance")

					mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
					rec := httptest.NewRecorder()
					require.NoError(t, mgr.HandleClientMetadataDocument(rec, cimdDocumentRequest(t, client.ID, false)))
					var doc struct {
						GrantTypes    []string `json:"grant_types"`
						ResponseTypes []string `json:"response_types"`
					}
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
					require.ElementsMatch(t, combined, doc.GrantTypes)
					require.Equal(t, []string{oauthwire.ResponseTypeCode}, doc.ResponseTypes)
					require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, in.ClientID, user))
				})
			}
		}
	}
}
