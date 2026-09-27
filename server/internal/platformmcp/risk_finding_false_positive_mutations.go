package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// maxRiskFindingFalsePositiveBatch bounds one dismiss or restore call. It
	// is smaller than the management API's 500 so the per-id receipt stays
	// well inside the 16 KiB operation receipt payload limit.
	maxRiskFindingFalsePositiveBatch = 200

	maxRiskFindingFalsePositiveReasonRunes = 500

	riskFindingResultCategoryDismissed = "dismissed"
	riskFindingResultCategoryRestored  = "restored"
	riskFindingResultCategoryNoChange  = "no_change"
)

// riskFindingFalsePositiveService dismisses and restores individual Watchdog
// findings. Finding ids are risk_results row ids, the same identifier the
// dashboard, the managed assistant's platform_mark_risk_false_positive tool,
// and the ClickHouse findings mirror all use.
type riskFindingFalsePositiveService struct {
	controls       *RiskMutationControls
	falsePositives *risk.FalsePositiveCore
}

type riskFindingFalsePositiveInput struct {
	ProjectSlug    string   `json:"project_slug"`
	FindingIDs     []string `json:"finding_ids"`
	Reason         string   `json:"reason,omitempty"`
	Confirmed      bool     `json:"confirmed"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// normalizedRiskFindingFalsePositive is the hashed replay identity: ids are
// deduplicated and sorted so the same set replays regardless of order.
type normalizedRiskFindingFalsePositive struct {
	ProjectSlug string   `json:"project_slug"`
	FindingIDs  []string `json:"finding_ids"`
	Reason      string   `json:"reason,omitempty"`
}

// RiskFindingFalsePositiveReceipt partitions every requested finding id into
// exactly one outcome. Unknown ids never fail the call: they are reported so
// the agent can tell the user which findings it could not act on.
type RiskFindingFalsePositiveReceipt struct {
	Project RiskMutationReceiptProject `json:"project"`

	// ChangedFindingIDs moved into the requested state in this call.
	ChangedFindingIDs []string `json:"changed_finding_ids"`

	// AlreadyInRequestedStateFindingIDs exist in the project but were already
	// dismissed (mark) or already active (unmark).
	AlreadyInRequestedStateFindingIDs []string `json:"already_in_requested_state_finding_ids"`

	// NotFoundFindingIDs do not exist in the named project.
	NotFoundFindingIDs []string `json:"not_found_finding_ids"`

	// ResultCategory is dismissed or restored when at least one finding
	// changed, and no_change otherwise.
	ResultCategory string `json:"result_category"`
}

type MarkRiskFindingsFalsePositiveReceiptResult struct {
	RiskFindingFalsePositiveReceipt
}

func (MarkRiskFindingsFalsePositiveReceiptResult) riskMutationReceiptOperation() string {
	return operationMarkRiskFindingsFalsePositive
}

type UnmarkRiskFindingsFalsePositiveReceiptResult struct {
	RiskFindingFalsePositiveReceipt
}

func (UnmarkRiskFindingsFalsePositiveReceiptResult) riskMutationReceiptOperation() string {
	return operationUnmarkRiskFindingsFalsePositive
}

type MarkRiskFindingsFalsePositiveToolOutput struct {
	MarkRiskFindingsFalsePositiveReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

type UnmarkRiskFindingsFalsePositiveToolOutput struct {
	UnmarkRiskFindingsFalsePositiveReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

func newRiskFindingFalsePositiveService(controls *RiskMutationControls, falsePositives *risk.FalsePositiveCore) *riskFindingFalsePositiveService {
	return &riskFindingFalsePositiveService{controls: controls, falsePositives: falsePositives}
}

func (s *riskFindingFalsePositiveService) markTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, MarkRiskFindingsFalsePositiveToolOutput, error) {
	var zero MarkRiskFindingsFalsePositiveToolOutput
	input, err := decodeRiskFindingFalsePositiveInput(raw)
	if err != nil {
		return riskMutationToolRefusal[MarkRiskFindingsFalsePositiveToolOutput](err)
	}
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	receipt, result, err := s.apply(ctx, principal, input, false)
	if err != nil {
		return riskMutationToolRefusal[MarkRiskFindingsFalsePositiveToolOutput](err)
	}
	return nil, MarkRiskFindingsFalsePositiveToolOutput{
		MarkRiskFindingsFalsePositiveReceiptResult: MarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: result},
		Receipt: riskMutationToolReceipt(receipt),
	}, nil
}

func (s *riskFindingFalsePositiveService) unmarkTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UnmarkRiskFindingsFalsePositiveToolOutput, error) {
	var zero UnmarkRiskFindingsFalsePositiveToolOutput
	input, err := decodeRiskFindingFalsePositiveInput(raw)
	if err != nil {
		return riskMutationToolRefusal[UnmarkRiskFindingsFalsePositiveToolOutput](err)
	}
	if input.Reason != "" {
		return riskMutationToolRefusal[UnmarkRiskFindingsFalsePositiveToolOutput](invalidRiskFindingRequest())
	}
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	receipt, result, err := s.apply(ctx, principal, input, true)
	if err != nil {
		return riskMutationToolRefusal[UnmarkRiskFindingsFalsePositiveToolOutput](err)
	}
	return nil, UnmarkRiskFindingsFalsePositiveToolOutput{
		UnmarkRiskFindingsFalsePositiveReceiptResult: UnmarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: result},
		Receipt: riskMutationToolReceipt(receipt),
	}, nil
}

// decodeRiskFindingFalsePositiveInput refuses an unconfirmed call before the
// principal is read, so a missing confirmation is reported the same way to an
// anonymous caller and an administrator.
func decodeRiskFindingFalsePositiveInput(raw map[string]any) (riskFindingFalsePositiveInput, error) {
	var input riskFindingFalsePositiveInput
	if err := decodeRiskMutationInput(raw, &input); err != nil {
		return riskFindingFalsePositiveInput{}, invalidRiskFindingRequest()
	}
	if !input.Confirmed {
		return riskFindingFalsePositiveInput{}, &RiskMutationError{
			Code:    "confirmation_required",
			Message: "Show the exact project and the findings that will be dismissed or restored; ask the user to confirm, then retry with confirmed: true.",
			Cause:   ErrRiskMutationInvalid,
		}
	}
	return input, nil
}

func (s *riskFindingFalsePositiveService) apply(ctx context.Context, principal Principal, input riskFindingFalsePositiveInput, restore bool) (OperationReceipt, RiskFindingFalsePositiveReceipt, error) {
	if s == nil || s.controls == nil || s.falsePositives == nil {
		return OperationReceipt{}, RiskFindingFalsePositiveReceipt{}, riskMutationUnavailable()
	}
	project, err := s.controls.Admit(ctx, principal, strings.TrimSpace(input.ProjectSlug))
	if err != nil {
		return OperationReceipt{}, RiskFindingFalsePositiveReceipt{}, err
	}
	ids, err := parseRiskFindingIDs(input.FindingIDs)
	if err != nil {
		return OperationReceipt{}, RiskFindingFalsePositiveReceipt{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > maxRiskFindingFalsePositiveReasonRunes {
		return OperationReceipt{}, RiskFindingFalsePositiveReceipt{}, invalidRiskFindingRequest()
	}

	operation := operationMarkRiskFindingsFalsePositive
	if restore {
		operation = operationUnmarkRiskFindingsFalsePositive
	}
	normalizedIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		normalizedIDs = append(normalizedIDs, id.String())
	}
	slices.Sort(normalizedIDs)
	mutation := risk.FalsePositiveMutation{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		IDs:              ids,
		Reason:           nil,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName: nil,
	}
	if !restore && reason != "" {
		mutation.Reason = &reason
	}

	receipt, err := s.controls.Receipts().Execute(ctx, principal, project, RiskMutationReceiptRequest{
		Operation: operation, IdempotencyKey: input.IdempotencyKey,
		Input: normalizedRiskFindingFalsePositive{ProjectSlug: project.Slug, FindingIDs: normalizedIDs, Reason: reason},
	}, func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error) {
		existing, err := riskrepo.New(tx).GetRiskResultsByIDs(ctx, riskrepo.GetRiskResultsByIDsParams{ProjectID: project.ID, Ids: ids})
		if err != nil {
			return nil, riskMutationUnavailableWithCause(fmt.Errorf("read risk findings for false positive mutation: %w", err))
		}
		var changed []riskrepo.RiskResult
		if restore {
			changed, err = s.falsePositives.UnmarkInTransaction(ctx, tx, mutation)
		} else {
			changed, err = s.falsePositives.MarkInTransaction(ctx, tx, mutation)
		}
		if err != nil {
			return nil, riskMutationUnavailableWithCause(err)
		}
		result := partitionRiskFindingOutcomes(project, ids, existing, changed, restore)
		if restore {
			return UnmarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: result}, nil
		}
		return MarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: result}, nil
	})
	if err != nil {
		return OperationReceipt{}, RiskFindingFalsePositiveReceipt{}, err
	}
	var result RiskFindingFalsePositiveReceipt
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return OperationReceipt{}, RiskFindingFalsePositiveReceipt{}, fmt.Errorf("decode risk finding false positive receipt result: %w", err)
	}
	return receipt, result, nil
}

// parseRiskFindingIDs validates and deduplicates the requested ids, preserving
// first-seen order so the receipt lists them the way the caller supplied them.
func parseRiskFindingIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 || len(raw) > maxRiskFindingFalsePositiveBatch {
		return nil, invalidRiskFindingRequest()
	}
	ids := make([]uuid.UUID, 0, len(raw))
	seen := make(map[uuid.UUID]struct{}, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || id == uuid.Nil {
			return nil, invalidRiskFindingRequest()
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// partitionRiskFindingOutcomes assigns every requested id to one bucket: the
// rows the UPDATE returned changed; rows that exist in the project but were
// not returned were already in the requested state; the rest are not in this
// project. Slices are always non-nil so the receipt encodes [] rather than
// null.
func partitionRiskFindingOutcomes(project ResolvedProject, requested []uuid.UUID, existing, changed []riskrepo.RiskResult, restore bool) RiskFindingFalsePositiveReceipt {
	changedSet := make(map[uuid.UUID]struct{}, len(changed))
	for _, row := range changed {
		changedSet[row.ID] = struct{}{}
	}
	existingSet := make(map[uuid.UUID]struct{}, len(existing))
	for _, row := range existing {
		existingSet[row.ID] = struct{}{}
	}
	result := RiskFindingFalsePositiveReceipt{
		Project:                           riskReceiptProject(project),
		ChangedFindingIDs:                 []string{},
		AlreadyInRequestedStateFindingIDs: []string{},
		NotFoundFindingIDs:                []string{},
		ResultCategory:                    riskFindingResultCategoryNoChange,
	}
	for _, id := range requested {
		switch {
		case hasID(changedSet, id):
			result.ChangedFindingIDs = append(result.ChangedFindingIDs, id.String())
		case hasID(existingSet, id):
			result.AlreadyInRequestedStateFindingIDs = append(result.AlreadyInRequestedStateFindingIDs, id.String())
		default:
			result.NotFoundFindingIDs = append(result.NotFoundFindingIDs, id.String())
		}
	}
	if len(result.ChangedFindingIDs) > 0 {
		result.ResultCategory = riskFindingResultCategoryDismissed
		if restore {
			result.ResultCategory = riskFindingResultCategoryRestored
		}
	}
	return result
}

func hasID(set map[uuid.UUID]struct{}, id uuid.UUID) bool {
	_, ok := set[id]
	return ok
}

// validRiskFindingFalsePositiveReceipt keeps the stored receipt closed: only
// well-formed ids in a bounded count and a fixed-vocabulary category.
func validRiskFindingFalsePositiveReceipt(result RiskFindingFalsePositiveReceipt, changedCategory string) bool {
	if !validRiskReceiptProject(result.Project) {
		return false
	}
	total := 0
	for _, ids := range [][]string{result.ChangedFindingIDs, result.AlreadyInRequestedStateFindingIDs, result.NotFoundFindingIDs} {
		if ids == nil {
			return false
		}
		for _, id := range ids {
			if uuid.Validate(id) != nil {
				return false
			}
		}
		total += len(ids)
	}
	if total == 0 || total > maxRiskFindingFalsePositiveBatch {
		return false
	}
	if len(result.ChangedFindingIDs) > 0 {
		return result.ResultCategory == changedCategory
	}
	return result.ResultCategory == riskFindingResultCategoryNoChange
}

func invalidRiskFindingRequest() error {
	return &RiskMutationError{Code: "invalid_request", Message: "The risk finding false positive request is invalid.", Cause: ErrRiskMutationInvalid}
}
