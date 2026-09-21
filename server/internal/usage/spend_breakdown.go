package usage

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	millionRateQuantity              = "1000000"
	gibRateQuantity                  = "1073741824"
	spendAvailabilityAvailable       = "available"
	spendAvailabilityUnsupportedPlan = "unsupported_plan"
)

type spendProductSpec struct {
	id           string
	label        string
	unit         string
	rateQuantity string
	rateUSD      string
}

var spendProductSpecs = [...]spendProductSpec{
	{
		id:           "agent_session_storage",
		label:        "Agent session storage",
		unit:         string(metering.UnitSTokens),
		rateQuantity: millionRateQuantity,
		rateUSD:      billing.TUMPricePerMillionUSD,
	},
	{
		id:           "risk_content_scans",
		label:        "Risk scanning",
		unit:         string(metering.UnitSTokens),
		rateQuantity: millionRateQuantity,
		rateUSD:      billing.RiskScanPricePerMillionUSD,
	},
	{
		id:           "mcp_egress",
		label:        "MCP gateway",
		unit:         string(metering.UnitBytes),
		rateQuantity: gibRateQuantity,
		rateUSD:      billing.MCPEgressPricePerGiBUSD,
	},
}

// GetSpendBreakdown reports availability of server-owned spend for the active
// organization and returns exact current PAYG list-price estimates when available.
func (s *Service) GetSpendBreakdown(ctx context.Context, payload *gen.GetSpendBreakdownPayload) (*gen.SpendBreakdownResponse, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, queriedAt, cycles, err := s.loadSpendBreakdownWindow(ctx, authCtx.ActiveOrganizationID, payload)
	if err != nil {
		return nil, err
	}
	accountType, err := s.repo.GetBillingOrganizationAccountType(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get organization billing type for spend breakdown").LogError(ctx, s.logger)
	}
	if accountType != string(billing.TierPayg) {
		response, err := buildSpendBreakdownResponse(from, to, queriedAt, cycles, spendAvailabilityUnsupportedPlan, nil)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build unsupported spend breakdown response").LogError(ctx, s.logger)
		}
		return response, nil
	}

	return s.calculateSpendBreakdown(ctx, authCtx.ActiveOrganizationID, from, to, queriedAt, cycles)
}

// GetSpendBreakdownForOrganization performs an unrestricted spend read for an
// already authorized, canonical organization ID. API handlers must authorize callers.
func (s *Service) GetSpendBreakdownForOrganization(ctx context.Context, organizationID string, payload *gen.GetSpendBreakdownPayload) (*gen.SpendBreakdownResponse, error) {
	if organizationID == "" {
		return nil, oops.C(oops.CodeNotFound)
	}

	from, to, queriedAt, cycles, err := s.loadSpendBreakdownWindow(ctx, organizationID, payload)
	if err != nil {
		return nil, err
	}
	return s.calculateSpendBreakdown(ctx, organizationID, from, to, queriedAt, cycles)
}

func (s *Service) loadSpendBreakdownWindow(ctx context.Context, organizationID string, payload *gen.GetSpendBreakdownPayload) (time.Time, time.Time, time.Time, []BillingCyclePeriod, error) {
	queriedAt := s.now().UTC()
	meta, err := s.repo.GetBillingMetadata(ctx, organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, time.Time{}, time.Time{}, nil, oops.E(oops.CodeUnexpected, err, "get billing metadata for spend breakdown").LogError(ctx, s.logger)
	}
	cycles := BillingCycles(queriedAt, int(meta.BillingCycleAnchorDay), tumHistoryCycles)
	from, to, err := resolveMeterUsageWindow(payload.From, payload.To, cycles[len(cycles)-1])
	if err != nil {
		if boundaryErr, ok := errors.AsType[*oops.ShareableError](err); ok {
			return time.Time{}, time.Time{}, time.Time{}, nil, boundaryErr.LogWarn(ctx, s.logger)
		}
		return time.Time{}, time.Time{}, time.Time{}, nil, err
	}
	return from, to, queriedAt, cycles, nil
}

func (s *Service) calculateSpendBreakdown(ctx context.Context, organizationID string, from, to, queriedAt time.Time, cycles []BillingCyclePeriod) (*gen.SpendBreakdownResponse, error) {
	rows, err := chrepo.New(s.meterReadConn).GetSpend(ctx, chrepo.SpendParams{
		OrganizationID: organizationID,
		From:           from,
		To:             to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "query meter spend quantities").LogError(ctx, s.logger)
	}

	response, err := buildSpendBreakdownResponse(from, to, queriedAt, cycles, spendAvailabilityAvailable, rows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build spend breakdown response").LogError(ctx, s.logger)
	}
	return response, nil
}

func buildSpendBreakdownResponse(from, to, queriedAt time.Time, cycles []BillingCyclePeriod, availability string, rows []chrepo.SpendRow) (*gen.SpendBreakdownResponse, error) {
	cycleViews := make([]*gen.MeterUsageWindow, 0, len(cycles))
	for _, cycle := range cycles {
		cycleViews = append(cycleViews, &gen.MeterUsageWindow{
			From: cycle.Start.UTC().Format(time.RFC3339),
			To:   cycle.End.UTC().Format(time.RFC3339),
		})
	}
	response := &gen.SpendBreakdownResponse{
		Availability: availability,
		Window: &gen.MeterUsageWindow{
			From: from.Format(time.RFC3339Nano),
			To:   to.Format(time.RFC3339Nano),
		},
		BillingCycles: cycleViews,
		Currency:      "USD",
		PricingBasis:  "current_payg_list_price",
		QueriedAt:     queriedAt.Format(time.RFC3339Nano),
		TotalCostUsd:  "0",
		Products:      []*gen.SpendProduct{},
	}
	if availability == spendAvailabilityUnsupportedPlan {
		return response, nil
	}

	bucketCount := int(to.Sub(from) / (24 * time.Hour))
	bucketIndexes := make(map[int64]int, bucketCount)
	for day, index := from, 0; day.Before(to); day, index = day.AddDate(0, 0, 1), index+1 {
		bucketIndexes[day.Unix()] = index
	}

	products := make([]*gen.SpendProduct, 0, len(spendProductSpecs))
	quantities := make(map[string][]*big.Int, len(spendProductSpecs))
	productTotals := make(map[string]*big.Int, len(spendProductSpecs))
	productSpecs := make(map[string]spendProductSpec, len(spendProductSpecs))
	for _, spec := range spendProductSpecs {
		buckets := make([]*gen.SpendBucket, 0, bucketCount)
		bucketQuantities := make([]*big.Int, 0, bucketCount)
		for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
			buckets = append(buckets, &gen.SpendBucket{
				From:     day.Format(time.RFC3339Nano),
				To:       day.AddDate(0, 0, 1).Format(time.RFC3339Nano),
				Quantity: "0",
				CostUsd:  "0",
			})
			bucketQuantities = append(bucketQuantities, new(big.Int))
		}
		products = append(products, &gen.SpendProduct{
			ID:           spec.id,
			Label:        spec.label,
			Unit:         spec.unit,
			Quantity:     "0",
			RateQuantity: spec.rateQuantity,
			RateUsd:      spec.rateUSD,
			CostUsd:      "0",
			Buckets:      buckets,
		})
		quantities[spec.id] = bucketQuantities
		productTotals[spec.id] = new(big.Int)
		productSpecs[spec.id] = spec
	}

	for _, row := range rows {
		bucketQuantities, ok := quantities[row.ProductID]
		if !ok {
			return nil, fmt.Errorf("query returned unknown spend product %q", row.ProductID)
		}
		bucketIndex, ok := bucketIndexes[utcDay(row.Day).Unix()]
		if !ok {
			return nil, fmt.Errorf("query returned day outside requested spend window: %s", row.Day)
		}
		quantity, ok := new(big.Int).SetString(row.Quantity, 10)
		if !ok {
			return nil, fmt.Errorf("invalid exact spend quantity %q", row.Quantity)
		}
		bucketQuantities[bucketIndex].Add(bucketQuantities[bucketIndex], quantity)
		productTotals[row.ProductID].Add(productTotals[row.ProductID], quantity)
	}

	totalCost := new(big.Rat)
	for _, product := range products {
		spec := productSpecs[product.ID]
		for index, quantity := range quantities[product.ID] {
			cost, err := priceSpendQuantity(quantity, spec)
			if err != nil {
				return nil, err
			}
			product.Buckets[index].Quantity = quantity.String()
			product.Buckets[index].CostUsd, err = exactDecimal(cost)
			if err != nil {
				return nil, err
			}
		}
		product.Quantity = productTotals[product.ID].String()
		cost, err := priceSpendQuantity(productTotals[product.ID], spec)
		if err != nil {
			return nil, err
		}
		product.CostUsd, err = exactDecimal(cost)
		if err != nil {
			return nil, err
		}
		totalCost.Add(totalCost, cost)
	}

	totalCostUSD, err := exactDecimal(totalCost)
	if err != nil {
		return nil, err
	}
	response.TotalCostUsd = totalCostUSD
	response.Products = products
	return response, nil
}

func priceSpendQuantity(quantity *big.Int, spec spendProductSpec) (*big.Rat, error) {
	rate, ok := new(big.Rat).SetString(spec.rateUSD)
	if !ok {
		return nil, fmt.Errorf("invalid spend rate %q", spec.rateUSD)
	}
	rateQuantity, ok := new(big.Int).SetString(spec.rateQuantity, 10)
	if !ok || rateQuantity.Sign() <= 0 {
		return nil, fmt.Errorf("invalid spend rate quantity %q", spec.rateQuantity)
	}
	return new(big.Rat).Quo(new(big.Rat).Mul(new(big.Rat).SetInt(quantity), rate), new(big.Rat).SetInt(rateQuantity)), nil
}

func exactDecimal(value *big.Rat) (string, error) {
	precision, exact := value.FloatPrec()
	if !exact {
		return "", fmt.Errorf("non-terminating exact decimal denominator %s", value.Denom())
	}
	return value.FloatString(precision), nil
}
