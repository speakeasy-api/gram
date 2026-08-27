package risk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
)

// An exact or regex exclusion's match_value is the literal string someone chose
// to suppress — frequently the secret that triggered the finding. It must never
// reach the model verbatim, while the identifier-shaped match types stay
// readable so the model can still tell what an exclusion covers.
func TestExclusionViewRedactsOnlyFreeTextMatchValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		matchType string
		redacted  bool
	}{
		{matchType: "exact", redacted: true},
		{matchType: "regex", redacted: true},
		{matchType: "rule_id", redacted: false},
		{matchType: "source", redacted: false},
		{matchType: "entity_type", redacted: false},
	}

	const secret = "AKIAIOSFODNN7EXAMPLE"
	redactor := exclusioncore.NewRedactor("test-redaction-key")

	for _, tt := range tests {
		t.Run(tt.matchType, func(t *testing.T) {
			t.Parallel()

			view := toExclusionView(redactor, &types.RiskExclusion{
				ID:           "0192bd2a-0000-7000-8000-000000000000",
				ProjectID:    "0192bd2a-0000-7000-8000-000000000001",
				RiskPolicyID: nil,
				MatchType:    tt.matchType,
				MatchValue:   secret,
				RuleIDFilter: "secret.aws_access_key",
				SourceFilter: "gitleaks",
				Enabled:      true,
				CreatedAt:    "2026-01-01T00:00:00Z",
				UpdatedAt:    "2026-01-01T00:00:00Z",
			})

			if tt.redacted {
				require.NotContains(t, view.MatchValue, secret)
				require.Regexp(t, `^redacted:hmac-sha256:[0-9a-f]{16}$`, view.MatchValue)
			} else {
				require.Equal(t, secret, view.MatchValue)
			}

			// Filters are rule/source identifiers, not captured content.
			require.Equal(t, "secret.aws_access_key", view.RuleIDFilter)
			require.Equal(t, "gitleaks", view.SourceFilter)
		})
	}
}

func TestFalsePositiveSchemasCapBatchSize(t *testing.T) {
	t.Parallel()

	for _, schema := range []string{
		string(NewMarkRiskResultsFalsePositiveTool(nil).Descriptor().InputSchema),
		string(NewUnmarkRiskResultsFalsePositiveTool(nil).Descriptor().InputSchema),
	} {
		require.Contains(t, schema, `"maxItems":500`)
		require.Contains(t, schema, `"minItems":1`)
	}
}
