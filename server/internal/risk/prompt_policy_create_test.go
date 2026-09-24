package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestCreateRiskPolicy_PromptBasedAvailableToEveryOrg verifies that creating a
// prompt_based policy needs no opt-in: the feature is unconditional for every
// organization.
func TestCreateRiskPolicy_PromptBasedAvailableToEveryOrg(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	name := "Prompt Policy"
	prompt := "Block destructive deletes"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:       &name,
		PolicyType: "prompt_based",
		Prompt:     &prompt,
	})
	require.NoError(t, err)
	require.Equal(t, "prompt_based", created.PolicyType)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: created.Name,
	})
	require.NoError(t, err)
	require.Equal(t, "prompt_based", updated.PolicyType)
}

func TestCreateRiskPolicy_PromptBasedRejectsDetectionSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

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
