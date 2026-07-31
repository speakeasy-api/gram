package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// seedRiskResultReturningID mirrors seedRiskResultWith but returns the new
// row's id, needed for the finding_ids-based suggestExclusion tests below.
func seedRiskResultReturningID(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string, policyID uuid.UUID, msgID uuid.UUID, source, ruleID, match string) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	resultID, err := uuid.NewV7()
	require.NoError(t, err)

	repo := riskrepo.New(ti.conn)
	_, err = repo.InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    orgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     uuid.NullUUID{UUID: msgID, Valid: true},
		Source:            source,
		Found:             true,
		RuleID:            pgtype.Text{String: ruleID, Valid: ruleID != ""},
		Description:       pgtype.Text{String: "", Valid: false},
		Match:             pgtype.Text{String: match, Valid: match != ""},
		StartPos:          pgtype.Int4{Int32: 0, Valid: true},
		EndPos:            pgtype.Int4{Int32: int32(len(match)), Valid: true},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: true},
		Tags:              nil,
	}})
	require.NoError(t, err)
	return resultID
}

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
// from the client) and fed into the same heuristic fallback used for a
// prompt, keyed off the looked-up row's match value.
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
	resultID := seedRiskResultReturningID(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, msgID, "gitleaks", "secret.aws_access_token", "AKIAIOSFODNN7EXAMPLE")

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		FindingIds: []string{resultID.String()},
	})
	require.NoError(t, err)
	require.Equal(t, "exact", result.MatchType)
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", result.MatchValue)
}
