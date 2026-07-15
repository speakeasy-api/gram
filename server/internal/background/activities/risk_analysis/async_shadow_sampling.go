package risk_analysis

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

func (a *AnalyzeBatch) asyncShadowEnabled(ctx context.Context, chatMessageID string) bool {
	if a.flags == nil {
		return false
	}

	on, err := a.flags.IsFlagEnabledLocal(ctx, feature.FlagRiskAsyncScanShadow, chatMessageID, nil)
	if err != nil {
		a.logger.ErrorContext(ctx, "async shadow flag local evaluation failed",
			attr.SlogError(err),
			attr.SlogMessageID(chatMessageID),
			attr.SlogValueString(string(feature.FlagRiskAsyncScanShadow)),
		)
		return false
	}
	return on
}
