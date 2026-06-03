package variations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestVariationsService_CreateGlobal_CreatesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	created, err := ti.service.CreateGlobal(ctx, &gen.CreateGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotNil(t, created.Group)
	require.NotEmpty(t, created.Group.ID)
	require.NotEmpty(t, created.Group.Name)
	require.NotEmpty(t, created.Group.CreatedAt)
	require.NotEmpty(t, created.Group.UpdatedAt)

	// Calling again must not create a second group — it returns the existing one.
	again, err := ti.service.CreateGlobal(ctx, &gen.CreateGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.Group.ID, again.Group.ID, "createGlobal should be idempotent")

	// listGroups should now surface exactly the created group.
	list, err := ti.service.ListGroups(ctx, &gen.ListGroupsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, list.Groups, 1)
	require.Equal(t, created.Group.ID, list.Groups[0].ID)
}

func TestVariationsService_CreateGlobal_NoProjectID(t *testing.T) {
	t.Parallel()

	_, ti := newTestVariationsService(t)

	ctx := t.Context()
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: "test-org",
		UserID:               "test-user",
		SessionID:            nil,
		ProjectID:            nil,
		OrganizationSlug:     "test-org",
		Email:                nil,
		AccountType:          "free",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.CreateGlobal(ctx, &gen.CreateGlobalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}
