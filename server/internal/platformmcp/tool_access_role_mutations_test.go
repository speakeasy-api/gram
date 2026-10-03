package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessRoleMutationToolsAreStableExternalOnlyMutations(t *testing.T) {
	t.Parallel()

	_, registrar := newTestServer(t)
	for _, name := range []string{operationCreateMCPAccessRole, operationUpdateMCPAccessRole, operationAssignMCPAccessRole} {
		descriptor := descriptorByName(t, registrar, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.NotNil(t, descriptor.Annotations)
		require.False(t, descriptor.Annotations.ReadOnlyHint)
		require.True(t, descriptor.Annotations.IdempotentHint)
		require.NotNil(t, descriptor.Annotations.DestructiveHint)
		require.Equal(t, name == operationUpdateMCPAccessRole, *descriptor.Annotations.DestructiveHint)

	}
}
