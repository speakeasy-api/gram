package variations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/variations"
)

func TestVariationsService_ListGroups_EmptyByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	result, err := ti.service.ListGroups(ctx, &gen.ListGroupsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Groups, "no groups should exist until one is created")
}

func TestVariationsService_ListGroups_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestVariationsService(t)

	_, err := ti.service.ListGroups(t.Context(), &gen.ListGroupsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}
