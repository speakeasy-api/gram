package usage

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const (
	// inferenceSpendHistoryMonths is how many trailing UTC calendar months
	// (including the current month) the history endpoint reports. Months with
	// no durable daily rows are omitted rather than reconstructed from
	// upstream OpenRouter history, which is not a reliable record across key
	// rotations. The current month is synthesized when collection has not
	// produced a completed day yet.
	inferenceSpendHistoryMonths = 12
	zeroInferenceSpendUSD       = "0.000000"
)

func (s *Service) GetInferenceSpendHistory(ctx context.Context, _ *gen.GetInferenceSpendHistoryPayload) (*gen.InferenceSpendHistory, error) {
	return s.inferenceSpendHistoryAt(ctx, time.Now().UTC())
}

func (s *Service) inferenceSpendHistoryAt(ctx context.Context, now time.Time) (*gen.InferenceSpendHistory, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgRead,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	now = now.UTC()
	today := startOfUTCDay(now)
	currentMonth := startOfUTCMonth(now)
	earliest := currentMonth.AddDate(0, -(inferenceSpendHistoryMonths - 1), 0)

	rows, err := s.repo.ListOpenRouterSpendByMonth(ctx, repo.ListOpenRouterSpendByMonthParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		KeyTypes:        openrouter.BillableKeyTypeStrings(),
		EarliestDay:     finiteDate(earliest),
		ExclusiveEndDay: finiteDate(today),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list inference spend history").LogError(ctx, s.logger)
	}

	months := groupInferenceSpendMonths(rows, currentMonth)
	if len(months) == 0 || !months[0].Current {
		months = append([]*gen.InferenceSpendMonth{emptyInferenceSpendMonth(currentMonth)}, months...)
	}

	return &gen.InferenceSpendHistory{Months: months}, nil
}

func groupInferenceSpendMonths(rows []repo.ListOpenRouterSpendByMonthRow, currentMonth time.Time) []*gen.InferenceSpendMonth {
	months := make([]*gen.InferenceSpendMonth, 0)
	var current *gen.InferenceSpendMonth
	var currentStart time.Time
	for _, row := range rows {
		if !row.MonthStart.Valid {
			continue
		}
		start := startOfUTCDay(row.MonthStart.Time)
		if current == nil || !start.Equal(currentStart) {
			current = inferenceSpendMonthFromRow(row, currentMonth)
			currentStart = start
			months = append(months, current)
		}
		if !knownInferenceSpendKeyType(row.KeyType) {
			continue
		}
		current.KeySpend = append(current.KeySpend, &gen.InferenceSpendMonthKey{
			KeyType:  row.KeyType,
			SpendUsd: row.SpendUsd,
		})
	}
	for _, month := range months {
		sortInferenceSpendKeys(month.KeySpend)
	}
	return months
}

func inferenceSpendMonthFromRow(row repo.ListOpenRouterSpendByMonthRow, currentMonth time.Time) *gen.InferenceSpendMonth {
	start := startOfUTCDay(row.MonthStart.Time)
	var recordedThrough *string
	if row.MonthRecordedThrough.Valid {
		value := row.MonthRecordedThrough.Time.UTC().Format(time.DateOnly)
		recordedThrough = &value
	}
	return &gen.InferenceSpendMonth{
		MonthStart:      start.Format(time.RFC3339),
		MonthEnd:        start.AddDate(0, 1, 0).Format(time.RFC3339),
		SpendUsd:        row.MonthSpendUsd,
		RecordedThrough: recordedThrough,
		Current:         start.Equal(currentMonth),
		KeySpend:        []*gen.InferenceSpendMonthKey{},
	}
}

func emptyInferenceSpendMonth(monthStart time.Time) *gen.InferenceSpendMonth {
	return &gen.InferenceSpendMonth{
		MonthStart:      monthStart.Format(time.RFC3339),
		MonthEnd:        monthStart.AddDate(0, 1, 0).Format(time.RFC3339),
		SpendUsd:        zeroInferenceSpendUSD,
		RecordedThrough: nil,
		Current:         true,
		KeySpend:        []*gen.InferenceSpendMonthKey{},
	}
}

func knownInferenceSpendKeyType(keyType string) bool {
	switch openrouter.KeyType(keyType) {
	case openrouter.KeyTypeChat, openrouter.KeyTypeInternal:
		return true
	default:
		return false
	}
}

func sortInferenceSpendKeys(keys []*gen.InferenceSpendMonthKey) {
	order := make(map[string]int, len(openrouter.BillableKeyTypes()))
	for index, keyType := range openrouter.BillableKeyTypes() {
		order[string(keyType)] = index
	}
	slices.SortStableFunc(keys, func(a, b *gen.InferenceSpendMonthKey) int {
		return order[a.KeyType] - order[b.KeyType]
	})
}

func startOfUTCMonth(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func startOfUTCDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func finiteDate(value time.Time) pgtype.Date {
	return pgtype.Date{Time: startOfUTCDay(value), InfinityModifier: pgtype.Finite, Valid: true}
}
