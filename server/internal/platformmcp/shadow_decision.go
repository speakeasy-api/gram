package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	operationDecideShadowMCPAccess = "decide_shadow_mcp_access"
	maxShadowDecisionRationale     = 4000
	maxShadowDecisionAudiences     = 100
)

var (
	ErrShadowDecisionInvalid     = errors.New("invalid platform mcp shadow access decision")
	ErrShadowDecisionNotFound    = errors.New("platform mcp shadow access decision target not found")
	ErrShadowDecisionUnavailable = errors.New("platform mcp shadow access decisions unavailable")
)

type DecideShadowMCPAccessInput struct {
	ProjectID          string   `json:"project_id" jsonschema:"explicit project ID used to inspect the Shadow MCP target"`
	TargetReference    string   `json:"target_reference" jsonschema:"short-lived opaque target reference returned by list_shadow_mcp_inventory or get_shadow_mcp_review"`
	Decision           string   `json:"decision" jsonschema:"decision to enforce: allow or deny"`
	Rationale          string   `json:"rationale" jsonschema:"bounded reason for the decision; required and stored in the audit trail"`
	AudienceReferences []string `json:"audience_references,omitempty" jsonschema:"for allow, one or more opaque role/directory references returned by list_plugin_assignments; use the explicit Everyone reference for organization-wide access; forbidden for deny"`
	ExpectedVersion    string   `json:"expected_version" jsonschema:"opaque decision version returned by get_shadow_mcp_review immediately before this write"`
	IdempotencyKey     string   `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact decision"`
	Confirmed          bool     `json:"confirmed" jsonschema:"set true only after the user confirms the exact project, target, decision, audiences, and rationale"`
}

type ShadowDecisionAudience struct {
	Kind        string        `json:"kind"`
	DisplayName string        `json:"display_name"`
	MemberCount *SubjectCount `json:"member_count,omitempty"`
}

type ShadowDecisionReceiptResult struct {
	Decision       string                   `json:"decision"`
	Audiences      []ShadowDecisionAudience `json:"audiences"`
	ResultCategory string                   `json:"result_category"`
}

type DecideShadowMCPAccessOutput struct {
	Decision    string                    `json:"decision"`
	Audiences   []ShadowDecisionAudience  `json:"audiences"`
	State       *GetShadowMCPReviewOutput `json:"state,omitempty"`
	StateStatus string                    `json:"state_status"`
	Receipt     RiskMutationToolReceipt   `json:"receipt"`
}

type normalizedShadowDecision struct {
	ProjectID          string   `json:"project_id"`
	TargetReference    string   `json:"target_reference"`
	Decision           string   `json:"decision"`
	Rationale          string   `json:"rationale"`
	AudienceReferences []string `json:"audience_references"`
	ExpectedVersion    string   `json:"expected_version"`
}

type shadowDecisionAudienceResolver interface {
	ResolveAssignmentReferences(context.Context, pgx.Tx, Principal, ResolvedProject, []string) ([]string, []PluginAssignmentSummaryResult, error)
}

type ShadowDecisionService struct {
	db            *pgxpool.Pool
	shadow        *ShadowInventoryService
	core          *mcpapproval.Service
	audiences     shadowDecisionAudienceResolver
	flags         feature.Provider
	organizations OrganizationSlugResolver
	budget        OperationBudget
	receipts      *ShadowDecisionReceiptStore
}

func NewShadowDecisionService(db *pgxpool.Pool, shadow *ShadowInventoryService, core *mcpapproval.Service, audiences shadowDecisionAudienceResolver, flags feature.Provider, organizations OrganizationSlugResolver, budget OperationBudget) *ShadowDecisionService {
	return &ShadowDecisionService{db: db, shadow: shadow, core: core, audiences: audiences, flags: flags, organizations: organizations, budget: budget, receipts: NewShadowDecisionReceiptStore(db)}
}

func (s *ShadowDecisionService) valid() bool {
	return s != nil && s.db != nil && s.shadow != nil && s.shadow.valid() && s.core != nil && s.audiences != nil && s.flags != nil && s.organizations != nil && s.budget.valid() && s.receipts != nil
}

func (s *ShadowDecisionService) Decide(ctx context.Context, principal Principal, input DecideShadowMCPAccessInput) (DecideShadowMCPAccessOutput, error) {
	if !input.Confirmed {
		return DecideShadowMCPAccessOutput{}, shadowDecisionError("confirmation_required", "Confirm the exact Shadow MCP decision and audiences before applying it.", ErrShadowDecisionInvalid)
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.TargetReference = strings.TrimSpace(input.TargetReference)
	input.Decision = strings.TrimSpace(input.Decision)
	input.Rationale = strings.TrimSpace(input.Rationale)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || input.TargetReference == "" || input.Rationale == "" || len([]rune(input.Rationale)) > maxShadowDecisionRationale || input.ExpectedVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || len(input.AudienceReferences) > maxShadowDecisionAudiences {
		return DecideShadowMCPAccessOutput{}, shadowDecisionInvalid("The Shadow MCP decision request is invalid.")
	}
	if input.Decision != "allow" && input.Decision != "deny" {
		return DecideShadowMCPAccessOutput{}, shadowDecisionInvalid("Decision must be allow or deny.")
	}
	references, err := normalizeShadowAudienceReferences(input.AudienceReferences)
	if err != nil {
		return DecideShadowMCPAccessOutput{}, err
	}
	if input.Decision == "allow" && len(references) == 0 {
		return DecideShadowMCPAccessOutput{}, shadowDecisionInvalid("Allow requires at least one explicit audience reference. Use the Everyone reference for organization-wide access.")
	}
	if input.Decision == "deny" && len(references) != 0 {
		return DecideShadowMCPAccessOutput{}, shadowDecisionInvalid("Deny does not accept audience references.")
	}
	if !s.valid() || principal.OrganizationID == "" || principal.UserID == "" {
		return DecideShadowMCPAccessOutput{}, shadowDecisionUnavailable(nil)
	}
	project, err := s.shadow.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, "")
	if err != nil {
		return DecideShadowMCPAccessOutput{}, mapShadowDecisionProjectError(err)
	}
	if err := s.admit(ctx, principal, project); err != nil {
		return DecideShadowMCPAccessOutput{}, err
	}
	normalized := normalizedShadowDecision{ProjectID: project.ID.String(), TargetReference: input.TargetReference, Decision: input.Decision, Rationale: input.Rationale, AudienceReferences: references, ExpectedVersion: input.ExpectedVersion}
	receipt, err := s.receipts.Execute(ctx, principal, project, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (ShadowDecisionReceiptResult, error) {
		targetKind, targetKey, err := s.shadow.ResolveTargetReference(principal, project.ID.String(), input.TargetReference)
		if err != nil {
			return ShadowDecisionReceiptResult{}, shadowDecisionNotFound()
		}
		if targetKind == shadowTargetKindStdioCommand {
			return ShadowDecisionReceiptResult{}, shadowDecisionInvalid("Shadow MCP local-command targets cannot be enforced by this tool.")
		}
		requestID, err := s.core.ResolveDecisionTarget(ctx, principal.OrganizationID, project.ID, targetKind, targetKey)
		if err != nil {
			return ShadowDecisionReceiptResult{}, mapShadowDecisionCoreError(err)
		}
		principalURNs := []string{}
		summaries := []PluginAssignmentSummaryResult{}
		if input.Decision == "allow" {
			principalURNs, summaries, err = s.audiences.ResolveAssignmentReferences(ctx, tx, principal, project, references)
			if err != nil {
				return ShadowDecisionReceiptResult{}, mapShadowAudienceError(err)
			}
		}
		decision := "denied"
		if input.Decision == "allow" {
			decision = "approved"
		}
		_, err = s.core.DecideInTransaction(ctx, tx, mcpapproval.DecisionCommandInput{
			OrganizationID: principal.OrganizationID, ProjectID: project.ID, RequestID: requestID,
			Decision: decision, Rationale: input.Rationale, GrantedPrincipalURNs: principalURNs,
			ResearchReportID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ActorUserID: principal.UserID, ActorEmail: nil,
			ValidateLocked: func(state mcpapproval.DecisionVersionState) error {
				if !s.shadow.versions.Match(input.ExpectedVersion, state) {
					return shadowDecisionError("conflict", "The Shadow MCP review changed after it was read. Read it again and retry with the new version.", ErrShadowDecisionConflict)
				}
				return nil
			},
		})
		if err != nil {
			return ShadowDecisionReceiptResult{}, mapShadowDecisionCoreError(err)
		}
		return ShadowDecisionReceiptResult{Decision: input.Decision, Audiences: shadowDecisionAudiences(summaries), ResultCategory: "decided"}, nil
	})
	if err != nil {
		return DecideShadowMCPAccessOutput{}, err
	}
	var stored ShadowDecisionReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil || !validShadowDecisionReceipt(stored) {
		return DecideShadowMCPAccessOutput{}, shadowDecisionUnavailable(err)
	}
	output := DecideShadowMCPAccessOutput{Decision: stored.Decision, Audiences: stored.Audiences, State: nil, StateStatus: "pending", Receipt: riskMutationToolReceipt(receipt)}
	state, stateErr := s.shadow.GetReview(ctx, principal, GetShadowMCPReviewInput{ProjectID: project.ID.String(), TargetReference: input.TargetReference})
	if stateErr == nil {
		output.State = &state
		output.StateStatus = "complete"
	}
	return output, nil
}

func (s *ShadowDecisionService) admit(ctx context.Context, principal Principal, project ResolvedProject) error {
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return shadowDecisionUnavailable(err)
	}
	groups := feature.OrgProjectGroups(organizationSlug, project.Slug)
	base, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagMCPApproval, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil || base != feature.EvaluationEnabled {
		return shadowDecisionUnavailable(err)
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagPlatformMCPShadowAccessDecisions, principal.OrganizationID, groups)
	if err != nil || evaluation != feature.EvaluationEnabled {
		return shadowDecisionUnavailable(err)
	}
	if err := s.budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return shadowDecisionError("rate_limited", "The Shadow MCP decision rate limit was reached.", err)
		}
		return shadowDecisionUnavailable(err)
	}
	return nil
}

func shadowDecisionAudiences(input []PluginAssignmentSummaryResult) []ShadowDecisionAudience {
	result := make([]ShadowDecisionAudience, 0, len(input))
	for _, item := range input {
		result = append(result, ShadowDecisionAudience(item))
	}
	return result
}

func normalizeShadowAudienceReferences(input []string) ([]string, error) {
	result := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, shadowDecisionInvalid("Audience references must not be blank.")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result, nil
}

func validShadowDecisionReceipt(result ShadowDecisionReceiptResult) bool {
	return (result.Decision == "allow" || result.Decision == "deny") && result.ResultCategory == "decided" && result.Audiences != nil
}

func mapShadowDecisionProjectError(err error) error {
	if errors.Is(err, ErrRiskReadNotFound) {
		return shadowDecisionNotFound()
	}
	if errors.Is(err, ErrRiskReadInvalid) {
		return shadowDecisionInvalid("The project is invalid.")
	}
	return shadowDecisionUnavailable(err)
}

func mapShadowAudienceError(err error) error {
	if errors.Is(err, ErrPluginAssignmentMutationNotFound) || errors.Is(err, ErrSubjectReferenceNotFound) {
		return shadowDecisionNotFound()
	}
	if errors.Is(err, ErrPluginAssignmentMutationInvalid) {
		return shadowDecisionInvalid("One or more selected audiences are no longer valid.")
	}
	return shadowDecisionUnavailable(err)
}

func mapShadowDecisionCoreError(err error) error {
	if _, ok := errors.AsType[*ShadowDecisionError](err); ok {
		return err
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		if shareable.Code == oops.CodeBadRequest || shareable.Code == oops.CodeInvalid {
			return shadowDecisionInvalid("The Shadow MCP decision is no longer valid. Read the review again and retry.")
		}
		if shareable.Code == oops.CodeNotFound {
			return shadowDecisionNotFound()
		}
	}
	return shadowDecisionUnavailable(err)
}

type ShadowDecisionError struct {
	Code    string
	Message string
	Cause   error
}

func (e *ShadowDecisionError) Error() string { return e.Message }
func (e *ShadowDecisionError) Unwrap() error { return e.Cause }

func shadowDecisionError(code, message string, cause error) error {
	return &ShadowDecisionError{Code: code, Message: message, Cause: cause}
}
func shadowDecisionInvalid(message string) error {
	return shadowDecisionError("invalid_request", message, ErrShadowDecisionInvalid)
}
func shadowDecisionNotFound() error {
	return shadowDecisionError("not_found", "The Shadow MCP target or audience is not available to this organization.", ErrShadowDecisionNotFound)
}
func shadowDecisionUnavailable(cause error) error {
	if cause == nil {
		cause = ErrShadowDecisionUnavailable
	}
	return shadowDecisionError("feature_unavailable", "Shadow MCP access decisions are not enabled or are temporarily unavailable.", fmt.Errorf("%w: %w", ErrShadowDecisionUnavailable, cause))
}

// ShadowDecisionReceiptStore writes the operation receipt in the same
// transaction as the decision core.
type ShadowDecisionReceiptStore struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewShadowDecisionReceiptStore(db *pgxpool.Pool) *ShadowDecisionReceiptStore {
	return &ShadowDecisionReceiptStore{db: db, now: time.Now}
}

func (s *ShadowDecisionReceiptStore) Execute(ctx context.Context, principal Principal, project ResolvedProject, key string, normalized normalizedShadowDecision, mutate func(context.Context, pgx.Tx) (ShadowDecisionReceiptResult, error)) (OperationReceipt, error) {
	payload, err := json.Marshal(normalized)
	if err != nil {
		return OperationReceipt{}, shadowDecisionInvalid("The decision input could not be normalized.")
	}
	digest := uuid.NewSHA1(uuid.NameSpaceOID, append([]byte(operationDecideShadowMCPAccess), payload...)).String()
	return executeMutationReceipt(ctx, mutationReceiptExecution[ShadowDecisionReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationDecideShadowMCPAccess,
		IdempotencyKey: key, InputHash: digest, Label: "shadow access decision",
		Invalid:     func(error) error { return shadowDecisionInvalid("The decision caller identity is invalid.") },
		Conflict:    func(message string) error { return shadowDecisionError("conflict", message, ErrShadowDecisionConflict) },
		Unavailable: shadowDecisionUnavailable,
		ValidateReplay: func(payload []byte) bool {
			var result ShadowDecisionReceiptResult
			return json.Unmarshal(payload, &result) == nil && validShadowDecisionReceipt(result)
		},
		EncodeResult: func(result ShadowDecisionReceiptResult) ([]byte, error) {
			if !validShadowDecisionReceipt(result) {
				return nil, shadowDecisionUnavailable(errors.New("invalid shadow decision receipt result"))
			}
			return json.Marshal(result)
		},
		Mutate: mutate,
	})
}
