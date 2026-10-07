package usage

import (
	"context"
	"fmt"
	"math/big"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

// CustomerUsageInterval selects how a customer's chart is bucketed.
type CustomerUsageInterval string

const (
	// CustomerUsageDaily buckets the current billing cycle by UTC day.
	CustomerUsageDaily CustomerUsageInterval = "daily"

	// CustomerUsageWeekly buckets the current billing cycle by Monday-start UTC
	// week, clipped to the cycle's boundaries.
	CustomerUsageWeekly CustomerUsageInterval = "weekly"

	// CustomerUsageMonthly gives one bucket per billing cycle, aligned to the
	// organization's own cycle boundaries rather than calendar months.
	CustomerUsageMonthly CustomerUsageInterval = "monthly"
)

const (
	// customerUsageMonthlyCycles is how many billing cycles the monthly view
	// covers, the current one included: half a year of month-over-month trend.
	customerUsageMonthlyCycles = 6

	// customerUsageComparisonCycles covers the current cycle and the previous
	// one, which daily and weekly views need for the change comparison.
	customerUsageComparisonCycles = 2
)

// CustomerUsageOrganization is one organization whose usage is reported.
type CustomerUsageOrganization struct {
	// ID is the canonical organization ID.
	ID string

	// BillingCycleAnchorDay is the stored 1-31 anchor. Zero or out-of-range
	// values fall back to calendar months, as on the per-organization page.
	BillingCycleAnchorDay int

	// CreatedAt bounds which cycles the organization existed in.
	CreatedAt time.Time
}

// CustomerUsageProductCost is one product's exact estimated cost.
type CustomerUsageProductCost struct {
	// ProductID is agent_session_storage, risk_content_scans, or mcp_egress.
	ProductID string

	// CostUSD is the exact decimal estimate at current PAYG list prices.
	CostUSD string
}

// CustomerUsage is one organization's usage report.
type CustomerUsage struct {
	// OrganizationID identifies the reported organization.
	OrganizationID string

	// CurrentCycle is the billing cycle containing the report time.
	CurrentCycle BillingCyclePeriod

	// Window spans the chart buckets, from the first bucket's start to the
	// last bucket's end.
	Window BillingCyclePeriod

	// Products lists the three priced products in display order. Each
	// product's Quantity and CostUsd cover the current cycle to date, and its
	// Buckets are the chart buckets for the requested interval.
	Products []*gen.SpendProduct

	// PreviousPeriod is the start of the previous billing cycle, cut to the
	// same number of elapsed days as the current one. Nil when the
	// organization did not exist before the current cycle.
	PreviousPeriod *BillingCyclePeriod

	// PreviousPeriodCosts are the per-product costs over PreviousPeriod, in
	// display order. Empty when PreviousPeriod is nil.
	PreviousPeriodCosts []CustomerUsageProductCost

	// Err is set when this organization's usage could not be reported. The
	// other fields are then empty.
	Err error
}

// CustomerUsageReport is the usage of several organizations read together.
type CustomerUsageReport struct {
	// QueriedAt is the report time that separates current and future buckets.
	QueriedAt time.Time

	// Customers holds one entry per requested organization, in request order.
	Customers []CustomerUsage
}

// customerUsagePlan is the cycle arithmetic for one organization, worked out
// before the usage read so the read can cover every organization's window.
type customerUsagePlan struct {
	organization   CustomerUsageOrganization
	current        BillingCyclePeriod
	previous       *BillingCyclePeriod
	buckets        []BillingCyclePeriod
	earliestNeeded time.Time
}

// GetCustomerUsage reports current-cycle usage, interval-bucketed chart data,
// and a same-elapsed-days comparison with the previous cycle for each
// organization, at current PAYG list prices. It reads usage for every
// organization in one query. API handlers must authorize callers.
func (s *Service) GetCustomerUsage(ctx context.Context, organizations []CustomerUsageOrganization, interval CustomerUsageInterval) (*CustomerUsageReport, error) {
	queriedAt := s.now().UTC()
	report := &CustomerUsageReport{QueriedAt: queriedAt, Customers: make([]CustomerUsage, 0, len(organizations))}
	if len(organizations) == 0 {
		return report, nil
	}

	plans := make([]customerUsagePlan, 0, len(organizations))
	ids := make([]string, 0, len(organizations))
	from := time.Time{}
	for _, organization := range organizations {
		plan, err := planCustomerUsage(organization, interval, queriedAt)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
		ids = append(ids, organization.ID)
		if from.IsZero() || plan.earliestNeeded.Before(from) {
			from = plan.earliestNeeded
		}
	}

	// Usage after today does not exist yet, so the read stops at tomorrow.
	to := utcDay(queriedAt).AddDate(0, 0, 1)
	spend, err := chrepo.New(s.meterReadConn).GetSpendForOrganizations(ctx, chrepo.OrganizationsSpendParams{
		OrganizationIDs: ids,
		From:            from,
		To:              to,
	})
	if err != nil {
		return nil, fmt.Errorf("query customer spend quantities: %w", err)
	}

	rowsByOrganization := make(map[string][]chrepo.OrganizationSpendRow, len(organizations))
	for _, row := range spend.Rows {
		rowsByOrganization[row.OrganizationID] = append(rowsByOrganization[row.OrganizationID], row)
	}

	for _, plan := range plans {
		if _, mixed := spend.MixedMeasurement[plan.organization.ID]; mixed {
			report.Customers = append(report.Customers, CustomerUsage{
				OrganizationID:      plan.organization.ID,
				CurrentCycle:        plan.current,
				Window:              BillingCyclePeriod{Start: time.Time{}, End: time.Time{}},
				Products:            nil,
				PreviousPeriod:      nil,
				PreviousPeriodCosts: nil,
				Err:                 chrepo.ErrMixedMeasurement,
			})
			continue
		}
		customer, err := buildCustomerUsage(plan, rowsByOrganization[plan.organization.ID])
		if err != nil {
			return nil, fmt.Errorf("build customer usage for %s: %w", plan.organization.ID, err)
		}
		report.Customers = append(report.Customers, customer)
	}
	return report, nil
}

func planCustomerUsage(organization CustomerUsageOrganization, interval CustomerUsageInterval, now time.Time) (customerUsagePlan, error) {
	cycleCount := customerUsageComparisonCycles
	if interval == CustomerUsageMonthly {
		cycleCount = customerUsageMonthlyCycles
	}
	cycles := BillingCycles(now, organization.BillingCycleAnchorDay, cycleCount)
	current := cycles[len(cycles)-1]

	var previous *BillingCyclePeriod
	if organization.CreatedAt.Before(current.Start) {
		prior := cycles[len(cycles)-2]
		elapsedDays := int(utcDay(now).Sub(current.Start)/(24*time.Hour)) + 1
		end := prior.Start.AddDate(0, 0, elapsedDays)
		if end.After(prior.End) {
			end = prior.End
		}
		previous = &BillingCyclePeriod{Start: prior.Start, End: end}
	}

	var buckets []BillingCyclePeriod
	switch interval {
	case CustomerUsageDaily:
		for day := current.Start; day.Before(current.End); day = day.AddDate(0, 0, 1) {
			buckets = append(buckets, BillingCyclePeriod{Start: day, End: day.AddDate(0, 0, 1)})
		}
	case CustomerUsageWeekly:
		for start := current.Start; start.Before(current.End); {
			// Weeks start Monday, matching the per-organization page.
			daysToMonday := (8 - int(start.Weekday())) % 7
			if daysToMonday == 0 {
				daysToMonday = 7
			}
			end := start.AddDate(0, 0, daysToMonday)
			if end.After(current.End) {
				end = current.End
			}
			buckets = append(buckets, BillingCyclePeriod{Start: start, End: end})
			start = end
		}
	case CustomerUsageMonthly:
		for index, cycle := range cycles {
			// A cycle that ended before the organization existed has nothing
			// to show. The current cycle is always kept.
			if cycle.End.After(organization.CreatedAt) || index == len(cycles)-1 {
				buckets = append(buckets, cycle)
			}
		}
	default:
		return customerUsagePlan{}, fmt.Errorf("unknown customer usage interval %q", interval)
	}

	earliest := buckets[0].Start
	if previous != nil && previous.Start.Before(earliest) {
		earliest = previous.Start
	}
	return customerUsagePlan{
		organization:   organization,
		current:        current,
		previous:       previous,
		buckets:        buckets,
		earliestNeeded: earliest,
	}, nil
}

func buildCustomerUsage(plan customerUsagePlan, rows []chrepo.OrganizationSpendRow) (CustomerUsage, error) {
	specs := SpendProductSpecs()
	productIndexes := make(map[string]int, len(specs))
	bucketQuantities := make([][]*big.Int, len(specs))
	currentQuantities := make([]*big.Int, len(specs))
	previousQuantities := make([]*big.Int, len(specs))
	for index, spec := range specs {
		productIndexes[spec.ID] = index
		bucketQuantities[index] = make([]*big.Int, len(plan.buckets))
		for bucket := range plan.buckets {
			bucketQuantities[index][bucket] = new(big.Int)
		}
		currentQuantities[index] = new(big.Int)
		previousQuantities[index] = new(big.Int)
	}

	for _, row := range rows {
		productIndex, ok := productIndexes[row.ProductID]
		if !ok {
			return CustomerUsage{}, fmt.Errorf("query returned unknown spend product %q", row.ProductID)
		}
		quantity, ok := new(big.Int).SetString(row.Quantity, 10)
		if !ok {
			return CustomerUsage{}, fmt.Errorf("invalid exact spend quantity %q", row.Quantity)
		}
		day := utcDay(row.Day)
		if periodContains(plan.current, day) {
			currentQuantities[productIndex].Add(currentQuantities[productIndex], quantity)
		}
		if plan.previous != nil && periodContains(*plan.previous, day) {
			previousQuantities[productIndex].Add(previousQuantities[productIndex], quantity)
		}
		for bucketIndex, bucket := range plan.buckets {
			if periodContains(bucket, day) {
				bucketQuantities[productIndex][bucketIndex].Add(bucketQuantities[productIndex][bucketIndex], quantity)
				break
			}
		}
	}

	products := make([]*gen.SpendProduct, 0, len(specs))
	previousCosts := make([]CustomerUsageProductCost, 0, len(specs))
	for index, spec := range specs {
		buckets := make([]*gen.SpendBucket, 0, len(plan.buckets))
		for bucketIndex, bucket := range plan.buckets {
			quantity := bucketQuantities[index][bucketIndex]
			cost, err := priceExact(quantity, spec)
			if err != nil {
				return CustomerUsage{}, err
			}
			buckets = append(buckets, &gen.SpendBucket{
				From:     bucket.Start.Format(time.RFC3339Nano),
				To:       bucket.End.Format(time.RFC3339Nano),
				Quantity: quantity.String(),
				CostUsd:  cost,
			})
		}
		cost, err := priceExact(currentQuantities[index], spec)
		if err != nil {
			return CustomerUsage{}, err
		}
		products = append(products, &gen.SpendProduct{
			ID:           spec.ID,
			Label:        spec.Label,
			Unit:         spec.Unit,
			Quantity:     currentQuantities[index].String(),
			RateQuantity: spec.RateQuantity,
			RateUsd:      spec.RateUSD,
			CostUsd:      cost,
			Buckets:      buckets,
		})
		if plan.previous != nil {
			previousCost, err := priceExact(previousQuantities[index], spec)
			if err != nil {
				return CustomerUsage{}, err
			}
			previousCosts = append(previousCosts, CustomerUsageProductCost{ProductID: spec.ID, CostUSD: previousCost})
		}
	}

	return CustomerUsage{
		OrganizationID: plan.organization.ID,
		CurrentCycle:   plan.current,
		Window: BillingCyclePeriod{
			Start: plan.buckets[0].Start,
			End:   plan.buckets[len(plan.buckets)-1].End,
		},
		Products:            products,
		PreviousPeriod:      plan.previous,
		PreviousPeriodCosts: previousCosts,
		Err:                 nil,
	}, nil
}

func periodContains(period BillingCyclePeriod, day time.Time) bool {
	return !day.Before(period.Start) && day.Before(period.End)
}

func priceExact(quantity *big.Int, spec SpendProductSpec) (string, error) {
	cost, err := PriceSpendQuantity(quantity, spec)
	if err != nil {
		return "", err
	}
	return exactDecimal(cost)
}
