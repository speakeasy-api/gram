package risk

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/risk/server"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// RiskAnalysisSignaler signals the per-project risk analysis coordinator workflow.
type RiskAnalysisSignaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
}

// RiskExclusionReconciler triggers the retroactive reconcile sweep for an
// exclusion (flag/unflag matching findings in risk_results). Best-effort: a
// failed trigger is logged, not fatal — the reconcile itself is idempotent.
type RiskExclusionReconciler interface {
	Reconcile(ctx context.Context, projectID, exclusionID uuid.UUID) error
}

// RiskPolicyResultsCleaner asynchronously deletes risk_results rows for a
// soft-deleted policy. Best-effort: a failed trigger is logged, not fatal.
type RiskPolicyResultsCleaner interface {
	Clean(ctx context.Context, projectID, policyID uuid.UUID) error
}

type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	db               *pgxpool.Pool
	repo             *repo.Queries
	auth             *auth.Auth
	authz            *authz.Engine
	signaler         RiskAnalysisSignaler
	reconciler       RiskExclusionReconciler
	resultsCleaner   RiskPolicyResultsCleaner
	completionClient openrouter.CompletionClient
	shadowMCPClient  *shadowmcp.Client
	audit            *audit.Logger
	// cache backs the rpbr2 policy-bypass request links: the link generator
	// stores request state here and CreateRiskPolicyBypassRequest reads it
	// back. Must be the same backing store the link generator uses.
	cache     cache.Cache
	jwtSecret string
	// flags gates the nl/LLM-judge policy MVP (FlagPromptPolicies). Optional:
	// when nil the feature is treated as disabled.
	flags feature.Provider
	// Scanners reused by the rule-playground endpoint (testDetectionRule)
	// so the dashboard sees the exact same matcher output the worker
	// produces during chat-message analysis. Optional: when nil the
	// playground returns an "unsupported" response for that scanner family.
	piiScanner      ra.PIIScanner
	piScanner       *promptinjection.Scanner
	gitleaksScanner *gitleaks.Scanner
	// celEng is the shared CEL env, injected at construction; used to compile
	// and validate scope/detection expressions. nil in the lightweight observer.
	celEng         *celenv.Engine
	builtinPresets *presetlib.Library
	// promptJudge replays an inline guardrail against a chat session for the
	// policy-eval workbench (EvaluatePromptGuardrail). It is the same LLM judge
	// the realtime scanner uses. Optional: when nil the eval endpoint returns
	// un-matched verdicts (judge unavailable).
	promptJudge promptpolicy.Evaluator
}

var _ chat.MessageObserver = (*Service)(nil)

// NewObserver creates a lightweight chat.MessageObserver that signals the risk
// drain workflow when new messages are stored. Use this in contexts (e.g. the
// worker process) where the full risk Service is not needed.
func NewObserver(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	signaler RiskAnalysisSignaler,
	auditLogger *audit.Logger,
) chat.MessageObserver {
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),

		logger:           logger.With(attr.SlogComponent("risk")),
		db:               db,
		repo:             repo.New(db),
		auth:             nil,
		authz:            nil,
		signaler:         signaler,
		reconciler:       nil,
		resultsCleaner:   nil,
		completionClient: nil,
		shadowMCPClient:  nil,
		audit:            auditLogger,
		cache:            nil,
		jwtSecret:        "",
		piiScanner:       nil,
		piScanner:        nil,
		gitleaksScanner:  nil,
		flags:            nil,
		celEng:           nil,
		builtinPresets:   nil,
		promptJudge:      nil,
	}
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	signaler RiskAnalysisSignaler,
	reconciler RiskExclusionReconciler,
	resultsCleaner RiskPolicyResultsCleaner,
	completionClient openrouter.CompletionClient,
	shadowMCPClient *shadowmcp.Client,
	auditLogger *audit.Logger,
	cacheImpl cache.Cache,
	jwtSecret string,
	piiScanner ra.PIIScanner,
	piScanner *promptinjection.Scanner,
	flags feature.Provider,
	celEng *celenv.Engine,
	builtinPresets *presetlib.Library,
	promptJudge promptpolicy.Evaluator,
) *Service {
	logger = logger.With(attr.SlogComponent("risk"))

	return &Service{
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),
		logger:           logger,
		db:               db,
		repo:             repo.New(db),
		auth:             auth.New(logger, db, sessions, authzEngine),
		authz:            authzEngine,
		signaler:         signaler,
		reconciler:       reconciler,
		resultsCleaner:   resultsCleaner,
		completionClient: completionClient,
		shadowMCPClient:  shadowMCPClient,
		audit:            auditLogger,
		cache:            cacheImpl,
		jwtSecret:        jwtSecret,
		piiScanner:       piiScanner,
		piScanner:        piScanner,
		gitleaksScanner:  gitleaks.NewScanner(),
		flags:            flags,
		celEng:           celEng,
		builtinPresets:   builtinPresets,
		promptJudge:      promptJudge,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// OnMessagesStored implements chat.MessageObserver. The caller
// (notifyObservers) already dispatches this in a goroutine with a
// detached context, so this method can safely perform I/O.
func (s *Service) OnMessagesStored(ctx context.Context, projectID uuid.UUID) {
	if err := s.signaler.Signal(ctx, projectID); err != nil {
		s.logger.ErrorContext(ctx, "signal risk coordinator",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
	}
}

func (s *Service) CreateRiskPolicy(ctx context.Context, payload *gen.CreateRiskPolicyPayload) (*types.RiskPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	name := ""
	if payload.Name != nil {
		name = *payload.Name
	}

	// Default auto_name to true only when the caller did not supply Name at
	// all (nil pointer). An explicitly empty Name (`Name: new("")`) is
	// treated as a validation error below — that path is exercised by
	// TestCreateRiskPolicy_EmptyName. An explicit auto_name in the payload
	// always wins over the default so callers can opt in/out without
	// ambiguity.
	autoName := payload.Name == nil
	if payload.AutoName != nil {
		autoName = *payload.AutoName
	}

	action := payload.Action
	if action == "" {
		action = "flag"
	}
	if err := validateAction(action); err != nil {
		return nil, err
	}

	policyType := payload.PolicyType
	if policyType == "" {
		policyType = ra.PolicyTypeStandard
	}
	if err := validatePolicyType(policyType); err != nil {
		return nil, err
	}

	// prompt_based (LLM-judge) policies carry a prompt + optional model config
	// instead of detection sources, and are gated behind FlagPromptPolicies
	// during the MVP.
	var prompt pgtype.Text
	var modelConfig []byte
	if policyType == ra.PolicyTypePromptBased {
		if !s.promptPoliciesEnabled(ctx, authCtx) {
			return nil, oops.E(oops.CodeForbidden, nil, "prompt-based policies are not enabled for this organization")
		}
		if payloadHasCreatePromptPolicyDetectionConfig(payload) {
			return nil, oops.E(oops.CodeInvalid, nil, "prompt-based policies do not support detection source configuration")
		}
		p, mc, err := validatePromptPolicyFields(payload.Prompt, payload.ModelConfig)
		if err != nil {
			return nil, err
		}
		prompt = pgtype.Text{String: p, Valid: true}
		modelConfig = mc
	}

	sources := payload.Sources
	if policyType == ra.PolicyTypePromptBased {
		// prompt_based policies evaluate the prompt via the LLM judge, not
		// detection sources. Persist an empty (non-null) source set.
		sources = []string{}
	} else {
		if sources == nil {
			sources = []string{ra.SourceGitleaks}
		}
		if err := validateSources(sources); err != nil {
			return nil, err
		}
		if err := validateSourceAction(sources, action); err != nil {
			return nil, err
		}
	}
	if err := validateCustomRuleIDs(payload.CustomRuleIds); err != nil {
		return nil, err
	}
	if err := validateMessageTypes(payload.MessageTypes); err != nil {
		return nil, err
	}

	audienceType := payload.AudienceType
	if audienceType == "" {
		audienceType = riskPolicyAudienceEveryone
	}
	audiencePrincipals, err := riskPolicyAudiencePrincipals(audienceType, payload.AudiencePrincipalUrns)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy audience")
	}
	for _, principal := range audiencePrincipals {
		if err := authz.ValidatePrincipal(ctx, s.db, authCtx.ActiveOrganizationID, principal); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid policy audience")
		}
	}
	audiencePrincipalURNs := principalStrings(audiencePrincipals)

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	// Auto-generate a name when the caller opted in (explicit auto_name=true
	// or omitted both auto_name and name). Setting auto_name=false with an
	// empty name surfaces a validation error below rather than silently
	// auto-generating.
	if autoName {
		existingPolicies, _ := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
		var existingNames []string
		for _, p := range existingPolicies {
			existingNames = append(existingNames, p.Name)
		}
		if policyType == ra.PolicyTypePromptBased {
			name = s.generatePromptPolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), prompt.String, existingNames)
		} else {
			customRuleTitles := s.customRuleTitlesForIDs(ctx, *authCtx.ProjectID, payload.CustomRuleIds)
			name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, payload.PresidioEntities, customRuleTitles, action, existingNames)
		}
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate policy id").LogError(ctx, s.logger)
	}

	// Scope predicates (CEL) apply to both standard and prompt policies, so they
	// are not gated by policyType like the detection fields.
	if err := validateScopeExpr(s.celEng, payload.ScopeInclude); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid scope_include")
	}
	if err := validateScopeExpr(s.celEng, payload.ScopeExempt); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid scope_exempt")
	}

	analyzerConfig, err := ra.WithPresidioScoreThreshold(nil, payload.PresidioScoreThreshold)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build analyzer config").LogError(ctx, s.logger)
	}
	if len(payload.ApprovedEmailDomains) > 0 {
		domains, err := validateApprovedEmailDomains(payload.ApprovedEmailDomains)
		if err != nil {
			return nil, err
		}
		analyzerConfig, err = ra.WithApprovedEmailDomains(analyzerConfig, domains)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build analyzer config").LogError(ctx, s.logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateRiskPolicy(ctx, repo.CreateRiskPolicyParams{
		ID:                   id,
		ProjectID:            *authCtx.ProjectID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		Name:                 name,
		PolicyType:           policyType,
		Sources:              sources,
		PresidioEntities:     createPolicyDetectionField(policyType, payload.PresidioEntities),
		AnalyzerConfig:       analyzerConfig,
		PromptInjectionRules: createPolicyDetectionField(policyType, payload.PromptInjectionRules),
		DisabledRules:        createPolicyDetectionField(policyType, payload.DisabledRules),
		CustomRuleIds:        createPolicyDetectionField(policyType, payload.CustomRuleIds),
		MessageTypes:         payload.MessageTypes,
		ScopeInclude:         conv.PtrToPGText(payload.ScopeInclude),
		ScopeExempt:          conv.PtrToPGText(payload.ScopeExempt),
		Enabled:              enabled,
		Action:               action,
		AudienceType:         audienceType,
		AutoName:             autoName,
		UserMessage:          conv.PtrToPGTextEmpty(payload.UserMessage),
		Prompt:               prompt,
		ModelConfig:          modelConfig,
		// Create payload applies the Goa Default(5), so Score always carries a value.
		Score: pgtype.Float8{Float64: payload.Score, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy").LogError(ctx, s.logger)
	}

	if err := syncRiskPolicyAudienceGrants(ctx, dbtx, authCtx.ActiveOrganizationID, row.ID.String(), audienceType, audiencePrincipalURNs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync risk policy audience").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyCreate(ctx, dbtx, audit.LogRiskPolicyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     row.ID,
		RiskPolicyName:   row.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy create").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy create").LogError(ctx, s.logger)
	}

	if s.shadowMCPClient != nil {
		s.shadowMCPClient.Invalidate(ctx, row.ProjectID)
	}

	if enabled {
		_ = s.signaler.Signal(ctx, row.ProjectID)
	}

	return s.policyToType(ctx, row)
}

func (s *Service) ListRiskPolicies(ctx context.Context, payload *gen.ListRiskPoliciesPayload) (*gen.ListRiskPoliciesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policies").LogError(ctx, s.logger)
	}

	policies := make([]*types.RiskPolicy, 0, len(rows))
	for _, row := range rows {
		p, err := s.policyToType(ctx, row)
		if err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}

	return &gen.ListRiskPoliciesResult{Policies: policies}, nil
}

// ListBuiltinExclusions returns the built-in exclusion library grouped by
// category. The catalog is static, embedded reference data (see presetlib), so
// this is a read gated by org admin with no project data access.
func (s *Service) ListBuiltinExclusions(ctx context.Context, _ *gen.ListBuiltinExclusionsPayload) (*gen.ListBuiltinExclusionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	categories := make([]*gen.BuiltinExclusionCategory, 0)
	index := make(map[string]int)
	for _, e := range s.builtinPresets.Entries() {
		label := cmp.Or(e.Category, "Other")
		idx, seen := index[label]
		if !seen {
			idx = len(categories)
			index[label] = idx
			categories = append(categories, &gen.BuiltinExclusionCategory{Label: label, Entries: nil})
		}
		// Deliberately omit engine-internal fields (sources, rule ids, matcher
		// type) — the library is presented to end users without detection-engine
		// details.
		categories[idx].Entries = append(categories[idx].Entries, &gen.BuiltinExclusionEntry{
			ID:          e.ID,
			Reason:      e.Reason,
			Description: e.Description,
			Samples:     e.Samples,
		})
	}

	return &gen.ListBuiltinExclusionsResult{
		Version:    s.builtinPresets.Version(),
		Categories: categories,
	}, nil
}

func (s *Service) GetRiskPolicy(ctx context.Context, payload *gen.GetRiskPolicyPayload) (*types.RiskPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	row, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	return s.policyToType(ctx, row)
}

func (s *Service) UpdateRiskPolicy(ctx context.Context, payload *gen.UpdateRiskPolicyPayload) (*types.RiskPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	// Name validation runs after the auto-name regeneration block below, so
	// callers can send auto_name=true with an empty name and have the server
	// generate one before validation rejects it.

	// Fetch the current policy so we can preserve fields not provided in the payload.
	current, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	// policy_type is immutable; gate edits to prompt_based policies behind the flag.
	if current.PolicyType == ra.PolicyTypePromptBased && !s.promptPoliciesEnabled(ctx, authCtx) {
		return nil, oops.E(oops.CodeForbidden, nil, "prompt-based policies are not enabled for this organization")
	}
	if current.PolicyType == ra.PolicyTypeStandard && (payload.Prompt != nil || payload.ModelConfig != nil) {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt and model_config are only supported for prompt-based policies")
	}
	if current.PolicyType == ra.PolicyTypePromptBased && payloadHasPromptPolicyDetectionConfig(payload) {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt-based policies do not support detection source configuration")
	}

	sources := current.Sources
	if payload.Sources != nil {
		sources = payload.Sources
		if err := validateSources(sources); err != nil {
			return nil, err
		}
	}

	presidioEntities := current.PresidioEntities
	if payload.PresidioEntities != nil {
		presidioEntities = payload.PresidioEntities
	}

	analyzerConfig := current.AnalyzerConfig
	if payload.PresidioScoreThreshold != nil {
		updated, err := ra.WithPresidioScoreThreshold(current.AnalyzerConfig, payload.PresidioScoreThreshold)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build analyzer config").LogError(ctx, s.logger)
		}
		analyzerConfig = updated
	}
	// Omit to preserve; send (possibly empty, to clear) to replace.
	if payload.ApprovedEmailDomains != nil {
		domains, err := validateApprovedEmailDomains(payload.ApprovedEmailDomains)
		if err != nil {
			return nil, err
		}
		updated, err := ra.WithApprovedEmailDomains(analyzerConfig, domains)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build analyzer config").LogError(ctx, s.logger)
		}
		analyzerConfig = updated
	}

	promptInjectionRules := current.PromptInjectionRules
	if payload.PromptInjectionRules != nil {
		promptInjectionRules = payload.PromptInjectionRules
	}

	disabledRules := current.DisabledRules
	if payload.DisabledRules != nil {
		disabledRules = payload.DisabledRules
	}

	customRuleIds := current.CustomRuleIds
	if payload.CustomRuleIds != nil {
		if err := validateCustomRuleIDs(payload.CustomRuleIds); err != nil {
			return nil, err
		}
		customRuleIds = payload.CustomRuleIds
	}

	messageTypes := current.MessageTypes
	if payload.MessageTypes != nil {
		if err := validateMessageTypes(payload.MessageTypes); err != nil {
			return nil, err
		}
		messageTypes = payload.MessageTypes
	}

	// Scope predicates (CEL): omit to preserve; send (possibly empty) to replace.
	scopeInclude := current.ScopeInclude
	if payload.ScopeInclude != nil {
		if err := validateScopeExpr(s.celEng, payload.ScopeInclude); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid scope_include")
		}
		scopeInclude = conv.PtrToPGText(payload.ScopeInclude)
	}
	scopeExempt := current.ScopeExempt
	if payload.ScopeExempt != nil {
		if err := validateScopeExpr(s.celEng, payload.ScopeExempt); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid scope_exempt")
		}
		scopeExempt = conv.PtrToPGText(payload.ScopeExempt)
	}

	enabled := current.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	action := current.Action
	if payload.Action != nil {
		if err := validateAction(*payload.Action); err != nil {
			return nil, err
		}
		action = *payload.Action
	}
	if err := validateSourceAction(sources, action); err != nil {
		return nil, err
	}

	audienceType := current.AudienceType
	if payload.AudienceType != nil {
		audienceType = *payload.AudienceType
	}
	if err := validateRiskPolicyAudienceType(audienceType); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy audience")
	}

	audiencePrincipalURNs := payload.AudiencePrincipalUrns
	if audienceType == riskPolicyAudienceTargeted && audiencePrincipalURNs == nil {
		audiencePrincipalURNs, err = riskPolicyAudiencePrincipalURNs(ctx, s.db, authCtx.ActiveOrganizationID, current.ID.String())
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load risk policy audience").LogError(ctx, s.logger)
		}
	}

	audiencePrincipals, err := riskPolicyAudiencePrincipals(audienceType, audiencePrincipalURNs)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy audience")
	}
	for _, principal := range audiencePrincipals {
		if err := authz.ValidatePrincipal(ctx, s.db, authCtx.ActiveOrganizationID, principal); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid policy audience")
		}
	}
	audiencePrincipalURNs = principalStrings(audiencePrincipals)

	autoName := current.AutoName
	if payload.AutoName != nil {
		autoName = *payload.AutoName
	}

	// user_message is preserved when not in the payload; an explicit empty
	// string clears it (rendered as NULL).
	userMessage := current.UserMessage
	if payload.UserMessage != nil {
		userMessage = conv.PtrToPGTextEmpty(payload.UserMessage)
	}

	// prompt + model_config apply to prompt_based policies. Omitted fields
	// preserve the current values; a provided prompt must be non-empty for a
	// prompt_based policy.
	prompt := current.Prompt
	if payload.Prompt != nil {
		p := strings.TrimSpace(*payload.Prompt)
		if current.PolicyType == ra.PolicyTypePromptBased && p == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "prompt must not be empty for prompt-based policies")
		}
		if len([]rune(p)) > maxPromptPolicyPromptLength {
			return nil, oops.E(oops.CodeInvalid, nil, "prompt must be at most %d characters", maxPromptPolicyPromptLength)
		}
		prompt = pgtype.Text{String: p, Valid: p != ""}
	}
	modelConfig := current.ModelConfig
	if payload.ModelConfig != nil {
		mc, err := marshalModelConfig(payload.ModelConfig)
		if err != nil {
			return nil, err
		}
		modelConfig = mc
	}

	// Regenerate the name only when the caller explicitly opts in on this
	// update via auto_name=true. Toggling unrelated fields (e.g. enabled)
	// should not silently rename the policy.
	name := payload.Name
	if payload.AutoName != nil && *payload.AutoName {
		existingPolicies, _ := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
		var existingNames []string
		for _, p := range existingPolicies {
			if p.ID != id {
				existingNames = append(existingNames, p.Name)
			}
		}
		if current.PolicyType == ra.PolicyTypePromptBased {
			name = s.generatePromptPolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), prompt.String, existingNames)
		} else {
			customRuleTitles := s.customRuleTitlesForIDs(ctx, *authCtx.ProjectID, customRuleIds)
			name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, presidioEntities, customRuleTitles, action, existingNames)
		}
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	currentAudiencePrincipalURNs, err := riskPolicyAudiencePrincipalURNs(ctx, s.db, authCtx.ActiveOrganizationID, current.ID.String())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk policy audience snapshot").LogError(ctx, s.logger)
	}
	snapshotBefore := policyRowSnapshotWithAudience(current, currentAudiencePrincipalURNs)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).UpdateRiskPolicy(ctx, repo.UpdateRiskPolicyParams{
		ID:                   id,
		ProjectID:            *authCtx.ProjectID,
		Name:                 name,
		Sources:              sources,
		PresidioEntities:     presidioEntities,
		AnalyzerConfig:       analyzerConfig,
		PromptInjectionRules: promptInjectionRules,
		DisabledRules:        disabledRules,
		CustomRuleIds:        customRuleIds,
		MessageTypes:         messageTypes,
		ScopeInclude:         scopeInclude,
		ScopeExempt:          scopeExempt,
		Enabled:              enabled,
		Action:               action,
		AudienceType:         audienceType,
		AutoName:             autoName,
		UserMessage:          userMessage,
		Prompt:               prompt,
		ModelConfig:          modelConfig,
		// Omit (nil) preserves the current score; the query COALESCEs to the
		// existing column value. Never contributes to the version bump.
		Score: conv.PtrToPGFloat8(payload.Score),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update risk policy").LogError(ctx, s.logger)
	}

	if err := syncRiskPolicyAudienceGrants(ctx, dbtx, authCtx.ActiveOrganizationID, row.ID.String(), audienceType, audiencePrincipalURNs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync risk policy audience").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyUpdate(ctx, dbtx, audit.LogRiskPolicyUpdateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     row.ID,
		RiskPolicyName:   row.Name,
		SnapshotBefore:   snapshotBefore,
		SnapshotAfter:    policyRowSnapshotWithAudience(row, audiencePrincipalURNs),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy update").LogError(ctx, s.logger)
	}

	if s.shadowMCPClient != nil {
		s.shadowMCPClient.Invalidate(ctx, row.ProjectID)
	}

	_ = s.signaler.Signal(ctx, row.ProjectID)

	return s.policyToType(ctx, row)
}

func (s *Service) DeleteRiskPolicy(ctx context.Context, payload *gen.DeleteRiskPolicyPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.C(oops.CodeInvalid)
	}

	// Fetch before delete so we can log the policy name.
	existing, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	if err := q.DeleteRiskPolicy(ctx, repo.DeleteRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk policy").LogError(ctx, s.logger)
	}

	if err := q.DeleteRiskPolicyBypassRequestsByPolicy(ctx, repo.DeleteRiskPolicyBypassRequestsByPolicyParams{
		RiskPolicyID: id,
		ProjectID:    *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk policy bypass requests").LogError(ctx, s.logger)
	}

	deletedExclusions, err := q.DeleteRiskExclusionsByPolicy(ctx, repo.DeleteRiskExclusionsByPolicyParams{
		RiskPolicyID: uuid.NullUUID{UUID: id, Valid: true},
		ProjectID:    *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk exclusions by policy").LogError(ctx, s.logger)
	}
	if deletedExclusions > 0 {
		s.logger.InfoContext(ctx, "deleted risk exclusions for policy",
			attr.SlogRiskPolicyID(id.String()),
			attr.SlogDBDeletedRowsCount(deletedExclusions),
		)
	}

	if err := clearRiskPolicyAudienceGrants(ctx, dbtx, authCtx.ActiveOrganizationID, id.String()); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear risk policy audience").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyDelete(ctx, dbtx, audit.LogRiskPolicyDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     id,
		RiskPolicyName:   existing.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log risk policy delete").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit risk policy delete").LogError(ctx, s.logger)
	}

	if s.shadowMCPClient != nil {
		s.shadowMCPClient.Invalidate(ctx, *authCtx.ProjectID)
	}

	s.cleanPolicyResults(ctx, *authCtx.ProjectID, id)

	return nil
}

func (s *Service) cleanPolicyResults(ctx context.Context, projectID, policyID uuid.UUID) {
	if s.resultsCleaner == nil {
		return
	}
	if err := s.resultsCleaner.Clean(ctx, projectID, policyID); err != nil {
		s.logger.ErrorContext(ctx, "trigger risk policy results cleanup",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
			attr.SlogRiskPolicyID(policyID.String()),
		)
	}
}

const riskDefaultPageSize = 50

func resolvePageSize(limit *int) int {
	if limit == nil || *limit <= 0 {
		return riskDefaultPageSize
	}
	if *limit > 200 {
		return 200
	}
	return *limit
}

// riskResultsCursor is the composite cursor used for paginating risk
// result listings. We sort by (message_created_at, id) so the cursor
// carries both fields. Encoded as "<RFC3339Nano>|<uuid>" — a small,
// opaque-to-the-client string that survives URL/query-param round-trips.
// Legacy id-only cursors are silently dropped (treated as no cursor)
// rather than rejected, so an in-flight infinite-scroll session that
// upgrades doesn't error mid-scroll.
type riskResultsCursor struct {
	MessageCreatedAt time.Time
	ID               uuid.UUID
}

func parseRiskResultsCursor(raw *string) (*riskResultsCursor, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	parts := strings.SplitN(*raw, "|", 2)
	if len(parts) != 2 {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("parse cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("parse cursor id: %w", err)
	}
	return &riskResultsCursor{MessageCreatedAt: t, ID: id}, nil
}

func encodeRiskResultsCursor(c riskResultsCursor) string {
	return c.MessageCreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
}

func cursorToParams(c *riskResultsCursor) (pgtype.Timestamptz, uuid.NullUUID) {
	if c == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return pgtype.Timestamptz{Time: c.MessageCreatedAt, InfinityModifier: pgtype.Finite, Valid: true}, uuid.NullUUID{UUID: c.ID, Valid: true}
}

// ListRiskResults serves the dashboard's default risk results listing. It
// always requires org:admin, but only returns raw `match`/`spans` content
// when the request is scoped to a single chat_id AND the caller separately
// holds chat:read for that exact chat — the same condition that already lets
// them load the chat's full transcript (which contains the same secret
// embedded in message content) via chat.LoadChat. That is a soft check
// (FindMatched, not Require): missing chat:read doesn't fail the request, it
// just falls back to a redacted response. Every other call — notably the
// Risk Events page, which lists across many chats with no single chat_id
// filter — gets match_redacted instead of the raw secret.
func (s *Service) ListRiskResults(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	raw, err := s.listRiskResultsRaw(ctx, payload)
	if err != nil {
		return nil, err
	}

	if payload.ChatID != nil && *payload.ChatID != "" {
		matched, err := s.authz.FindMatched(ctx, []authz.Check{authz.ChatReadCheck(*payload.ChatID)})
		if err == nil && len(matched) == 1 && matched[0] {
			return raw, nil
		}
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	for _, r := range raw.Results {
		redactResultMatchInPlace(r, authCtx.ActiveOrganizationID)
	}
	return raw, nil
}

// redactResultMatchInPlace clears a result's raw match/spans and populates
// match_redacted in their place, reusing the same fingerprint format (and
// shadow_mcp/account_identity passthrough carve-out) as
// redactMatch/redactRiskResult below.
// Spans aren't given a redacted counterpart here because nothing on this
// (non-agent) redacted path renders them.
func redactResultMatchInPlace(r *types.RiskResult, orgID string) {
	matchRedacted := redactMatch(r.Source, r.Match, orgID)
	r.MatchRedacted = &matchRedacted
	r.Match = nil
	r.Spans = nil
}

// listRiskResultsRaw is the shared, always-unredacted fetch behind both
// ListRiskResults (which may redact its output) and ListRiskResultsForAgent
// (which always redacts). Keeping this as the single source of raw data
// means neither caller can accidentally double-redact or leak a bypassed
// raw response to a consumer that must always see redacted content.
func (s *Service) listRiskResultsRaw(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").LogError(ctx, s.logger)
	}

	pageSize := resolvePageSize(payload.Limit)

	if payload.ChatID != nil && *payload.ChatID != "" {
		totalCount, err := s.repo.CountAllFindings(ctx, *authCtx.ProjectID)
		if err != nil {
			totalCount = 0
		}
		return s.listResultsByChat(ctx, *authCtx.ProjectID, *payload.ChatID, cursor, pageSize, totalCount)
	}
	// A policy filter is applied alongside the other filters rather than
	// short-circuiting to a separate listing, so combinations like
	// policy_id + rule_id are honored. When set, the count is scoped to that
	// policy (and includes disabled policies for historical findings) to match
	// the list query's semantics.
	var policyIDInput *string
	if payload.PolicyID != nil && *payload.PolicyID != "" {
		policyIDInput = payload.PolicyID
	}
	policyID, err := conv.PtrToNullUUID(policyIDInput)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy ID")
	}

	category := ""
	if payload.Category != nil {
		category = *payload.Category
	}
	ruleID := ""
	if payload.RuleID != nil {
		ruleID = *payload.RuleID
	}
	userID := ""
	if payload.UserID != nil {
		userID = *payload.UserID
	}
	uniqueMatch := false
	if payload.UniqueMatch != nil {
		uniqueMatch = *payload.UniqueMatch
	}
	nonAssistant := false
	if payload.NonAssistant != nil {
		nonAssistant = *payload.NonAssistant
	}
	var assistantIDInput *string
	if payload.AssistantID != nil && *payload.AssistantID != "" {
		assistantIDInput = payload.AssistantID
	}
	assistantID, err := conv.PtrToNullUUID(assistantIDInput)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid assistant ID")
	}
	fromTime, err := parseOptionalTimestamptz(payload.From)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid from").LogError(ctx, s.logger)
	}
	toTime, err := parseOptionalTimestamptz(payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid to").LogError(ctx, s.logger)
	}

	var totalCount int64
	if policyID.Valid {
		totalCount, err = s.repo.CountRiskResultsByProjectAndPolicy(ctx, repo.CountRiskResultsByProjectAndPolicyParams{
			ProjectID:    *authCtx.ProjectID,
			RiskPolicyID: policyID.UUID,
		})
	} else {
		totalCount, err = s.repo.CountAllFindings(ctx, *authCtx.ProjectID)
	}
	if err != nil {
		totalCount = 0
	}
	return s.listResultsByProject(ctx, *authCtx.ProjectID, cursor, pageSize, totalCount, policyID, category, ruleID, userID, uniqueMatch, nonAssistant, assistantID, fromTime, toTime)
}

func parseOptionalTimestamptz(raw *string) (pgtype.Timestamptz, error) {
	empty := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if raw == nil {
		return empty, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return empty, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return empty, fmt.Errorf("parse timestamp: %w", err)
	}
	return pgtype.Timestamptz{Time: parsed.UTC(), InfinityModifier: pgtype.Finite, Valid: true}, nil
}

// ListRiskResultsForAgent serves the same data as ListRiskResults but strips
// raw `match` content from non-shadow_mcp findings before returning, so the
// agent / MCP surface never holds secret values in model context. Shadow-MCP
// findings pass `match` through verbatim because the value is a server URL
// or stdio command identifier the dashboard already exposes unmasked.
func (s *Service) ListRiskResultsForAgent(ctx context.Context, payload *gen.ListRiskResultsForAgentPayload) (*gen.ListRiskResultsForAgentResult, error) {
	// Calls listRiskResultsRaw directly (not the public ListRiskResults) so
	// this always redacts from raw match content, independent of the
	// chat:read bypass ListRiskResults applies for its own dashboard callers.
	base, err := s.listRiskResultsRaw(ctx, &gen.ListRiskResultsPayload{
		ApikeyToken:      payload.ApikeyToken,
		SessionToken:     payload.SessionToken,
		ProjectSlugInput: payload.ProjectSlugInput,
		PolicyID:         payload.PolicyID,
		ChatID:           payload.ChatID,
		Category:         payload.Category,
		RuleID:           payload.RuleID,
		UserID:           payload.UserID,
		UniqueMatch:      payload.UniqueMatch,
		NonAssistant:     payload.NonAssistant,
		AssistantID:      payload.AssistantID,
		From:             payload.From,
		To:               payload.To,
		Cursor:           payload.Cursor,
		Limit:            payload.Limit,
	})
	if err != nil {
		return nil, err
	}

	// ListRiskResults already enforced auth above; this lookup is safe and
	// only used to derive the per-org salt for match-fingerprint hashing.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	redacted := make([]*types.RiskResultRedacted, 0, len(base.Results))
	for _, r := range base.Results {
		redacted = append(redacted, redactRiskResult(r, authCtx.ActiveOrganizationID))
	}

	return &gen.ListRiskResultsForAgentResult{
		Results:    redacted,
		TotalCount: base.TotalCount,
		NextCursor: base.NextCursor,
	}, nil
}

// UnmaskRiskResult returns the plaintext match for a single risk result, on
// demand. Unlike ListRiskResults it is gated solely on chat:read for the
// result's chat — not org:admin — so a reveal is a discrete, audited access
// event distinct from browsing the redacted list.
func (s *Service) UnmaskRiskResult(ctx context.Context, payload *gen.UnmaskRiskResultPayload) (*gen.RiskUnmaskResultResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	row, err := s.repo.GetRiskResultByID(ctx, repo.GetRiskResultByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk result not found").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.ChatReadCheck(row.ChatID.String())); err != nil {
		return nil, err
	}

	if err := s.audit.LogRiskResultUnmask(ctx, s.db, audit.LogRiskResultUnmaskEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskResultID:     row.ID,
		ChatID:           row.ChatID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record risk result unmask audit log").LogError(ctx, s.logger)
	}

	return &gen.RiskUnmaskResultResult{
		ID:    row.ID.String(),
		Match: conv.FromPGTextOrEmpty[string](row.Match),
	}, nil
}

// redactRiskResult converts a RiskResult into a RiskResultRedacted by
// replacing raw `match` content with an opaque length+sha256-prefix
// fingerprint and coarsening position info to a single boolean. Source ==
// "shadow_mcp" is a deliberate carve-out: its match is a server URL or
// command identifier (already shown unmasked in the dashboard) and the agent
// needs to be able to name it to be useful.
//
// orgID is mixed into the hash so fingerprints cannot correlate the same
// secret across organizations even if some future code path widens the
// surface beyond org-scoped access.
func redactRiskResult(r *types.RiskResult, orgID string) *types.RiskResultRedacted {
	matchRedacted := redactMatch(r.Source, r.Match, orgID)

	var spansRedacted []*types.RiskSpanRedacted
	if len(r.Spans) > 0 {
		spansRedacted = make([]*types.RiskSpanRedacted, 0, len(r.Spans))
		for _, sp := range r.Spans {
			match := sp.Match
			spansRedacted = append(spansRedacted, &types.RiskSpanRedacted{
				MatchRedacted: redactMatch(r.Source, &match, orgID),
				Field:         sp.Field,
				Path:          sp.Path,
				PositionKnown: sp.StartPos != nil && sp.EndPos != nil,
			})
		}
	}

	return &types.RiskResultRedacted{
		ID:            r.ID,
		PolicyID:      r.PolicyID,
		PolicyVersion: r.PolicyVersion,
		ChatMessageID: r.ChatMessageID,
		ChatID:        r.ChatID,
		ChatTitle:     r.ChatTitle,
		UserID:        r.UserID,
		Source:        r.Source,
		RuleID:        r.RuleID,
		Description:   r.Description,
		MatchRedacted: matchRedacted,
		PositionKnown: r.StartPos != nil && r.EndPos != nil,
		Confidence:    r.Confidence,
		Tags:          r.Tags,
		SpansRedacted: spansRedacted,
		CreatedAt:     r.CreatedAt,
	}
}

// redactMatch encodes a match value as `<redacted len=N sha=XXXXXXXX>`,
// except for shadow_mcp and account_identity findings whose match passes
// through verbatim: an MCP server URL or an account email IS the report, not
// a secret. A nil/empty match collapses to `<redacted len=0>` without a sha
// component so the absence of a finding payload is distinguishable from a
// real hash.
//
// The hash is salted by orgID with a NUL separator so two different orgs
// holding the same secret produce different fingerprints — defense in depth
// against any future surface that crosses an org boundary. Within an org the
// fingerprint stays deterministic so agents can still dedupe.
func redactMatch(source string, match *string, orgID string) string {
	if match == nil || *match == "" {
		return "<redacted len=0>"
	}
	if source == shadowmcp.SourceShadowMCP || source == ra.SourceAccountIdentity {
		return *match
	}
	var buf []byte
	buf = append(buf, orgID...)
	buf = append(buf, 0x00)
	buf = append(buf, *match...)
	sum := sha256.Sum256(buf)
	return fmt.Sprintf("<redacted len=%d sha=%s>", len(*match), hex.EncodeToString(sum[:4]))
}

func (s *Service) ListRiskResultsByChat(ctx context.Context, payload *gen.ListRiskResultsByChatPayload) (*gen.ListRiskResultsByChatResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := conv.PtrToNullUUID(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").LogError(ctx, s.logger)
	}

	pageSize := resolvePageSize(payload.Limit)

	rows, err := s.repo.ListRiskResultsGroupedByChat(ctx, repo.ListRiskResultsGroupedByChatParams{
		ProjectID: *authCtx.ProjectID,
		Cursor:    cursor,
		PageLimit: conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").LogError(ctx, s.logger)
	}

	chats := make([]*types.RiskChatSummary, 0, len(rows))
	for _, row := range rows {
		chats = append(chats, &types.RiskChatSummary{
			ChatID:         row.ChatID.String(),
			ChatTitle:      conv.FromPGText[string](row.ChatTitle),
			UserID:         conv.FromPGText[string](row.ChatUserID),
			FindingsCount:  row.FindingsCount,
			LatestDetected: row.LatestDetected.Time.Format(time.RFC3339),
		})
	}

	var nextCursor *string
	if len(chats) > pageSize {
		nextCursor = &chats[pageSize].ChatID
		chats = chats[:pageSize]
	}

	return &gen.ListRiskResultsByChatResult{Chats: chats, NextCursor: nextCursor}, nil
}

func (s *Service) GetRiskOverview(ctx context.Context, payload *gen.GetRiskOverviewPayload) (*gen.RiskOverviewResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid overview window").LogError(ctx, s.logger)
	}

	window := riskOverviewWindowParams(from, to)
	scanCounts, err := s.repo.GetRiskOverviewScanCounts(ctx, repo.GetRiskOverviewScanCountsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk overview scan counts").LogError(ctx, s.logger)
	}

	findingCounts, err := s.repo.GetRiskOverviewFindingCounts(ctx, repo.GetRiskOverviewFindingCountsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk overview finding counts").LogError(ctx, s.logger)
	}

	userRows, err := s.repo.ListRiskOverviewTopUsers(ctx, repo.ListRiskOverviewTopUsersParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
		// Returns enough rows for the "view all users" page to show the full
		// long-tail without pagination. The main /risk-overview widget only
		// renders the top 10 of these.
		RowLimit: 200,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top users").LogError(ctx, s.logger)
	}

	timeSeriesRows, err := s.repo.ListRiskOverviewTimeSeriesFindings(ctx, repo.ListRiskOverviewTimeSeriesFindingsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview time series findings").LogError(ctx, s.logger)
	}

	ruleRows, err := s.repo.ListRiskOverviewTopRules(ctx, repo.ListRiskOverviewTopRulesParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
		// Returns enough rows for the "view all rules" page to show the long
		// tail without pagination. The main /risk-overview widget only renders
		// the top 10 of these.
		RowLimit: 200,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top rules").LogError(ctx, s.logger)
	}

	topCategories := riskOverviewTopCategories(timeSeriesRows, 10)

	topRules := make([]*gen.RiskRuleBreakdownEntry, 0, len(ruleRows))
	for _, row := range ruleRows {
		topRules = append(topRules, &gen.RiskRuleBreakdownEntry{
			RuleID:   row.RuleID,
			Source:   row.Source,
			Findings: row.Findings,
		})
	}

	topUsers := make([]*gen.RiskOverviewUser, 0, len(userRows))
	for _, row := range userRows {
		topUsers = append(topUsers, &gen.RiskOverviewUser{
			Email:          row.Email,
			ExternalUserID: row.ExternalUserID,
			Findings:       row.Findings,
		})
	}

	timeSeriesFindings := make([]*gen.RiskOverviewTimeSeriesFinding, 0, len(timeSeriesRows))
	for _, row := range timeSeriesRows {
		timeSeriesFindings = append(timeSeriesFindings, &gen.RiskOverviewTimeSeriesFinding{
			BucketStart: row.BucketStart.Time.UTC().Format(time.RFC3339),
			Category:    row.Category,
			Findings:    row.Findings,
		})
	}

	return &gen.RiskOverviewResult{
		From:               from.UTC().Format(time.RFC3339),
		To:                 to.UTC().Format(time.RFC3339),
		MessagesScanned:    scanCounts.MessagesScanned,
		Findings:           findingCounts.Findings,
		FlaggedSessions:    findingCounts.FlaggedSessions,
		ActivePolicies:     scanCounts.ActivePolicies,
		TopCategories:      topCategories,
		TopUsers:           topUsers,
		TopRules:           topRules,
		TimeSeriesFindings: timeSeriesFindings,
	}, nil
}

type riskOverviewWindow struct {
	from pgtype.Timestamptz
	to   pgtype.Timestamptz
}

const maxRiskOverviewWindow = 31 * 24 * time.Hour

func resolveRiskOverviewWindow(rawFrom, rawTo *string) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	if rawTo != nil && strings.TrimSpace(*rawTo) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*rawTo))
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse to: %w", err)
		}
		to = parsed.UTC()
	}

	toYear, toMonth, toDay := to.Date()
	from := time.Date(toYear, toMonth, toDay, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
	if rawFrom != nil && strings.TrimSpace(*rawFrom) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*rawFrom))
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse from: %w", err)
		}
		from = parsed.UTC()
	}

	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be before to")
	}
	if to.Sub(from) > maxRiskOverviewWindow {
		return time.Time{}, time.Time{}, fmt.Errorf("window must be %s or less", maxRiskOverviewWindow)
	}

	return from, to, nil
}

func riskOverviewWindowParams(from, to time.Time) riskOverviewWindow {
	return riskOverviewWindow{
		from: pgtype.Timestamptz{Time: from, InfinityModifier: pgtype.Finite, Valid: true},
		to:   pgtype.Timestamptz{Time: to, InfinityModifier: pgtype.Finite, Valid: true},
	}
}

func riskOverviewTopCategories(rows []repo.ListRiskOverviewTimeSeriesFindingsRow, limit int) []*gen.RiskOverviewCategory {
	if limit <= 0 {
		return nil
	}

	counts := make(map[string]int64)
	for _, row := range rows {
		counts[row.Category] += row.Findings
	}

	categories := make([]*gen.RiskOverviewCategory, 0, len(counts))
	for category, findings := range counts {
		if findings == 0 {
			continue
		}
		categories = append(categories, &gen.RiskOverviewCategory{
			Category: category,
			Findings: findings,
		})
	}

	slices.SortFunc(categories, func(a, b *gen.RiskOverviewCategory) int {
		if a.Findings != b.Findings {
			return cmp.Compare(b.Findings, a.Findings)
		}

		return cmp.Compare(a.Category, b.Category)
	})

	if len(categories) > limit {
		categories = categories[:limit]
	}

	return categories
}

// paginateResults trims the over-fetched extra row and, if present, encodes
// the (message_created_at, id) cursor from it so the next page starts
// strictly after the last returned row.
func (s *Service) paginateResults(results []*types.RiskResult, extraRow *riskResultsCursor, pageSize int, totalCount int64) *gen.ListRiskResultsResult {
	var nextCursor *string
	if len(results) > pageSize && extraRow != nil {
		encoded := encodeRiskResultsCursor(*extraRow)
		nextCursor = &encoded
		results = results[:pageSize]
	}
	return &gen.ListRiskResultsResult{Results: results, TotalCount: totalCount, NextCursor: nextCursor}
}

func (s *Service) listResultsByChat(ctx context.Context, projectID uuid.UUID, rawChatID string, cursor *riskResultsCursor, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	chatID, err := uuid.Parse(rawChatID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByChatFound(ctx, repo.ListRiskResultsByChatFoundParams{
		ChatID:                 chatID,
		ProjectID:              projectID,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").LogError(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		cid := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.BlockID, row.ChatMessageID, &cid, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.Spans, row.MessageCreatedAt, row.Replayed))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

func (s *Service) listResultsByProject(ctx context.Context, projectID uuid.UUID, cursor *riskResultsCursor, pageSize int, totalCount int64, policyID uuid.NullUUID, category string, ruleID string, userID string, uniqueMatch bool, nonAssistant bool, assistantID uuid.NullUUID, fromTime, toTime pgtype.Timestamptz) (*gen.ListRiskResultsResult, error) {
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByProjectFound(ctx, repo.ListRiskResultsByProjectFoundParams{
		ProjectID:              projectID,
		PolicyID:               policyID,
		FromTime:               fromTime,
		ToTime:                 toTime,
		Category:               category,
		RuleID:                 ruleID,
		UserID:                 userID,
		UniqueMatch:            uniqueMatch,
		NonAssistant:           nonAssistant,
		AssistantID:            assistantID,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results").LogError(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.BlockID, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.Spans, row.MessageCreatedAt, row.Replayed))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

func (s *Service) ListRiskCategories(ctx context.Context, payload *gen.ListRiskCategoriesPayload) (*gen.RiskCategoriesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	defs := categories.All()
	out := make([]*gen.RiskCategoryDefinition, 0, len(defs))
	for _, def := range defs {
		ruleIDs := def.RuleIDs
		if ruleIDs == nil {
			ruleIDs = []string{}
		}
		out = append(out, &gen.RiskCategoryDefinition{
			Key:          string(def.Category),
			Label:        def.Label,
			Description:  def.Description,
			Icon:         def.Icon,
			Source:       def.Source,
			RuleIds:      ruleIDs,
			RuleIDPrefix: def.RulePrefix,
		})
	}
	return &gen.RiskCategoriesResult{Categories: out}, nil
}

// CompileExpr compiles a single CEL expression without evaluating it, so the
// editor can validate as the author types. It mirrors the save-time gate
// (celenv.Compile via the shared engine) so an expression that compiles here
// also saves. An empty expression is valid.
func (s *Service) CompileExpr(ctx context.Context, payload *gen.CompileExprPayload) (*gen.ExprCompileResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Authoring endpoint: gate on org-admin like the rest of the risk policy
	// authoring surface.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	expr := strings.TrimSpace(payload.Expr)
	if expr == "" {
		return &gen.ExprCompileResult{OK: true, Error: ""}, nil
	}
	if s.celEng == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "cel engine unavailable")
	}
	if _, err := s.celEng.Compile(expr); err != nil {
		return &gen.ExprCompileResult{OK: false, Error: err.Error()}, nil
	}
	return &gen.ExprCompileResult{OK: true, Error: ""}, nil
}

func (s *Service) GetRiskUserBreakdown(ctx context.Context, payload *gen.GetRiskUserBreakdownPayload) (*gen.RiskUserBreakdownResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid window").LogError(ctx, s.logger)
	}
	window := riskOverviewWindowParams(from, to)

	categoryRows, err := s.repo.ListRiskUserCategoryBreakdown(ctx, repo.ListRiskUserCategoryBreakdownParams{
		ProjectID:      *authCtx.ProjectID,
		FromTime:       window.from,
		ToTime:         window.to,
		ExternalUserID: payload.ExternalUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user category breakdown").LogError(ctx, s.logger)
	}

	ruleRows, err := s.repo.ListRiskUserRuleBreakdown(ctx, repo.ListRiskUserRuleBreakdownParams{
		ProjectID:      *authCtx.ProjectID,
		FromTime:       window.from,
		ToTime:         window.to,
		ExternalUserID: payload.ExternalUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user rule breakdown").LogError(ctx, s.logger)
	}

	categories := make([]*gen.RiskOverviewCategory, 0, len(categoryRows))
	var total int64
	for _, row := range categoryRows {
		categories = append(categories, &gen.RiskOverviewCategory{
			Category: row.Category,
			Findings: row.Findings,
		})
		total += row.Findings
	}

	rules := make([]*gen.RiskRuleBreakdownEntry, 0, len(ruleRows))
	for _, row := range ruleRows {
		rules = append(rules, &gen.RiskRuleBreakdownEntry{
			RuleID:   row.RuleID,
			Source:   row.Source,
			Findings: row.Findings,
		})
	}

	return &gen.RiskUserBreakdownResult{
		From:           from.UTC().Format(time.RFC3339),
		To:             to.UTC().Format(time.RFC3339),
		ExternalUserID: payload.ExternalUserID,
		Findings:       total,
		Categories:     categories,
		Rules:          rules,
	}, nil
}

func (s *Service) GetRiskRuleBreakdown(ctx context.Context, payload *gen.GetRiskRuleBreakdownPayload) (*gen.RiskRuleBreakdownResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid window").LogError(ctx, s.logger)
	}

	window := riskOverviewWindowParams(from, to)
	rows, err := s.repo.ListRiskRulesByCategory(ctx, repo.ListRiskRulesByCategoryParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
		Category:  payload.Category,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list rule breakdown").LogError(ctx, s.logger)
	}

	rules := make([]*gen.RiskRuleBreakdownEntry, 0, len(rows))
	var total int64
	for _, row := range rows {
		rules = append(rules, &gen.RiskRuleBreakdownEntry{
			RuleID:   row.RuleID,
			Source:   row.Source,
			Findings: row.Findings,
		})
		total += row.Findings
	}

	return &gen.RiskRuleBreakdownResult{
		From:     from.UTC().Format(time.RFC3339),
		To:       to.UTC().Format(time.RFC3339),
		Category: payload.Category,
		Rules:    rules,
		Total:    total,
	}, nil
}

func (s *Service) GetRiskPolicyStatus(ctx context.Context, payload *gen.GetRiskPolicyStatusPayload) (*types.RiskPolicyStatus, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	policy, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	totalMessages, err := s.repo.CountTotalMessages(ctx, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count total messages").LogError(ctx, s.logger)
	}

	analyzedMessages, err := s.repo.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         *authCtx.ProjectID,
		RiskPolicyID:      id,
		RiskPolicyVersion: policy.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count analyzed messages").LogError(ctx, s.logger)
	}

	findingsCount, err := s.repo.CountFindingsByPolicy(ctx, repo.CountFindingsByPolicyParams{
		ProjectID:         *authCtx.ProjectID,
		RiskPolicyID:      id,
		RiskPolicyVersion: policy.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count findings").LogError(ctx, s.logger)
	}

	pending := max(totalMessages-analyzedMessages, 0)

	// We report a simplified workflow status based on pending work.
	workflowStatus := "not_started"
	if pending > 0 && policy.Enabled {
		workflowStatus = "running"
	} else if pending == 0 && policy.Enabled {
		workflowStatus = "sleeping"
	}

	return &types.RiskPolicyStatus{
		PolicyID:         id.String(),
		PolicyVersion:    policy.Version,
		TotalMessages:    totalMessages,
		AnalyzedMessages: analyzedMessages,
		PendingMessages:  pending,
		FindingsCount:    findingsCount,
		WorkflowStatus:   workflowStatus,
	}, nil
}

func (s *Service) TriggerRiskAnalysis(_ context.Context, _ *gen.TriggerRiskAnalysisPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "operation not supported")
}

func (s *Service) CreateCustomDetectionRule(ctx context.Context, payload *gen.CreateCustomDetectionRulePayload) (*types.RiskCustomDetectionRule, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	ruleID := strings.TrimSpace(payload.RuleID)
	title := strings.TrimSpace(payload.Title)
	description := ""
	if payload.Description != nil {
		description = strings.TrimSpace(*payload.Description)
	}
	detectionExpr := strings.TrimSpace(conv.PtrValOr(payload.DetectionExpr, ""))
	severity := payload.Severity
	if severity == "" {
		severity = "medium"
	}
	if err := validateCustomDetectionRule(s.celEng, ruleID, title, detectionExpr, severity); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateCustomDetectionRule(ctx, repo.CreateCustomDetectionRuleParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		RuleID:         ruleID,
		Title:          title,
		Description:    description,
		DetectionExpr:  conv.ToPGTextEmpty(detectionExpr),
		Severity:       severity,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "custom detection rule already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create custom detection rule").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit custom detection rule create").LogError(ctx, s.logger)
	}

	return customDetectionRuleToType(row), nil
}

func (s *Service) ListCustomDetectionRules(ctx context.Context, payload *gen.ListCustomDetectionRulesPayload) (*gen.ListCustomDetectionRulesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListCustomDetectionRules(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list custom detection rules").LogError(ctx, s.logger)
	}

	rules := make([]*types.RiskCustomDetectionRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, customDetectionRuleToType(row))
	}

	return &gen.ListCustomDetectionRulesResult{Rules: rules}, nil
}

func (s *Service) GetCustomDetectionRule(ctx context.Context, payload *gen.GetCustomDetectionRulePayload) (*types.RiskCustomDetectionRule, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	row, err := s.repo.GetCustomDetectionRule(ctx, repo.GetCustomDetectionRuleParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "custom detection rule not found").LogError(ctx, s.logger)
	}

	return customDetectionRuleToType(row), nil
}

func (s *Service) UpdateCustomDetectionRule(ctx context.Context, payload *gen.UpdateCustomDetectionRulePayload) (*types.RiskCustomDetectionRule, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	title := strings.TrimSpace(payload.Title)
	description := ""
	if payload.Description != nil {
		description = strings.TrimSpace(*payload.Description)
	}
	detectionExpr := strings.TrimSpace(conv.PtrValOr(payload.DetectionExpr, ""))
	if err := validateCustomDetectionRuleFields(s.celEng, title, detectionExpr, payload.Severity); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).UpdateCustomDetectionRule(ctx, repo.UpdateCustomDetectionRuleParams{
		ID:            id,
		ProjectID:     *authCtx.ProjectID,
		Title:         title,
		Description:   description,
		DetectionExpr: conv.ToPGTextEmpty(detectionExpr),
		Severity:      payload.Severity,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "custom detection rule not found").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit custom detection rule update").LogError(ctx, s.logger)
	}

	return customDetectionRuleToType(row), nil
}

func (s *Service) DeleteCustomDetectionRule(ctx context.Context, payload *gen.DeleteCustomDetectionRulePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.C(oops.CodeInvalid)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := repo.New(dbtx).DeleteCustomDetectionRule(ctx, repo.DeleteCustomDetectionRuleParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete custom detection rule").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit custom detection rule delete").LogError(ctx, s.logger)
	}

	return nil
}

// SuggestCustomDetectionRule turns a natural-language description ("what do
// you want to detect?") into a structured custom-rule suggestion. The
// response is intentionally minimal — the dashboard prefills its create
// form with these values and the operator edits before saving.
func (s *Service) SuggestCustomDetectionRule(ctx context.Context, payload *gen.SuggestCustomDetectionRulePayload) (*gen.SuggestCustomDetectionRuleResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt is required")
	}

	if s.completionClient == nil {
		s.logger.WarnContext(ctx, "completion client not configured; returning heuristic suggestion")
		return heuristicCustomRuleSuggestion(prompt, payload.ExistingRuleIds), nil
	}

	suggestion, err := s.suggestCustomRuleViaLLM(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), prompt, payload.ExistingRuleIds)
	if err != nil {
		s.logger.WarnContext(ctx, "openrouter suggestion failed; returning heuristic suggestion", attr.SlogError(err))
		return heuristicCustomRuleSuggestion(prompt, payload.ExistingRuleIds), nil
	}
	return suggestion, nil
}

// SuggestExclusion turns a natural-language description of findings an
// operator wants to stop flagging into a structured exclusion suggestion
// (match_type, match_value, filters), validated with the same gate the
// create/update exclusion handlers use (RE2 compile, 512-char cap). The
// exclusion form serializes the result into its criteria expression via the
// existing client-side mapping. Falls back to an editable exact-match
// prefill when the LLM is unavailable, mirroring
// SuggestCustomDetectionRule's heuristic fallback.
func (s *Service) SuggestExclusion(ctx context.Context, payload *gen.SuggestExclusionPayload) (*gen.SuggestExclusionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt is required")
	}

	if s.completionClient == nil {
		s.logger.WarnContext(ctx, "completion client not configured; returning heuristic exclusion suggestion")
		return heuristicExclusionSuggestion(prompt), nil
	}

	suggestion, err := s.suggestExclusionViaLLM(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), prompt, payload.KnownRuleIds)
	if err != nil {
		s.logger.WarnContext(ctx, "openrouter exclusion suggestion failed; returning heuristic suggestion", attr.SlogError(err))
		return heuristicExclusionSuggestion(prompt), nil
	}
	return suggestion, nil
}

// exclusionSuggestionResult wraps structured exclusion fields in the
// suggestExclusion result type.
func exclusionSuggestionResult(matchType, matchValue, ruleIDFilter, sourceFilter string) *gen.SuggestExclusionResult {
	return &gen.SuggestExclusionResult{
		MatchType:    matchType,
		MatchValue:   matchValue,
		RuleIDFilter: conv.PtrEmpty(ruleIDFilter),
		SourceFilter: conv.PtrEmpty(sourceFilter),
	}
}

// heuristicExclusionSuggestion is the deterministic fallback when the LLM is
// unavailable: treat the prompt as the literal value to suppress. Usually
// wrong as-is, but it prefills an editable expression rather than dead-ending
// the operator (mirrors heuristicCustomRuleSuggestion).
func heuristicExclusionSuggestion(prompt string) *gen.SuggestExclusionResult {
	return exclusionSuggestionResult("exact", strings.TrimSpace(prompt), "", "")
}

// heuristicCustomRuleSuggestion is the deterministic fallback when the LLM
// is unavailable (no provisioned key, transport failure, etc). It produces
// a usable starting point so the operator can finish the form rather than
// hit a dead end.
func heuristicCustomRuleSuggestion(prompt string, existingIDs []string) *gen.SuggestCustomDetectionRuleResult {
	slug := slugifyPrompt(prompt)
	if slug == "" {
		slug = "rule"
	}
	ruleID := "custom." + slug
	if slices.Contains(existingIDs, ruleID) {
		ruleID = ruleID + "_" + time.Now().UTC().Format("20060102150405")
	}

	title := titleizeSlug(slug)
	if title == "" {
		title = "Custom Rule"
	}

	return &gen.SuggestCustomDetectionRuleResult{
		RuleID:        ruleID,
		Title:         title,
		Description:   strings.TrimSpace(prompt),
		DetectionExpr: nil,
		Regex:         "",
		Severity:      "medium",
	}
}

var slugStripRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugifyPrompt(prompt string) string {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	slug := slugStripRE.ReplaceAllString(lower, "_")
	slug = strings.Trim(slug, "_")
	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.TrimRight(slug, "_")
	}
	return slug
}

func titleizeSlug(slug string) string {
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "_")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i >= 5 {
			break
		}
		if p == "" {
			continue
		}
		out = append(out, strings.ToUpper(p[:1])+p[1:])
	}
	return strings.Join(out, " ")
}

// Severity returned by the LLM is constrained by the JSON schema; we still
// enforce membership in case the model strays.
var customRuleSeverityAllow = map[string]bool{
	"info":     true,
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

var customRuleIDPattern = regexp.MustCompile(`^custom\.[a-z0-9_]+$`)

func validateCustomRuleIDs(ids []string) error {
	for _, id := range ids {
		if !strings.HasPrefix(id, "custom.") {
			return oops.E(oops.CodeInvalid, nil, "custom rule id %q must start with custom.", id)
		}
	}
	return nil
}

func validateMessageTypes(messageTypes []string) error {
	for _, messageType := range messageTypes {
		if message.IsTypeValid(messageType) {
			continue
		}
		return oops.E(
			oops.CodeInvalid,
			nil,
			"message_type %q must be one of: %s",
			messageType,
			strings.Join(message.AllTypes(), ", "),
		)
	}
	return nil
}

func validateCustomDetectionRule(eng *celenv.Engine, ruleID, title, detectionExpr, severity string) error {
	if !customRuleIDPattern.MatchString(ruleID) {
		return oops.E(oops.CodeInvalid, nil, "rule_id must match custom.[a-z0-9_]+")
	}
	return validateCustomDetectionRuleFields(eng, title, detectionExpr, severity)
}

func validateCustomDetectionRuleFields(eng *celenv.Engine, title, detectionExpr, severity string) error {
	if title == "" {
		return oops.E(oops.CodeInvalid, nil, "title must not be empty")
	}
	// A custom rule is its detection predicate, so an empty expression is
	// rejected rather than saved as an inert (never-firing) rule. This also makes
	// an update that omits detection_expr fail loudly instead of silently
	// clearing the existing predicate.
	if strings.TrimSpace(detectionExpr) == "" {
		return oops.E(oops.CodeInvalid, nil, "detection_expr must not be empty")
	}
	if err := validateExpr(eng, detectionExpr); err != nil {
		return oops.E(oops.CodeInvalid, err, "detection_expr is invalid")
	}
	if !customRuleSeverityAllow[severity] {
		return oops.E(oops.CodeInvalid, nil, "severity must be one of info, low, medium, high, critical")
	}
	return nil
}

// validateExpr compiles a CEL predicate (scope or detection) so an invalid
// expression is rejected at save time. Empty is valid.
func validateExpr(eng *celenv.Engine, expr string) error {
	if strings.TrimSpace(expr) == "" {
		return nil
	}
	if _, err := eng.Compile(expr); err != nil {
		return fmt.Errorf("compile cel: %w", err)
	}
	return nil
}

// validateScopeExpr validates an optional CEL scope predicate from a payload.
func validateScopeExpr(eng *celenv.Engine, expr *string) error {
	if expr == nil {
		return nil
	}
	return validateExpr(eng, *expr)
}

func (s *Service) suggestCustomRuleViaLLM(ctx context.Context, orgID, projectID, userID, userEmail, userPrompt string, existingIDs []string) (*gen.SuggestCustomDetectionRuleResult, error) {
	systemPrompt := `You are a security-rules assistant for a runtime risk detection product.

Given a single natural-language description of what an operator wants to detect, return a JSON object the dashboard uses to prefill a "create custom detection rule" form. The rule matches an agent message via a CEL (Common Expression Language) boolean expression in "detection_expr".

Fields:
- "rule_id": starts with the literal prefix "custom." and contains only [a-z0-9_]. A stable, descriptive slug (e.g. "custom.acme_internal_token"). Must NOT appear in existing_rule_ids.
- "title": 2-6 words, title case.
- "description": 1-2 sentences on what is detected and why it matters. No marketing copy.
- "severity": one of "info","low","medium","high","critical" — by leakage/impact cost (credentials, PII, financial, healthcare are typically high/critical).
- "detection_expr": a CEL boolean expression that evaluates to true when a message matches.

CEL environment for "detection_expr":
- Message body fields (each auto-scoped to the right message type — reference them directly, no need to check "kind"):
  - content   — the message's raw text body, any message type.
  - prompt    — the body of a user message (empty otherwise).
  - assistant — the body of an assistant message (empty otherwise).
  - tool_result — the output of a tool response message (empty otherwise). Singular: one response carries one tool's output.
- tool_calls — the tool calls on a tool-request message (plural: one request can fan out parallel calls). Iterate with tool_calls.exists(t, <predicate on t>). Each t has correlated fields: t.name (raw tool-call name, e.g. mcp__mise__run_task), t.server (MCP server name, "" for native tools like Bash), t.function (bare function name, e.g. run_task), t.args (the raw tool arguments JSON).
- kind — message type string (user_message, assistant_message, tool_request, tool_response). Usually unnecessary because the body fields are already auto-scoped.

Matchers (call as a method on a field; all return bool):
- field.matchRegex(pattern)  — RE2 regex match. Use for secret/PII/text patterns.
- field.matchText(substr)    — case-insensitive substring.
- field.matchExact(value)    — exact equality ("" matches native tools for t.server).
- field.matchPrefix(s) / field.matchSuffix(s) — prefix / suffix match.
- field.matchGlob(pattern)   — glob over the whole value (e.g. *_exec, shell:*).
- field.present()            — the field has a non-empty value.
- field.get(path)            — drill into a field's JSON at a gjson path (command, payload.sql, items.0.ssn) and return a sub-field the matchers compose over. Use on t.args to inspect tool arguments.

Compose with && (and), || (or), ! (not), and parentheses.

Examples:
- prompt.matchText("password")
- content.matchRegex("sk-[A-Za-z0-9]{32}")
- tool_calls.exists(t, t.server.matchExact("shell"))
- tool_calls.exists(t, t.function.matchGlob("*_exec"))
- tool_calls.exists(t, t.function.matchRegex("bash") && t.args.get("command").matchRegex("DROP TABLE"))
- tool_result.get("error").present()

Output ONLY the JSON object. No prose, no markdown fences.`

	existingList := strings.Join(existingIDs, ", ")
	if existingList == "" {
		existingList = "(none)"
	}
	userMessage := fmt.Sprintf("Operator request: %s\n\nExisting rule ids (avoid colliding): %s", userPrompt, existingList)

	strict := false
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rule_id":        map[string]any{"type": "string", "pattern": "^custom\\.[a-z0-9_]+$"},
			"title":          map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
			"description":    map[string]any{"type": "string", "minLength": 1, "maxLength": 400},
			"severity":       map[string]any{"type": "string", "enum": []string{"info", "low", "medium", "high", "critical"}},
			"detection_expr": map[string]any{"type": "string", "minLength": 1, "maxLength": 1000},
		},
		"required":             []string{"rule_id", "title", "description", "severity", "detection_expr"},
		"additionalProperties": false,
	}

	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        "custom_detection_rule_suggestion",
		Schema:      schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}

	suggestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	temperature := 0.2
	response, err := s.completionClient.GetObjectCompletion(suggestCtx, openrouter.ObjectCompletionRequest{
		OrgID:        orgID,
		ProjectID:    projectID,
		Model:        "",
		SystemPrompt: systemPrompt,
		Prompt:       userMessage,
		Temperature:  &temperature,
		UsageSource:  billing.ModelUsageSourceGram,
		KeyType:      openrouter.KeyTypeInternal,
		KeySlot:      "",
		// The admin who asked for the suggestion — this completion is
		// user-initiated, so usage attributes to them, not "(unset)". (cubic)
		UserID:         userID,
		ExternalUserID: "",
		UserEmail:      userEmail,
		HTTPMetadata:   nil,
		JSONSchema:     &jsonSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouter object completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return nil, fmt.Errorf("empty completion response")
	}

	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if raw == "" {
		return nil, fmt.Errorf("empty completion content")
	}

	var parsed struct {
		RuleID        string `json:"rule_id"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		Severity      string `json:"severity"`
		DetectionExpr string `json:"detection_expr"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	parsed.RuleID = strings.TrimSpace(parsed.RuleID)
	parsed.Title = strings.TrimSpace(parsed.Title)
	parsed.Description = strings.TrimSpace(parsed.Description)
	parsed.Severity = strings.ToLower(strings.TrimSpace(parsed.Severity))

	if !strings.HasPrefix(parsed.RuleID, "custom.") || !customRuleIDPattern.MatchString(parsed.RuleID) {
		return nil, fmt.Errorf("model returned invalid rule_id %q", parsed.RuleID)
	}
	parsed.DetectionExpr = strings.TrimSpace(parsed.DetectionExpr)
	if parsed.DetectionExpr == "" {
		return nil, fmt.Errorf("model returned empty detection_expr")
	}
	if err := validateExpr(s.celEng, parsed.DetectionExpr); err != nil {
		return nil, fmt.Errorf("model returned invalid detection_expr: %w", err)
	}
	if !customRuleSeverityAllow[parsed.Severity] {
		parsed.Severity = "medium"
	}
	if slices.Contains(existingIDs, parsed.RuleID) {
		parsed.RuleID = parsed.RuleID + "_" + time.Now().UTC().Format("20060102150405")
	}

	return &gen.SuggestCustomDetectionRuleResult{
		RuleID:        parsed.RuleID,
		Title:         parsed.Title,
		Description:   parsed.Description,
		DetectionExpr: conv.PtrEmpty(parsed.DetectionExpr),
		Regex:         "",
		Severity:      parsed.Severity,
	}, nil
}

// Match types the exclusion suggestion may return; mirrors the enum the
// create/update exclusion payloads accept (shared.RiskExclusionMatchTypeEnum).
var exclusionMatchTypeAllow = map[string]bool{
	"exact":       true,
	"regex":       true,
	"rule_id":     true,
	"source":      true,
	"entity_type": true,
}

func (s *Service) suggestExclusionViaLLM(ctx context.Context, orgID, projectID, userID, userEmail, userPrompt string, knownRuleIDs []string) (*gen.SuggestExclusionResult, error) {
	systemPrompt := `You are a security-rules assistant for a runtime risk detection product.

Given a single natural-language description of findings an operator wants to stop flagging, return a JSON object the dashboard uses to prefill a "create exclusion" form. An exclusion suppresses matching findings retroactively and going forward.

Each finding carries: the matched text ("match", e.g. the detected email address or token), the id of the rule that flagged it ("rule_id", e.g. "pii.email_address", "secret.aws_access_token", "custom.acme_token"), and the detector source ("source", e.g. "gitleaks", "presidio", "prompt_injection", "custom").

Fields:
- "match_type": how match_value is compared, one of:
  - "exact"       — the finding's matched text equals match_value. Use when the operator names one specific value.
  - "regex"       — match_value is an RE2 regex (max 512 chars) matched against the finding's matched text. Use for families of values (test accounts, sandbox tokens, name variants). No lookarounds or backreferences (unsupported in RE2).
  - "rule_id"     — suppress every finding from the rule id in match_value.
  - "source"      — suppress every finding from the detector source in match_value.
  - "entity_type" — suppress a Presidio PII entity type; match_value is the UPPER_SNAKE entity name (e.g. "EMAIL_ADDRESS").
- "match_value": the value compared per match_type.
- "rule_id_filter": only suppress when the finding's rule_id equals this — use it to narrow an exact/regex match to one rule. Empty string means any rule.
- "source_filter": only suppress when the finding's source equals this. Empty string means any source.

Prefer the narrowest exclusion that satisfies the request: an exact value over a regex, and set rule_id_filter when the operator names a specific rule or data type. The known rule ids are provided for choosing rule_id values and filters.

Output ONLY the JSON object. No prose, no markdown fences.`

	knownList := strings.Join(knownRuleIDs, ", ")
	if knownList == "" {
		knownList = "(none)"
	}
	userMessage := fmt.Sprintf("Operator request: %s\n\nKnown rule ids: %s", userPrompt, knownList)

	strict := false
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"match_type":     map[string]any{"type": "string", "enum": []string{"exact", "regex", "rule_id", "source", "entity_type"}},
			"match_value":    map[string]any{"type": "string", "minLength": 1, "maxLength": exclusionRegexMaxLength},
			"rule_id_filter": map[string]any{"type": "string", "maxLength": 200},
			"source_filter":  map[string]any{"type": "string", "maxLength": 200},
		},
		"required":             []string{"match_type", "match_value", "rule_id_filter", "source_filter"},
		"additionalProperties": false,
	}

	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        "risk_exclusion_suggestion",
		Schema:      schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}

	suggestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	temperature := 0.2
	response, err := s.completionClient.GetObjectCompletion(suggestCtx, openrouter.ObjectCompletionRequest{
		OrgID:        orgID,
		ProjectID:    projectID,
		Model:        "",
		SystemPrompt: systemPrompt,
		Prompt:       userMessage,
		Temperature:  &temperature,
		UsageSource:  billing.ModelUsageSourceGram,
		KeyType:      openrouter.KeyTypeInternal,
		KeySlot:      "",
		// The admin who asked for the suggestion — this completion is
		// user-initiated, so usage attributes to them, not "(unset)".
		UserID:         userID,
		ExternalUserID: "",
		UserEmail:      userEmail,
		HTTPMetadata:   nil,
		JSONSchema:     &jsonSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouter object completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return nil, fmt.Errorf("empty completion response")
	}

	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if raw == "" {
		return nil, fmt.Errorf("empty completion content")
	}

	var parsed struct {
		MatchType    string `json:"match_type"`
		MatchValue   string `json:"match_value"`
		RuleIDFilter string `json:"rule_id_filter"`
		SourceFilter string `json:"source_filter"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	parsed.MatchType = strings.ToLower(strings.TrimSpace(parsed.MatchType))
	parsed.MatchValue = strings.TrimSpace(parsed.MatchValue)
	parsed.RuleIDFilter = strings.TrimSpace(parsed.RuleIDFilter)
	parsed.SourceFilter = strings.TrimSpace(parsed.SourceFilter)

	if !exclusionMatchTypeAllow[parsed.MatchType] {
		return nil, fmt.Errorf("model returned invalid match_type %q", parsed.MatchType)
	}
	// Same gate the create/update exclusion handlers apply: non-empty value,
	// and a regex must compile (RE2) and fit the length cap.
	if err := validateExclusionMatchValue(parsed.MatchType, parsed.MatchValue); err != nil {
		return nil, fmt.Errorf("model returned invalid match_value: %w", err)
	}

	return exclusionSuggestionResult(parsed.MatchType, parsed.MatchValue, parsed.RuleIDFilter, parsed.SourceFilter), nil
}

// TestDetectionRule runs a single detection rule against pasted sample text
// and returns its matches. The handler dispatches to the same scanners the
// worker uses during chat-message analysis (gitleaks for secrets.*, the
// configured PIIScanner for pii.*, the prompt-injection scanner for
// prompt_injection.*, and a regex matcher for custom.*) so the playground
// output mirrors what would be recorded as a risk_result in production.
//
// shadow_mcp.* and destructive_tool.* are inherently tool-call shaped —
// they have no text-only detector — so the handler returns supported:false
// for them rather than fabricating a match.
func (s *Service) TestDetectionRule(ctx context.Context, payload *gen.TestDetectionRulePayload) (*gen.TestDetectionRuleResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	ruleID := strings.TrimSpace(payload.RuleID)
	text := payload.Text
	if ruleID == "" || text == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "rule_id and text are required")
	}

	switch {
	case strings.HasPrefix(ruleID, "secret."):
		return s.testGitleaksRule(ctx, ruleID, text)
	case strings.HasPrefix(ruleID, "pii."):
		return s.testPresidioRule(ctx, ruleID, text)
	case ruleID == "prompt_injection.default" || strings.HasPrefix(ruleID, "prompt_injection."):
		return s.testPromptInjectionRule(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), text)
	case strings.HasPrefix(ruleID, "custom."):
		return s.testCustomRule(ruleID, conv.PtrValOr(payload.DetectionExpr, ""), text)
	default:
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("This rule has no text-only detector. Playground requires gitleaks/presidio/prompt-injection/custom rule families."),
		}, nil
	}
}

// EvaluatePromptGuardrail replays an inline guardrail without persisting findings.
func (s *Service) EvaluatePromptGuardrail(ctx context.Context, payload *gen.EvaluatePromptGuardrailPayload) (*gen.PromptGuardrailEvalResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt is required")
	}
	chatID, err := uuid.Parse(payload.ChatID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat_id")
	}
	projectID := *authCtx.ProjectID

	modelConfig, err := marshalModelConfig(payload.ModelConfig)
	if err != nil {
		return nil, err
	}
	cfg := promptpolicy.ParseConfig(modelConfig)

	return s.evaluateGuardrailForChat(
		ctx,
		projectID,
		authCtx.ActiveOrganizationID,
		chatID,
		prompt,
		cfg,
		payload.MessageTypes,
		conv.PtrValOr(payload.ScopeInclude, ""),
		conv.PtrValOr(payload.ScopeExempt, ""),
	)
}

func (s *Service) evaluateGuardrailForChat(
	ctx context.Context,
	projectID uuid.UUID,
	orgID string,
	chatID uuid.UUID,
	prompt string,
	cfg promptpolicy.Config,
	messageTypes []string,
	includeCEL string,
	exemptCEL string,
) (*gen.PromptGuardrailEvalResult, error) {
	// GetChat filters soft-deleted chats.
	chatRepo := chatrepo.New(s.db)
	chatRow, err := chatRepo.GetChat(ctx, chatID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "chat not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load chat").LogError(ctx, s.logger)
	case chatRow.ProjectID != projectID:
		return nil, oops.C(oops.CodeNotFound)
	}

	rows, err := chatRepo.ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load chat messages").LogError(ctx, s.logger)
	}

	messages := make([]ra.EvalMessage, len(rows))
	for i, row := range rows {
		messages[i] = ra.EvalMessage{
			ID:        row.ID,
			Role:      row.Role,
			Content:   row.Content,
			ToolCalls: row.ToolCalls,
		}
	}

	verdicts, err := ra.EvalPromptGuardrail(
		ctx,
		s.logger,
		s.promptJudge,
		s.celEng,
		orgID,
		projectID.String(),
		prompt,
		cfg,
		messages,
		messageTypes,
		includeCEL,
		exemptCEL,
	)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid scope expression")
	}

	out := make([]*gen.PromptGuardrailMessageVerdict, 0, len(verdicts))
	flagged := false
	totalCostUSD := 0.0
	var totalLatencyMs int64
	for _, v := range verdicts {
		row := rows[v.Index]
		if v.Matched {
			flagged = true
		}
		totalCostUSD += v.CostUSD
		totalLatencyMs += v.LatencyMs
		out = append(out, &gen.PromptGuardrailMessageVerdict{
			MessageID:        row.ID.String(),
			Seq:              row.Seq,
			MessageType:      v.Type,
			ToolName:         conv.PtrEmpty(v.ToolName),
			Matched:          v.Matched,
			Confidence:       v.Confidence,
			Rationale:        v.Rationale,
			LatencyMs:        v.LatencyMs,
			CostUsd:          v.CostUSD,
			PromptTokens:     v.PromptTokens,
			CompletionTokens: v.CompletionTokens,
			TotalTokens:      v.TotalTokens,
		})
	}

	return &gen.PromptGuardrailEvalResult{
		ChatID:         chatID.String(),
		Flagged:        flagged,
		JudgedCount:    len(out),
		TotalCostUsd:   totalCostUSD,
		TotalLatencyMs: totalLatencyMs,
		Verdicts:       out,
	}, nil
}

// SaveRiskEvalReview records one review verdict in the policy regression set.
func (s *Service) SaveRiskEvalReview(ctx context.Context, payload *gen.SaveRiskEvalReviewPayload) (*types.RiskPolicyEvalReview, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	policyID, err := uuid.Parse(payload.PolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy_id")
	}
	chatID, err := uuid.Parse(payload.ChatID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat_id")
	}
	projectID := *authCtx.ProjectID

	policy, err := s.requirePromptPolicy(ctx, policyID, projectID)
	if err != nil {
		return nil, err
	}

	chatBelongs, err := s.repo.RiskEvalChatBelongsToProject(ctx, repo.RiskEvalChatBelongsToProjectParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "validate eval review chat").LogError(ctx, s.logger)
	}
	if !chatBelongs {
		return nil, oops.E(oops.CodeNotFound, nil, "chat not found")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin eval review save").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	row, err := q.UpsertRiskPolicyEvalReview(ctx, repo.UpsertRiskPolicyEvalReviewParams{
		ProjectID:         projectID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: policy.Version,
		ChatID:            chatID,
		Verdict:           payload.Verdict,
		ReviewedBy:        authCtx.UserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save eval review").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskPolicyEvalReviewSave(ctx, dbtx, audit.LogRiskPolicyEvalReviewEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     policy.ID,
		RiskPolicyName:   policy.Name,
		Metadata: &audit.RiskPolicyEvalReviewMetadata{
			ReviewID:   row.ID.String(),
			ChatID:     chatID.String(),
			Verdict:    row.Verdict,
			ReviewedBy: row.ReviewedBy,
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log eval review save").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit eval review save").LogError(ctx, s.logger)
	}
	return evalReviewToType(row), nil
}

// ListRiskEvalReviews returns the active regression set for a policy: every
// reviewer's current verdicts.
func (s *Service) ListRiskEvalReviews(ctx context.Context, payload *gen.ListRiskEvalReviewsPayload) (*gen.ListRiskEvalReviewsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	policyID, err := uuid.Parse(payload.PolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy_id")
	}

	rows, err := s.repo.ListRiskPolicyEvalReviews(ctx, repo.ListRiskPolicyEvalReviewsParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policyID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list eval reviews").LogError(ctx, s.logger)
	}

	reviews := make([]*types.RiskPolicyEvalReview, 0, len(rows))
	for _, row := range rows {
		reviews = append(reviews, evalReviewToType(row))
	}
	return &gen.ListRiskEvalReviewsResult{Reviews: reviews}, nil
}

// DeleteRiskEvalReview clears the current reviewer's verdict.
func (s *Service) DeleteRiskEvalReview(ctx context.Context, payload *gen.DeleteRiskEvalReviewPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	policyID, err := uuid.Parse(payload.PolicyID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid policy_id")
	}
	chatID, err := uuid.Parse(payload.ChatID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid chat_id")
	}
	projectID := *authCtx.ProjectID

	policy, err := s.requirePromptPolicy(ctx, policyID, projectID)
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin eval review delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	row, err := q.SoftDeleteRiskPolicyEvalReview(ctx, repo.SoftDeleteRiskPolicyEvalReviewParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
		ChatID:       chatID,
		ReviewedBy:   authCtx.UserID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "delete eval review").LogError(ctx, s.logger)
	}
	if err == nil {
		if err := s.audit.LogRiskPolicyEvalReviewDelete(ctx, dbtx, audit.LogRiskPolicyEvalReviewEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        projectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			RiskPolicyID:     policy.ID,
			RiskPolicyName:   policy.Name,
			Metadata: &audit.RiskPolicyEvalReviewMetadata{
				ReviewID:   row.ID.String(),
				ChatID:     chatID.String(),
				Verdict:    row.Verdict,
				ReviewedBy: row.ReviewedBy,
			},
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log eval review delete").LogError(ctx, s.logger)
		}
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit eval review delete").LogError(ctx, s.logger)
	}
	return nil
}

// requirePromptPolicy loads a policy scoped to the project and verifies it is
// prompt_based (the only kind evals apply to). Returns not-found when missing.
func (s *Service) requirePromptPolicy(ctx context.Context, policyID, projectID uuid.UUID) (repo.RiskPolicy, error) {
	policy, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.RiskPolicy{}, oops.E(oops.CodeNotFound, err, "policy not found")
	case err != nil:
		return repo.RiskPolicy{}, oops.E(oops.CodeUnexpected, err, "load policy").LogError(ctx, s.logger)
	case policy.PolicyType != ra.PolicyTypePromptBased:
		return repo.RiskPolicy{}, oops.E(oops.CodeInvalid, nil, "evals apply only to prompt_based policies")
	}
	return policy, nil
}

func evalReviewToType(row repo.RiskPolicyEvalReview) *types.RiskPolicyEvalReview {
	return &types.RiskPolicyEvalReview{
		ID:            row.ID.String(),
		PolicyID:      row.RiskPolicyID.String(),
		PolicyVersion: row.RiskPolicyVersion,
		ChatID:        row.ChatID.String(),
		Verdict:       row.Verdict,
		ReviewedBy:    row.ReviewedBy,
		CreatedAt:     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func (s *Service) testGitleaksRule(ctx context.Context, ruleID, text string) (*gen.TestDetectionRuleResult, error) {
	findings, err := s.gitleaksScanner.Scan(ctx, text)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run gitleaks").LogError(ctx, s.logger)
	}
	matches := make([]*gen.TestDetectionRuleMatch, 0, len(findings))
	for _, f := range findings {
		if f.RuleID != ruleID {
			continue
		}
		matches = append(matches, findingToMatch(f))
	}
	return &gen.TestDetectionRuleResult{
		Matches:   matches,
		Supported: true,
		Reason:    nil,
	}, nil
}

func (s *Service) testPresidioRule(ctx context.Context, ruleID, text string) (*gen.TestDetectionRuleResult, error) {
	if s.piiScanner == nil {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("PII scanner is not configured on this server."),
		}, nil
	}
	entity := strings.ToUpper(strings.TrimPrefix(ruleID, "pii."))
	// No policy context here; apply the default threshold (0 resolves to it).
	batches, err := s.piiScanner.AnalyzeBatch(ctx, []string{text}, []string{entity}, ra.DefaultPresidioScoreThreshold, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run presidio").LogError(ctx, s.logger)
	}
	matches := make([]*gen.TestDetectionRuleMatch, 0)
	if len(batches) > 0 {
		for _, f := range batches[0] {
			if f.DeadLetterReason != "" {
				continue
			}
			if f.RuleID != ruleID {
				continue
			}
			matches = append(matches, findingToMatch(f))
		}
	}
	return &gen.TestDetectionRuleResult{
		Matches:   matches,
		Supported: true,
		Reason:    nil,
	}, nil
}

func (s *Service) testPromptInjectionRule(ctx context.Context, orgID, projectID, text string) (*gen.TestDetectionRuleResult, error) {
	if s.piScanner == nil {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("Prompt-injection scanner is not configured on this server."),
		}, nil
	}
	findings, err := s.piScanner.Scan(ctx, text, orgID, projectID, "", judgemessage.New(message.User, "", text))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run prompt-injection scanner").LogError(ctx, s.logger)
	}
	matches := make([]*gen.TestDetectionRuleMatch, 0, len(findings))
	for _, f := range findings {
		if f.DeadLetterReason != "" {
			continue
		}
		matches = append(matches, findingToMatch(f))
	}
	return &gen.TestDetectionRuleResult{
		Matches:   matches,
		Supported: true,
		Reason:    nil,
	}, nil
}

func (s *Service) testCustomRule(ruleID, detectionExpr, text string) (*gen.TestDetectionRuleResult, error) {
	detectionExpr = strings.TrimSpace(detectionExpr)
	if detectionExpr == "" {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("Custom rules require a detection_expr expression. Custom rules are stored client-side, so pass one in the request body."),
		}, nil
	}

	eng := s.celEng
	// The playground only has pasted text, so it evaluates against a user
	// message (content/prompt populated, no tool calls). Tool-targeted rules
	// simply won't match here; save them and run analysis to see matches.
	compiled, err := ra.CompileCELRules(eng, []customrules.Rule{{
		RuleID:        ruleID,
		Title:         "",
		Description:   "Custom rule match",
		DetectionExpr: detectionExpr,
		Regex:         "",
	}})
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid detection_expr")
	}

	findings, err := ra.ScanCELRules(eng, ra.MessageView{Content: text, Type: message.User, Tools: []ra.ToolView{}}, compiled)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "evaluate detection rule")
	}
	matches := make([]*gen.TestDetectionRuleMatch, 0, len(findings))
	for _, f := range findings {
		matches = append(matches, findingToMatch(f))
	}
	return &gen.TestDetectionRuleResult{
		Matches:   matches,
		Supported: true,
		Reason:    nil,
	}, nil
}

func findingToMatch(f scanners.Finding) *gen.TestDetectionRuleMatch {
	return &gen.TestDetectionRuleMatch{
		RuleID:      f.RuleID,
		Description: new(f.Description),
		Match:       f.Match,
		StartPos:    f.StartPos,
		EndPos:      f.EndPos,
		Source:      f.Source,
		Confidence:  f.Confidence,
		Tags:        f.Tags,
	}
}

// policyToType converts a database row to the API type, enriching it with
// message counts.
func (s *Service) policyToType(ctx context.Context, row repo.RiskPolicy) (*types.RiskPolicy, error) {
	totalMessages, err := s.repo.CountTotalMessages(ctx, uuid.NullUUID{UUID: row.ProjectID, Valid: true})
	if err != nil {
		totalMessages = 0
	}

	analyzedMessages, err := s.repo.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         row.ProjectID,
		RiskPolicyID:      row.ID,
		RiskPolicyVersion: row.Version,
	})
	if err != nil {
		analyzedMessages = 0
	}
	pendingMessages := max(totalMessages-analyzedMessages, 0)

	audiencePrincipalURNs, err := riskPolicyAudiencePrincipalURNs(ctx, s.db, row.OrganizationID, row.ID.String())
	if err != nil {
		return nil, fmt.Errorf("load risk policy audience: %w", err)
	}

	return &types.RiskPolicy{
		ID:                     row.ID.String(),
		ProjectID:              row.ProjectID.String(),
		Name:                   row.Name,
		PolicyType:             row.PolicyType,
		Sources:                row.Sources,
		PresidioEntities:       row.PresidioEntities,
		PresidioScoreThreshold: ra.PresidioScoreThresholdPtr(row.AnalyzerConfig),
		ApprovedEmailDomains:   ra.ApprovedEmailDomainsFromConfig(row.AnalyzerConfig),
		PromptInjectionRules:   row.PromptInjectionRules,
		DisabledRules:          row.DisabledRules,
		CustomRuleIds:          row.CustomRuleIds,
		MessageTypes:           row.MessageTypes,
		ScopeInclude:           conv.FromPGText[string](row.ScopeInclude),
		ScopeExempt:            conv.FromPGText[string](row.ScopeExempt),
		Enabled:                row.Enabled,
		Action:                 row.Action,
		AudienceType:           row.AudienceType,
		AudiencePrincipalUrns:  audiencePrincipalURNs,
		AutoName:               row.AutoName,
		UserMessage:            conv.FromPGText[string](row.UserMessage),
		Prompt:                 conv.FromPGText[string](row.Prompt),
		ModelConfig:            unmarshalModelConfig(row.ModelConfig),
		Score:                  row.Score,
		Version:                row.Version,
		CreatedAt:              row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:        pendingMessages,
		TotalMessages:          totalMessages,
	}, nil
}

func policyRowSnapshotWithAudience(row repo.RiskPolicy, audiencePrincipalURNs []string) *types.RiskPolicy {
	if audiencePrincipalURNs == nil {
		audiencePrincipalURNs = []string{}
	}
	return &types.RiskPolicy{
		ID:                     row.ID.String(),
		ProjectID:              row.ProjectID.String(),
		Name:                   row.Name,
		PolicyType:             row.PolicyType,
		Sources:                row.Sources,
		PresidioEntities:       row.PresidioEntities,
		PresidioScoreThreshold: ra.PresidioScoreThresholdPtr(row.AnalyzerConfig),
		ApprovedEmailDomains:   ra.ApprovedEmailDomainsFromConfig(row.AnalyzerConfig),
		PromptInjectionRules:   row.PromptInjectionRules,
		DisabledRules:          row.DisabledRules,
		CustomRuleIds:          row.CustomRuleIds,
		MessageTypes:           row.MessageTypes,
		ScopeInclude:           conv.FromPGText[string](row.ScopeInclude),
		ScopeExempt:            conv.FromPGText[string](row.ScopeExempt),
		Enabled:                row.Enabled,
		Action:                 row.Action,
		AudienceType:           row.AudienceType,
		AudiencePrincipalUrns:  audiencePrincipalURNs,
		AutoName:               row.AutoName,
		UserMessage:            conv.FromPGText[string](row.UserMessage),
		Prompt:                 conv.FromPGText[string](row.Prompt),
		ModelConfig:            unmarshalModelConfig(row.ModelConfig),
		Score:                  row.Score,
		Version:                row.Version,
		CreatedAt:              row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:        -1,
		TotalMessages:          -1,
	}
}

func customDetectionRuleToType(row repo.RiskCustomDetectionRule) *types.RiskCustomDetectionRule {
	return &types.RiskCustomDetectionRule{
		ID:            row.ID.String(),
		RuleID:        row.RuleID,
		Title:         row.Title,
		Description:   row.Description,
		Regex:         conv.PtrValOr(conv.FromPGText[string](row.Regex), ""),
		DetectionExpr: conv.FromPGText[string](row.DetectionExpr),
		Severity:      row.Severity,
		CreatedAt:     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func (s *Service) generatePolicyName(ctx context.Context, orgID, projectID string, sources, presidioEntities, customRuleTitles []string, action string, existingNames []string) string {
	if s.completionClient == nil {
		return s.fallbackPolicyName(sources, customRuleTitles, action)
	}

	// Policy authors think in *what* is detected, not *how* (gitleaks,
	// presidio). Translate sources to user-facing category labels and
	// scrub library names so the LLM cannot regurgitate them. See AGE-2378.
	// Custom rules are author-defined, so their human titles are passed through
	// as-is — they carry the most policy-specific signal for the name.
	categories := sourcesToCategoryLabels(sources)

	prompt := fmt.Sprintf(
		"Generate a short, human-friendly name (2-5 words) for a security policy with these settings:\n"+
			"- Detection categories: %v\n"+
			"- PII entity types: %v\n"+
			"- Custom detection rules: %v\n"+
			"- Action: %s\n"+
			"- Existing policy names to avoid: %v\n\n"+
			"Return ONLY the name, no quotes or explanation. Make it descriptive and distinct from existing names. "+
			"When custom detection rules are present, let them drive the name since they are the most specific signal. "+
			"Do not mention internal tool or library names; describe what is detected.",
		categories, presidioEntities, customRuleTitles, action, existingNames,
	)

	// Tight timeout: this runs synchronously in the API request path. If
	// OpenRouter is slow we fall back to fallbackPolicyName rather than
	// blocking the create/update RPC for long.
	nameCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	response, err := s.completionClient.GetCompletion(nameCtx, openrouter.CompletionRequest{
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageUser(prompt),
		},
		Tools:                     nil,
		Temperature:               nil,
		Model:                     "",
		Stream:                    false,
		UsageSource:               billing.ModelUsageSourceGram,
		KeyType:                   openrouter.KeyTypeInternal,
		KeySlot:                   "",
		UserID:                    "",
		ExternalUserID:            "",
		UserEmail:                 "",
		HTTPMetadata:              nil,
		APIKeyID:                  "",
		JSONSchema:                nil,
		Reasoning:                 &openrouter.Reasoning{Effort: "none", MaxTokens: nil, Exclude: nil, Enabled: nil},
		CacheControl:              nil,
		NormalizeOutboundMessages: false,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate policy name via OpenRouter", attr.SlogError(err))
		return s.fallbackPolicyName(sources, customRuleTitles, action)
	}

	name := strings.TrimSpace(openrouter.GetText(*response.Message))
	if name == "" {
		return s.fallbackPolicyName(sources, customRuleTitles, action)
	}

	// Truncate to 100 chars
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}

	return name
}

// customRuleTitlesForIDs resolves selected custom rule ids to their
// human-friendly titles for the auto-namer. Unknown ids and rules without a
// title fall back to the bare id so the name still reflects them. Returns nil
// on lookup failure (the namer simply omits custom rules).
func (s *Service) customRuleTitlesForIDs(ctx context.Context, projectID uuid.UUID, ruleIDs []string) []string {
	if len(ruleIDs) == 0 {
		return nil
	}
	rows, err := s.repo.ListCustomDetectionRules(ctx, projectID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load custom rules for policy name", attr.SlogError(err))
		return nil
	}
	titleByID := make(map[string]string, len(rows))
	for _, r := range rows {
		titleByID[r.RuleID] = r.Title
	}
	out := make([]string, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		if title := strings.TrimSpace(titleByID[id]); title != "" {
			out = append(out, title)
		} else {
			out = append(out, id)
		}
	}
	return out
}

// sourcesToCategoryLabels maps internal source identifiers (gitleaks,
// presidio, …) to the user-facing detection category they implement.
// Library names are an implementation detail; policy authors think in
// what is detected, not how. See AGE-2378.
func sourcesToCategoryLabels(sources []string) []string {
	out := make([]string, 0, len(sources))
	for _, src := range sources {
		switch src {
		case ra.SourceGitleaks:
			out = append(out, "Secrets")
		case ra.SourcePresidio:
			out = append(out, "PII")
		case shadowmcp.SourceShadowMCP:
			out = append(out, "Shadow MCP")
		case shadowmcp.SourceDestructiveTool:
			out = append(out, "Destructive Tool")
		case ra.SourceCLIDestructive:
			out = append(out, "Destructive CLI Command")
		case ra.SourcePromptInjection:
			out = append(out, "Prompt Injection")
		case ra.SourceAccountIdentity:
			out = append(out, "Non-Corporate Account")
		}
	}
	return out
}

func (s *Service) fallbackPolicyName(sources, customRuleTitles []string, action string) string {
	parts := sourcesToCategoryLabels(sources)
	// Singularize the leading "Secrets" label when used in a name like
	// "Secret Blocker" — preserves the previous look of fallback names.
	for i, p := range parts {
		if p == "Secrets" {
			parts[i] = "Secret"
		}
	}
	// Fold in custom rule titles so a custom-only policy still names what it
	// detects. Cap the combined label list so the fallback name stays short.
	parts = append(parts, customRuleTitles...)
	const maxParts = 2
	if len(parts) > maxParts {
		parts = parts[:maxParts]
	}
	if len(parts) == 0 {
		parts = append(parts, "Risk")
	}

	actionLabel := "Scanner"
	switch action {
	case "block":
		actionLabel = "Blocker"
	case "warn":
		actionLabel = "Warner"
	}

	return strings.Join(parts, " & ") + " " + actionLabel
}

func (s *Service) generatePromptPolicyName(ctx context.Context, orgID, projectID, prompt string, existingNames []string) string {
	fallback := fallbackPromptPolicyName(prompt, existingNames)
	if s.completionClient == nil {
		return fallback
	}

	namePrompt := fmt.Sprintf(
		"Generate a short, human-friendly name (2-5 words) for a prompt-based security policy.\n"+
			"- Guardrail prompt: %q\n"+
			"- Existing policy names to avoid: %v\n\n"+
			"Return ONLY the name, no quotes or explanation. Make it descriptive and distinct from existing names. "+
			"Name what the policy is trying to catch or prevent.",
		prompt, existingNames,
	)

	// Tight timeout: this runs synchronously in the API request path. If
	// OpenRouter is slow we fall back to a deterministic prompt-derived name.
	nameCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	response, err := s.completionClient.GetCompletion(nameCtx, openrouter.CompletionRequest{
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageUser(namePrompt),
		},
		Tools:                     nil,
		Temperature:               nil,
		Model:                     "",
		Stream:                    false,
		UsageSource:               billing.ModelUsageSourceGram,
		KeyType:                   openrouter.KeyTypeInternal,
		KeySlot:                   "",
		UserID:                    "",
		ExternalUserID:            "",
		UserEmail:                 "",
		HTTPMetadata:              nil,
		APIKeyID:                  "",
		JSONSchema:                nil,
		Reasoning:                 &openrouter.Reasoning{Effort: "none", MaxTokens: nil, Exclude: nil, Enabled: nil},
		CacheControl:              nil,
		NormalizeOutboundMessages: false,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate prompt policy name via OpenRouter", attr.SlogError(err))
		return fallback
	}
	if response == nil || response.Message == nil {
		return fallback
	}

	name := strings.TrimSpace(openrouter.GetText(*response.Message))
	if name == "" {
		return fallback
	}

	return promptPolicyNameFromBase(name, existingNames)
}

func validateAction(action string) error {
	switch action {
	case "flag", "block", "warn":
		return nil
	default:
		return oops.E(oops.CodeInvalid, nil, "action must be one of: flag, warn, block")
	}
}

func validateSources(sources []string) error {
	for _, src := range sources {
		switch src {
		case ra.SourceGitleaks, ra.SourcePresidio, shadowmcp.SourceShadowMCP, shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive, ra.SourcePromptInjection, ra.SourceAccountIdentity:
		default:
			return oops.E(oops.CodeInvalid, nil, "source %q is not a recognized policy source", src)
		}
	}
	return nil
}

func validateSourceAction(sources []string, action string) error {
	// warn (challenge) can end in a block, so it is subject to the same
	// flag-only-source constraint as block: only "flag" is unconstrained.
	if action == "flag" {
		return nil
	}
	for _, src := range []string{shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive, ra.SourceAccountIdentity} {
		if slices.Contains(sources, src) {
			return oops.E(oops.CodeInvalid, nil, "source %q supports flagging only", src)
		}
	}
	return nil
}

// approvedDomainFormat matches a plausible DNS domain: LDH (letters, digits,
// hyphen) labels joined by dots, at least two labels.
var approvedDomainFormat = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// validateApprovedEmailDomains normalizes the account_identity domain
// allowlist (lowercase, trimmed, optional leading "@" stripped, deduped) and
// rejects entries that are not plausible domains.
func validateApprovedEmailDomains(domains []string) ([]string, error) {
	out := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, raw := range domains {
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
		if domain == "" {
			continue
		}
		if !approvedDomainFormat.MatchString(domain) {
			return nil, oops.E(oops.CodeInvalid, nil, "approved email domain %q is not a valid domain", raw)
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	return out, nil
}

func validatePolicyName(name string) error {
	if name == "" {
		return oops.E(oops.CodeInvalid, nil, "name must not be empty")
	}
	if len([]rune(name)) > 100 {
		return oops.E(oops.CodeInvalid, nil, "name must be at most 100 characters")
	}
	return nil
}

// validatePolicyType ensures policy_type is one of the supported discriminators.
func validatePolicyType(policyType string) error {
	switch policyType {
	case ra.PolicyTypeStandard, ra.PolicyTypePromptBased:
		return nil
	default:
		return oops.E(oops.CodeInvalid, nil, "policy_type must be one of: standard, prompt_based")
	}
}

func payloadHasPromptPolicyDetectionConfig(payload *gen.UpdateRiskPolicyPayload) bool {
	return len(payload.Sources) > 0 ||
		len(payload.PresidioEntities) > 0 ||
		len(payload.PromptInjectionRules) > 0 ||
		len(payload.DisabledRules) > 0 ||
		len(payload.CustomRuleIds) > 0 ||
		len(payload.ApprovedEmailDomains) > 0
}

func payloadHasCreatePromptPolicyDetectionConfig(payload *gen.CreateRiskPolicyPayload) bool {
	return len(payload.Sources) > 0 ||
		len(payload.PresidioEntities) > 0 ||
		len(payload.PromptInjectionRules) > 0 ||
		len(payload.DisabledRules) > 0 ||
		len(payload.CustomRuleIds) > 0 ||
		len(payload.ApprovedEmailDomains) > 0
}

func createPolicyDetectionField(policyType string, values []string) []string {
	if policyType == ra.PolicyTypePromptBased {
		return nil
	}
	return values
}

// maxPromptPolicyPromptLength bounds the guardrail prompt a prompt_based policy
// can carry. The judge prompt is operator-authored and short; the cap keeps
// per-message judge calls bounded.
const maxPromptPolicyPromptLength = 8000

// validatePromptPolicyFields validates the prompt + model config supplied for a
// prompt_based policy and returns the normalized prompt plus the JSON-encoded
// model config (nil when none was supplied).
func validatePromptPolicyFields(promptPtr *string, mc *types.RiskPolicyModelConfig) (string, []byte, error) {
	prompt := strings.TrimSpace(conv.PtrValOr(promptPtr, ""))
	if prompt == "" {
		return "", nil, oops.E(oops.CodeInvalid, nil, "prompt is required for prompt-based policies")
	}
	if len([]rune(prompt)) > maxPromptPolicyPromptLength {
		return "", nil, oops.E(oops.CodeInvalid, nil, "prompt must be at most %d characters", maxPromptPolicyPromptLength)
	}
	encoded, err := marshalModelConfig(mc)
	if err != nil {
		return "", nil, err
	}
	return prompt, encoded, nil
}

// promptModelConfig is the JSONB storage shape for a prompt_based policy's
// model_config column. Stored with stable snake_case keys independent of the
// generated API type.
type promptModelConfig struct {
	Model       *string  `json:"model,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	FailOpen    *bool    `json:"fail_open,omitempty"`
}

// marshalModelConfig encodes an API model config into the JSONB column value.
// Returns nil (SQL NULL) when no config was supplied.
func marshalModelConfig(mc *types.RiskPolicyModelConfig) ([]byte, error) {
	if mc == nil {
		return nil, nil
	}
	var model *string
	if mc.Model != nil {
		trimmed := strings.TrimSpace(*mc.Model)
		if trimmed != "" {
			model = &trimmed
		}
	}
	raw, err := json.Marshal(promptModelConfig{
		Model:       model,
		Temperature: mc.Temperature,
		FailOpen:    mc.FailOpen,
	})
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid model_config")
	}
	return raw, nil
}

// unmarshalModelConfig decodes the JSONB column value into the API model config.
// Returns nil for NULL/empty or unparseable values so a malformed row never
// breaks policy reads.
func unmarshalModelConfig(raw []byte) *types.RiskPolicyModelConfig {
	if len(raw) == 0 {
		return nil
	}
	var mc promptModelConfig
	if err := json.Unmarshal(raw, &mc); err != nil {
		return nil
	}
	return &types.RiskPolicyModelConfig{
		Model:       mc.Model,
		Temperature: mc.Temperature,
		FailOpen:    mc.FailOpen,
	}
}

// fallbackPromptPolicyName derives a stable display name from the guardrail
// prompt when the LLM namer is unavailable.
func fallbackPromptPolicyName(prompt string, existing []string) string {
	return promptPolicyNameFromBase(prompt, existing)
}

func promptPolicyNameFromBase(base string, existing []string) string {
	base = strings.TrimSpace(strings.Join(strings.Fields(base), " "))
	if len([]rune(base)) > 60 {
		base = string([]rune(base)[:60])
	}
	if base == "" {
		base = "Prompt Policy"
	}

	taken := make(map[string]struct{}, len(existing))
	for _, n := range existing {
		taken[n] = struct{}{}
	}
	if _, ok := taken[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s %d", base, i)
		if _, ok := taken[candidate]; !ok {
			return candidate
		}
	}
}

// promptPoliciesEnabled reports whether the prompt-based policy MVP is enabled
// for the org. The flag is targeted by PostHog group (org/project slug) the
// same way the dashboard evaluates it, so we forward the groups built from the
// auth context. A nil provider or a failed lookup degrades to disabled.
func (s *Service) promptPoliciesEnabled(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if s.flags == nil {
		return false
	}
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	on, err := s.flags.IsFlagEnabled(ctx, feature.FlagPromptPolicies, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "prompt-policies flag check failed; treating as disabled",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
		return false
	}
	return on
}

func foundRowToResult(
	id, policyID uuid.UUID, policyVersion int64, blockID uuid.UUID, chatMessageID uuid.UUID, chatID *string, chatTitle, chatUserID pgtype.Text,
	source string, ruleID, description, match pgtype.Text,
	startPos, endPos pgtype.Int4,
	confidence pgtype.Float8, tags []string, spans []byte, createdAt pgtype.Timestamptz,
	replayed bool,
) *types.RiskResult {
	return &types.RiskResult{
		ID:            id.String(),
		PolicyID:      policyID.String(),
		PolicyVersion: policyVersion,
		BlockID:       blockIDPtr(blockID),
		ChatMessageID: chatMessageID.String(),
		ChatID:        chatID,
		ChatTitle:     conv.FromPGText[string](chatTitle),
		UserID:        conv.FromPGText[string](chatUserID),
		Source:        source,
		RuleID:        conv.FromPGText[string](ruleID),
		Description:   conv.FromPGText[string](description),
		Match:         conv.FromPGText[string](match),
		StartPos:      conv.PtrInt32ToInt(conv.FromPGInt4(startPos)),
		EndPos:        conv.PtrInt32ToInt(conv.FromPGInt4(endPos)),
		Confidence:    conv.FromPGFloat8(confidence),
		Tags:          tags,
		Spans:         parseRiskSpans(spans),
		// MatchRedacted is populated later by redactResultMatchInPlace, only
		// for callers ListRiskResults decides shouldn't see raw match/spans.
		MatchRedacted: nil,
		CreatedAt:     createdAt.Time.Format(time.RFC3339),
		Replayed:      replayed,
	}
}

// blockIDPtr maps the COALESCE'd block id to an optional string: a nil UUID
// means the finding's message has no durable tool call block.
func blockIDPtr(blockID uuid.UUID) *string {
	if blockID == uuid.Nil {
		return nil
	}
	return conv.PtrEmpty(blockID.String())
}

// parseRiskSpans decodes the risk_results.spans JSONB column into the API span
// type. A nil/empty/invalid column yields nil so legacy rows simply have no
// spans (the top-level match still carries the primary span).
func parseRiskSpans(raw []byte) []*types.RiskSpan {
	if len(raw) == 0 {
		return nil
	}
	var spans []ra.FindingSpan
	if err := json.Unmarshal(raw, &spans); err != nil {
		return nil
	}
	out := make([]*types.RiskSpan, 0, len(spans))
	for _, s := range spans {
		start := s.StartPos
		end := s.EndPos
		out = append(out, &types.RiskSpan{
			Match:    s.Match,
			Field:    conv.PtrEmpty(s.Field),
			Path:     conv.PtrEmpty(s.Path),
			StartPos: &start,
			EndPos:   &end,
		})
	}
	return out
}
