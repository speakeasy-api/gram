package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetUserSessionIssuerByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "get-by-id",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	id := created.ID
	got, err := ti.service.GetUserSessionIssuer(ctx, &gen.GetUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &id,
		Slug:             nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "get-by-id", got.Slug)
}

func TestGetUserSessionIssuerBySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "get-by-slug",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	slug := "get-by-slug"
	got, err := ti.service.GetUserSessionIssuer(ctx, &gen.GetUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               nil,
		Slug:             &slug,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
}

func TestGetUserSessionIssuer_BothIDAndSlugRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := "x"
	_, err := ti.service.GetUserSessionIssuer(ctx, &gen.GetUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &id,
		Slug:             &slug,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetUserSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetUserSessionIssuer(ctx, &gen.GetUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &id,
		Slug:             nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetUserSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "rbac-get",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	// Empty grant set under enterprise enforcement — get must be denied.
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	id := created.ID
	_, err = ti.service.GetUserSessionIssuer(ctx, &gen.GetUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &id,
		Slug:             nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
