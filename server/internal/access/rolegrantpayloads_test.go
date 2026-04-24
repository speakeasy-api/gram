package access

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestRoleGrantPayloadsPreservesNilAndNonNilSelectors(t *testing.T) {
	t.Parallel()

	grants := roleGrantPayloads([]*gen.RoleGrant{
		{Scope: string(authz.ScopeProjectRead), Selectors: nil},
		{Scope: string(authz.ScopeProjectWrite), Selectors: []map[string]string{
			{"resource_kind": "project", "resource_id": "proj-1"},
		}},
	})

	require.Len(t, grants, 2)
	require.Nil(t, grants[0].Selectors)
	require.Len(t, grants[1].Selectors, 1)
	require.Equal(t, "proj-1", grants[1].Selectors[0].ResourceID())
}
