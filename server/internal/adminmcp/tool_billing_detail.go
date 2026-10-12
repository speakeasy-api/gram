//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

const (
	// maxBillingDetailTextBytes bounds curated labels, statuses and timestamps.
	maxBillingDetailTextBytes = 256

	// maxBillingDetailNumberBytes accommodates decimal quantities without unbounded text.
	maxBillingDetailNumberBytes = 64

	// maxBillingDetailBuckets covers every day in a three-calendar-month query.
	maxBillingDetailBuckets = 93

	// maxBillingDetailProducts matches the three ordinary metered product families.
	maxBillingDetailProducts = 3

	// maxBillingDetailOutputBytes leaves response headroom below the 64 KiB proposal/request bound.
	maxBillingDetailOutputBytes = 48 << 10 // 48 KiB
)

var errBillingDetailUnavailable = errors.New("organization billing details are unavailable")

// BillingDetailReader exposes organisation-scoped billing reads, never arbitrary provider lookups.
type BillingDetailReader interface {
	// GetStripeSubscription derives the subscription from the canonical organisation.
	GetStripeSubscription(context.Context, *gen.GetStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error)

	// GetMeterUsage returns ordinary usage for one supported family and window.
	GetMeterUsage(context.Context, *gen.GetMeterUsagePayload) (*gen.AdminMeterUsageResponse, error)

	// GetSpendBreakdown returns current-list-price estimates for the window.
	GetSpendBreakdown(context.Context, *gen.GetSpendBreakdownPayload) (*gen.AdminSpendBreakdownResponse, error)
}

// BillingDateWindowInput selects an exact organisation and an optional billing window.
type BillingDateWindowInput struct {
	// OrganizationID is canonical, never a provider identifier.
	OrganizationID string `json:"organization_id" jsonschema:"Exact organization ID returned by find_organizations"`

	// From is the inclusive UTC-midnight boundary.
	From *string `json:"from,omitempty" jsonschema:"Optional inclusive UTC-midnight RFC3339 timestamp; must be paired with to"`

	// To is the exclusive UTC-midnight boundary.
	To *string `json:"to,omitempty" jsonschema:"Optional exclusive UTC-midnight RFC3339 timestamp; must be paired with from; maximum three calendar months"`
}

// OrganizationMeterUsageDetailInput selects one ordinary meter family.
type OrganizationMeterUsageDetailInput struct {
	// BillingDateWindowInput fixes the organisation and time window.
	BillingDateWindowInput

	// Family names one of the supported metered products.
	Family string `json:"family" jsonschema:"One of agent_session_storage, mcp_bandwidth, risk_content_scans"`
}

// BillingStatusDetail omits provider identifiers and payment/contact records.
type BillingStatusDetail struct {
	// OrganizationID identifies the resolved account.
	OrganizationID string `json:"organization_id"`

	// HasStripeCustomer reports a stored association, not provider availability.
	HasStripeCustomer bool `json:"has_stripe_customer"`

	// HasStripeSubscription reports a stored subscription association.
	HasStripeSubscription bool `json:"has_stripe_subscription"`

	// SubscriptionStatus distinguishes no subscription from a provider state.
	SubscriptionStatus string `json:"subscription_status"`

	// CurrentPeriodStart is the associated subscription's period start.
	CurrentPeriodStart string `json:"current_period_start,omitempty"`

	// CurrentPeriodEnd is the associated subscription's period end.
	CurrentPeriodEnd string `json:"current_period_end,omitempty"`

	// TrialStart is absent when there is no provider trial.
	TrialStart *string `json:"trial_start,omitempty"`

	// TrialEnd is the provider trial's end date, when present.
	TrialEnd *string `json:"trial_end,omitempty"`

	// CancelAtPeriodEnd reports scheduled cancellation.
	CancelAtPeriodEnd bool `json:"cancel_at_period_end"`

	// CancelAt is the scheduled cancellation time, when present.
	CancelAt *string `json:"cancel_at,omitempty"`

	// CanceledAt records completed cancellation, when present.
	CanceledAt *string `json:"canceled_at,omitempty"`

	// PaymentFailed reports payment failure without payment-method details.
	PaymentFailed bool `json:"payment_failed"`
}

// MeterUsageBucketDetail is one daily ordinary usage quantity.
type MeterUsageBucketDetail struct {
	// From is inclusive.
	From string `json:"from"`

	// To is exclusive.
	To string `json:"to"`

	// Total preserves quantity precision as decimal text.
	Total string `json:"total"`
}

// OrganizationMeterUsageDetail is a bounded daily series without provider metadata.
type OrganizationMeterUsageDetail struct {
	// OrganizationID identifies the resolved account.
	OrganizationID string `json:"organization_id"`

	// Family names the queried meter.
	Family string `json:"family"`

	// WindowFrom is the inclusive report boundary.
	WindowFrom string `json:"window_from"`

	// WindowTo is the exclusive report boundary.
	WindowTo string `json:"window_to"`

	// Unit describes the quantities.
	Unit string `json:"unit"`

	// Total is the entire report quantity as decimal text.
	Total string `json:"total"`

	// Buckets contains the bounded daily quantities.
	Buckets []MeterUsageBucketDetail `json:"buckets"`

	// QueriedAt identifies when the source was read.
	QueriedAt string `json:"queried_at"`
}

// OrganizationSpendBreakdownInput controls optional daily detail.
type OrganizationSpendBreakdownInput struct {
	// BillingDateWindowInput fixes the organisation and time window.
	BillingDateWindowInput

	// IncludeDailyBuckets requests daily quantity and cost estimates.
	IncludeDailyBuckets bool `json:"include_daily_buckets,omitempty" jsonschema:"Include daily quantity and estimated cost buckets for each product"`
}

// SpendBucketDetail preserves decimal precision for a daily estimate.
type SpendBucketDetail struct {
	// From is inclusive.
	From string `json:"from"`

	// To is exclusive.
	To string `json:"to"`

	// Quantity is the metered amount as decimal text.
	Quantity string `json:"quantity"`

	// CostUSD is the current-list-price estimate, not an invoice amount.
	CostUSD string `json:"cost_usd"`
}

// SpendProductDetail is an ordinary metered product's estimate.
type SpendProductDetail struct {
	// Label is the curated product name.
	Label string `json:"label"`

	// Unit describes the metered amount.
	Unit string `json:"unit"`

	// Quantity is the entire product quantity as decimal text.
	Quantity string `json:"quantity"`

	// RateQuantity is the quantity covered by RateUSD.
	RateQuantity string `json:"rate_quantity"`

	// RateUSD is the current list price as decimal text.
	RateUSD string `json:"rate_usd"`

	// CostUSD is the product's estimated cost.
	CostUSD string `json:"cost_usd"`

	// Buckets is present only when daily detail was requested.
	Buckets []SpendBucketDetail `json:"buckets,omitempty"`
}

// OrganizationSpendBreakdown contains bounded per-product estimates, not billing identities.
type OrganizationSpendBreakdown struct {
	// OrganizationID identifies the resolved account.
	OrganizationID string `json:"organization_id"`

	// WindowFrom is the inclusive report boundary.
	WindowFrom string `json:"window_from"`

	// WindowTo is the exclusive report boundary.
	WindowTo string `json:"window_to"`

	// Currency describes the estimate's currency.
	Currency string `json:"currency"`

	// PricingBasis describes the list prices used.
	PricingBasis string `json:"pricing_basis"`

	// TotalCostUSD is the entire estimated cost as decimal text.
	TotalCostUSD string `json:"total_cost_usd"`

	// QueriedAt identifies when the source was read.
	QueriedAt string `json:"queried_at"`

	// Products contains bounded ordinary metered product estimates.
	Products []SpendProductDetail `json:"products"`
}

func registerBillingDetailTools(server *mcp.Server, organizations OrganizationReader, reads BillingDetailReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_billing_status", Title: "Get Organization Billing Status",
		Description: "Read stored Stripe-association presence and the associated subscription status for one exact organization. A missing subscription is distinct from an unavailable billing dependency. No provider IDs, payment details, contact data or provider metadata are returned.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, BillingStatusDetail, error) {
		if !verifiedStaff(ctx) {
			return nil, BillingStatusDetail{}, errBillingDetailUnavailable
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, BillingStatusDetail{}, err
		}
		out := BillingStatusDetail{
			OrganizationID: org.ID, HasStripeCustomer: org.StripeCustomerID != nil,
			HasStripeSubscription: org.StripeSubscriptionID != nil, SubscriptionStatus: "not_subscribed",
		}
		if !out.HasStripeSubscription {
			return nil, out, nil
		}
		if reads == nil {
			return nil, BillingStatusDetail{}, errBillingDetailUnavailable
		}
		subscription, err := reads.GetStripeSubscription(ctx, &gen.GetStripeSubscriptionPayload{OrganizationID: org.ID})
		if err != nil || subscription == nil || !validBillingStatus(subscription) {
			return nil, BillingStatusDetail{}, errBillingDetailUnavailable
		}
		out.SubscriptionStatus = subscription.Status
		out.CurrentPeriodStart = subscription.CurrentPeriodStart
		out.CurrentPeriodEnd = subscription.CurrentPeriodEnd
		out.TrialStart = subscription.TrialStart
		out.TrialEnd = subscription.TrialEnd
		out.CancelAtPeriodEnd = subscription.CancelAtPeriodEnd
		out.CancelAt = subscription.CancelAt
		out.CanceledAt = subscription.CanceledAt
		out.PaymentFailed = subscription.PaymentFailed
		if !withinBillingDetailOutputLimit(out) {
			return nil, BillingStatusDetail{}, errBillingDetailUnavailable
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_meter_usage_detail", Title: "Get Organization Meter Usage Detail",
		Description: "Read daily ordinary usage for one exact organization and supported meter family. Optional date boundaries use the billing service's inclusive/exclusive UTC-midnight convention and three-calendar-month limit. No billing-cycle details or provider identifiers are returned.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationMeterUsageDetailInput) (*mcp.CallToolResult, OrganizationMeterUsageDetail, error) {
		if !verifiedStaff(ctx) {
			return nil, OrganizationMeterUsageDetail{}, errBillingDetailUnavailable
		}
		if !validMeterFamily(input.Family) || !validBillingDateWindow(input.From, input.To) {
			return nil, OrganizationMeterUsageDetail{}, errors.New("provide a supported meter family and a valid billing date window")
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationMeterUsageDetail{}, err
		}
		if reads == nil {
			return nil, OrganizationMeterUsageDetail{}, errBillingDetailUnavailable
		}
		report, err := reads.GetMeterUsage(ctx, &gen.GetMeterUsagePayload{OrganizationID: org.ID, Family: input.Family, From: input.From, To: input.To})
		if err != nil || report == nil || report.Window == nil || report.Family != input.Family || len(report.Buckets) > maxBillingDetailBuckets || !validMeterReport(report) {
			return nil, OrganizationMeterUsageDetail{}, errBillingDetailUnavailable
		}
		out := OrganizationMeterUsageDetail{
			OrganizationID: org.ID, Family: report.Family, WindowFrom: report.Window.From,
			WindowTo: report.Window.To, Unit: report.Unit, Total: report.Total,
			Buckets: make([]MeterUsageBucketDetail, 0, len(report.Buckets)), QueriedAt: report.QueriedAt,
		}
		for _, bucket := range report.Buckets {
			if bucket == nil || !validBillingText(bucket.From) || !validBillingText(bucket.To) || !validBillingNumber(bucket.Total) {
				return nil, OrganizationMeterUsageDetail{}, errBillingDetailUnavailable
			}
			out.Buckets = append(out.Buckets, MeterUsageBucketDetail{From: bucket.From, To: bucket.To, Total: bucket.Total})
		}
		if !withinBillingDetailOutputLimit(out) {
			return nil, OrganizationMeterUsageDetail{}, errBillingDetailUnavailable
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_spend_breakdown", Title: "Get Organization Spend Breakdown",
		Description: "Read bounded per-product usage and current-list-price estimates for one exact organization. Optional UTC-midnight date boundaries use the billing service's three-calendar-month limit; daily buckets are opt-in. Estimates are not invoices. No product/provider IDs, billing cycles or provider metadata are returned.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationSpendBreakdownInput) (*mcp.CallToolResult, OrganizationSpendBreakdown, error) {
		if !verifiedStaff(ctx) {
			return nil, OrganizationSpendBreakdown{}, errBillingDetailUnavailable
		}
		if !validBillingDateWindow(input.From, input.To) {
			return nil, OrganizationSpendBreakdown{}, errors.New("provide a valid billing date window")
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationSpendBreakdown{}, err
		}
		if reads == nil {
			return nil, OrganizationSpendBreakdown{}, errBillingDetailUnavailable
		}
		report, err := reads.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{OrganizationID: org.ID, From: input.From, To: input.To})
		if err != nil || report == nil || report.Window == nil || len(report.Products) > maxBillingDetailProducts || !validSpendReport(report) {
			return nil, OrganizationSpendBreakdown{}, errBillingDetailUnavailable
		}
		out := OrganizationSpendBreakdown{
			OrganizationID: org.ID, WindowFrom: report.Window.From, WindowTo: report.Window.To,
			Currency: report.Currency, PricingBasis: report.PricingBasis,
			TotalCostUSD: report.TotalCostUsd, QueriedAt: report.QueriedAt,
			Products: make([]SpendProductDetail, 0, len(report.Products)),
		}
		for _, product := range report.Products {
			if product == nil || !validBillingText(product.Label) || !validBillingText(product.Unit) || !validBillingNumber(product.Quantity) || !validBillingNumber(product.RateQuantity) || !validBillingNumber(product.RateUsd) || !validBillingNumber(product.CostUsd) || len(product.Buckets) > maxBillingDetailBuckets {
				return nil, OrganizationSpendBreakdown{}, errBillingDetailUnavailable
			}
			item := SpendProductDetail{Label: product.Label, Unit: product.Unit, Quantity: product.Quantity, RateQuantity: product.RateQuantity, RateUSD: product.RateUsd, CostUSD: product.CostUsd}
			if input.IncludeDailyBuckets {
				item.Buckets = make([]SpendBucketDetail, 0, len(product.Buckets))
				for _, bucket := range product.Buckets {
					if bucket == nil || !validBillingText(bucket.From) || !validBillingText(bucket.To) || !validBillingNumber(bucket.Quantity) || !validBillingNumber(bucket.CostUsd) {
						return nil, OrganizationSpendBreakdown{}, errBillingDetailUnavailable
					}
					item.Buckets = append(item.Buckets, SpendBucketDetail{From: bucket.From, To: bucket.To, Quantity: bucket.Quantity, CostUSD: bucket.CostUsd})
				}
			}
			out.Products = append(out.Products, item)
		}
		if !withinBillingDetailOutputLimit(out) {
			return nil, OrganizationSpendBreakdown{}, errBillingDetailUnavailable
		}
		return nil, out, nil
	})
}

func validMeterFamily(family string) bool {
	return family == "agent_session_storage" || family == "mcp_bandwidth" || family == "risk_content_scans"
}

func validBillingDateWindow(fromText, toText *string) bool {
	_, _, err := usage.ResolveMeterUsageWindow(fromText, toText, usage.BillingCyclePeriod{})
	return err == nil
}

func validBillingText(value string) bool {
	return value != "" && len(value) <= maxBillingDetailTextBytes
}

func validBillingNumber(value string) bool {
	return value != "" && len(value) <= maxBillingDetailNumberBytes
}

func validBillingStatus(value *gen.AdminStripeSubscription) bool {
	return validBillingText(value.Status) && len(value.CurrentPeriodStart) <= maxBillingDetailTextBytes && len(value.CurrentPeriodEnd) <= maxBillingDetailTextBytes && optionalBillingText(value.TrialStart) && optionalBillingText(value.TrialEnd) && optionalBillingText(value.CancelAt) && optionalBillingText(value.CanceledAt)
}

func optionalBillingText(value *string) bool {
	return value == nil || len(*value) <= maxBillingDetailTextBytes
}

func validMeterReport(report *gen.AdminMeterUsageResponse) bool {
	if !validBillingText(report.Unit) || !validBillingNumber(report.Total) || !validBillingText(report.Window.From) || !validBillingText(report.Window.To) || !validBillingText(report.QueriedAt) {
		return false
	}

	return true
}

func validSpendReport(report *gen.AdminSpendBreakdownResponse) bool {
	if !validBillingText(report.Window.From) || !validBillingText(report.Window.To) || !validBillingText(report.Currency) || !validBillingText(report.PricingBasis) || !validBillingNumber(report.TotalCostUsd) || !validBillingText(report.QueriedAt) {
		return false
	}

	for _, product := range report.Products {
		if product == nil || len(product.Buckets) > maxBillingDetailBuckets {
			return false
		}
	}

	return true
}

func withinBillingDetailOutputLimit(value any) bool {
	encoded, err := json.Marshal(value)
	return err == nil && len(encoded) <= maxBillingDetailOutputBytes
}
