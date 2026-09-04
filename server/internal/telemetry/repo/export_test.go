package repo

// This file exposes the unexported query registries and SQL expression
// constants to black-box tests (package repo_test) — in particular the
// semantic-definition sync test, which asserts that the embedded
// semantic/definition.json can never drift from these Go registries while
// both exist.

import "maps"

// DimensionSpecForTest is the exported projection of telemetryDimension.
type DimensionSpecForTest struct {
	AggregateColumn      string
	RawExpr              string
	Kind                 string // "scalar" | "array" | "project"
	EmptyIsNotApplicable bool
}

// TelemetryDimensionRegistryForTest exposes the public-dimension allowlist.
func TelemetryDimensionRegistryForTest() map[string]DimensionSpecForTest {
	out := make(map[string]DimensionSpecForTest, len(telemetryDimensionRegistry))
	for key, dim := range telemetryDimensionRegistry {
		var kind string
		switch dim.kind {
		case attributeDimScalar:
			kind = "scalar"
		case attributeDimArray:
			kind = "array"
		case attributeDimProject:
			kind = "project"
		}
		out[key] = DimensionSpecForTest{
			AggregateColumn:      dim.aggregateColumn,
			RawExpr:              dim.rawExpr,
			Kind:                 kind,
			EmptyIsNotApplicable: dim.emptyIsNotApplicable,
		}
	}
	return out
}

// AttributeMeasureSelectsForTest exposes the aggregate measure SELECT list.
func AttributeMeasureSelectsForTest() []string {
	out := make([]string, len(attributeMeasureSelects))
	copy(out, attributeMeasureSelects)
	return out
}

// Session SQL expression constants shared with the semantic definition.
const (
	SessionClaudeAPIRequestPredicateForTest = sessionClaudeAPIRequestPredicate
	SessionClaudeToolResultPredicateForTest = sessionClaudeToolResultPredicate
	SessionCodexAPIRequestPredicateForTest  = sessionCodexAPIRequestPredicate
	SessionAgentToolCallPredicateForTest    = sessionAgentToolCallPredicate
	SessionOpencodeUsageRowPredicateForTest = sessionOpencodeUsageRowPredicate
	SessionLiteLLMUsageRowPredicateForTest  = sessionLiteLLMUsageRowPredicate
	SessionCountedToolCallPredicateForTest  = sessionCountedToolCallPredicate
	SessionToolCallDedupIDExprForTest       = sessionToolCallDedupIDExpr
	SessionUsageMeasureFilterForTest        = sessionUsageMeasureFilter
	SessionInputTokensExprForTest           = sessionInputTokensExpr
	SessionOutputTokensExprForTest          = sessionOutputTokensExpr
	SessionTotalTokensExprForTest           = sessionTotalTokensExpr
	SessionCacheReadTokensExprForTest       = sessionCacheReadTokensExpr
	SessionCacheCreationTokensExprForTest   = sessionCacheCreationTokensExpr
	SessionCostExprForTest                  = sessionCostExpr
	SessionModelExprForTest                 = sessionModelExpr
	SessionMessageIDExprForTest             = sessionMessageIDExpr
	SessionMessageCountExprForTest          = sessionMessageCountExpr
)

// SessionSummaryMeasureSelectsForTest exposes the merged-aggregate measure
// SELECTs over chat_session_summaries (the s.-qualified expressions).
func SessionSummaryMeasureSelectsForTest() map[string]string {
	out := make(map[string]string, len(sessionSummaryMeasureSelects))
	maps.Copy(out, sessionSummaryMeasureSelects)
	return out
}
