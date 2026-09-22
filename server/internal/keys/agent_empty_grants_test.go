package keys_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestKeysService_AgentKeyRequiresRequestedGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)

	// Live agent and owner policy cannot supply an omitted credential ceiling.
	for _, test := range []struct {
		name   string
		grants []*gen.AgentPolicyGrantForm
		legacy bool
	}{
		{name: "empty", grants: []*gen.AgentPolicyGrantForm{}},
		{name: "omitted"},
		{name: "legacy agent ID only", legacy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestKeysService(t)
			agentID, projectID := createAgentKeyFixture(t, ctx, ti)
			payload := agentKeyPayload(agentID, projectID)
			payload.RequestedGrants = test.grants
			if test.legacy {
				payload.DelegatedGrantsVersion = nil
			}
			created, err := ti.service.CreateKey(ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.Nil(t, created)
			listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
			require.NoError(t, err)
			require.Empty(t, listed.Keys)
		})
	}
	listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
	require.NoError(t, err)
	require.Empty(t, listed.Keys)

	// Rotation also issues a new credential, and rejection must preserve the old key.
	created, err := ti.service.CreateKey(ctx, agentKeyPayload(agentID, projectID))
	require.NoError(t, err)
	for _, grants := range [][]*gen.AgentPolicyGrantForm{nil, {}} {
		rotated, err := ti.service.RotateKey(ctx, &gen.RotateKeyPayload{
			ID: created.ID, Name: "replacement", DelegatedGrantsVersion: 1, RequestedGrants: grants,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.Nil(t, rotated)
		_, err = ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
		require.NoError(t, err)
	}
	listed, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
	require.NoError(t, err)
	require.Len(t, listed.Keys, 1)
	require.Equal(t, created.ID, listed.Keys[0].ID)

	// The legacy ordinary-key form remains ordinary, never an agent credential.
	ordinary, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "ordinary legacy key"})
	require.NoError(t, err)
	require.Nil(t, ordinary.SubjectUrn)
	require.Nil(t, ordinary.DelegatedGrantsVersion)
	require.NotEmpty(t, ordinary.Scopes)
}
