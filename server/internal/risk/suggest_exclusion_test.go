package risk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// suggestExclusionCompletionClient scripts GetObjectCompletion calls so tests
// can drive the parse/validate/retry path behind SuggestExclusion. Call i
// returns responses[i]/errs[i]; every request is recorded.
type suggestExclusionCompletionClient struct {
	responses []*openrouter.CompletionResponse
	errs      []error
	requests  []openrouter.ObjectCompletionRequest
}

func (c *suggestExclusionCompletionClient) GetObjectCompletion(_ context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	i := len(c.requests)
	c.requests = append(c.requests, request)
	if i >= len(c.responses) {
		return nil, errors.New("unexpected extra completion call")
	}
	return c.responses[i], c.errs[i]
}

func (c *suggestExclusionCompletionClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *suggestExclusionCompletionClient) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not implemented")
}

func (c *suggestExclusionCompletionClient) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func (c *suggestExclusionCompletionClient) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

func suggestExclusionResponse(text string) *openrouter.CompletionResponse {
	content := or.CreateChatAssistantMessageContentStr(text)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	return &openrouter.CompletionResponse{
		StartTime: time.Time{},
		Message:   &msg,
		Model:     "test-model",
		Content:   text,
	}
}
func seedSuggestExclusionFinding(t *testing.T, ti *testInstance, projectID uuid.UUID, organizationID string, policyID uuid.UUID, chatID, messageID uuid.UUID, source, ruleID, match string, start int) uuid.UUID {
	t.Helper()
	at := time.Now().UTC()
	finding := chListFinding(t, projectID, organizationID, chatID, messageID, policyID.String(), at, at, source, ruleID, "", "", "", "")
	finding.StartPos = int32(start)            // #nosec G115 -- fixture offsets are small constants.
	finding.EndPos = int32(start + len(match)) // #nosec G115 -- fixture offsets are small constants.
	finding.MatchLen = uint32(len(match))      // #nosec G115 -- fixture matches are bounded literals.
	finding.Surface = scanners.SurfaceContent
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))
	return finding.ID
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

// The batch fallback must never surface a finding's raw match: that would
// bypass the audited risk.unmaskResult path.
func TestSuggestExclusion_FindingIDsOnly_UsesClickHouseMetadataAndRejectsForeignProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Suggest Exclusion Batch Test")})
	require.NoError(t, err)
	policyID := uuid.MustParse(policy.ID)
	chatID, messageID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	findingID := seedSuggestExclusionFinding(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, chatID, messageID, "gitleaks", "secret.generic", "secret", 20)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		FindingIds: []string{findingID.String()},
	})
	require.NoError(t, err)
	require.Equal(t, "rule_id", result.MatchType)
	require.Equal(t, "secret.generic", result.MatchValue)
	require.Nil(t, result.RuleIDFilter)
	require.Nil(t, result.SourceFilter)

	foreignID := seedSuggestExclusionFinding(t, ti, uuid.New(), authCtx.ActiveOrganizationID, policyID, chatID, messageID, "gitleaks", "secret.generic", "secret", 20)
	_, err = ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		FindingIds: []string{foreignID.String()},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestSuggestExclusion_FindingIDsOnly_DifferentRuleSameSource_FallsBackToSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Suggest Exclusion Batch Source Test")})
	require.NoError(t, err)
	policyID := uuid.MustParse(policy.ID)
	chatID, messageID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	id1 := seedSuggestExclusionFinding(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, chatID, messageID, "presidio", "pii.text", "test", 0)
	id2 := seedSuggestExclusionFinding(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, chatID, messageID, "presidio", "pii.secret", "secret", 20)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		FindingIds: []string{id1.String(), id2.String()},
	})
	require.NoError(t, err)
	require.Equal(t, "source", result.MatchType)
	require.Equal(t, "presidio", result.MatchValue)
}

// TestSuggestExclusion_RetriesOnceWhenModelReturnsInvalidRegex: a completion
// whose regex does not compile as RE2 (a lookahead here) must trigger one
// corrective retry carrying the validation error, and the retry's valid
// suggestion — RE2-only syntax like "(?i)" included — must come back verbatim
// rather than the whole-prompt heuristic fallback.
func TestSuggestExclusion_RetriesOnceWhenModelReturnsInvalidRegex(t *testing.T) {
	t.Parallel()
	fake := &suggestExclusionCompletionClient{
		responses: []*openrouter.CompletionResponse{
			suggestExclusionResponse(`{"match_type":"regex","match_value":"(?=acct_)test","rule_id_filter":"","source_filter":""}`),
			suggestExclusionResponse(`{"match_type":"regex","match_value":"(?i)^acct_(test|sandbox)_[a-z0-9]+$","rule_id_filter":"","source_filter":""}`),
		},
		errs:     []error{nil, nil},
		requests: nil,
	}
	ctx, ti := newTestRiskService(t, func(ti *testInstance) { ti.completionClient = fake })

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		Prompt: new("stop flagging sandbox account ids"),
	})
	require.NoError(t, err)
	require.Equal(t, "regex", result.MatchType)
	require.Equal(t, "(?i)^acct_(test|sandbox)_[a-z0-9]+$", result.MatchValue)
	require.Len(t, fake.requests, 2)
	require.Contains(t, fake.requests[1].Prompt, "rejected")
	require.Contains(t, fake.requests[1].Prompt, "invalid regex pattern")
}

// TestSuggestExclusion_InvalidTwice_FallsBackToHeuristic: when the corrective
// retry also fails validation, the handler falls back to the deterministic
// heuristic (the operator's own prompt as an exact match) instead of erroring.
func TestSuggestExclusion_InvalidTwice_FallsBackToHeuristic(t *testing.T) {
	t.Parallel()
	fake := &suggestExclusionCompletionClient{
		responses: []*openrouter.CompletionResponse{
			suggestExclusionResponse(`{"match_type":"regex","match_value":"(?=acct_)test","rule_id_filter":"","source_filter":""}`),
			suggestExclusionResponse(`{"match_type":"regex","match_value":"(?!still)bad","rule_id_filter":"","source_filter":""}`),
		},
		errs:     []error{nil, nil},
		requests: nil,
	}
	ctx, ti := newTestRiskService(t, func(ti *testInstance) { ti.completionClient = fake })

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		Prompt: new("stop flagging sandbox account ids"),
	})
	require.NoError(t, err)
	require.Len(t, fake.requests, 2)
	require.Equal(t, "exact", result.MatchType)
	require.Equal(t, "stop flagging sandbox account ids", result.MatchValue)
}

// TestSuggestExclusion_UnparseableResponse_NoRetry: the corrective retry
// prompt carries only the error text, not the model's raw output, so a
// parse-level failure has nothing to self-correct against — it must skip the
// retry and fall back after a single call.
func TestSuggestExclusion_UnparseableResponse_NoRetry(t *testing.T) {
	t.Parallel()
	fake := &suggestExclusionCompletionClient{
		responses: []*openrouter.CompletionResponse{
			suggestExclusionResponse("not json at all"),
		},
		errs:     []error{nil},
		requests: nil,
	}
	ctx, ti := newTestRiskService(t, func(ti *testInstance) { ti.completionClient = fake })

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		Prompt: new("jane.doe@acme.com"),
	})
	require.NoError(t, err)
	require.Len(t, fake.requests, 1)
	require.Equal(t, "exact", result.MatchType)
	require.Equal(t, "jane.doe@acme.com", result.MatchValue)
}

// TestSuggestExclusion_TransportError_NoRetry: transport failures are not the
// model's fault, so they skip the corrective retry and drop straight to the
// heuristic fallback after a single call.
func TestSuggestExclusion_TransportError_NoRetry(t *testing.T) {
	t.Parallel()
	fake := &suggestExclusionCompletionClient{
		responses: []*openrouter.CompletionResponse{nil},
		errs:      []error{errors.New("openrouter unreachable")},
		requests:  nil,
	}
	ctx, ti := newTestRiskService(t, func(ti *testInstance) { ti.completionClient = fake })

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{
		Prompt: new("jane.doe@acme.com"),
	})
	require.NoError(t, err)
	require.Len(t, fake.requests, 1)
	require.Equal(t, "exact", result.MatchType)
	require.Equal(t, "jane.doe@acme.com", result.MatchValue)
}
