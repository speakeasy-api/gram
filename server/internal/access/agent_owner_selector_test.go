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
			require.Equal(t, selector, genSelectorToAuthz(authzSelectorToGen(selector)))
		})
	}
}
