package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	defaultRiskPolicyAction            = "flag"
	defaultRiskPolicyScore             = 5.0
	defaultRiskPolicyPresidioThreshold = 0.5
	riskPolicyAudienceEveryone         = "everyone"
	maxRiskPolicyPromptRunes           = 4000
	maxRiskPolicyUserMessageRunes      = 500
	maxRiskPolicyApprovedEmailDomains  = 50
)

type riskPolicyMutationService struct {
	db       *pgxpool.Pool
	controls *RiskMutationControls
	policies *policycore.Core
	catalog  policycatalog.Catalog
}

type createRiskPolicyInput struct {
	ProjectSlug            string               `json:"project_slug"`
	PolicyType             string               `json:"policy_type"`
	Name                   string               `json:"name"`
	Enabled                bool                 `json:"enabled"`
	Action                 string               `json:"action,omitempty"`
	Score                  *float64             `json:"score,omitempty"`
	MessageTypes           []string             `json:"message_types,omitempty"`
	UserMessage            *string              `json:"user_message,omitempty"`
	Sources                []string             `json:"sources,omitempty"`
	PresidioEntities       []string             `json:"presidio_entities,omitempty"`
	PresidioScoreThreshold *float64             `json:"presidio_score_threshold,omitempty"`
	PromptInjectionRules   []string             `json:"prompt_injection_rules,omitempty"`
	DisabledRules          []string             `json:"disabled_rules,omitempty"`
	ApprovedEmailDomains   []string             `json:"approved_email_domains,omitempty"`
	DetectionScopes        []riskDetectionScope `json:"detection_scopes,omitempty"`
	Prompt                 string               `json:"prompt,omitempty"`
	IdempotencyKey         string               `json:"idempotency_key"`
}

type updateRiskPolicyInput struct {
	ProjectSlug     string                     `json:"project_slug"`
	PolicyID        string                     `json:"policy_id"`
	ExpectedVersion string                     `json:"expected_version"`
	IdempotencyKey  string                     `json:"idempotency_key"`
	Patch           map[string]json.RawMessage `json:"patch"`
}

type riskDetectionScope struct {
	Category     string   `json:"category"`
	MessageTypes []string `json:"message_types"`
}

type normalizedCreateRiskPolicy struct {
	ProjectSlug            string               `json:"project_slug"`
	PolicyType             string               `json:"policy_type"`
	Name                   string               `json:"name"`
	Enabled                bool                 `json:"enabled"`
	Action                 string               `json:"action"`
	Score                  float64              `json:"score"`
	MessageTypes           []string             `json:"message_types"`
	UserMessage            *string              `json:"user_message,omitempty"`
	Sources                []string             `json:"sources"`
	PresidioEntities       []string             `json:"presidio_entities"`
	PresidioScoreThreshold *float64             `json:"presidio_score_threshold,omitempty"`
	PromptInjectionRules   []string             `json:"prompt_injection_rules"`
	DisabledRules          []string             `json:"disabled_rules"`
	ApprovedEmailDomains   []string             `json:"approved_email_domains"`
	DetectionScopes        []riskDetectionScope `json:"detection_scopes"`
	PromptDigest           string               `json:"prompt_digest,omitempty"`
}

type preparedRiskPolicyCreate struct {
	normalized normalizedCreateRiskPolicy
	params     riskrepo.CreateRiskPolicyParams
}

// NewRiskPolicyMutationHandlers retains the policy-only composition used by the
// preceding rollout slice.
func NewRiskPolicyMutationHandlers(db *pgxpool.Pool, controls *RiskMutationControls, policies *policycore.Core) (*RiskMutationHandlers, error) {
	return newRiskMutationHandlers(db, controls, policies, nil)
}

// NewRiskMutationHandlers activates policy and exclusion mutation callbacks.
func NewRiskMutationHandlers(db *pgxpool.Pool, controls *RiskMutationControls, policies *policycore.Core, exclusions *exclusioncore.Core) (*RiskMutationHandlers, error) {
	return newRiskMutationHandlers(db, controls, policies, exclusions)
}

func newRiskMutationHandlers(db *pgxpool.Pool, controls *RiskMutationControls, policies *policycore.Core, exclusions *exclusioncore.Core) (*RiskMutationHandlers, error) {
	if db == nil || controls == nil || policies == nil {
		return nil, ErrRiskMutationUnavailable
	}
	catalog, err := policycatalog.Build()
	if err != nil {
		return nil, fmt.Errorf("build risk policy mutation catalog: %w", err)
	}
	policyService := &riskPolicyMutationService{db: db, controls: controls, policies: policies, catalog: catalog}
	handlers := &RiskMutationHandlers{
		Controls:        controls,
		CreatePolicy:    policyService.createPolicyTool,
		UpdatePolicy:    policyService.updatePolicyTool,
		CreateExclusion: nil,
		UpdateExclusion: nil,
	}
	if exclusions != nil {
		exclusionService := newRiskExclusionMutationService(controls, exclusions, catalog)
		handlers.CreateExclusion = exclusionService.createExclusionTool
		handlers.UpdateExclusion = exclusionService.updateExclusionTool
	}
	return handlers, nil
}

func (s *riskPolicyMutationService) createPolicyTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, CreateRiskPolicyToolOutput, error) {
	var zero CreateRiskPolicyToolOutput
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	var input createRiskPolicyInput
	if err := decodeRiskMutationInput(raw, &input); err != nil {
		return riskMutationToolRefusal[CreateRiskPolicyToolOutput](err)
	}
	project, err := s.controls.Admit(ctx, principal, strings.TrimSpace(input.ProjectSlug))
	if err != nil {
		return riskMutationToolRefusal[CreateRiskPolicyToolOutput](err)
	}
	prepared, err := s.prepareCreate(ctx, principal, project, input)
	if err != nil {
		return riskMutationToolRefusal[CreateRiskPolicyToolOutput](err)
	}

	var committed *policycore.MutationResult
	receipt, err := s.controls.Receipts().Execute(ctx, principal, project, RiskMutationReceiptRequest{
		Operation: operationCreateRiskPolicy, IdempotencyKey: input.IdempotencyKey, Input: prepared.normalized,
	}, func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error) {
		if err := riskrepo.New(tx).LockRiskPolicyMutations(ctx, project.ID.String()); err != nil {
			return nil, fmt.Errorf("lock risk policy create convergence: %w", err)
		}
		matched, err := s.matchExistingCreate(ctx, tx, principal.OrganizationID, project.ID, prepared)
		if err != nil {
			return nil, err
		}
		if matched != nil {
			result, err := s.createReceiptResult(ctx, tx, project, matched.Row, matched.AudiencePrincipalURNs, true)
			return result, err
		}
		result, err := s.policies.CreatePolicyInTransaction(ctx, tx, policycore.CreateMutation{
			Params:             prepared.params,
			AudiencePrincipals: []urn.Principal{authz.AllUsersPrincipal()},
			AllowedURLs:        nil,
			AllowedURLsSet:     false,
			BlockedURLs:        nil,
			BlockedURLsSet:     false,
			Actor: policycore.Actor{
				Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
				DisplayName: nil,
				Slug:        nil,
			},
		})
		if err != nil {
			return nil, mapRiskPolicyMutationError(err)
		}
		committed = &result
		return s.createReceiptResult(ctx, tx, project, result.Row, result.AudiencePrincipalURNs, false)
	})
	if err != nil {
		return riskMutationToolRefusal[CreateRiskPolicyToolOutput](err)
	}
	if committed != nil {
		s.policies.AfterCreatePolicy(ctx, *committed)
	}
	var result CreateRiskPolicyReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return nil, zero, fmt.Errorf("decode create risk policy receipt result: %w", err)
	}
	return nil, CreateRiskPolicyToolOutput{CreateRiskPolicyReceiptResult: result, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func (s *riskPolicyMutationService) updatePolicyTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
	var zero UpdateRiskPolicyToolOutput
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	var input updateRiskPolicyInput
	if err := decodeRiskMutationInput(raw, &input); err != nil {
		return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](err)
	}
	input.ProjectSlug = strings.TrimSpace(input.ProjectSlug)
	project, err := s.controls.Admit(ctx, principal, input.ProjectSlug)
	if err != nil {
		return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](err)
	}
	policyID, err := uuid.Parse(input.PolicyID)
	if err != nil || input.ExpectedVersion == "" || len(input.Patch) == 0 {
		return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](invalidRiskPolicyRequest())
	}
	normalizedPatch, err := normalizeRiskPolicyPatchForReceipt(input.Patch)
	if err != nil {
		return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](err)
	}

	var committed *policycore.MutationResult
	receipt, err := s.controls.Receipts().Execute(ctx, principal, project, RiskMutationReceiptRequest{
		Operation: operationUpdateRiskPolicy, IdempotencyKey: input.IdempotencyKey,
		Input: map[string]any{"project_slug": project.Slug, "policy_id": policyID.String(), "expected_version": input.ExpectedVersion, "patch": normalizedPatch},
	}, func(ctx context.Context, tx pgx.Tx) (RiskMutationReceiptResult, error) {
		current, err := riskrepo.New(tx).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: policyID, ProjectID: project.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, riskPolicyNotFound()
		}
		if err != nil {
			return nil, fmt.Errorf("load risk policy update target: %w", err)
		}
		mutation, _, err := s.prepareUpdate(ctx, principal, project, current, input)
		if err != nil {
			return nil, err
		}
		mutation.ValidateLocked = func(ctx context.Context, tx pgx.Tx, locked policycore.Policy) (policycore.Policy, error) {
			state, err := riskPolicyVersionState(ctx, tx, locked, true)
			if err != nil {
				return policycore.Policy{}, riskMutationUnavailableWithCause(err)
			}
			if !s.controls.Versions().ValidPolicyVersion(state, input.ExpectedVersion) {
				return policycore.Policy{}, riskMutationConflict("The risk policy changed after it was read. Read it again and retry with the new version.")
			}
			return state.Policy, nil
		}
		result, err := s.policies.UpdatePolicyInTransaction(ctx, tx, mutation)
		if err != nil {
			return nil, mapRiskPolicyMutationError(err)
		}
		committed = &result
		return s.updateReceiptResult(ctx, tx, project, result.Row, result.AudiencePrincipalURNs)
	})
	if err != nil {
		return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](err)
	}
	if committed != nil {
		s.policies.AfterUpdatePolicy(ctx, *committed)
	}
	var result UpdateRiskPolicyReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return nil, zero, fmt.Errorf("decode update risk policy receipt result: %w", err)
	}
	if receipt.Replayed && result.Policy.PolicyType == "prompt_based" {
		if err := s.requirePromptPolicies(ctx, principal, project); err != nil {
			return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](err)
		}
	}
	return nil, UpdateRiskPolicyToolOutput{UpdateRiskPolicyReceiptResult: result, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func normalizeRiskPolicyPatchForReceipt(patch map[string]json.RawMessage) (map[string]any, error) {
	if len(patch) == 0 {
		return nil, invalidRiskPolicyRequest()
	}
	normalized := make(map[string]any, len(patch))
	for field, raw := range patch {
		switch field {
		case "name", "action":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			normalized[field] = strings.TrimSpace(value)
		case "user_message":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			normalized[field] = value
		case "prompt":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
			normalized[field] = hex.EncodeToString(digest[:])
		case "enabled":
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			normalized[field] = value
		case "score", "presidio_score_threshold":
			var value float64
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			normalized[field] = value
		case "message_types", "sources", "presidio_entities", "prompt_injection_rules", "disabled_rules", "approved_email_domains":
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			if field == "approved_email_domains" {
				var err error
				value, err = policycore.NormalizeApprovedEmailDomains(value)
				if err != nil {
					return nil, invalidRiskPolicyRequest()
				}
			}
			normalized[field] = canonicalStrings(value)
		case "detection_scopes":
			var value []riskDetectionScope
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalidRiskPolicyRequest()
			}
			for index := range value {
				value[index].MessageTypes = canonicalStrings(value[index].MessageTypes)
			}
			slices.SortFunc(value, func(a, b riskDetectionScope) int { return strings.Compare(a.Category, b.Category) })
			normalized[field] = value
		default:
			return nil, invalidRiskPolicyRequest()
		}
	}
	return normalized, nil
}

func decodeRiskMutationInput(raw map[string]any, target any) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return invalidRiskPolicyRequest()
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidRiskPolicyRequest()
	}
	return nil
}

func (s *riskPolicyMutationService) prepareCreate(ctx context.Context, principal Principal, project ResolvedProject, input createRiskPolicyInput) (preparedRiskPolicyCreate, error) {
	input.ProjectSlug = strings.TrimSpace(input.ProjectSlug)
	input.Name = strings.TrimSpace(input.Name)
	input.PolicyType = strings.TrimSpace(input.PolicyType)
	if input.ProjectSlug != project.Slug || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || policycore.ValidateName(input.Name) != nil || !slices.Contains(s.catalog.PolicyTypes, input.PolicyType) {
		return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
	}
	action := input.Action
	if action == "" {
		action = defaultRiskPolicyAction
	}
	score := defaultRiskPolicyScore
	if input.Score != nil {
		score = *input.Score
	}
	if !slices.Contains(s.catalog.Actions, action) || score < 0.1 || score > 10 || !utf8.ValidString(input.Name) {
		return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
	}
	messageTypes := canonicalStrings(input.MessageTypes)
	if input.MessageTypes == nil {
		messageTypes = slices.Clone(s.catalog.PolicyMessageTypes)
	}
	if !allIn(messageTypes, s.catalog.PolicyMessageTypes) || len(messageTypes) == 0 || invalidUserMessage(input.UserMessage) {
		return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
	}

	normalized := normalizedCreateRiskPolicy{
		ProjectSlug: project.Slug, PolicyType: input.PolicyType, Name: input.Name, Enabled: input.Enabled,
		Action: action, Score: score, MessageTypes: messageTypes, UserMessage: input.UserMessage,
		Sources: []string{}, PresidioEntities: []string{}, PresidioScoreThreshold: nil, PromptInjectionRules: []string{}, DisabledRules: []string{}, ApprovedEmailDomains: []string{}, DetectionScopes: []riskDetectionScope{}, PromptDigest: "",
	}
	params := riskrepo.CreateRiskPolicyParams{
		ID: uuid.Nil, ProjectID: project.ID, OrganizationID: principal.OrganizationID, Name: input.Name,
		PolicyType: input.PolicyType, Sources: []string{}, PresidioEntities: []string{}, AnalyzerConfig: nil,
		PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{}, MessageTypes: messageTypes,
		ScopeInclude: pgtype.Text{String: "", Valid: false}, ScopeExempt: pgtype.Text{String: "", Valid: false}, Enabled: input.Enabled, Action: action,
		AudienceType: riskPolicyAudienceEveryone, ShadowMcpDisposition: pgtype.Text{String: "", Valid: false}, AutoName: false,
		UserMessage: conv.PtrToPGTextEmpty(input.UserMessage), Prompt: pgtype.Text{String: "", Valid: false}, ModelConfig: nil,
		Score: pgtype.Float8{Float64: score, Valid: true},
	}

	switch input.PolicyType {
	case "prompt_based":
		prompt := strings.TrimSpace(input.Prompt)
		if prompt == "" || utf8.RuneCountInString(prompt) > maxRiskPolicyPromptRunes || len(input.Sources)+len(input.PresidioEntities)+len(input.PromptInjectionRules)+len(input.DisabledRules)+len(input.ApprovedEmailDomains)+len(input.DetectionScopes) > 0 || input.PresidioScoreThreshold != nil {
			return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
		}
		if err := s.requirePromptPolicies(ctx, principal, project); err != nil {
			return preparedRiskPolicyCreate{}, err
		}
		digest := sha256.Sum256([]byte(prompt))
		normalized.PromptDigest = hex.EncodeToString(digest[:])
		params.Prompt = conv.ToPGText(prompt)
	case "standard":
		sources := canonicalStrings(input.Sources)
		entities := canonicalStrings(input.PresidioEntities)
		disabledRules := canonicalStrings(input.DisabledRules)
		promptRules := canonicalStrings(input.PromptInjectionRules)
		if len(sources) == 0 || !allIn(sources, s.catalog.Sources) || !allIn(entities, s.catalog.PresidioEntities) || !allIn(disabledRules, s.catalog.DisabledRules) || !allIn(promptRules, s.catalog.PromptInjectionRules) || policycore.ValidateSourceAction(sources, action) != nil {
			return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
		}
		hasPresidio := slices.Contains(sources, policycatalog.PresidioSource)
		if hasPresidio != (len(entities) > 0) {
			return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
		}
		domains, err := policycore.NormalizeApprovedEmailDomains(input.ApprovedEmailDomains)
		if err != nil || len(domains) > maxRiskPolicyApprovedEmailDomains {
			return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
		}
		slices.Sort(domains)
		threshold := defaultRiskPolicyPresidioThreshold
		if input.PresidioScoreThreshold != nil {
			threshold = *input.PresidioScoreThreshold
		}
		if threshold < 0 || threshold > 1 || (!hasPresidio && input.PresidioScoreThreshold != nil) {
			return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
		}
		scopes, analyzer, err := s.buildDetectionScopes(input.DetectionScopes, sources, entities, nil)
		if err != nil {
			return preparedRiskPolicyCreate{}, err
		}
		analyzer, err = ra.WithPresidioScoreThreshold(analyzer, &threshold)
		if err == nil {
			analyzer, err = ra.WithApprovedEmailDomains(analyzer, domains)
		}
		if err != nil {
			return preparedRiskPolicyCreate{}, riskMutationUnavailableWithCause(err)
		}
		normalized.Sources, normalized.PresidioEntities = sources, entities
		normalized.PresidioScoreThreshold = &threshold
		normalized.PromptInjectionRules, normalized.DisabledRules = promptRules, disabledRules
		normalized.ApprovedEmailDomains, normalized.DetectionScopes = domains, scopes
		params.Sources, params.PresidioEntities, params.AnalyzerConfig = sources, entities, analyzer
		params.PromptInjectionRules, params.DisabledRules = promptRules, disabledRules
	default:
		return preparedRiskPolicyCreate{}, invalidRiskPolicyRequest()
	}
	id, err := uuid.NewV7()
	if err != nil {
		return preparedRiskPolicyCreate{}, riskMutationUnavailableWithCause(err)
	}
	params.ID = id
	return preparedRiskPolicyCreate{normalized: normalized, params: params}, nil
}

func (s *riskPolicyMutationService) prepareUpdate(ctx context.Context, principal Principal, project ResolvedProject, current riskrepo.RiskPolicy, input updateRiskPolicyInput) (policycore.UpdateMutation, map[string]any, error) {
	if strings.TrimSpace(input.ProjectSlug) != project.Slug || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
	}
	params := riskrepo.UpdateRiskPolicyParams{
		ID: current.ID, ProjectID: project.ID, Name: current.Name, Sources: slices.Clone(current.Sources), PresidioEntities: slices.Clone(current.PresidioEntities), AnalyzerConfig: slices.Clone(current.AnalyzerConfig),
		PromptInjectionRules: slices.Clone(current.PromptInjectionRules), DisabledRules: slices.Clone(current.DisabledRules), CustomRuleIds: slices.Clone(current.CustomRuleIds), MessageTypes: slices.Clone(current.MessageTypes),
		ScopeInclude: current.ScopeInclude, ScopeExempt: current.ScopeExempt, Enabled: current.Enabled, Action: current.Action, AudienceType: current.AudienceType, AutoName: current.AutoName,
		UserMessage: current.UserMessage, Prompt: current.Prompt, ModelConfig: slices.Clone(current.ModelConfig), Score: pgtype.Float8{Float64: current.Score, Valid: true},
	}
	normalized := make(map[string]any, len(input.Patch))
	changedSources, changedAction := false, false
	// Apply dependencies before fields that validate against them. Iterating the
	// caller's map directly would make a combined sources + detection_scopes patch
	// depend on randomized Go map order.
	for _, field := range []string{
		"sources", "presidio_entities", "action", "name", "enabled", "score", "prompt", "user_message",
		"message_types", "presidio_score_threshold", "approved_email_domains", "prompt_injection_rules", "disabled_rules", "detection_scopes",
	} {
		raw, ok := input.Patch[field]
		if !ok {
			continue
		}
		switch field {
		case "name":
			var value string
			if json.Unmarshal(raw, &value) != nil || policycore.ValidateName(strings.TrimSpace(value)) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.Name = strings.TrimSpace(value)
			normalized[field] = params.Name
		case "enabled":
			if json.Unmarshal(raw, &params.Enabled) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			normalized[field] = params.Enabled
		case "action":
			if json.Unmarshal(raw, &params.Action) != nil || !slices.Contains(s.catalog.Actions, params.Action) {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			normalized[field] = params.Action
			changedAction = true
		case "score":
			var value float64
			if json.Unmarshal(raw, &value) != nil || value < 0.1 || value > 10 {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.Score = pgtype.Float8{Float64: value, Valid: true}
			normalized[field] = value
		case "prompt":
			if current.PolicyType != "prompt_based" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value = strings.TrimSpace(value)
			if value == "" || utf8.RuneCountInString(value) > maxRiskPolicyPromptRunes {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.Prompt = conv.ToPGText(value)
			digest := sha256.Sum256([]byte(value))
			normalized[field] = hex.EncodeToString(digest[:])
		case "user_message":
			var value string
			if json.Unmarshal(raw, &value) != nil || utf8.RuneCountInString(value) > maxRiskPolicyUserMessageRunes {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.UserMessage = conv.ToPGTextEmpty(value)
			normalized[field] = value
		case "message_types":
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value = canonicalStrings(value)
			if len(value) == 0 || !allIn(value, s.catalog.PolicyMessageTypes) {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.MessageTypes = value
			normalized[field] = value
		case "sources":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value = canonicalStrings(value)
			if len(value) == 0 || !allIn(value, s.catalog.Sources) {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.Sources = value
			normalized[field] = value
			changedSources = true
		case "presidio_entities":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value = canonicalStrings(value)
			if !allIn(value, s.catalog.PresidioEntities) {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.PresidioEntities = value
			normalized[field] = value
		case "presidio_score_threshold":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value float64
			if json.Unmarshal(raw, &value) != nil || value < 0 || value > 1 {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			updated, err := ra.WithPresidioScoreThreshold(params.AnalyzerConfig, &value)
			if err != nil {
				return policycore.UpdateMutation{}, nil, riskMutationUnavailableWithCause(err)
			}
			params.AnalyzerConfig = updated
			normalized[field] = value
		case "approved_email_domains":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value, err := policycore.NormalizeApprovedEmailDomains(value)
			if err != nil || len(value) > maxRiskPolicyApprovedEmailDomains {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			slices.Sort(value)
			updated, err := ra.WithApprovedEmailDomains(params.AnalyzerConfig, value)
			if err != nil {
				return policycore.UpdateMutation{}, nil, riskMutationUnavailableWithCause(err)
			}
			params.AnalyzerConfig = updated
			normalized[field] = value
		case "detection_scopes":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value []riskDetectionScope
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			scopes, updated, err := s.buildDetectionScopes(value, params.Sources, params.PresidioEntities, params.AnalyzerConfig)
			if err != nil {
				return policycore.UpdateMutation{}, nil, err
			}
			params.AnalyzerConfig = updated
			normalized[field] = scopes
		case "prompt_injection_rules":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value = canonicalStrings(value)
			if !allIn(value, s.catalog.PromptInjectionRules) {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.PromptInjectionRules = value
			normalized[field] = value
		case "disabled_rules":
			if current.PolicyType != "standard" {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			value = canonicalStrings(value)
			if !allIn(value, s.catalog.DisabledRules) {
				return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
			}
			params.DisabledRules = value
			normalized[field] = value
		}
	}
	if len(normalized) != len(input.Patch) {
		return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
	}
	if current.PolicyType == "prompt_based" {
		if err := s.requirePromptPolicies(ctx, principal, project); err != nil {
			return policycore.UpdateMutation{}, nil, err
		}
	}
	if changedSources || changedAction {
		if !slices.Contains(s.catalog.Actions, params.Action) || policycore.ValidateSourceAction(params.Sources, params.Action) != nil {
			return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
		}
	}
	effectiveDisposition := valueOrEmpty(policycore.Project(current, nil, nil).ShadowMCPDisposition)
	if effectiveDisposition != "" && shadowmcp.EffectiveDisposition(current.ShadowMcpDisposition, params.Sources, params.Action) == "" {
		// A Platform patch cannot author or remove Shadow MCP URL decisions. Do
		// not silently drop an effective blocking posture and orphan its URL grants,
		// including legacy block_all policies with no stored disposition value.
		return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
	}
	if current.PolicyType == "standard" {
		hasPresidio := slices.Contains(params.Sources, policycatalog.PresidioSource)
		if (changedSources || input.Patch["presidio_entities"] != nil) && hasPresidio != (len(params.PresidioEntities) > 0) {
			return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
		}
		if input.Patch["presidio_score_threshold"] != nil && !hasPresidio {
			return policycore.UpdateMutation{}, nil, invalidRiskPolicyRequest()
		}
		if (changedSources || input.Patch["presidio_entities"] != nil) && input.Patch["detection_scopes"] == nil {
			if err := s.validatePreservedDetectionScopes(current, params.Sources, params.PresidioEntities); err != nil {
				return policycore.UpdateMutation{}, nil, err
			}
		}
	}
	return policycore.UpdateMutation{
		Current: current, Params: params, AudiencePrincipals: nil, AudienceChanged: false,
		AllowedURLs: nil, AllowedURLsSet: false, BlockedURLs: nil, BlockedURLsSet: false,
		EffectiveDisposition: effectiveDisposition,
		SupersedeDecisions:   false,
		ValidateLocked:       nil,
		Actor: policycore.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			DisplayName: nil,
			Slug:        nil,
		},
	}, normalized, nil
}

func (s *riskPolicyMutationService) buildDetectionScopes(input []riskDetectionScope, sources, entities []string, base []byte) ([]riskDetectionScope, []byte, error) {
	normalized := make([]riskDetectionScope, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	configs := make([]ra.DetectionScopeConfig, 0, len(input))
	for _, scope := range input {
		messages := canonicalStrings(scope.MessageTypes)
		if !slices.Contains(s.catalog.DetectionScopeCategories, scope.Category) || len(messages) == 0 || !allIn(messages, s.catalog.PolicyMessageTypes) {
			return nil, nil, invalidRiskPolicyRequest()
		}
		if _, ok := seen[scope.Category]; ok || !riskCategoryReachable(scope.Category, sources, entities) {
			return nil, nil, invalidRiskPolicyRequest()
		}
		seen[scope.Category] = struct{}{}
		encoded, err := policycatalog.EncodeDetectionScope(messages, s.catalog)
		if err != nil {
			return nil, nil, invalidRiskPolicyRequest()
		}
		normalized = append(normalized, riskDetectionScope{Category: scope.Category, MessageTypes: messages})
		configs = append(configs, ra.DetectionScopeConfig{Category: scope.Category, ScopeInclude: encoded, ScopeExempt: ""})
	}
	slices.SortFunc(normalized, func(a, b riskDetectionScope) int { return strings.Compare(a.Category, b.Category) })
	slices.SortFunc(configs, func(a, b ra.DetectionScopeConfig) int { return strings.Compare(a.Category, b.Category) })
	updated, err := ra.WithDetectionScopes(base, configs)
	if err != nil {
		return nil, nil, riskMutationUnavailableWithCause(err)
	}
	return normalized, updated, nil
}

func (s *riskPolicyMutationService) validatePreservedDetectionScopes(current riskrepo.RiskPolicy, sources, entities []string) error {
	policy := policycore.Project(current, nil, nil)
	for _, scope := range policy.DetectionScopes {
		include := valueOrEmpty(scope.ScopeInclude)
		exempt := valueOrEmpty(scope.ScopeExempt)
		if _, supported := policycatalog.DecodeDetectionScope(include, exempt, s.catalog); !supported {
			// Preserve legacy/raw CEL without reinterpreting it as D3-owned state.
			return nil
		}
		if !slices.Contains(s.catalog.DetectionScopeCategories, scope.Category) || !riskCategoryReachable(scope.Category, sources, entities) {
			return invalidRiskPolicyRequest()
		}
	}
	return nil
}

func riskCategoryReachable(category string, sources, entities []string) bool {
	for _, source := range sources {
		if string(categories.Classify(source, "")) == category {
			return true
		}
	}
	for _, entity := range entities {
		if string(categories.Classify(policycatalog.PresidioSource, policycatalog.CanonicalPresidioRuleID(entity))) == category {
			return true
		}
	}
	return false
}

func (s *riskPolicyMutationService) requirePromptPolicies(ctx context.Context, principal Principal, project ResolvedProject) error {
	organizationSlug, err := s.controls.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return riskMutationUnavailable()
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.controls.flags, feature.FlagPromptPolicies, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, project.Slug))
	if err != nil || evaluation != feature.EvaluationEnabled {
		return riskMutationUnavailable()
	}
	return nil
}

func (s *riskPolicyMutationService) matchExistingCreate(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, prepared preparedRiskPolicyCreate) (*policycore.MutationResult, error) {
	rows, err := riskrepo.New(tx).ListRiskPolicyCreateCandidates(ctx, riskrepo.ListRiskPolicyCreateCandidatesParams{
		ProjectID:  projectID,
		Name:       prepared.params.Name,
		PolicyType: prepared.normalized.PolicyType,
	})
	if err != nil {
		return nil, fmt.Errorf("list risk policies for create convergence: %w", err)
	}
	matches := make([]policycore.MutationResult, 0, 1)
	for _, row := range rows {
		audience, err := policycore.New(tx).AudiencePrincipalURNs(ctx, organizationID, row.ID.String())
		if err != nil {
			return nil, fmt.Errorf("load risk policy audience for create convergence: %w", err)
		}
		if riskPolicyCreateMatches(row, audience, prepared.params) {
			matches = append(matches, policycore.MutationResult{Row: row, AudiencePrincipalURNs: audience})
		}
	}
	if len(matches) > 1 {
		return nil, riskMutationConflict("More than one existing policy matches this definition.")
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	return nil, nil
}

func riskPolicyCreateMatches(row riskrepo.RiskPolicy, audience []string, desired riskrepo.CreateRiskPolicyParams) bool {
	return row.OrganizationID == desired.OrganizationID && row.Name == desired.Name && row.PolicyType == desired.PolicyType && row.Enabled == desired.Enabled && row.Action == desired.Action && row.AudienceType == desired.AudienceType && row.AutoName == desired.AutoName && row.Score == desired.Score.Float64 &&
		reflect.DeepEqual(canonicalStrings(row.Sources), canonicalStrings(desired.Sources)) && reflect.DeepEqual(canonicalStrings(row.PresidioEntities), canonicalStrings(desired.PresidioEntities)) && reflect.DeepEqual(canonicalStrings(row.PromptInjectionRules), canonicalStrings(desired.PromptInjectionRules)) && reflect.DeepEqual(canonicalStrings(row.DisabledRules), canonicalStrings(desired.DisabledRules)) && len(row.CustomRuleIds) == 0 && reflect.DeepEqual(canonicalStrings(row.MessageTypes), canonicalStrings(desired.MessageTypes)) &&
		canonicalJSONEqual(row.AnalyzerConfig, desired.AnalyzerConfig) && row.ScopeInclude == desired.ScopeInclude && row.ScopeExempt == desired.ScopeExempt && row.ShadowMcpDisposition == desired.ShadowMcpDisposition && row.UserMessage == desired.UserMessage && row.Prompt == desired.Prompt && len(row.ModelConfig) == 0 && reflect.DeepEqual(canonicalStrings(audience), []string{authz.AllUsersPrincipal().String()})
}

func canonicalJSONEqual(a, b []byte) bool {
	ca, errA := canonicalJSON(a)
	cb, errB := canonicalJSON(b)
	return errA == nil && errB == nil && reflect.DeepEqual(ca, cb)
}

func (s *riskPolicyMutationService) createReceiptResult(ctx context.Context, db riskrepo.DBTX, project ResolvedProject, row riskrepo.RiskPolicy, audience []string, matched bool) (CreateRiskPolicyReceiptResult, error) {
	state, err := riskPolicyVersionState(ctx, db, policycore.Project(row, audience, nil), true)
	if err != nil {
		return CreateRiskPolicyReceiptResult{}, err
	}
	version, err := s.controls.Versions().PolicyVersion(state)
	if err != nil {
		return CreateRiskPolicyReceiptResult{}, err
	}
	category := "created"
	if matched {
		category = "matched_existing"
	}
	return CreateRiskPolicyReceiptResult{Project: riskReceiptProject(project), Policy: riskPolicyReceiptSummary(row), Version: version, MatchedExisting: matched, ResultCategory: category}, nil
}

func (s *riskPolicyMutationService) updateReceiptResult(ctx context.Context, db riskrepo.DBTX, project ResolvedProject, row riskrepo.RiskPolicy, audience []string) (UpdateRiskPolicyReceiptResult, error) {
	state, err := riskPolicyVersionState(ctx, db, policycore.Project(row, audience, nil), true)
	if err != nil {
		return UpdateRiskPolicyReceiptResult{}, err
	}
	version, err := s.controls.Versions().PolicyVersion(state)
	if err != nil {
		return UpdateRiskPolicyReceiptResult{}, err
	}
	return UpdateRiskPolicyReceiptResult{Project: riskReceiptProject(project), Policy: riskPolicyReceiptSummary(row), Version: version, ResultCategory: "updated"}, nil
}

func riskReceiptProject(project ResolvedProject) RiskMutationReceiptProject {
	return RiskMutationReceiptProject{ID: project.ID.String(), Slug: project.Slug}
}
func riskPolicyReceiptSummary(row riskrepo.RiskPolicy) RiskPolicyReceiptSummary {
	return RiskPolicyReceiptSummary{ID: row.ID.String(), PolicyType: row.PolicyType, Enabled: row.Enabled, Action: row.Action}
}
func riskMutationToolReceipt(receipt OperationReceipt) RiskMutationToolReceipt {
	return RiskMutationToolReceipt{ID: receipt.ID.String(), Replayed: receipt.Replayed}
}

func riskMutationToolRefusal[Out any](err error) (*mcp.CallToolResult, Out, error) {
	var zero Out
	var mutation *RiskMutationError
	if !errors.As(err, &mutation) {
		err = riskMutationUnavailableWithCause(err)
		if !errors.As(err, &mutation) {
			return nil, zero, err
		}
	}
	payload, marshalErr := json.Marshal(featureUnavailableResult{Code: mutation.Code, Feature: "risk_mutations", Message: mutation.Message})
	if marshalErr != nil {
		return nil, zero, fmt.Errorf("encode risk mutation refusal: %w", marshalErr)
	}
	return nil, zero, &ToolRefusalError{Payload: string(payload)}
}

func mapRiskPolicyMutationError(err error) error {
	var mutation *RiskMutationError
	var stale *policycore.StalePolicyError
	var blockingConflict *policycore.BlockingPolicyConflictError
	var decisionConflict *policycore.DecisionConflictError
	switch {
	case errors.As(err, &mutation):
		return mutation
	case errors.As(err, &stale), errors.As(err, &blockingConflict), errors.As(err, &decisionConflict):
		return riskMutationConflict("The risk policy could not be changed because its current state conflicts with the request.")
	case errors.Is(err, policycore.ErrLoadPolicy):
		return riskPolicyNotFound()
	default:
		return riskMutationUnavailableWithCause(err)
	}
}

func invalidRiskPolicyRequest() error {
	return &RiskMutationError{Code: "invalid_request", Message: "The risk policy mutation request is invalid.", Cause: ErrRiskMutationInvalid}
}
func riskPolicyNotFound() error {
	return &RiskMutationError{Code: "not_found", Message: "The requested risk policy is not available in this project.", Cause: ErrRiskMutationNotFound}
}
func invalidUserMessage(value *string) bool {
	return value != nil && utf8.RuneCountInString(*value) > maxRiskPolicyUserMessageRunes
}
func canonicalStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return slices.Compact(result)
}
func allIn(values, allowed []string) bool {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}
