package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxRiskMutationReceiptPayloadBytes = 16 << 10

// RiskMutationReceiptRequest is the safe identity of one normalized write.
// Input must already have schema defaults, canonical ordering, and transport
// aliases resolved. It is hashed and is never persisted or logged directly.
type RiskMutationReceiptRequest struct {
	Operation      string
	IdempotencyKey string
	Input          any
}

// RiskMutationReceiptResult is implemented only by the four closed, redacted
// result projections below. Callbacks cannot supply an open JSON object, so a
// new user-authored field cannot silently enter operation receipts.
type RiskMutationReceiptResult interface {
	riskMutationReceiptOperation() string
}

type RiskMutationReceiptProject struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// RiskPolicyReceiptSummary contains only fixed-vocabulary administrative state.
// Policy names, prompts, CEL, model configuration, principals, and URLs are
// deliberately absent.
type RiskPolicyReceiptSummary struct {
	ID         string `json:"id"`
	PolicyType string `json:"policy_type"`
	Enabled    bool   `json:"enabled"`
	Action     string `json:"action,omitempty"`
}

// RiskExclusionReceiptSummary omits match values and filters even for
// allowlisted match types so exact and legacy-regex content can never leak.
type RiskExclusionReceiptSummary struct {
	ID        string  `json:"id"`
	PolicyID  *string `json:"policy_id,omitempty"`
	MatchType string  `json:"match_type"`
	Enabled   bool    `json:"enabled"`
}

type CreateRiskPolicyReceiptResult struct {
	Project         RiskMutationReceiptProject `json:"project"`
	Policy          RiskPolicyReceiptSummary   `json:"policy"`
	Version         string                     `json:"version"`
	MatchedExisting bool                       `json:"matched_existing"`
	ResultCategory  string                     `json:"result_category"`
}

func (CreateRiskPolicyReceiptResult) riskMutationReceiptOperation() string {
	return operationCreateRiskPolicy
}

type UpdateRiskPolicyReceiptResult struct {
	Project        RiskMutationReceiptProject `json:"project"`
	Policy         RiskPolicyReceiptSummary   `json:"policy"`
	Version        string                     `json:"version"`
	ResultCategory string                     `json:"result_category"`
}

func (UpdateRiskPolicyReceiptResult) riskMutationReceiptOperation() string {
	return operationUpdateRiskPolicy
}

type CreateRiskExclusionReceiptResult struct {
	Project         RiskMutationReceiptProject  `json:"project"`
	Exclusion       RiskExclusionReceiptSummary `json:"exclusion"`
	Version         string                      `json:"version"`
	MatchedExisting bool                        `json:"matched_existing"`
	ResultCategory  string                      `json:"result_category"`
	Reconciliation  string                      `json:"reconciliation"`
}

func (CreateRiskExclusionReceiptResult) riskMutationReceiptOperation() string {
	return operationCreateRiskExclusion
}

type UpdateRiskExclusionReceiptResult struct {
	Project        RiskMutationReceiptProject  `json:"project"`
	Exclusion      RiskExclusionReceiptSummary `json:"exclusion"`
	Version        string                      `json:"version"`
	ResultCategory string                      `json:"result_category"`
	Reconciliation string                      `json:"reconciliation"`
}

func (UpdateRiskExclusionReceiptResult) riskMutationReceiptOperation() string {
	return operationUpdateRiskExclusion
}

// RiskMutationTransaction performs the domain write and audit write using the
// same transaction that owns the receipt. Its result must be one of the closed
// operation-specific projections above.
type RiskMutationTransaction func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error)

type RiskMutationReceiptStore struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewRiskMutationReceiptStore(db *pgxpool.Pool) *RiskMutationReceiptStore {
	return &RiskMutationReceiptStore{db: db, now: time.Now}
}

// Execute serializes one user's exact operation/project/key, replays an exact
// completed input from its stored result, and commits receipt + domain + audit
// atomically. Any callback, completion, or commit failure rolls all three back.
func (s *RiskMutationReceiptStore) Execute(ctx context.Context, principal Principal, project ResolvedProject, request RiskMutationReceiptRequest, mutate RiskMutationTransaction) (OperationReceipt, error) {
	if s == nil || s.db == nil || s.now == nil || mutate == nil || principal.OrganizationID == "" || principal.UserID == "" || project.ID == uuid.Nil || project.Slug == "" || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 || !riskMutationOperation(request.Operation) {
		return OperationReceipt{}, &RiskMutationError{Code: "invalid_request", Message: "The risk mutation request is invalid.", Cause: ErrRiskMutationInvalid}
	}
	inputHash, err := riskMutationInputHash(request.Operation, request.Input)
	if err != nil {
		return OperationReceipt{}, &RiskMutationError{Code: "invalid_request", Message: "The risk mutation request could not be normalized.", Cause: ErrRiskMutationInvalid}
	}
	return executeMutationReceipt(ctx, mutationReceiptExecution[RiskMutationReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: request.Operation, IdempotencyKey: request.IdempotencyKey, InputHash: inputHash, Label: "risk mutation",
		Invalid: func(cause error) error {
			return &RiskMutationError{Code: "invalid_request", Message: "The risk mutation caller identity is invalid.", Cause: fmt.Errorf("%w: %w", ErrRiskMutationInvalid, cause)}
		},
		Conflict: riskMutationConflict,
		Unavailable: func(cause error) error {
			return &RiskMutationError{Code: unavailableCode, Message: "The risk mutation is temporarily unavailable.", Cause: fmt.Errorf("%w: %w", ErrRiskMutationUnavailable, cause)}
		},
		ValidateReplay: func(payload []byte) bool {
			var result any
			return len(payload) <= maxRiskMutationReceiptPayloadBytes && json.Unmarshal(payload, &result) == nil
		},
		EncodeResult: func(result RiskMutationReceiptResult) ([]byte, error) {
			return encodeRiskMutationResult(request.Operation, result)
		},
		Mutate: func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error) {
			result, err := mutate(ctx, tx)
			if err != nil {
				return nil, fmt.Errorf("apply risk mutation transaction: %w", err)
			}
			return result, nil
		},
	})
}

func riskMutationInputHash(operation string, normalized any) (string, error) {
	if !riskMutationOperation(operation) || normalized == nil {
		return "", ErrRiskMutationInvalid
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode normalized risk mutation input: %w", err)
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-risk-mutation-v1\x00"+operation+"\x00"), payload...))
	return hex.EncodeToString(digest[:]), nil
}

func riskMutationOperation(operation string) bool {
	switch operation {
	case operationCreateRiskPolicy, operationUpdateRiskPolicy, operationCreateRiskExclusion, operationUpdateRiskExclusion:
		return true
	default:
		return false
	}
}

func encodeRiskMutationResult(operation string, result RiskMutationReceiptResult) (json.RawMessage, error) {
	normalized, valid := normalizedRiskMutationReceiptResult(result)
	if !valid || normalized.riskMutationReceiptOperation() != operation || !validRiskMutationReceiptResult(normalized) {
		return nil, invalidRiskMutationReceiptResult()
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode risk mutation receipt result: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxRiskMutationReceiptPayloadBytes {
		return nil, &RiskMutationError{Code: unavailableCode, Message: "The risk mutation result could not be stored safely.", Cause: ErrRiskMutationUnavailable}
	}
	return payload, nil
}

func normalizedRiskMutationReceiptResult(result RiskMutationReceiptResult) (RiskMutationReceiptResult, bool) {
	switch typed := result.(type) {
	case CreateRiskPolicyReceiptResult, UpdateRiskPolicyReceiptResult, CreateRiskExclusionReceiptResult, UpdateRiskExclusionReceiptResult:
		return typed, true
	case *CreateRiskPolicyReceiptResult:
		if typed != nil {
			return *typed, true
		}
	case *UpdateRiskPolicyReceiptResult:
		if typed != nil {
			return *typed, true
		}
	case *CreateRiskExclusionReceiptResult:
		if typed != nil {
			return *typed, true
		}
	case *UpdateRiskExclusionReceiptResult:
		if typed != nil {
			return *typed, true
		}
	}
	return nil, false
}

func validRiskMutationReceiptResult(result RiskMutationReceiptResult) bool {
	switch typed := result.(type) {
	case CreateRiskPolicyReceiptResult:
		return validRiskReceiptProject(typed.Project) && validRiskPolicyReceiptSummary(typed.Policy) && validRiskReceiptVersion(typed.Version) && validRiskResultCategory(typed.ResultCategory, "created", "matched_existing")
	case UpdateRiskPolicyReceiptResult:
		return validRiskReceiptProject(typed.Project) && validRiskPolicyReceiptSummary(typed.Policy) && validRiskReceiptVersion(typed.Version) && validRiskResultCategory(typed.ResultCategory, "updated")
	case CreateRiskExclusionReceiptResult:
		return validRiskReceiptProject(typed.Project) && validRiskExclusionReceiptSummary(typed.Exclusion) && validRiskReceiptVersion(typed.Version) && validRiskResultCategory(typed.ResultCategory, "created", "matched_existing") && typed.Reconciliation == "scheduled"
	case UpdateRiskExclusionReceiptResult:
		return validRiskReceiptProject(typed.Project) && validRiskExclusionReceiptSummary(typed.Exclusion) && validRiskReceiptVersion(typed.Version) && validRiskResultCategory(typed.ResultCategory, "updated") && typed.Reconciliation == "scheduled"
	default:
		return false
	}
}

func validRiskReceiptProject(project RiskMutationReceiptProject) bool {
	return uuid.Validate(project.ID) == nil && validRiskReceiptSlug(project.Slug)
}

func validRiskReceiptSlug(slug string) bool {
	if slug == "" || len(slug) > 128 {
		return false
	}
	for _, char := range slug {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func validRiskPolicyReceiptSummary(policy RiskPolicyReceiptSummary) bool {
	return uuid.Validate(policy.ID) == nil && (policy.PolicyType == "standard" || policy.PolicyType == "prompt_based") && (policy.Action == "" || policy.Action == "flag" || policy.Action == "warn" || policy.Action == "block" || policy.Action == "quarantine")
}

func validRiskExclusionReceiptSummary(exclusion RiskExclusionReceiptSummary) bool {
	if uuid.Validate(exclusion.ID) != nil || (exclusion.MatchType != "exact" && exclusion.MatchType != "rule_id" && exclusion.MatchType != "source" && exclusion.MatchType != "entity_type" && exclusion.MatchType != "regex") {
		return false
	}
	return exclusion.PolicyID == nil || uuid.Validate(*exclusion.PolicyID) == nil
}

func validRiskReceiptVersion(version string) bool {
	if version == "" || len(version) > 2048 {
		return false
	}
	for _, char := range version {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func validRiskResultCategory(category string, allowed ...string) bool {
	return slices.Contains(allowed, category)
}

func invalidRiskMutationReceiptResult() error {
	return &RiskMutationError{Code: unavailableCode, Message: "The risk mutation result could not be stored safely.", Cause: ErrRiskMutationUnavailable}
}
