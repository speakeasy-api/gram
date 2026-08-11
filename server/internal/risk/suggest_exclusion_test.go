package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSuggestExclusion_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{Prompt: new("stop flagging test accounts")})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestSuggestExclusion_RequiresPromptOrFindingIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestSuggestExclusion_InvalidFindingID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{FindingIds: []string{"not-a-uuid"}})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

// TestSuggestExclusion_PromptOnly_HeuristicFallback exercises the no-LLM
// heuristic path (newTestRiskService's default completionClient is nil): a
// prompt-only request must still return a usable (if naive) suggestion
// rather than erroring, matching heuristicCustomRuleSuggestion's contract.
func TestSuggestExclusion_PromptOnly_HeuristicFallback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		Prompt: new("jane.doe@acme.com"),
	})
	require.NoError(t, err)
	require.Equal(t, "exact", result.MatchType)
	require.Equal(t, "jane.doe@acme.com", result.MatchValue)
}

// TestSuggestExclusion_FindingIDsOnly_HeuristicFallback guards the batch
// path end to end: finding_ids must be looked up server-side (not trusted
// from the client), and the fallback must never surface a finding's raw
// matched value — a detected secret here ("AKIAIOSFODNN7EXAMPLE") the
// operator hasn't reviewed or disclosed — since that would bypass the
// audited risk.unmaskResult path every other raw-match disclosure goes
// through. It falls back to a rule_id-scoped exclusion instead.
func TestSuggestExclusion_FindingIDsOnly_HeuristicFallback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Suggest Exclusion Batch Test")})
	require.NoError(t, err)
	policyID, err := uuid.Parse(policy.ID)
	require.NoError(t, err)

	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	resultID := seedRiskResultWith(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, msgID, "gitleaks", "secret.aws_access_token", "AKIAIOSFODNN7EXAMPLE")

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		FindingIds: []string{resultID.String()},
	})
	require.NoError(t, err)
	require.Equal(t, "rule_id", result.MatchType)
	require.Equal(t, "secret.aws_access_token", result.MatchValue)
	require.NotContains(t, result.MatchValue, "AKIAIOSFODNN7EXAMPLE")
}

// TestSuggestExclusion_FindingIDsOnly_DifferentRuleSameSource_FallsBackToSource
// covers the batch heuristic's second tier: findings that don't share a
// rule_id but do share a source fall back to a source-scoped exclusion,
// still without ever touching the raw matched values.
func TestSuggestExclusion_FindingIDsOnly_DifferentRuleSameSource_FallsBackToSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Suggest Exclusion Batch Source Test")})
	require.NoError(t, err)
	policyID, err := uuid.Parse(policy.ID)
	require.NoError(t, err)

	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	id1 := seedRiskResultWith(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, msgID, "presidio", "pii.email_address", "jane.doe@acme.com")
	id2 := seedRiskResultWith(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, msgID, "presidio", "pii.phone_number", "555-0100")

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		FindingIds: []string{id1.String(), id2.String()},
	})
	require.NoError(t, err)
	require.Equal(t, "source", result.MatchType)
	require.Equal(t, "presidio", result.MatchValue)
}
