package admin

import (
	"context"
	"errors"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

const customerUsageMixedMeasurementMessage = "Usage readings for this organization mix units or measurement methods, so its spend cannot be estimated."

func (s *Service) ListCustomerUsage(ctx context.Context, payload *gen.ListCustomerUsagePayload) (*gen.AdminCustomerUsageResponse, error) {
	if s.billing == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "billing operations are temporarily unavailable").LogWarn(ctx, s.logger)
	}

	interval := usage.CustomerUsageInterval(payload.Interval)
	switch interval {
	case usage.CustomerUsageDaily, usage.CustomerUsageWeekly, usage.CustomerUsageMonthly:
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid interval").LogWarn(ctx, s.logger)
	}

	rows, err := repo.New(s.db).AdminListCustomerUsageOrganizations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list customer usage organizations").LogError(ctx, s.logger)
	}

	organizations := make([]usage.CustomerUsageOrganization, len(rows))
	for index, row := range rows {
		organizations[index] = usage.CustomerUsageOrganization{
			ID:                    row.ID,
			BillingCycleAnchorDay: int(row.BillingCycleAnchorDay),
			CreatedAt:             row.CreatedAt.Time.UTC(),
		}
	}

	report, err := s.billing.GetCustomerUsage(ctx, organizations, interval)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get customer usage").LogError(ctx, s.logger)
	}

	customers := make([]*gen.AdminCustomerUsage, len(rows))
	for index, row := range rows {
		customers[index] = customerUsageView(row, report.Customers[index])
	}

	return &gen.AdminCustomerUsageResponse{
		Interval:     string(interval),
		Currency:     "USD",
		PricingBasis: "current_payg_list_price",
		QueriedAt:    report.QueriedAt.Format(time.RFC3339Nano),
		Customers:    customers,
	}, nil
}

func customerUsageView(row repo.AdminListCustomerUsageOrganizationsRow, customer usage.CustomerUsage) *gen.AdminCustomerUsage {
	view := &gen.AdminCustomerUsage{
		OrganizationID: row.ID,
		Name:           row.Name,
		Slug:           row.Slug,
		AccountType:    row.AccountType,
		TrialState:     row.TrialState,
		CurrentCycle:   meterUsageWindowView(customer.CurrentCycle),
		// A customer whose usage could not be read has no chart buckets, so its
		// window is the current cycle rather than a zero-valued range.
		Window:              meterUsageWindowView(customer.CurrentCycle),
		Products:            []*gen.SpendProduct{},
		PreviousPeriod:      nil,
		PreviousPeriodCosts: []*gen.AdminCustomerUsageProductCost{},
		Error:               nil,
	}
	if customer.Err != nil {
		message := "Usage for this organization could not be read."
		if errors.Is(customer.Err, chrepo.ErrMixedMeasurement) {
			message = customerUsageMixedMeasurementMessage
		}
		view.Error = &message
		return view
	}

	view.Window = meterUsageWindowView(customer.Window)
	for _, product := range customer.Products {
		buckets := make([]*gen.SpendBucket, len(product.Buckets))
		for index, bucket := range product.Buckets {
			buckets[index] = &gen.SpendBucket{
				From:     bucket.From,
				To:       bucket.To,
				Quantity: bucket.Quantity,
				CostUsd:  bucket.CostUsd,
			}
		}
		view.Products = append(view.Products, &gen.SpendProduct{
			ID:           product.ID,
			Label:        product.Label,
			Unit:         product.Unit,
			Quantity:     product.Quantity,
			RateQuantity: product.RateQuantity,
			RateUsd:      product.RateUsd,
			CostUsd:      product.CostUsd,
			Buckets:      buckets,
		})
	}
	if customer.PreviousPeriod != nil {
		view.PreviousPeriod = meterUsageWindowView(*customer.PreviousPeriod)
		for _, cost := range customer.PreviousPeriodCosts {
			view.PreviousPeriodCosts = append(view.PreviousPeriodCosts, &gen.AdminCustomerUsageProductCost{
				ProductID: cost.ProductID,
				CostUsd:   cost.CostUSD,
			})
		}
	}
	return view
}

func meterUsageWindowView(period usage.BillingCyclePeriod) *gen.MeterUsageWindow {
	return &gen.MeterUsageWindow{
		From: period.Start.UTC().Format(time.RFC3339),
		To:   period.End.UTC().Format(time.RFC3339),
	}
}
