package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// A refresh that re-read the document must move token_endpoint_auth_method
// with the rest of the metadata. A row whose display fields tracked the
// current document while its authentication method kept an older value would
// leave the token endpoint enforcing something the client no longer declares.
func TestUpsertUserSessionClientFromCIMD_RefreshesAuthMethod(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "cimd-auth-method-refresh")

	documentURL := "https://client.example.com/oauth/client.json"
	queries := repo.New(ti.conn)

	seeded, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Original Name",
		RedirectUris:            []string{"https://client.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v1"`, Valid: true},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	require.Equal(t, "none", seeded.TokenEndpointAuthMethod.String)
	requireOrganizationID(t, ctx, seeded.OrganizationID)

	// The same client_id again, as an authorize-time refresh would, this time
	// declaring an asymmetric method.
	refreshed, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Renamed",
		RedirectUris:            []string{"https://client.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v2"`, Valid: true},
		TokenEndpointAuthMethod: "private_key_jwt",
	})
	require.NoError(t, err)
	require.Equal(t, seeded.ID, refreshed.ID, "the refresh must update the same row, not insert a second one")
	require.Equal(t, "Renamed", refreshed.ClientName)
	require.True(t, refreshed.TokenEndpointAuthMethod.Valid)
	require.Equal(t, "private_key_jwt", refreshed.TokenEndpointAuthMethod.String)
	requireOrganizationID(t, ctx, refreshed.OrganizationID)
}

// An authorize-time refresh fills in a client's organization when it has none
// but never moves one it already carries. The DO UPDATE branch leaves
// project_id untouched, so adopting the issuer's current organization instead
// would leave a row whose two tenancy columns name different owners.
func TestUpsertUserSessionClientFromCIMD_KeepsOrganizationOnRefresh(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "cimd-org-refresh")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	documentURL := "https://org-refresh.example.com/oauth/client.json"
	queries := repo.New(ti.conn)

	seeded, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Original Name",
		RedirectUris:            []string{"https://org-refresh.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v1"`, Valid: true},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	requireOrganizationID(t, ctx, seeded.OrganizationID)

	// Repoint the issuer at a second organization, the only way to make the
	// parent's tenancy disagree with the child's.
	otherOrgID := "org-" + uuid.NewString()
	trialStart := time.Now().UTC()
	testQueries := testrepo.New(ti.conn)
	require.NoError(t, testQueries.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 otherOrgID,
		Name:               "Other Org",
		Slug:               "other-org-" + uuid.NewString()[:8],
		GramAccountType:    "free",
		WorkosID:           pgtype.Text{String: "", Valid: false},
		Whitelisted:        false,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: trialStart, InfinityModifier: 0, Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: trialStart.Add(14 * 24 * time.Hour), InfinityModifier: 0, Valid: true},
		DisabledAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		CreatedAt:          pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	}))
	require.NoError(t, testQueries.SetUserSessionIssuerOrganizationID(ctx, testrepo.SetUserSessionIssuerOrganizationIDParams{
		OrganizationID: conv.ToPGText(otherOrgID),
		ID:             issuerID,
		ProjectID:      *authCtx.ProjectID,
	}))

	refreshed, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Renamed",
		RedirectUris:            []string{"https://org-refresh.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v2"`, Valid: true},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	require.Equal(t, seeded.ID, refreshed.ID, "the refresh must update the same row, not insert a second one")
	require.Equal(t, "Renamed", refreshed.ClientName, "the refresh must still replace mutable metadata")
	require.Equal(t, seeded.OrganizationID.String, refreshed.OrganizationID.String, "a refresh must not move the client to the issuer's new organization")
	require.Equal(t, seeded.ProjectID, refreshed.ProjectID)
}

// A refresh replaces both key-source columns wholesale, writing the unused
// one as NULL. A client that moves from an inline key set to a jwks_uri must
// not leave both set: the schema forbids that, and an upsert that only wrote
// the column the new document supplied would trip the constraint on every
// retry, wedging the client with a 500 on the authorize path.
func TestUpsertUserSessionClientFromCIMD_ReplacesKeySourceWholesale(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "cimd-key-source-swap")

	documentURL := "https://client.example.com/oauth/client.json"
	queries := repo.New(ti.conn)

	inline, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Inline Keys",
		RedirectUris:            []string{"https://client.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v1"`, Valid: true},
		TokenEndpointAuthMethod: "private_key_jwt",
		ClientJwks:              []byte(`{"keys":[]}`),
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[]}`, string(inline.ClientJwks))
	require.False(t, inline.ClientJwksUri.Valid)

	remote, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Remote Keys",
		RedirectUris:            []string{"https://client.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v2"`, Valid: true},
		TokenEndpointAuthMethod: "private_key_jwt",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "https://client.example.com/jwks.json", Valid: true},
	})
	require.NoError(t, err, "moving between key-source forms must not trip the mutual-exclusion constraint")
	require.Equal(t, inline.ID, remote.ID)
	require.Empty(t, remote.ClientJwks, "the inline set must be cleared when the document moves to a jwks_uri")
	require.Equal(t, "https://client.example.com/jwks.json", remote.ClientJwksUri.String)
}

// A 304 revalidation deliberately leaves the authentication method alone: the
// host asserted the document has not changed, and this is the one CIMD writer
// that re-read no document to derive a method from.
func TestUpdateUserSessionClientCIMDCache_LeavesAuthMethodAlone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "cimd-auth-method-304")

	documentURL := "https://client.example.com/oauth/client.json"
	queries := repo.New(ti.conn)

	seeded, err := queries.UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                documentURL,
		ClientName:              "Cached Name",
		RedirectUris:            []string{"https://client.example.com/cb"},
		CacheTtlSeconds:         1,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v1"`, Valid: true},
		TokenEndpointAuthMethod: "private_key_jwt",
	})
	require.NoError(t, err)

	revalidated, err := queries.UpdateUserSessionClientCIMDCache(ctx, repo.UpdateUserSessionClientCIMDCacheParams{
		ID:                   seeded.ID,
		CacheTtlSeconds:      3600,
		ClientIDMetadataEtag: pgtype.Text{String: `"v1"`, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, "private_key_jwt", revalidated.TokenEndpointAuthMethod.String,
		"a 304 confirms the stored document, so the method it declared still stands")
	require.Equal(t, "Cached Name", revalidated.ClientName)
}

// The DCR path records the method the registration named, so a row inserted
// after this column existed never reads as the NULL that means "predates it".
func TestCreateUserSessionClient_PersistsAuthMethod(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "dcr-auth-method")

	row, err := repo.New(ti.conn).CreateUserSessionClient(ctx, repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: "", Valid: false},
		ClientName:              "dcr client",
		RedirectUris:            []string{"https://app.acme.test/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	require.True(t, row.TokenEndpointAuthMethod.Valid, "a freshly registered client must not read as predating the column")
	require.Equal(t, "none", row.TokenEndpointAuthMethod.String)
}
