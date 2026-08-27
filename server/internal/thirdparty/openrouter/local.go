package openrouter

import (
	"context"
	"time"
)

type Development struct {
	apiKey string
}

var (
	_ Provisioner = (*Development)(nil)
	_ SpendClient = (*Development)(nil)
)

func NewDevelopment(apiKey string) *Development {
	return &Development{apiKey: apiKey}
}

func (o *Development) ProvisionAPIKey(context.Context, string, KeyType) (string, error) {
	return o.apiKey, nil
}

func (o *Development) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error) {
	return 0, nil
}

func (o *Development) RefreshAPIKeyLimitWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, limit *int) (int, error) {
	return o.RefreshAPIKeyLimit(ctx, orgID, keyType, limit)
}

func (*Development) AddAPIKeyDisableCause(context.Context, string, KeyType, DisableCause) (DisableCauseChange, error) {
	return DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}, nil
}

func (o *Development) AddAPIKeyDisableCauseWithDB(ctx context.Context, _ DBTX, orgID string, keyType KeyType, cause DisableCause) (DisableCauseChange, error) {
	return o.AddAPIKeyDisableCause(ctx, orgID, keyType, cause)
}

func (*Development) RemoveAPIKeyDisableCause(context.Context, string, KeyType, DisableCause, *int) (int, DisableCauseChange, error) {
	return 0, DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}, nil
}

func (o *Development) RemoveAPIKeyDisableCauseWithDB(ctx context.Context, _ DBTX, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error) {
	return o.RemoveAPIKeyDisableCause(ctx, orgID, keyType, cause, limit)
}

func (o *Development) GetCreditsUsed(ctx context.Context, orgID string, keyType KeyType) (float64, int, error) {
	return 12.5, 10, nil // arbitrary local numbers
}

func (o *Development) GetKeyUsage(ctx context.Context, apiKey string) (float64, *int64, error) {
	return 12.5, nil, nil // arbitrary local number; unlimited dev key
}

func (o *Development) ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	return currentLimit, nil
}

func (o *Development) ReconcileMonthlyCreditsWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	return o.ReconcileMonthlyCredits(ctx, orgID, keyType, currentLimit, currentGeneration, upstreamLimit)
}

func (o *Development) GetModelUsage(ctx context.Context, generationID string, orgID string, keyType KeyType) (*ModelUsage, error) {
	totalCost := 12.5
	return &ModelUsage{
		TotalCost:             &totalCost,
		CacheDiscount:         0,
		UpstreamInferenceCost: 0,
		Model:                 DefaultChatModel,
		TokensPrompt:          0,
		TokensCompletion:      0,
		NativeTokensCached:    0,
		NativeTokensReasoning: 0,
	}, nil
}

func (o *Development) GetDailySpend(ctx context.Context, keyHash string, startDay, endDay time.Time) (DailySpendResult, error) {
	return DailySpendResult{Days: nil, Source: DailySpendSourceAnalytics}, nil
}
