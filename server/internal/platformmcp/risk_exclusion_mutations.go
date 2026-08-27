package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const maxRiskExclusionMatchRunes = 256

type riskExclusionMutationService struct {
	controls   *RiskMutationControls
	exclusions *exclusioncore.Core
	catalog    policycatalog.Catalog
}

type createRiskExclusionInput struct {
	ProjectSlug    string `json:"project_slug"`
	PolicyID       string `json:"policy_id,omitempty"`
	MatchType      string `json:"match_type"`
	MatchValue     string `json:"match_value"`
	Enabled        bool   `json:"enabled"`
	RuleIDFilter   string `json:"rule_id_filter,omitempty"`
	SourceFilter   string `json:"source_filter,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type updateRiskExclusionInput struct {
	ProjectSlug     string `json:"project_slug"`
	ExclusionID     string `json:"exclusion_id"`
	Enabled         bool   `json:"enabled"`
	ExpectedVersion string `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type normalizedCreateRiskExclusion struct {
	ProjectSlug  string  `json:"project_slug"`
	PolicyID     *string `json:"policy_id,omitempty"`
	MatchType    string  `json:"match_type"`
	MatchValue   string  `json:"match_value"`
	Enabled      bool    `json:"enabled"`
	RuleIDFilter string  `json:"rule_id_filter"`
	SourceFilter string  `json:"source_filter"`
}

type preparedRiskExclusionCreate struct {
	normalized normalizedCreateRiskExclusion
	params     riskrepo.CreateRiskExclusionParams
}

func newRiskExclusionMutationService(controls *RiskMutationControls, exclusions *exclusioncore.Core, catalog policycatalog.Catalog) *riskExclusionMutationService {
	return &riskExclusionMutationService{controls: controls, exclusions: exclusions, catalog: catalog}
}

func (s *riskExclusionMutationService) createExclusionTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, CreateRiskExclusionToolOutput, error) {
	var zero CreateRiskExclusionToolOutput
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	var input createRiskExclusionInput
	if err := decodeRiskMutationInput(raw, &input); err != nil {
		return riskMutationToolRefusal[CreateRiskExclusionToolOutput](err)
	}
	project, err := s.controls.Admit(ctx, principal, strings.TrimSpace(input.ProjectSlug))
	if err != nil {
		return riskMutationToolRefusal[CreateRiskExclusionToolOutput](err)
	}
	prepared, err := s.prepareCreate(principal, project, input)
	if err != nil {
		return riskMutationToolRefusal[CreateRiskExclusionToolOutput](err)
	}

	var reconcile *exclusioncore.Exclusion
	receipt, err := s.controls.Receipts().Execute(ctx, principal, project, RiskMutationReceiptRequest{
		Operation: operationCreateRiskExclusion, IdempotencyKey: input.IdempotencyKey, Input: prepared.normalized,
	}, func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error) {
		queries := riskrepo.New(tx)
		if err := queries.LockRiskExclusionMutations(ctx, project.ID.String()); err != nil {
			return nil, fmt.Errorf("lock risk exclusion create convergence: %w", err)
		}
		matched, err := matchExistingRiskExclusion(ctx, queries, prepared.params)
		if err != nil {
			return nil, err
		}
		if matched != nil {
			reconcile = matched
			return s.createReceiptResult(project, *matched, true)
		}
		created, err := s.exclusions.CreateInTransaction(ctx, tx, exclusioncore.CreateMutation{
			Params: prepared.params,
			Actor:  riskExclusionActor(principal),
		})
		if err != nil {
			return nil, mapRiskExclusionMutationError(err)
		}
		reconcile = &created
		return s.createReceiptResult(project, created, false)
	})
	if err != nil {
		return riskMutationToolRefusal[CreateRiskExclusionToolOutput](err)
	}
	if reconcile != nil {
		s.exclusions.AfterCommit(ctx, *reconcile)
	}
	var result CreateRiskExclusionReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return nil, zero, fmt.Errorf("decode create risk exclusion receipt result: %w", err)
	}
	return nil, CreateRiskExclusionToolOutput{CreateRiskExclusionReceiptResult: result, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func (s *riskExclusionMutationService) updateExclusionTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UpdateRiskExclusionToolOutput, error) {
	var zero UpdateRiskExclusionToolOutput
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	var input updateRiskExclusionInput
	if err := decodeRiskMutationInput(raw, &input); err != nil {
		return riskMutationToolRefusal[UpdateRiskExclusionToolOutput](err)
	}
	project, err := s.controls.Admit(ctx, principal, strings.TrimSpace(input.ProjectSlug))
	if err != nil {
		return riskMutationToolRefusal[UpdateRiskExclusionToolOutput](err)
	}
	exclusionID, err := uuid.Parse(input.ExclusionID)
	if err != nil || input.ExpectedVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return riskMutationToolRefusal[UpdateRiskExclusionToolOutput](invalidRiskExclusionRequest())
	}

	var committed *exclusioncore.Exclusion
	receipt, err := s.controls.Receipts().Execute(ctx, principal, project, RiskMutationReceiptRequest{
		Operation: operationUpdateRiskExclusion, IdempotencyKey: input.IdempotencyKey,
		Input: map[string]any{"project_slug": project.Slug, "exclusion_id": exclusionID.String(), "enabled": input.Enabled, "expected_version": input.ExpectedVersion},
	}, func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error) {
		updated, err := s.exclusions.ToggleInTransaction(ctx, tx, exclusioncore.ToggleMutation{
			ID: exclusionID, ProjectID: project.ID, Enabled: input.Enabled, Actor: riskExclusionActor(principal),
		}, func(locked exclusioncore.Exclusion) error {
			if !s.controls.Versions().ValidExclusionVersion(locked, input.ExpectedVersion) {
				return riskMutationConflict("The risk exclusion changed after it was read. Read it again and retry with the new version.")
			}
			return nil
		})
		if err != nil {
			return nil, mapRiskExclusionMutationError(err)
		}
		committed = &updated
		return s.updateReceiptResult(project, updated)
	})
	if err != nil {
		return riskMutationToolRefusal[UpdateRiskExclusionToolOutput](err)
	}
	if committed != nil {
		s.exclusions.AfterCommit(ctx, *committed)
	}
	var result UpdateRiskExclusionReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return nil, zero, fmt.Errorf("decode update risk exclusion receipt result: %w", err)
	}
	return nil, UpdateRiskExclusionToolOutput{UpdateRiskExclusionReceiptResult: result, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func (s *riskExclusionMutationService) prepareCreate(principal Principal, project ResolvedProject, input createRiskExclusionInput) (preparedRiskExclusionCreate, error) {
	input.ProjectSlug = strings.TrimSpace(input.ProjectSlug)
	input.PolicyID = strings.TrimSpace(input.PolicyID)
	input.MatchType = strings.TrimSpace(input.MatchType)
	input.RuleIDFilter = strings.TrimSpace(input.RuleIDFilter)
	input.SourceFilter = strings.TrimSpace(input.SourceFilter)
	if input.ProjectSlug != project.Slug || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || !utf8.ValidString(input.MatchValue) || utf8.RuneCountInString(input.MatchValue) > maxRiskExclusionMatchRunes {
		return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
	}
	if input.MatchType != "exact" {
		input.MatchValue = strings.TrimSpace(input.MatchValue)
	}
	if input.MatchValue == "" || input.MatchType == "regex" {
		return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
	}

	policyID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	var normalizedPolicyID *string
	if input.PolicyID != "" {
		parsed, err := uuid.Parse(input.PolicyID)
		if err != nil {
			return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
		}
		policyID = uuid.NullUUID{UUID: parsed, Valid: true}
		value := parsed.String()
		normalizedPolicyID = &value
	}

	switch input.MatchType {
	case "exact":
		if (input.RuleIDFilter != "" && !slices.Contains(s.catalog.DisabledRules, input.RuleIDFilter)) || (input.SourceFilter != "" && !slices.Contains(s.catalog.Sources, input.SourceFilter)) {
			return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
		}
	case "rule_id":
		if !slices.Contains(s.catalog.DisabledRules, input.MatchValue) || input.RuleIDFilter != "" || (input.SourceFilter != "" && !slices.Contains(s.catalog.Sources, input.SourceFilter)) {
			return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
		}
	case "source":
		if !slices.Contains(s.catalog.Sources, input.MatchValue) || input.SourceFilter != "" || (input.RuleIDFilter != "" && !slices.Contains(s.catalog.DisabledRules, input.RuleIDFilter)) {
			return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
		}
	case "entity_type":
		if !slices.Contains(s.catalog.PresidioEntities, input.MatchValue) || input.RuleIDFilter != "" || (input.SourceFilter != "" && input.SourceFilter != policycatalog.PresidioSource) {
			return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
		}
		input.SourceFilter = policycatalog.PresidioSource
	default:
		return preparedRiskExclusionCreate{}, invalidRiskExclusionRequest()
	}

	normalized := normalizedCreateRiskExclusion{
		ProjectSlug: project.Slug, PolicyID: normalizedPolicyID, MatchType: input.MatchType, MatchValue: input.MatchValue,
		Enabled: input.Enabled, RuleIDFilter: input.RuleIDFilter, SourceFilter: input.SourceFilter,
	}
	return preparedRiskExclusionCreate{
		normalized: normalized,
		params: riskrepo.CreateRiskExclusionParams{
			ProjectID: project.ID, OrganizationID: principal.OrganizationID, RiskPolicyID: policyID,
			MatchType: input.MatchType, MatchValue: input.MatchValue,
			RuleIDFilter: nullableRiskExclusionText(input.RuleIDFilter), SourceFilter: nullableRiskExclusionText(input.SourceFilter), Enabled: input.Enabled,
		},
	}, nil
}

func matchExistingRiskExclusion(ctx context.Context, queries *riskrepo.Queries, params riskrepo.CreateRiskExclusionParams) (*exclusioncore.Exclusion, error) {
	rows, err := queries.ListRiskExclusionsByProject(ctx, riskrepo.ListRiskExclusionsByProjectParams{ProjectID: params.ProjectID, RiskPolicyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	if err != nil {
		return nil, fmt.Errorf("list risk exclusion create convergence candidates: %w", err)
	}
	var matched *exclusioncore.Exclusion
	for _, row := range rows {
		if row.RiskPolicyID != params.RiskPolicyID || row.MatchType != params.MatchType || row.MatchValue != params.MatchValue || row.RuleIDFilter != params.RuleIDFilter || row.SourceFilter != params.SourceFilter || row.Enabled != params.Enabled {
			continue
		}
		if matched != nil {
			return nil, riskMutationConflict("Multiple existing risk exclusions match this request. Resolve the duplicates and retry.")
		}
		projected := exclusioncore.Project(row)
		matched = &projected
	}
	return matched, nil
}

func (s *riskExclusionMutationService) createReceiptResult(project ResolvedProject, exclusion exclusioncore.Exclusion, matched bool) (CreateRiskExclusionReceiptResult, error) {
	version, err := s.controls.Versions().ExclusionVersion(exclusion)
	if err != nil {
		return CreateRiskExclusionReceiptResult{}, riskMutationUnavailableWithCause(err)
	}
	category := "created"
	if matched {
		category = "matched_existing"
	}
	return CreateRiskExclusionReceiptResult{
		Project: riskReceiptProject(project), Exclusion: riskExclusionReceiptSummary(exclusion), Version: version,
		MatchedExisting: matched, ResultCategory: category, Reconciliation: "scheduled",
	}, nil
}

func (s *riskExclusionMutationService) updateReceiptResult(project ResolvedProject, exclusion exclusioncore.Exclusion) (UpdateRiskExclusionReceiptResult, error) {
	version, err := s.controls.Versions().ExclusionVersion(exclusion)
	if err != nil {
		return UpdateRiskExclusionReceiptResult{}, riskMutationUnavailableWithCause(err)
	}
	return UpdateRiskExclusionReceiptResult{
		Project: riskReceiptProject(project), Exclusion: riskExclusionReceiptSummary(exclusion), Version: version,
		ResultCategory: "updated", Reconciliation: "scheduled",
	}, nil
}

func riskExclusionReceiptSummary(exclusion exclusioncore.Exclusion) RiskExclusionReceiptSummary {
	var policyID *string
	if exclusion.RiskPolicyID.Valid {
		value := exclusion.RiskPolicyID.UUID.String()
		policyID = &value
	}
	return RiskExclusionReceiptSummary{ID: exclusion.ID.String(), PolicyID: policyID, MatchType: exclusion.MatchType, Enabled: exclusion.Enabled}
}

func riskExclusionActor(principal Principal) exclusioncore.Actor {
	return exclusioncore.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil, Slug: nil}
}

func nullableRiskExclusionText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func mapRiskExclusionMutationError(err error) error {
	var mutation *RiskMutationError
	var validation *exclusioncore.ValidationError
	switch {
	case errors.As(err, &mutation):
		return mutation
	case errors.As(err, &validation):
		return invalidRiskExclusionRequest()
	case errors.Is(err, exclusioncore.ErrPolicyNotFound), errors.Is(err, exclusioncore.ErrExclusionNotFound), errors.Is(err, pgx.ErrNoRows):
		return &RiskMutationError{Code: "not_found", Message: "The requested risk exclusion or policy was not found.", Cause: fmt.Errorf("%w: %w", ErrRiskMutationNotFound, err)}
	}
	var coreMutation *exclusioncore.MutationError
	if errors.As(err, &coreMutation) {
		return riskMutationUnavailableWithCause(err)
	}
	return riskMutationUnavailableWithCause(err)
}

func invalidRiskExclusionRequest() error {
	return &RiskMutationError{Code: "invalid_request", Message: "The risk exclusion mutation input is invalid.", Cause: ErrRiskMutationInvalid}
}
