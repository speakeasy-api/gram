package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

// A server-selected client ID does not make an invalid issuer binding visible.
// Keep the inherited tiers usable for both project and organization rotation.
func TestRotationIssuerVisibility(t *testing.T) {
	t.Parallel()
	for _, clientScope := range []string{"project", "legacy-project", "organization"} {
		for _, issuerScope := range []string{"project", "sibling-project", "organization", "foreign-organization", "global"} {
			t.Run(clientScope+"/"+issuerScope, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestService(t)
				auth, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				q := repo.New(ti.conn)
				registration, registrations := newRegistrationServer(t)
				project := conv.ToNullUUID(*auth.ProjectID)
				organization := conv.ToPGText(auth.ActiveOrganizationID)
				issuerProject, issuerOrganization := uuid.NullUUID{}, organization
				switch issuerScope {
				case "project":
					issuerProject = project
				case "sibling-project":
					issuerProject = conv.ToNullUUID(createProject(t, ctx, ti.conn, "rotation-sibling"))
				case "foreign-organization":
					foreign := uuid.NewString()
					require.NoError(t, q.CreateTestTrustedIssuerOrganization(ctx, repo.CreateTestTrustedIssuerOrganizationParams{ID: foreign, Name: "Rotation fixture", Slug: foreign}))
					issuerOrganization = conv.ToPGText(foreign)
				case "global":
					issuerOrganization = pgtype.Text{}
				}
				issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
					ProjectID: issuerProject, OrganizationID: issuerOrganization,
					Slug: "rotation-visibility-" + uuid.NewString(), Issuer: registration.URL,
					AuthorizationEndpoint:             conv.ToPGText(registration.URL + "/authorize"),
					RegistrationEndpoint:              conv.ToPGText(registration.URL + "/register"),
					ScopesSupported:                   []string{"openid"},
					TokenEndpoint:                     conv.ToPGText(registration.URL + "/token"),
					GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
					ResponseTypesSupported:            []string{"code"},
					TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
				})
				require.NoError(t, err)
				clientProject, clientOrganization := project, organization
				switch clientScope {
				case "organization":
					clientProject = uuid.NullUUID{}
				case "legacy-project":
					clientOrganization = pgtype.Text{}
				}
				client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
					ProjectID: clientProject, OrganizationID: clientOrganization,
					RemoteSessionIssuerID: issuer.ID, ClientID: "original-registration",
					TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"),
				})
				require.NoError(t, err)
				row, err := q.GetRemoteSessionClientForRotation(ctx, client.ID)
				visible := issuerScope == "organization" || issuerScope == "global" || (issuerScope == "project" && clientScope != "organization")
				if !visible {
					require.ErrorIs(t, err, pgx.ErrNoRows)
					_, err = ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{ID: client.ID.String()})
					require.Error(t, err)
					require.Zero(t, registrations.Load())
					return
				}
				require.NoError(t, err)
				require.Equal(t, issuer.ProjectID, row.IssuerProjectID)
				require.Equal(t, issuer.OrganizationID, row.IssuerOrganizationID)
				// The admin list requires organization_id on the client or issuer;
				// a legacy project client under a global issuer is login-only.
				if clientScope == "legacy-project" && issuerScope == "global" {
					return
				}
				rotated, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{ID: client.ID.String()})
				require.NoError(t, err)
				require.Equal(t, "rotated-cid", rotated.ClientID)
				require.EqualValues(t, 1, registrations.Load())
			})
		}
	}
}
