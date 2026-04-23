package access

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestRoleGrantPayloadsPreservesNilAndEmptyResources(t *testing.T) {
	t.Parallel()

	grants := roleGrantPayloads([]*gen.RoleGrant{
		{Scope: string(authz.ScopeProjectRead), Resources: nil},
		{Scope: string(authz.ScopeProjectWrite), Resources: []string{}},
	})

	require.Len(t, grants, 2)
	require.Nil(t, grants[0].Resources)
	require.NotNil(t, grants[1].Resources)
	require.Empty(t, grants[1].Resources)
}
