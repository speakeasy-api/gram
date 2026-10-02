//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

var errBillingDiagnosticsUnavailable = errors.New("organization usage diagnostics are unavailable")

type BillingDiagnosticsReader interface {
	GetInferenceKeys(context.Context, *gen.GetInferenceKeysPayload) ([]*gen.AdminInferenceKey, error)
	GetInferenceSpendHistory(context.Context, *gen.GetInferenceSpendHistoryPayload) ([]*gen.AdminInferenceSpendMonth, error)
	GetMeterUsage(context.Context, *gen.GetMeterUsagePayload) (*gen.AdminMeterUsageResponse, error)
	GetSpendBreakdown(context.Context, *gen.GetSpendBreakdownPayload) (*gen.AdminSpendBreakdownResponse, error)
}

type InferenceKeyState struct {
	KeyType                 string   `json:"key_type"`
	CreditsUsed             float64  `json:"credits_used"`
	MonthlyCredits          int64    `json:"monthly_credits"`
	Disabled                bool     `json:"disabled"`
	DisableCauses           []string `json:"disable_causes"`
	DisableCausesClassified bool     `json:"disable_causes_classified"`
}

type InferenceKeyStates struct {
	OrganizationID string              `json:"organization_id"`
	Keys           []InferenceKeyState `json:"keys"`
}

type InferenceSpendMonth struct {
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	SpendUSD    string `json:"spend_usd"`
}

type InferenceSpendHistory struct {
	OrganizationID string                `json:"organization_id"`
	Months         []InferenceSpendMonth `json:"months"`
}

type MeterUsageInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact organization ID returned by find_organizations"`
	Family         string `json:"family" jsonschema:"One of agent_session_storage, mcp_bandwidth, risk_content_scans"`
}

type MeterUsageSummary struct {
	OrganizationID string `json:"organization_id"`
	Family         string `json:"family"`
	WindowFrom     string `json:"window_from"`
	WindowTo       string `json:"window_to"`
	Unit           string `json:"unit"`
	Total          string `json:"total"`
	QueriedAt      string `json:"queried_at"`
}

type SpendBreakdownSummary struct {
	OrganizationID string `json:"organization_id"`
	WindowFrom     string `json:"window_from"`
	WindowTo       string `json:"window_to"`
	Currency       string `json:"currency"`
	PricingBasis   string `json:"pricing_basis"`
	TotalCostUSD   string `json:"total_cost_usd"`
	QueriedAt      string `json:"queried_at"`
}

func registerBillingDiagnosticTools(server *mcp.Server, organizations OrganizationReader, reads BillingDiagnosticsReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_inference_key_state", Title: "Get Inference Key State",
		Description: "Read configured state and usage of up to four platform-managed inference key types for an exact organization. No key material or provider identifiers are returned.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, InferenceKeyStates, error) {
		out := InferenceKeyStates{Keys: []InferenceKeyState{}}
		if !verifiedStaff(ctx) {
			return nil, out, errBillingDiagnosticsUnavailable
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, out, err
		}
		if reads == nil {
			return nil, out, errBillingDiagnosticsUnavailable
		}
		keys, err := reads.GetInferenceKeys(ctx, &gen.GetInferenceKeysPayload{OrganizationID: org.ID})
		if err != nil || len(keys) > 4 {
			return nil, out, errBillingDiagnosticsUnavailable
		}
		for _, key := range keys {
			if key == nil || len(key.KeyType) > 32 || len(key.DisableCauses) > 8 {
				return nil, InferenceKeyStates{}, errBillingDiagnosticsUnavailable
			}
			for _, cause := range key.DisableCauses {
				if len(cause) > 64 {
					return nil, InferenceKeyStates{}, errBillingDiagnosticsUnavailable
				}
			}
			// Legacy unclassified keys have nil causes; disable_causes_classified carries that distinction.
			causes := append([]string{}, key.DisableCauses...)
			out.Keys = append(out.Keys, InferenceKeyState{KeyType: key.KeyType, CreditsUsed: key.CreditsUsed, MonthlyCredits: key.MonthlyCredits, Disabled: key.Disabled, DisableCauses: causes, DisableCausesClassified: key.DisableCausesClassified})
		}
		out.OrganizationID = org.ID
		return nil, out, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_inference_spend_history", Title: "Get Inference Spend History",
		Description: "Read up to twelve complete UTC calendar months of recorded inference spend for an exact organization. No provider identifiers or credentials.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, InferenceSpendHistory, error) {
		out := InferenceSpendHistory{Months: []InferenceSpendMonth{}}
		if !verifiedStaff(ctx) {
			return nil, out, errBillingDiagnosticsUnavailable
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, out, err
		}
		if reads == nil {
			return nil, out, errBillingDiagnosticsUnavailable
		}
		months, err := reads.GetInferenceSpendHistory(ctx, &gen.GetInferenceSpendHistoryPayload{OrganizationID: org.ID})
		if err != nil || len(months) > 12 {
			return nil, out, errBillingDiagnosticsUnavailable
		}
		for _, month := range months {
			if month == nil || len(month.SpendUsd) > 64 || len(month.PeriodStart) > 32 || len(month.PeriodEnd) > 32 {
				return nil, InferenceSpendHistory{}, errBillingDiagnosticsUnavailable
			}
			out.Months = append(out.Months, InferenceSpendMonth{PeriodStart: month.PeriodStart, PeriodEnd: month.PeriodEnd, SpendUSD: month.SpendUsd})
		}
		out.OrganizationID = org.ID
		return nil, out, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_meter_usage", Title: "Get Meter Usage",
		Description: "Read the default billing-cycle totals-only ordinary meter usage for one exact organization and one named meter family. Daily buckets and billing identifiers are omitted.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input MeterUsageInput) (*mcp.CallToolResult, MeterUsageSummary, error) {
		if !verifiedStaff(ctx) || (input.Family != "agent_session_storage" && input.Family != "mcp_bandwidth" && input.Family != "risk_content_scans") {
			return nil, MeterUsageSummary{}, errBillingDiagnosticsUnavailable
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, MeterUsageSummary{}, err
		}
		if reads == nil {
			return nil, MeterUsageSummary{}, errBillingDiagnosticsUnavailable
		}
		report, err := reads.GetMeterUsage(ctx, &gen.GetMeterUsagePayload{OrganizationID: org.ID, Family: input.Family})
		if err != nil || report == nil || report.Window == nil || report.Family != input.Family || len(report.Total) > 64 || len(report.Window.From) > 64 || len(report.Window.To) > 64 || len(report.Unit) > 64 || len(report.QueriedAt) > 64 {
			return nil, MeterUsageSummary{}, errBillingDiagnosticsUnavailable
		}
		return nil, MeterUsageSummary{OrganizationID: org.ID, Family: report.Family, WindowFrom: report.Window.From, WindowTo: report.Window.To, Unit: report.Unit, Total: report.Total, QueriedAt: report.QueriedAt}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_spend_breakdown_total", Title: "Get Metered Spend Total",
		Description: "Read only the current default billing-cycle estimated total for three metered products for an exact organization. Uses current PAYG list prices; not a bill. Product buckets and billing identities are omitted.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, SpendBreakdownSummary, error) {
		if !verifiedStaff(ctx) {
			return nil, SpendBreakdownSummary{}, errBillingDiagnosticsUnavailable
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, SpendBreakdownSummary{}, err
		}
		if reads == nil {
			return nil, SpendBreakdownSummary{}, errBillingDiagnosticsUnavailable
		}
		report, err := reads.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{OrganizationID: org.ID})
		if err != nil || report == nil || report.Window == nil || len(report.TotalCostUsd) > 64 || len(report.Window.From) > 64 || len(report.Window.To) > 64 || len(report.Currency) > 64 || len(report.PricingBasis) > 64 || len(report.QueriedAt) > 64 {
			return nil, SpendBreakdownSummary{}, errBillingDiagnosticsUnavailable
		}
		return nil, SpendBreakdownSummary{OrganizationID: org.ID, WindowFrom: report.Window.From, WindowTo: report.Window.To, Currency: report.Currency, PricingBasis: report.PricingBasis, TotalCostUSD: report.TotalCostUsd, QueriedAt: report.QueriedAt}, nil
	})
}
