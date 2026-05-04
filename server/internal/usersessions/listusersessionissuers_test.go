package usersessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListUserSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	for _, slug := range []string{"a", "b", "c"} {
		_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
			SessionToken:       nil,
			ApikeyToken:        nil,
			ProjectSlugInput:   nil,
			Slug:               "list-" + slug,
			AuthnChallengeMode: "chain",
			SessionDurationHours: 24,
		})
		require.NoError(t, err)
	}

	got, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 3)
	require.Nil(t, got.NextCursor, "non-paged result must not return a cursor")
}

func TestListUserSessionIssuers_BadCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bad := "not-a-timestamp"
	_, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           &bad,
		Limit:            nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListUserSessionIssuers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Empty grant set under enterprise enforcement — list must be denied.
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
