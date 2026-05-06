package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListUserSessionClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "list-clients-issuer",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	for _, name := range []string{"c1", "c2"} {
		_, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, name)
		require.NoError(t, err)
	}

	got, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
}

func TestListUserSessionClients_FilterByIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerA, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "client-filter-a",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerB, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "client-filter-b",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuerA.ID), "client-a-1")
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuerB.ID), "client-b-1")
	require.NoError(t, err)

	filter := issuerA.ID
	got, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: &filter,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, issuerA.ID, got.Items[0].UserSessionIssuerID)
}

func TestListUserSessionClients_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
