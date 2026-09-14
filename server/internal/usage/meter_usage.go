package usage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const maxMeterUsageMonths = 3

// GetMeterUsage returns incrementally aggregated daily ordinary usage quantities
// and a bounded full-period facet breakdown for the active organization.
func (s *Service) GetMeterUsage(ctx context.Context, payload *gen.GetMeterUsagePayload) (*gen.MeterUsageResponse, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	family := metering.UsageFamily(payload.Family)
	breakdown := ""
	if payload.Breakdown != nil {
		breakdown = *payload.Breakdown
	}
	resolvedBreakdown, selection, err := metering.ResolveUsageSelection(family, breakdown)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid family or breakdown").LogError(ctx, s.logger)
	}

	queriedAt := s.now().UTC()
	meta, err := s.repo.GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "get billing metadata for meter usage").LogError(ctx, s.logger)
	}
	cycles := BillingCycles(queriedAt, int(meta.BillingCycleAnchorDay), tumHistoryCycles)

	from, to, err := resolveMeterUsageWindow(payload.From, payload.To, cycles[len(cycles)-1])
	if err != nil {
		if boundaryErr, ok := errors.AsType[*oops.ShareableError](err); ok {
			return nil, boundaryErr.LogWarn(ctx, s.logger)
		}
		return nil, err
	}

	result, err := chrepo.New(s.meterReadConn).GetUsage(ctx, chrepo.UsageParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Selection:      selection,
		From:           from,
		To:             to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "query meter usage").LogError(ctx, s.logger)
	}

	response, err := buildMeterUsageResponse(payload, resolvedBreakdown, from, to, queriedAt, cycles, result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build meter usage response").LogError(ctx, s.logger)
	}
	return response, nil
}

func resolveMeterUsageWindow(fromText, toText *string, activeCycle BillingCyclePeriod) (time.Time, time.Time, error) {
	if (fromText == nil) != (toText == nil) {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, nil, "from and to must be provided together")
	}
	if fromText == nil {
		return activeCycle.Start.UTC(), activeCycle.End.UTC(), nil
	}

	from, err := time.Parse(time.RFC3339, *fromText)
	if err != nil {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, err, "invalid from: expected an RFC3339 timestamp")
	}
	to, err := time.Parse(time.RFC3339, *toText)
	if err != nil {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, err, "invalid to: expected an RFC3339 timestamp")
	}
	from = from.UTC()
	to = to.UTC()
	if !from.Equal(utcDay(from)) {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, nil, "from must be a UTC midnight boundary")
	}
	if !to.Equal(utcDay(to)) {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, nil, "to must be a UTC midnight boundary")
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, nil, "from must be before to")
	}
	maxTo := anchoredCycleStart(from.Year(), from.Month()+maxMeterUsageMonths, from.Day())
	if to.After(maxTo) {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, nil, "meter usage window must not exceed three calendar months")
	}
	return from, to, nil
}

type meterSeriesAccumulator struct {
	series *gen.MeterUsageSeries
	total  *big.Int
}

func buildMeterUsageResponse(payload *gen.GetMeterUsagePayload, breakdown string, from, to, queriedAt time.Time, cycles []BillingCyclePeriod, result chrepo.UsageResult) (*gen.MeterUsageResponse, error) {
	bucketCount := int(to.Sub(from) / (24 * time.Hour))
	buckets := make([]*gen.MeterUsageBucket, 0, bucketCount)
	bucketIndexes := make(map[int64]int, bucketCount)
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		bucketIndexes[day.Unix()] = len(buckets)
		buckets = append(buckets, &gen.MeterUsageBucket{
			From:  day.Format(time.RFC3339Nano),
			To:    day.AddDate(0, 0, 1).Format(time.RFC3339Nano),
			Total: "0",
		})
	}

	bucketTotals := make([]*big.Int, len(buckets))
	for i := range bucketTotals {
		bucketTotals[i] = new(big.Int)
	}
	seriesByIdentity := make(map[string]*meterSeriesAccumulator)
	periodTotal := new(big.Int)
	for _, row := range result.Rows {
		quantity, ok := new(big.Int).SetString(row.Total, 10)
		if !ok {
			return nil, fmt.Errorf("invalid exact quantity %q", row.Total)
		}
		bucketIndex, ok := bucketIndexes[utcDay(row.Day).Unix()]
		if !ok {
			return nil, fmt.Errorf("query returned day outside requested window: %s", row.Day)
		}
		bucketTotals[bucketIndex].Add(bucketTotals[bucketIndex], quantity)
		periodTotal.Add(periodTotal, quantity)

		identity := row.Kind + "\x00" + row.Key
		accumulator, ok := seriesByIdentity[identity]
		if !ok {
			values := make([]string, len(buckets))
			for i := range values {
				values[i] = "0"
			}
			var key *string
			if row.Kind == "value" {
				keyValue := row.Key
				key = &keyValue
			}
			accumulator = &meterSeriesAccumulator{
				series: &gen.MeterUsageSeries{
					Kind:   row.Kind,
					Key:    key,
					Label:  row.Label,
					Total:  "0",
					Values: values,
				},
				total: new(big.Int),
			}
			seriesByIdentity[identity] = accumulator
		} else if row.Label > accumulator.series.Label {
			accumulator.series.Label = row.Label
		}
		value, ok := new(big.Int).SetString(accumulator.series.Values[bucketIndex], 10)
		if !ok {
			return nil, errors.New("invalid accumulated meter usage quantity")
		}
		accumulator.series.Values[bucketIndex] = value.Add(value, quantity).String()
		accumulator.total.Add(accumulator.total, quantity)
	}

	series := make([]*gen.MeterUsageSeries, 0, len(seriesByIdentity))
	seriesTotals := make(map[*gen.MeterUsageSeries]*big.Int, len(seriesByIdentity))
	for _, accumulator := range seriesByIdentity {
		accumulator.series.Total = accumulator.total.String()
		if payload.Family == string(metering.UsageFamilyRiskContentScans) && breakdown == "scanner" && accumulator.series.Kind == "value" {
			switch *accumulator.series.Key {
			case string(metering.MeterRiskGitleaks):
				accumulator.series.Label = "Secret scanning"
			case string(metering.MeterRiskPresidio):
				accumulator.series.Label = "Sensitive data"
			}
		}
		series = append(series, accumulator.series)
		seriesTotals[accumulator.series] = accumulator.total
	}
	slices.SortFunc(series, func(left, right *gen.MeterUsageSeries) int {
		if left.Kind == "remainder" || right.Kind == "remainder" {
			switch left.Kind {
			case right.Kind:
				return 0
			case "remainder":
				return 1
			default:
				return -1
			}
		}
		order := seriesTotals[right].Cmp(seriesTotals[left])
		if order != 0 {
			return order
		}
		if left.Kind != right.Kind {
			return cmp.Compare(left.Kind, right.Kind)
		}
		if left.Key != nil && right.Key != nil {
			return cmp.Compare(*left.Key, *right.Key)
		}
		return 0
	})

	for i, total := range bucketTotals {
		buckets[i].Total = total.String()
	}
	cycleViews := make([]*gen.MeterUsageWindow, 0, len(cycles))
	for _, cycle := range cycles {
		cycleViews = append(cycleViews, &gen.MeterUsageWindow{
			From: cycle.Start.UTC().Format(time.RFC3339),
			To:   cycle.End.UTC().Format(time.RFC3339),
		})
	}
	return &gen.MeterUsageResponse{
		Family: payload.Family,
		Window: &gen.MeterUsageWindow{
			From: from.Format(time.RFC3339Nano),
			To:   to.Format(time.RFC3339Nano),
		},
		BillingCycles:     cycleViews,
		Unit:              result.Unit,
		MeasurementMethod: result.MeasurementMethod,
		Total:             periodTotal.String(),
		Buckets:           buckets,
		Breakdown: &gen.MeterUsageBreakdown{
			Dimension: breakdown,
			Series:    series,
		},
		QueriedAt: queriedAt.Format(time.RFC3339Nano),
	}, nil
}

func utcDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
