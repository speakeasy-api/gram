package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
