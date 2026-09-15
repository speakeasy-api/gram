package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessRoleMutationToolsAreStableExternalOnlyMutations(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	for _, name := range []string{operationCreateMCPAccessRole, operationUpdateMCPAccessRole, operationAssignMCPAccessRole} {
		descriptor := descriptorByName(t, registrar, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.NotNil(t, descriptor.Annotations)
		require.False(t, descriptor.Annotations.ReadOnlyHint)
		require.True(t, descriptor.Annotations.IdempotentHint)
		require.NotNil(t, descriptor.Annotations.DestructiveHint)
		require.Equal(t, name == operationUpdateMCPAccessRole, *descriptor.Annotations.DestructiveHint)

		arguments := json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001","role_reference":"opaque","expected_version":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","add_rules":[],"remove_rules":[],"idempotency_key":"key","confirmed":true}`)
		if name == operationCreateMCPAccessRole {
			arguments = json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001","name":"Operators","rules":[],"idempotency_key":"key","confirmed":true}`)
		}
		if name == operationAssignMCPAccessRole {
			arguments = json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001","member_reference":"opaque-member","role_reference":"opaque-role","expected_version":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","idempotency_key":"key","confirmed":true}`)
		}
		_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), registrationServicePrincipal()), arguments)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.JSONEq(t, `{"code":"feature_unavailable","feature":"access_role_mutations","message":"This Platform MCP capability is not enabled for the current rollout."}`, refusal.Payload)
	}
}
