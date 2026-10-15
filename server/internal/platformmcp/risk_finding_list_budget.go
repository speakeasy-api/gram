package platformmcp

import (
	"context"
	"fmt"
)

// riskFindingListLister is the handler-selection seam for the per-finding
// reads: the live service, its budgeted wrapper, or nil for the stubs.
type riskFindingListLister interface {
	valid() bool
	List(context.Context, Principal, ListRiskFindingPageInput) (ListRiskFindingPageOutput, error)
	ListByChat(context.Context, Principal, ListRiskFindingsByChatInput) (ListRiskFindingsByChatOutput, error)
	RuleBreakdown(context.Context, Principal, GetRiskRuleBreakdownInput) (GetRiskRuleBreakdownOutput, error)
}

// budgetedRiskFindingList charges the row-level Watchdog budget before every
// read. Individual findings expose more per call than the rule-level alerts,
// so they share that sensitive-read allowance rather than the unmetered policy
// and exclusion reads.
type budgetedRiskFindingList struct {
	service riskFindingListLister
	budget  OperationBudget
}

func (s *budgetedRiskFindingList) valid() bool {
	return s != nil && s.service != nil && s.service.valid() && s.budget.valid()
}

func (s *budgetedRiskFindingList) List(ctx context.Context, principal Principal, input ListRiskFindingPageInput) (ListRiskFindingPageOutput, error) {
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ListRiskFindingPageOutput{}, err
	}
	output, err := s.service.List(ctx, principal, input)
	if err != nil {
		return output, fmt.Errorf("list risk finding page: %w", err)
	}
	return output, nil
}

func (s *budgetedRiskFindingList) ListByChat(ctx context.Context, principal Principal, input ListRiskFindingsByChatInput) (ListRiskFindingsByChatOutput, error) {
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ListRiskFindingsByChatOutput{}, err
	}
	output, err := s.service.ListByChat(ctx, principal, input)
	if err != nil {
		return output, fmt.Errorf("list risk findings by chat: %w", err)
	}
	return output, nil
}

func (s *budgetedRiskFindingList) RuleBreakdown(ctx context.Context, principal Principal, input GetRiskRuleBreakdownInput) (GetRiskRuleBreakdownOutput, error) {
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetRiskRuleBreakdownOutput{}, err
	}
	output, err := s.service.RuleBreakdown(ctx, principal, input)
	if err != nil {
		return output, fmt.Errorf("get risk rule breakdown: %w", err)
	}
	return output, nil
}
