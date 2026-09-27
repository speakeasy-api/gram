//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

var errUsageUnavailable = errors.New("organization usage summary is unavailable")

type OrganizationUsageSummary struct {
	OrganizationID         string  `json:"organization_id"`
	PeriodStart            string  `json:"period_start"`
	PeriodEnd              string  `json:"period_end"`
	TokensUnderManagement  int64   `json:"tokens_under_management"`
	TumUnitPriceUSD        string  `json:"tum_unit_price_usd"`
	TumCostUSD             string  `json:"tum_cost_usd"`
	OtherInferenceSpendUSD string  `json:"other_inference_spend_usd"`
	RecordedThrough        *string `json:"recorded_through,omitempty"`
	EstimatedTotalUSD      string  `json:"estimated_total_usd"`
}

func registerUsageTools(server *mcp.Server, organizations OrganizationReader, usage UsageReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_usage_summary",
		Title:       "Get Organization Usage Summary",
		Description: "Read the current billing-cycle PAYG usage estimate for an exact organization ID. This is not a bill: inference spend includes completed UTC days only, and invoices can finalize up to 72 hours after the cycle ends. Does not return billing identities or inference keys.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, OrganizationUsageSummary, error) {
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationUsageSummary{}, err
		}
		if usage == nil {
			return nil, OrganizationUsageSummary{}, errUsageUnavailable
		}
		summary, err := usage.GetPaygBillingSummary(ctx, &gen.GetPaygBillingSummaryPayload{OrganizationID: org.ID})
		if err != nil || summary == nil {
			return nil, OrganizationUsageSummary{}, errUsageUnavailable
		}
		return nil, OrganizationUsageSummary{
			OrganizationID: org.ID, PeriodStart: summary.PeriodStart, PeriodEnd: summary.PeriodEnd,
			TokensUnderManagement: summary.TumTokens, TumUnitPriceUSD: summary.TumUnitPriceUsd,
			TumCostUSD: summary.TumCostUsd, OtherInferenceSpendUSD: summary.OtherInferenceSpendUsd,
			RecordedThrough: summary.RecordedThrough, EstimatedTotalUSD: summary.EstimatedTotalUsd,
		}, nil
	})
}
