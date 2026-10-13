package workloadpolicy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func getCustomFlowsPayload() *gen.GetCustomFlowsPayload {
	return &gen.GetCustomFlowsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func TestGetCustomFlows_ReturnsTheFourForms(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.GetCustomFlows(withScopes(t, ctx, ti, authz.ScopeWorkloadRead), getCustomFlowsPayload())
	require.NoError(t, err)

	require.Equal(t, "Register new access", result.RegisterPlatform.Title)
	require.Equal(t, "Edit platform", result.EditPlatform.Title)
	require.Equal(t, "Allow access", result.AllowAccess.Title)
	require.Equal(t, "Edit access", result.EditAccess.Title)

	var issuer *gen.WorkloadFormBlock
	for _, block := range result.EditPlatform.Steps[0].Blocks {
		if block.Type == "input" && block.Field == "issuer" {
			issuer = block
		}
	}
	require.NotNil(t, issuer)
	require.True(t, issuer.ReadOnly)
	require.Equal(t, "issuer_url", issuer.Format)
}

func TestGetCustomFlows_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetCustomFlows(withScopes(t, ctx, ti), getCustomFlowsPayload())
	requireOopsCode(t, err, oops.CodeForbidden)
}
