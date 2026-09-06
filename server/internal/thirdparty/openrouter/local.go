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

func (o *Development) ReinstateAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error) {
	return 0, nil
}

func (o *Development) ReinstateAPIKeyLimitWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, limit *int) (int, error) {
	return o.ReinstateAPIKeyLimit(ctx, orgID, keyType, limit)
}

func (o *Development) AddAPIKeyDisableCause(context.Context, string, KeyType, DisableCause) (DisableCauseChange, error) {
	return unchangedDisableCauseChange(), nil
}

func (o *Development) AddAPIKeyDisableCauseWithDB(context.Context, DBTX, string, KeyType, DisableCause) (DisableCauseChange, error) {
	return unchangedDisableCauseChange(), nil
}

func (o *Development) RemoveAPIKeyDisableCause(context.Context, string, KeyType, DisableCause, *int) (int, DisableCauseChange, error) {
	return 0, unchangedDisableCauseChange(), nil
}

func (o *Development) RemoveAPIKeyDisableCauseWithDB(context.Context, DBTX, string, KeyType, DisableCause, *int) (int, DisableCauseChange, error) {
	return 0, unchangedDisableCauseChange(), nil
}

func (o *Development) PrepareEnterpriseTrialConversionKeyWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, floor int64) (EnterpriseTrialConversionKeyChange, error) {
	return new(OpenRouter).PrepareEnterpriseTrialConversionKeyWithDB(ctx, db, orgID, keyType, floor)
}

func (o *Development) ReconcileAPIKeyDisabled(context.Context, string, KeyType) error {
	return nil
}

func (o *Development) ReconcileAPIKeyConversionPolicy(context.Context, string, KeyType) error {
	return nil
}

func (o *Development) DisableAPIKey(ctx context.Context, orgID string, keyType KeyType) error {
	return nil
}

func (o *Development) DisableAPIKeyWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType) error {
	return o.DisableAPIKey(ctx, orgID, keyType)
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
