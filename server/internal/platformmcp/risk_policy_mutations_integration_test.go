package platformmcp

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type noopRiskPolicySignaler struct{}

func (noopRiskPolicySignaler) Signal(context.Context, uuid.UUID) error { return nil }

func TestRiskPolicyMutationHandlersCreateUpdateReplayAndRedact(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_risk_policy_mutations")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	principal.ClientID = "test-client"
	principal.Surface = SurfacePlatformMCP
	ctx = ContextWithPrincipal(ctx, principal)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPRiskMutations, principal.OrganizationID, true)
	flags.SetFlag(feature.FlagPromptPolicies, principal.OrganizationID, true)
	controls, err := NewRiskMutationControls(conn, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget(), "risk-policy-test-key")
	require.NoError(t, err)
	policies := risk.NewPolicyMutationCore(conn, audit.NewLogger(), nil, noopRiskPolicySignaler{}, nil)
	handlers, err := NewRiskPolicyMutationHandlers(conn, controls, policies)
	require.NoError(t, err)
	require.Nil(t, handlers.CreateExclusion)
	require.Nil(t, handlers.UpdateExclusion)

	createInput := map[string]any{
		"project_slug":    project.Slug,
		"policy_type":     "standard",
		"name":            "Policy",
		"enabled":         true,
		"sources":         []string{"gitleaks"},
		"idempotency_key": "create-policy-key",
	}
	_, created, err := handlers.CreatePolicy(ctx, nil, createInput)
	require.NoError(t, err)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "created", created.ResultCategory)
	require.Equal(t, project.ID.String(), created.Project.ID)
	require.NotEmpty(t, created.Version)

	policyID, err := uuid.Parse(created.Policy.ID)
	require.NoError(t, err)
	stored, err := riskrepo.New(conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: policyID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "flag", stored.Action)
	require.InDelta(t, 5.0, stored.Score, 0)
	require.ElementsMatch(t, []string{"gitleaks"}, stored.Sources)
	require.ElementsMatch(t, []string{"assistant_message", "tool_request", "tool_response", "user_message"}, stored.MessageTypes)

	createAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionRiskPolicyCreate)
	require.NoError(t, err)
	require.Equal(t, policyID.String(), createAudit.SubjectID)
	require.Equal(t, principal.UserID, createAudit.ActorID)

	reads, err := newRiskReadService(conn, "risk-policy-test-key")
	require.NoError(t, err)
	read, err := reads.GetPolicy(ctx, principal, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: policyID.String()})
	require.NoError(t, err)
	require.Equal(t, created.Version, read.Policy.Version)
	require.Equal(t, created.Policy.ID, read.Policy.ID)

	_, replayedCreate, err := handlers.CreatePolicy(ctx, nil, createInput)
	require.NoError(t, err)
	require.True(t, replayedCreate.Receipt.Replayed)
	require.Equal(t, created.Receipt.ID, replayedCreate.Receipt.ID)
	require.Equal(t, created.Version, replayedCreate.Version)

	changedCreate := cloneRiskMutationInput(createInput)
	changedCreate["name"] = "Different"
	_, _, err = handlers.CreatePolicy(ctx, nil, changedCreate)
	requireRiskMutationRefusal(t, err, "conflict")

	updateInput := map[string]any{
		"project_slug":     "  " + project.Slug + "  ",
		"policy_id":        policyID.String(),
		"expected_version": read.Policy.Version,
		"idempotency_key":  "update-policy-key",
		"patch":            map[string]any{"name": "Renamed"},
	}
	_, updated, err := handlers.UpdatePolicy(ctx, nil, updateInput)
	require.NoError(t, err)
	require.False(t, updated.Receipt.Replayed)
	require.Equal(t, "updated", updated.ResultCategory)
	require.NotEqual(t, created.Version, updated.Version)

	stored, err = riskrepo.New(conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: policyID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "Renamed", stored.Name)
	require.ElementsMatch(t, []string{"gitleaks"}, stored.Sources, "omitted sparse fields are preserved")
	require.True(t, stored.Enabled)

	targetedAudience := urn.NewPrincipal(urn.PrincipalTypeUser, "targeted-user")
	require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{
		Resource:   authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: policyID.String()},
		Principals: []urn.Principal{targetedAudience},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID.String()),
	}))
	targetedRead, err := reads.GetPolicy(ctx, principal, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: policyID.String()})
	require.NoError(t, err)
	require.NotEqual(t, updated.Version, targetedRead.Policy.Version, "an audience-only enforcement change invalidates the policy version")

	secondUpdate := map[string]any{
		"project_slug":     project.Slug,
		"policy_id":        policyID.String(),
		"expected_version": targetedRead.Policy.Version,
		"idempotency_key":  "update-policy-key-2",
		"patch":            map[string]any{"enabled": false},
	}
	_, second, err := handlers.UpdatePolicy(ctx, nil, secondUpdate)
	require.NoError(t, err)
	require.NotEqual(t, targetedRead.Policy.Version, second.Version)
	evaluationGrants, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyEvaluate, ResourceID: policyID.String()})
	require.NoError(t, err)
	require.Len(t, evaluationGrants, 1)
	require.Equal(t, targetedAudience.String(), evaluationGrants[0].PrincipalUrn, "a sparse update preserves the locked audience")
	postUpdateRead, err := reads.GetPolicy(ctx, principal, GetRiskPolicyInput{ProjectSlug: project.Slug, PolicyID: policyID.String()})
	require.NoError(t, err)
	require.Equal(t, second.Version, postUpdateRead.Policy.Version, "the mutation result uses the locked authoritative audience")

	_, replayedUpdate, err := handlers.UpdatePolicy(ctx, nil, updateInput)
	require.NoError(t, err)
	require.True(t, replayedUpdate.Receipt.Replayed)
	require.Equal(t, updated.Version, replayedUpdate.Version, "replay returns the original stored result rather than mutable current state")

	changedUpdate := cloneRiskMutationInput(updateInput)
	changedUpdate["patch"] = map[string]any{"name": "Other"}
	_, _, err = handlers.UpdatePolicy(ctx, nil, changedUpdate)
	requireRiskMutationRefusal(t, err, "conflict")

	staleUpdate := cloneRiskMutationInput(updateInput)
	staleUpdate["idempotency_key"] = "stale-update-key"
	_, _, err = handlers.UpdatePolicy(ctx, nil, staleUpdate)
	requireRiskMutationRefusal(t, err, "conflict")

	stored, err = riskrepo.New(conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: policyID, ProjectID: project.ID})
	require.NoError(t, err)
	serverURL := "https://version-audience.example.test/mcp"
	require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBypass, policyID.String(), serverURL, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "first-user"),
	}))
	policy := policycore.Project(stored, []string{authz.AllUsersPrincipal().String()}, nil)
	firstGrantState, err := riskPolicyVersionState(ctx, conn, policy, false)
	require.NoError(t, err)
	firstGrantVersion, err := controls.Versions().PolicyVersion(firstGrantState)
	require.NoError(t, err)
	require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBypass, policyID.String(), serverURL, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "second-user"),
	}))
	secondGrantState, err := riskPolicyVersionState(ctx, conn, policy, false)
	require.NoError(t, err)
	secondGrantVersion, err := controls.Versions().PolicyVersion(secondGrantState)
	require.NoError(t, err)
	require.NotEqual(t, firstGrantVersion, secondGrantVersion, "changing only a URL grant audience must invalidate the policy version")

	prompt := "sensitive prompt that must not enter the receipt"
	promptInput := map[string]any{
		"project_slug":    project.Slug,
		"policy_type":     "prompt_based",
		"name":            "Prompt policy",
		"enabled":         true,
		"prompt":          prompt,
		"idempotency_key": "prompt-policy-key",
	}
	_, promptCreated, err := handlers.CreatePolicy(ctx, nil, promptInput)
	require.NoError(t, err)
	candidates, err := riskrepo.New(conn).ListRiskPolicyCreateCandidates(ctx, riskrepo.ListRiskPolicyCreateCandidatesParams{ProjectID: project.ID, Name: "Renamed", PolicyType: "standard"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, policyID, candidates[0].ID, "create convergence narrows by project, name, and policy type")
	flags.SetFlag(feature.FlagPromptPolicies, principal.OrganizationID, false)
	promptReplay := cloneRiskMutationInput(promptInput)
	_, _, err = handlers.CreatePolicy(ctx, nil, promptReplay)
	requireRiskMutationRefusal(t, err, unavailableCode)
	flags.SetFlag(feature.FlagPromptPolicies, principal.OrganizationID, true)

	promptReceiptID, err := uuid.Parse(promptCreated.Receipt.ID)
	require.NoError(t, err)
	receipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID,
		Operation:      operationCreateRiskPolicy,
		IdempotencyKey: "prompt-policy-key",
	})
	require.NoError(t, err)
	require.Equal(t, promptReceiptID, receipt.ID)
	require.NotContains(t, string(receipt.ResultPayload), prompt)
	require.NotContains(t, string(receipt.ResultPayload), "Prompt policy")
	var safeResult CreateRiskPolicyReceiptResult
	require.NoError(t, json.Unmarshal(receipt.ResultPayload, &safeResult))
	require.Equal(t, promptCreated.Policy.ID, safeResult.Policy.ID)

	legacyBlockingInput := map[string]any{
		"project_slug":    project.Slug,
		"policy_type":     "standard",
		"name":            "Legacy blocking policy",
		"enabled":         true,
		"action":          "block",
		"sources":         []string{"shadow_mcp"},
		"idempotency_key": "legacy-blocking-policy-key",
	}
	_, legacyBlocking, err := handlers.CreatePolicy(ctx, nil, legacyBlockingInput)
	require.NoError(t, err)
	legacyBlockingID, err := uuid.Parse(legacyBlocking.Policy.ID)
	require.NoError(t, err)
	legacyStored, err := riskrepo.New(conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: legacyBlockingID, ProjectID: project.ID})
	require.NoError(t, err)
	require.False(t, legacyStored.ShadowMcpDisposition.Valid, "legacy block_all posture has no stored disposition")

	_, _, err = handlers.UpdatePolicy(ctx, nil, map[string]any{
		"project_slug":     project.Slug,
		"policy_id":        legacyBlocking.Policy.ID,
		"expected_version": legacyBlocking.Version,
		"idempotency_key":  "drop-legacy-blocking-posture-key",
		"patch":            map[string]any{"sources": []string{"gitleaks"}},
	})
	requireRiskMutationRefusal(t, err, "invalid_request")
	legacyStored, err = riskrepo.New(conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: legacyBlockingID, ProjectID: project.ID})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"shadow_mcp"}, legacyStored.Sources)
	require.Equal(t, "block", legacyStored.Action)

	_, _, err = unavailableRiskMutationTool[CreateRiskExclusionToolOutput]()(ctx, nil, map[string]any{})
	requireRiskMutationRefusal(t, err, unavailableCode)
	_, _, err = unavailableRiskMutationTool[UpdateRiskExclusionToolOutput]()(ctx, nil, map[string]any{})
	requireRiskMutationRefusal(t, err, unavailableCode)
}

func cloneRiskMutationInput(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input))
	maps.Copy(cloned, input)
	return cloned
}

func requireRiskMutationRefusal(t *testing.T, err error, code string) {
	t.Helper()
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var payload struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &payload))
	require.Equal(t, code, payload.Code)
}
