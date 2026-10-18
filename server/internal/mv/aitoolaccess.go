package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// BuildAIToolAccessSummaryView renders an access summary for the API. One
// builder for every surface that carries one, so a detection, a decision
// result and a scan target never disagree about what a verdict looks like.
func BuildAIToolAccessSummaryView(summary aitargets.AccessSummary) *types.AIToolAccessSummary {
	return &types.AIToolAccessSummary{
		State:       string(summary.State),
		Decision:    string(summary.Decision),
		Enforceable: summary.Enforceable,
		Rationale:   conv.PtrEmpty(summary.Record.Rationale),
	}
}
