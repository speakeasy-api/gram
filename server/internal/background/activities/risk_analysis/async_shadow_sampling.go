package risk_analysis

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func (a *AnalyzeBatch) asyncShadowEnabled(ctx context.Context, chatMessageID string) bool {
	if a.flags == nil {
		return false
	}

	on, err := a.flags.IsFlagEnabledLocal(ctx, feature.FlagRiskAsyncScanShadow, chatMessageID, nil)
	if err != nil {
		return false
	}
	return on
}
