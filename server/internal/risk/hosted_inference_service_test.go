package risk_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const riskHostedInferenceDenialNote = "Hosted inference is paused for this account."

type riskHostedInferenceEvaluator struct {
	result killswitches.EvaluationResult
	calls  int
}

func (e *riskHostedInferenceEvaluator) Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult {
	e.calls++
	return e.result
}

type riskCheckpointCompletionClient struct {
	checkpoint hostedinference.AttemptCheckpoint
}

func (c *riskCheckpointCompletionClient) check(ctx context.Context, organizationID string) error {
	if err := c.checkpoint.Check(ctx, organizationID); err != nil {
		return fmt.Errorf("check hosted inference: %w", err)
	}
	return nil
}

func wrapRiskTestError(operation string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (c *riskCheckpointCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, c.check(ctx, request.OrgID)
}

func (c *riskCheckpointCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, c.check(ctx, request.OrgID)
}

func (c *riskCheckpointCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, c.check(ctx, request.OrgID)
}

func (c *riskCheckpointCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, _ string, _ []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, c.check(ctx, orgID)
}

func (c *riskCheckpointCompletionClient) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

func runDeniedRiskAuthoringPath(t *testing.T, invoke func(context.Context, *testInstance) error) {
	t.Helper()

	result, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", riskHostedInferenceDenialNote)
	require.NoError(t, err)
	evaluator := &riskHostedInferenceEvaluator{result: result}
	ctx, ti := newTestRiskService(t, func(ti *testInstance) {
		registry, registryErr := mcptoolexecution.NewRegistry(ti.conn)
		require.NoError(t, registryErr)
		checkpoint, checkpointErr := hostedinference.NewCheckpoint(registry, evaluator, time.Second)
		require.NoError(t, checkpointErr)
		gate := &riskCheckpointCompletionClient{checkpoint: checkpoint}
		ti.completionClient = chat.NewAgenticChatClient(testenv.NewLogger(t), nil, nil, nil, gate, nil)
	})

	err = invoke(ctx, ti)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeAIAccessDenied, shareable.Code)
	require.Equal(t, riskHostedInferenceDenialNote, shareable.Error())
	require.Equal(t, 1, evaluator.calls)
}

func TestRiskAuthoringPathsPreserveGovernedClassificationThroughAgenticClient(t *testing.T) {
	t.Parallel()

	t.Run("custom detection rule", func(t *testing.T) {
		t.Parallel()
		runDeniedRiskAuthoringPath(t, func(ctx context.Context, ti *testInstance) error {
			_, err := ti.service.SuggestCustomDetectionRule(ctx, &gen.SuggestCustomDetectionRulePayload{Prompt: "detect placeholder secrets"})
			return wrapRiskTestError("suggest custom detection rule", err)
		})
	})

	t.Run("exclusion", func(t *testing.T) {
		t.Parallel()
		runDeniedRiskAuthoringPath(t, func(ctx context.Context, ti *testInstance) error {
			_, err := ti.service.SuggestExclusion(ctx, &gen.SuggestExclusionPayload{Prompt: new("ignore placeholder accounts")})
			return wrapRiskTestError("suggest exclusion", err)
		})
	})

	t.Run("standard policy name", func(t *testing.T) {
		t.Parallel()
		runDeniedRiskAuthoringPath(t, func(ctx context.Context, ti *testInstance) error {
			_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Sources: []string{"gitleaks"}})
			return wrapRiskTestError("create standard risk policy", err)
		})
	})

	t.Run("prompt policy name", func(t *testing.T) {
		t.Parallel()
		runDeniedRiskAuthoringPath(t, func(ctx context.Context, ti *testInstance) error {
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ti.flags.SetFlag(feature.FlagPromptPolicies, authCtx.ActiveOrganizationID, true)
			prompt := "Block destructive placeholder actions"
			_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{PolicyType: "prompt_based", Prompt: &prompt})
			return wrapRiskTestError("create prompt risk policy", err)
		})
	})
}
