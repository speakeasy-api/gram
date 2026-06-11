package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestCreateRiskPolicy_PromptBasedGatedByFlag verifies that creating a
// prompt_based policy is rejected with Forbidden when the FlagPromptPolicies
// feature flag is not enabled (the test service is wired with a nil flag
// provider, so the MVP gate is closed).
func TestCreateRiskPolicy_PromptBasedGatedByFlag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	name := "Prompt Policy"
	prompt := "Block destructive deletes"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:       &name,
		PolicyType: "prompt_based",
		Prompt:     &prompt,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCreateRiskPolicy_PromptBasedRejectsDetectionSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagPromptPolicies, authCtx.ActiveOrganizationID, true)

	name := "Prompt Policy"
	prompt := "Block destructive deletes"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:       &name,
		PolicyType: "prompt_based",
		Prompt:     &prompt,
		Sources:    []string{"gitleaks"},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestUpdateRiskPolicy_StandardRejectsPromptFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	name := "Standard Policy"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: &name})
	require.NoError(t, err)

	prompt := "Block destructive deletes"
	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:     created.ID,
		Name:   created.Name,
		Prompt: &prompt,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestUpdateRiskPolicy_PromptBasedRejectsDetectionSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagPromptPolicies, authCtx.ActiveOrganizationID, true)

	name := "Prompt Policy"
	prompt := "Block destructive deletes"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:       &name,
		PolicyType: "prompt_based",
		Prompt:     &prompt,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    created.Name,
		Sources: []string{"gitleaks"},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}
