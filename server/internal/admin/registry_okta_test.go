package admin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	idprepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// plantOktaTenant creates an organization with a verified Okta connection and
// the given active application names, one app per name, all assigned.
func plantOktaTenant(t *testing.T, ctx context.Context, db *pgxpool.Pool, apps map[string]string) {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Okta Tenant", Slug: "okta-tenant-" + uuid.NewString(), WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	connectionID := provisiontest.CreateConnection(t, ctx, db, orgID, identityproviderconnections.ProviderOkta)
	issuerID := provisiontest.CreateIssuer(t, ctx, db, orgID, uuid.NullUUID{}, "https://tenant.okta.com/oauth2/v1/token")
	issuer, err := repo.New(db).AdminGetIssuerFixture(ctx, issuerID)
	require.NoError(t, err)
	client, err := remotesessionsrepo.New(db).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:                       uuid.NullUUID{},
		OrganizationID:                  conv.ToPGText(orgID),
		RemoteSessionIssuerID:           issuerID,
		ClientID:                        "0oa" + uuid.NewString()[:17],
		ClientSecretEncrypted:           pgtype.Text{},
		ClientIDIssuedAt:                pgtype.Timestamptz{},
		ClientSecretExpiresAt:           pgtype.Timestamptz{},
		TokenEndpointAuthMethod:         pgtype.Text{},
		TokenEndpointAuthAudienceFormat: pgtype.Text{},
		Scope:                           []string{},
		Audience:                        pgtype.Text{},
		LegacyCallbackUrl:               false,
		JsonWebKeySetID:                 uuid.NullUUID{},
		IdentityProviderConnectionID:    uuid.NullUUID{},
	})
	require.NoError(t, err)
	_, err = idprepo.New(db).CreateOktaIdentityProviderConnection(ctx, idprepo.CreateOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: connectionID, OrganizationID: orgID, OrgUrl: "https://tenant.okta.com", IssuerUrl: issuer.Issuer,
		RemoteSessionIssuerID: issuerID, RemoteSessionClientID: client.ID, ListingMode: identityproviderconnections.ListingModeCustomApp,
	})
	require.NoError(t, err)
	names, labels, modes, statuses := []string{}, []string{}, []string{}, []string{}
	ids := []string{}
	for name, label := range apps {
		ids = append(ids, "0oa"+uuid.NewString()[:17])
		names = append(names, name)
		labels = append(labels, label)
		modes = append(modes, "SAML_2_0")
		statuses = append(statuses, "ACTIVE")
	}
	q := apprepo.New(db)
	seen := pgtype.Timestamptz{Valid: true, InfinityModifier: pgtype.Finite}
	require.NoError(t, q.UpsertApplications(ctx, apprepo.UpsertApplicationsParams{
		OrganizationID: orgID, IdentityProviderConnectionID: connectionID,
		OktaAppIds: ids, Labels: labels, Names: names, SignOnModes: modes, Statuses: statuses,
		FeaturesCsv: make([]string, len(ids)), OktaCreatedAts: make([]pgtype.Timestamptz, len(ids)), OktaLastUpdatedAts: make([]pgtype.Timestamptz, len(ids)), SeenAt: seen,
	}))
}

func TestRegistryOktaCandidatesAndUnmapped(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)

	plantOktaTenant(t, ctx, db, map[string]string{"notion": "Notion", "integrator-4080826_linear_1": "Linear", "realtime_board": "Miro", "okta_enduser": "Okta Dashboard"})
	plantOktaTenant(t, ctx, db, map[string]string{"notion": "Notion (SAML)", "slack": "Slack"})

	registry := registryrepo.New(db)
	linear := uuid.New()
	require.NoError(t, registry.InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: linear, Published: true, Data: json.RawMessage(`{"server":{"name":"app.linear/mcp","title":"Linear","description":"Linear","version":"1","remotes":[{"type":"streamable-http","url":"https://mcp.linear.app/mcp"}]}}`)}))
	notion := uuid.New()
	require.NoError(t, registry.InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: notion, Published: true, Data: json.RawMessage(`{"server":{"name":"com.notion/mcp","title":"Notion","description":"Notion","version":"1","remotes":[{"type":"streamable-http","url":"https://mcp.notion.com/mcp"}]},"_meta":{"com.speakeasy.ai/okta":{"oinNames":["notion"]}}}`)}))
	miro := uuid.New()
	require.NoError(t, registry.InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: miro, Published: false, Data: json.RawMessage(`{"server":{"name":"com.miro/mcp","title":"Miro","description":"Miro","version":"1","remotes":[{"type":"streamable-http","url":"https://mcp.miro.com/mcp"}]}}`)}))

	// Linear: the integrator instance matches on domain and is unclaimed.
	res, err := svc.GetRegistryOktaCandidates(ctx, &gen.GetRegistryOktaCandidatesPayload{AdminSessionToken: nil, ID: linear.String()})
	require.NoError(t, err)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, "integrator-4080826_linear_1", res.Candidates[0].OinName)
	require.Equal(t, "domain", res.Candidates[0].Reason)
	require.Equal(t, 1, res.Candidates[0].Organizations)
	require.Nil(t, res.Candidates[0].MappedBy)

	// Notion already carries its key, so nothing is proposed for it.
	res, err = svc.GetRegistryOktaCandidates(ctx, &gen.GetRegistryOktaCandidatesPayload{AdminSessionToken: nil, ID: notion.String()})
	require.NoError(t, err)
	require.Empty(t, res.Candidates)

	// Miro matches on a tenant label; a key another entry holds says so.
	res, err = svc.GetRegistryOktaCandidates(ctx, &gen.GetRegistryOktaCandidatesPayload{AdminSessionToken: nil, ID: miro.String()})
	require.NoError(t, err)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, "realtime_board", res.Candidates[0].OinName)
	require.Equal(t, "label", res.Candidates[0].Reason)

	_, err = svc.GetRegistryOktaCandidates(ctx, &gen.GetRegistryOktaCandidatesPayload{AdminSessionToken: nil, ID: "nope"})
	require.Error(t, err)

	// Unmapped: notion is claimed, Okta's own app is dropped, the rest are
	// ranked by organizations with the proposed entry when one matches.
	unmapped, err := svc.ListRegistryOktaUnmapped(ctx, &gen.ListRegistryOktaUnmappedPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	names := make([]string, 0, len(unmapped.Names))
	for _, n := range unmapped.Names {
		names = append(names, n.OinName)
	}
	require.Equal(t, []string{"integrator-4080826_linear_1", "realtime_board", "slack"}, names)
	require.Equal(t, "app.linear/mcp", conv.PtrValOrEmpty(unmapped.Names[0].SuggestedEntryName, ""))
	require.Equal(t, linear.String(), conv.PtrValOrEmpty(unmapped.Names[0].SuggestedEntryID, ""))
	require.Equal(t, "com.miro/mcp", conv.PtrValOrEmpty(unmapped.Names[1].SuggestedEntryName, ""))
	require.Nil(t, unmapped.Names[2].SuggestedEntryID)
}
