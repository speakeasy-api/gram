package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListFacets_ServersAndUsers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "facets-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	iid := uuid.MustParse(issuer.ID)

	_, err = seedUserSession(t, ctx, ti.conn, iid, urn.NewUserSubject("alice"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, iid, urn.NewUserSubject("alice"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, iid, urn.NewUserSubject("bob"))
	require.NoError(t, err)

	got, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Len(t, got.Servers, 1)
	require.Equal(t, issuer.ID, got.Servers[0].Value)
	require.Equal(t, "facets-issuer", got.Servers[0].DisplayName)
	require.Equal(t, int64(3), got.Servers[0].Count)

	require.Len(t, got.Users, 2)
	require.Equal(t, "user:alice", got.Users[0].Value)
	require.Equal(t, int64(2), got.Users[0].Count)
	require.Equal(t, "user:bob", got.Users[1].Value)
	require.Equal(t, int64(1), got.Users[1].Count)
}
