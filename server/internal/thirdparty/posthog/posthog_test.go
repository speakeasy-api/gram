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
	result   *posthoggo.FeatureFlagResult
	err      error
	flags    map[string]any
	payload  posthoggo.FeatureFlagPayload
	allFlags posthoggo.FeatureFlagPayloadNoKey
}

func (c *evaluationClient) GetFeatureFlagResult(payload posthoggo.FeatureFlagPayload) (*posthoggo.FeatureFlagResult, error) {
	c.payload = payload
	return c.result, c.err
}

func (c *evaluationClient) GetAllFlags(payload posthoggo.FeatureFlagPayloadNoKey) (map[string]any, error) {
	c.allFlags = payload
	return c.flags, c.err
}

const testFeatureFlag feature.Flag = "test-feature"

func TestPosthogEvaluateFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		result          *posthoggo.FeatureFlagResult
		err             error
		flags           map[string]any
		localEvaluation bool
		want            feature.Evaluation
		isErr           bool
	}{
		{name: "enabled", result: &posthoggo.FeatureFlagResult{Enabled: true}, want: feature.EvaluationEnabled},
		{name: "disabled", result: &posthoggo.FeatureFlagResult{Enabled: false}, want: feature.EvaluationDisabled},
		{name: "missing", err: fmt.Errorf("lookup: %w", posthoggo.ErrFlagNotFound), want: feature.EvaluationIndeterminate},
		{name: "nil result", want: feature.EvaluationIndeterminate},
		{name: "variant", result: &posthoggo.FeatureFlagResult{Enabled: true, Variant: new("control")}, want: feature.EvaluationIndeterminate},
		{name: "provider failure", err: errors.New("unavailable"), want: feature.EvaluationIndeterminate, isErr: true},
		{name: "local enabled", localEvaluation: true, flags: map[string]any{string(testFeatureFlag): true}, want: feature.EvaluationEnabled},
		{name: "local disabled", localEvaluation: true, flags: map[string]any{string(testFeatureFlag): false}, want: feature.EvaluationDisabled},
		{name: "local missing", localEvaluation: true, flags: map[string]any{}, want: feature.EvaluationIndeterminate},
		{name: "local variant", localEvaluation: true, flags: map[string]any{string(testFeatureFlag): "control"}, want: feature.EvaluationIndeterminate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &evaluationClient{result: test.result, err: test.err, flags: test.flags}
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
			if test.localEvaluation {
				require.Equal(t, "organization-id", client.allFlags.DistinctId)
				require.Equal(t, posthoggo.Groups{"organization": "organization-slug"}, client.allFlags.Groups)
				require.False(t, *client.allFlags.SendFeatureFlagEvents)
			} else {
				require.Equal(t, "organization-id", client.payload.DistinctId)
				require.Equal(t, posthoggo.Groups{"organization": "organization-slug"}, client.payload.Groups)
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
