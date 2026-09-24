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

func TestReviewSecurityCIMDPreparationRequiresExplicitGrantEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                      string
		recorded, confirmed, want []string
		state                     string
		rejected                  bool
	}{
		{"unknown", nil, nil, nil, "unknown_grants", false},
		{"unconfirmed empty", []string{}, nil, []string{}, "unknown_grants", false},
		{"unconfirmed interactive", []string{oauthwire.GrantTypeAuthorizationCode}, nil, []string{oauthwire.GrantTypeAuthorizationCode}, "unknown_grants", false},
		{"confirmed interactive is not jwt consent", nil, []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}, []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}, "manual_setup_required", false},
		{"confirmed empty clears noninteractive grants", []string{oauthwire.GrantTypeJWTBearer}, []string{}, []string{}, "manual_setup_required", false},
		{"confirmed empty preserves explicitly empty grants", []string{}, []string{}, []string{}, "manual_setup_required", false},
		{"confirmed empty cannot clear legacy interactive publication", nil, []string{}, nil, "", true},
		{"confirmed jwt cannot replace legacy interactive publication", nil, []string{oauthwire.GrantTypeJWTBearer}, nil, "", true},
		{"confirmed authorization code cannot remove legacy refresh", nil, []string{oauthwire.GrantTypeAuthorizationCode}, nil, "", true},
		{"confirmed jwt is exact", []string{}, []string{oauthwire.GrantTypeJWTBearer}, []string{oauthwire.GrantTypeJWTBearer}, "published_acceptance_unverified", false},
		{"client jwt without binding provenance needs confirmation", []string{oauthwire.GrantTypeJWTBearer}, nil, []string{oauthwire.GrantTypeJWTBearer}, "unknown_grants", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			issuer := createCIMDIssuer(t, ctx, ti, "security-cimd", "https://idp.example.com/authorize", "https://idp.example.com/token")
			preparationAdvertise(t, ctx, ti, issuer)
			user := createUserSessionIssuer(t, ctx, ti.conn, "security-human")
			client := createCimdClient(t, ctx, ti, issuer.String(), user.String(), []string{"openid"})
			in := remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: uuid.MustParse(client.ID), Resource: "https://resource.example.com/", Mechanism: "cimd", Scopes: []string{"openid"}, ConfirmGrants: tc.confirmed}
			preparationRecordGrants(t, ctx, ti, in.ClientID, tc.recorded)
			auth, _ := contextvalues.GetAuthContext(ctx)
			q := testrepo.New(ti.conn)
			grantsKey := testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}
			// A read ignores even explicit mutation inputs and must create no binding.
			_, err := ti.service.ReadIdentityChaining(ctx, in)
			require.NoError(t, err)
			count, err := q.CountPreparationFixtureBindings(ctx, *auth.ProjectID)
			require.NoError(t, err)
			require.Zero(t, count)
			grants, err := q.GetPreparationFixtureClientGrants(ctx, grantsKey)
			require.NoError(t, err)
			require.Equal(t, tc.recorded, grants)
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			if tc.rejected {
				requireOopsCode(t, err, oops.CodeBadRequest)
				require.Nil(t, result)
				count, err = q.CountPreparationFixtureBindings(ctx, *auth.ProjectID)
				require.NoError(t, err)
				require.Zero(t, count, "rejected confirmation must not persist a binding")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.state, result.State)
			}
			if tc.rejected {
				// Protecting legacy publication must not infer recorded grants.
				grants, err = q.GetPreparationFixtureClientGrants(ctx, grantsKey)
				require.NoError(t, err)
				require.Nil(t, grants)
			} else if tc.confirmed == nil {
				// Client-wide grants do not establish binding provenance.
				require.Nil(t, result.GrantTypes)
				require.Equal(t, uuid.Nil, result.ClientID)
				count, err = q.CountPreparationFixtureBindings(ctx, *auth.ProjectID)
				require.NoError(t, err)
				require.Zero(t, count, "unconfirmed selection must not persist a binding")
			} else {
				require.Equal(t, tc.want, result.GrantTypes)
			}
			grants, err = q.GetPreparationFixtureClientGrants(ctx, grantsKey)
			require.NoError(t, err)
			require.Equal(t, tc.want, grants)
			// Public metadata uses client-wide recorded grants, not a binding's state.
			mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
			rec := httptest.NewRecorder()
			require.NoError(t, mgr.HandleClientMetadataDocument(rec, cimdDocumentRequest(t, client.ID, false)))
			var doc struct {
				GrantTypes []string `json:"grant_types"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
			publicGrants := append([]string{}, tc.want...)
			if tc.want == nil {
				// Legacy interactive publication is not recorded EMA evidence.
				publicGrants = []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}
			}
			require.Equal(t, publicGrants, doc.GrantTypes)
			// After preparation, reads still cannot apply the caller's proposed grants.
			in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
			first, err := ti.service.ReadIdentityChaining(ctx, in)
			require.NoError(t, err)
			second, err := ti.service.ReadIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, first, second)
			grants, err = q.GetPreparationFixtureClientGrants(ctx, grantsKey)
			require.NoError(t, err)
			require.Equal(t, tc.want, grants)
		})
	}
}
