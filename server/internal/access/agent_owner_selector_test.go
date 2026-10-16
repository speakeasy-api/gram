package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestAgentOwnerSelectorRoundTripsThroughManagementAPI(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"self", "employee", "*"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			selector := authz.AgentOwnerSelector(authz.ScopeAgentWrite, owner)
			generated := authzSelectorToGen(selector)
			require.Equal(t, new(owner), generated.Owner)
			require.Equal(t, selector, genSelectorToAuthz(generated))
			encoded, err := genSelectorToAuthz(generated).MarshalJSON()
			require.NoError(t, err)
			require.JSONEq(t, `{"resource_kind":"agent","resource_id":"*","owner":"`+owner+`"}`, string(encoded))
		})
	}
}
