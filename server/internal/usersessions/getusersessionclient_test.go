package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestGetUserSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "get-client-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "the-client")
	require.NoError(t, err)

	got, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, client.ID.String(), got.ID)
	require.Equal(t, "the-client", got.ClientID)
	// client_secret_hash is not part of the API type — the Goa-generated struct
	// has no such field, so it can never reach the wire.
}

// A client's own detail response reports the same live-session tally the
// listing does, so the two surfaces cannot disagree about how many sessions a
// client holds.
func TestGetUserSessionClient_ActiveSessionCount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "get-client-count-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(issuer.ID)

	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "get-count-client")
	require.NoError(t, err)

	for _, subject := range []string{"get-count-1", "get-count-2"} {
		_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, client.ID, urn.NewUserSubject(subject))
		require.NoError(t, err)
	}

	got, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 2, got.ActiveSessionCount)
}

// A CIMD-resolved row exposes its document cache state (source URI, last
// read, expiry, validator) so the dashboard's detail panel can be built from
// the get view alone.
func TestGetUserSessionClient_CIMDCacheFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "get-client-cimd-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	documentURL := "https://client.example.com/oauth/client.json"
	seeded, err := repo.New(ti.conn).UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     uuid.MustParse(issuer.ID),
		ClientID:                documentURL,
		ClientName:              "cimd-cache-fields-client",
		RedirectUris:            []string{"https://client.example.com/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    pgtype.Text{String: `"v1"`, Valid: true},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)

	got, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               seeded.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, got.ClientIDMetadataURI)
	require.Equal(t, documentURL, *got.ClientIDMetadataURI)
	require.NotNil(t, got.ClientIDMetadataFetchedAt)
	require.NotNil(t, got.ClientIDMetadataCacheExpiresAt)
	require.NotNil(t, got.ClientIDMetadataEtag)
	require.Equal(t, `"v1"`, *got.ClientIDMetadataEtag)
}

// A DCR row keeps every CIMD cache field null; the dashboard uses the null
// metadata URI to suppress the CIMD panel entirely.
func TestGetUserSessionClient_DCRCacheFieldsNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "get-client-dcr-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "dcr-cache-fields-client")
	require.NoError(t, err)

	got, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, got.ClientIDMetadataURI)
	require.Nil(t, got.ClientIDMetadataFetchedAt)
	require.Nil(t, got.ClientIDMetadataCacheExpiresAt)
	require.Nil(t, got.ClientIDMetadataEtag)
}

func TestGetUserSessionClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetUserSessionClient_BadID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               "not-a-uuid",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// The credential kind is derived from two columns, and only one of them is on
// the wire. This pins the pair the API actually reports for a registration that
// authenticates with a signed assertion.
func TestGetUserSessionClient_ReportsCredentialKind(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "get-client-credential-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClientWithAuth(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "get-key-client", "private_key_jwt", pgtype.Text{String: "", Valid: false})
	require.NoError(t, err)

	got, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, "key", got.CredentialKind)
	require.NotNil(t, got.TokenEndpointAuthMethod)
	require.Equal(t, "private_key_jwt", *got.TokenEndpointAuthMethod)
}
