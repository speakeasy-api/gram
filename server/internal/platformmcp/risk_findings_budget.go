package platformmcp

import (
	"context"
	"fmt"
)

const (
	RiskFindingsConnectionLimitName   = "platform-mcp-risk-findings-connection"
	RiskFindingsOrganizationLimitName = "platform-mcp-risk-findings-organization"
	// Findings expose row-level metadata, so use the sensitive-read allowance
	// without spending the diagnostics or risk-mutation buckets.
	RiskFindingsQueriesPerConnectionPerMinute   = SensitiveDiagnosticQueriesPerConnectionPerMinute
	RiskFindingsQueriesPerOrganizationPerMinute = SensitiveDiagnosticQueriesPerOrganizationPerMinute
)

type riskFindingsLister interface {
	valid() bool
	List(context.Context, Principal, ListRiskFindingsInput) (ListRiskFindingsOutput, error)
}

type budgetedRiskFindings struct {
	service riskFindingsLister
	budget  OperationBudget
}

func (s *budgetedRiskFindings) valid() bool {
	return s != nil && s.service != nil && s.service.valid() && s.budget.valid()
}

func (s *budgetedRiskFindings) List(ctx context.Context, principal Principal, input ListRiskFindingsInput) (ListRiskFindingsOutput, error) {
	if err := s.budget.Allow(ctx, principal); err != nil {
		var zero ListRiskFindingsOutput
		return zero, err
	}
	output, err := s.service.List(ctx, principal, input)
	if err != nil {
		return output, fmt.Errorf("list risk findings: %w", err)
	}
	return output, nil
}
