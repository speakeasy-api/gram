package posthog

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	posthoggo "github.com/posthog/posthog-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

type evaluationClient struct {
	posthoggo.Client
	result         *posthoggo.FeatureFlagResult
	err            error
	definitions    []posthoggo.FeatureFlag
	definitionsErr error
	called         bool
	payload        posthoggo.FeatureFlagPayload
}

func (c *evaluationClient) GetFeatureFlagResult(payload posthoggo.FeatureFlagPayload) (*posthoggo.FeatureFlagResult, error) {
	c.called = true
	c.payload = payload
	return c.result, c.err
}

func (c *evaluationClient) GetFeatureFlags() ([]posthoggo.FeatureFlag, error) {
	return c.definitions, c.definitionsErr
}

const testFeatureFlag feature.Flag = "test-feature"

func TestPosthogEvaluateFlag(t *testing.T) {
	t.Parallel()

	definitions := []posthoggo.FeatureFlag{{Key: string(testFeatureFlag)}}

	tests := []struct {
		name            string
		result          *posthoggo.FeatureFlagResult
		err             error
		definitions     []posthoggo.FeatureFlag
		definitionsErr  error
		localEvaluation bool
		want            feature.Evaluation
		wantCalled      bool
		isErr           bool
	}{
		{name: "enabled", result: &posthoggo.FeatureFlagResult{Enabled: true}, want: feature.EvaluationEnabled, wantCalled: true},
		{name: "disabled", result: &posthoggo.FeatureFlagResult{Enabled: false}, want: feature.EvaluationDisabled, wantCalled: true},
		{name: "missing", err: fmt.Errorf("lookup: %w", posthoggo.ErrFlagNotFound), want: feature.EvaluationIndeterminate, wantCalled: true},
		{name: "nil result", want: feature.EvaluationIndeterminate, wantCalled: true},
		{name: "variant", result: &posthoggo.FeatureFlagResult{Enabled: true, Variant: new("control")}, want: feature.EvaluationIndeterminate, wantCalled: true},
		{name: "provider failure", err: errors.New("unavailable"), want: feature.EvaluationIndeterminate, wantCalled: true, isErr: true},
		{name: "local enabled", localEvaluation: true, definitions: definitions, result: &posthoggo.FeatureFlagResult{Enabled: true}, want: feature.EvaluationEnabled, wantCalled: true},
		{name: "local disabled", localEvaluation: true, definitions: definitions, result: &posthoggo.FeatureFlagResult{Enabled: false}, want: feature.EvaluationDisabled, wantCalled: true},
		{name: "local variant", localEvaluation: true, definitions: definitions, result: &posthoggo.FeatureFlagResult{Enabled: true, Variant: new("control")}, want: feature.EvaluationIndeterminate, wantCalled: true},
		// A flag absent from the cached definitions is indeterminate without a
		// remote round trip, rather than the SDK's coerced false.
		{name: "local undefined", localEvaluation: true, definitions: nil, result: &posthoggo.FeatureFlagResult{Enabled: false}, want: feature.EvaluationIndeterminate, wantCalled: false},
		{name: "local undefined among other flags", localEvaluation: true, definitions: []posthoggo.FeatureFlag{{Key: "other-feature"}}, result: &posthoggo.FeatureFlagResult{Enabled: false}, want: feature.EvaluationIndeterminate, wantCalled: false},
		// A flag deleted between polls is still in the cached definitions, so
		// the SDK's not-found result is what keeps it indeterminate.
		{name: "local deleted between polls", localEvaluation: true, definitions: definitions, err: fmt.Errorf("lookup: %w", posthoggo.ErrFlagNotFound), want: feature.EvaluationIndeterminate, wantCalled: true},
		// Definitions that have not loaded yet defer to the SDK's own fallback.
		{name: "local definitions unavailable", localEvaluation: true, definitionsErr: errors.New("flags were not successfully fetched yet"), result: &posthoggo.FeatureFlagResult{Enabled: true}, want: feature.EvaluationEnabled, wantCalled: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &evaluationClient{result: test.result, err: test.err, definitions: test.definitions, definitionsErr: test.definitionsErr}
			provider := &Posthog{
				client:          client,
				localEvaluation: test.localEvaluation,
				logger:          slog.New(slog.DiscardHandler), //nolint:forbidigo // importing testenv would create an import cycle through thirdparty/posthog
			}

			got, err := provider.EvaluateFlag(
				t.Context(),
				testFeatureFlag,
				"organization-id",
				map[string]string{"organization": "organization-slug"},
			)

			if test.isErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, got)
			require.Equal(t, test.wantCalled, client.called)
			if test.wantCalled {
				require.Equal(t, string(testFeatureFlag), client.payload.Key)
				require.Equal(t, "organization-id", client.payload.DistinctId)
				require.Equal(t, posthoggo.Groups{"organization": "organization-slug"}, client.payload.Groups)
				require.False(t, *client.payload.SendFeatureFlagEvents)
			}
		})
	}
}

// A nil provider is the shape callers hold before wiring completes; resolving a
// variant on it must yield "no variant", not a nil-pointer panic on the logger.
func TestPosthogFlagVariantNilProvider(t *testing.T) {
	t.Parallel()

	var p *Posthog
	variant, err := p.FlagVariant(t.Context(), feature.FlagAssistantPlatformMCP, "org-test", nil)
	require.NoError(t, err)
	require.Empty(t, variant)
}
