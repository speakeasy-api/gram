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
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
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
	jwtSecret        string
	piClassifier     bool
	// flags gates the nl/LLM-judge policy MVP (FlagPromptPolicies). Optional:
	// when nil the feature is treated as disabled.
	flags feature.Provider
	// Scanners reused by the rule-playground endpoint (testDetectionRule)
	// so the dashboard sees the exact same matcher output the worker
	// produces during chat-message analysis. Optional: when nil the
	// playground returns an "unsupported" response for that scanner family.
	piiScanner ra.PIIScanner
	piScanner  *ra.PromptInjectionScanner
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
		jwtSecret:        "",
		piClassifier:     false,
		piiScanner:       nil,
		piScanner:        nil,
		flags:            nil,
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
	jwtSecret string,
	piClassifier bool,
	piiScanner ra.PIIScanner,
	piScanner *ra.PromptInjectionScanner,
	flags feature.Provider,
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
		jwtSecret:        jwtSecret,
		piClassifier:     piClassifier,
		piiScanner:       piiScanner,
		piScanner:        piScanner,
		flags:            flags,
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

func (s *Service) GetRiskCapabilities(ctx context.Context, payload *gen.GetRiskCapabilitiesPayload) (*gen.RiskCapabilitiesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	return &gen.RiskCapabilitiesResult{
		PiClassifierEnabled: s.piClassifier,
	}, nil
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
		policyType = "standard"
	}
	if err := validatePolicyType(policyType); err != nil {
		return nil, err
	}

	// prompt_based (LLM-judge) policies carry a prompt + optional model config
	// instead of detection sources, and are gated behind FlagPromptPolicies
	// during the MVP.
	var prompt pgtype.Text
	var modelConfig []byte
	if policyType == "prompt_based" {
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
	if policyType == "prompt_based" {
		// prompt_based policies evaluate the prompt via the LLM judge, not
		// detection sources. Persist an empty (non-null) source set.
		sources = []string{}
	} else {
		if sources == nil {
			sources = []string{"gitleaks"}
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
		if policyType == "prompt_based" {
			name = s.generatePromptPolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), prompt.String, existingNames)
		} else {
			name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, payload.PresidioEntities, action, existingNames)
		}
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate policy id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
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
		PromptInjectionRules: createPolicyDetectionField(policyType, payload.PromptInjectionRules),
		DisabledRules:        createPolicyDetectionField(policyType, payload.DisabledRules),
		CustomRuleIds:        createPolicyDetectionField(policyType, payload.CustomRuleIds),
		MessageTypes:         payload.MessageTypes,
		Enabled:              enabled,
		Action:               action,
		AutoName:             autoName,
		UserMessage:          conv.PtrToPGTextEmpty(payload.UserMessage),
		Prompt:               prompt,
		ModelConfig:          modelConfig,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy create").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy create").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policies").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	// policy_type is immutable; gate edits to prompt_based policies behind the flag.
	if current.PolicyType == "prompt_based" && !s.promptPoliciesEnabled(ctx, authCtx) {
		return nil, oops.E(oops.CodeForbidden, nil, "prompt-based policies are not enabled for this organization")
	}
	if current.PolicyType == "standard" && (payload.Prompt != nil || payload.ModelConfig != nil) {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt and model_config are only supported for prompt-based policies")
	}
	if current.PolicyType == "prompt_based" && payloadHasPromptPolicyDetectionConfig(payload) {
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
		if current.PolicyType == "prompt_based" && p == "" {
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
		if current.PolicyType == "prompt_based" {
			name = s.generatePromptPolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), prompt.String, existingNames)
		} else {
			name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, presidioEntities, action, existingNames)
		}
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	snapshotBefore := policyRowSnapshot(current)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).UpdateRiskPolicy(ctx, repo.UpdateRiskPolicyParams{
		ID:                   id,
		ProjectID:            *authCtx.ProjectID,
		Name:                 name,
		Sources:              sources,
		PresidioEntities:     presidioEntities,
		PromptInjectionRules: promptInjectionRules,
		DisabledRules:        disabledRules,
		CustomRuleIds:        customRuleIds,
		MessageTypes:         messageTypes,
		Enabled:              enabled,
		Action:               action,
		AutoName:             autoName,
		UserMessage:          userMessage,
		Prompt:               prompt,
		ModelConfig:          modelConfig,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update risk policy").Log(ctx, s.logger)
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
		SnapshotAfter:    policyRowSnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy update").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy update").Log(ctx, s.logger)
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
		return oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	if err := q.DeleteRiskPolicy(ctx, repo.DeleteRiskPolicyParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk policy").Log(ctx, s.logger)
	}

	if err := q.DeleteRiskPolicyBypassRequestsByPolicy(ctx, repo.DeleteRiskPolicyBypassRequestsByPolicyParams{
		RiskPolicyID: id,
		ProjectID:    *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk policy bypass requests").Log(ctx, s.logger)
	}

	deletedExclusions, err := q.DeleteRiskExclusionsByPolicy(ctx, repo.DeleteRiskExclusionsByPolicyParams{
		RiskPolicyID: uuid.NullUUID{UUID: id, Valid: true},
		ProjectID:    *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk exclusions by policy").Log(ctx, s.logger)
	}
	if deletedExclusions > 0 {
		s.logger.InfoContext(ctx, "deleted risk exclusions for policy",
			attr.SlogRiskPolicyID(id.String()),
			attr.SlogDBDeletedRowsCount(deletedExclusions),
		)
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
		return oops.E(oops.CodeUnexpected, err, "log risk policy delete").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit risk policy delete").Log(ctx, s.logger)
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

func (s *Service) ListRiskResults(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").Log(ctx, s.logger)
	}

	pageSize := resolvePageSize(payload.Limit)

	totalCount, err := s.repo.CountAllFindings(ctx, *authCtx.ProjectID)
	if err != nil {
		totalCount = 0
	}

	if payload.ChatID != nil && *payload.ChatID != "" {
		return s.listResultsByChat(ctx, *authCtx.ProjectID, *payload.ChatID, cursor, pageSize, totalCount)
	}
	if payload.PolicyID != nil && *payload.PolicyID != "" {
		return s.listResultsByPolicy(ctx, *authCtx.ProjectID, *payload.PolicyID, cursor, pageSize, totalCount)
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
	fromTime, err := parseOptionalTimestamptz(payload.From)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid from").Log(ctx, s.logger)
	}
	toTime, err := parseOptionalTimestamptz(payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid to").Log(ctx, s.logger)
	}
	return s.listResultsByProject(ctx, *authCtx.ProjectID, cursor, pageSize, totalCount, category, ruleID, userID, uniqueMatch, fromTime, toTime)
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
	base, err := s.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		ApikeyToken:      payload.ApikeyToken,
		SessionToken:     payload.SessionToken,
		ProjectSlugInput: payload.ProjectSlugInput,
		PolicyID:         payload.PolicyID,
		ChatID:           payload.ChatID,
		Category:         payload.Category,
		RuleID:           payload.RuleID,
		UserID:           payload.UserID,
		UniqueMatch:      payload.UniqueMatch,
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
		CreatedAt:     r.CreatedAt,
	}
}

// redactMatch encodes a match value as `<redacted len=N sha=XXXXXXXX>` for
// non-shadow_mcp sources, or passes it through verbatim for shadow_mcp.
// A nil/empty match collapses to `<redacted len=0>` without a sha component
// so the absence of a finding payload is distinguishable from a real hash.
//
// The hash is salted by orgID with a NUL separator so two different orgs
// holding the same secret produce different fingerprints — defense in depth
// against any future surface that crosses an org boundary. Within an org the
// fingerprint stays deterministic so agents can still dedupe.
func redactMatch(source string, match *string, orgID string) string {
	if match == nil || *match == "" {
		return "<redacted len=0>"
	}
	if source == shadowmcp.SourceShadowMCP {
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
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").Log(ctx, s.logger)
	}

	pageSize := resolvePageSize(payload.Limit)

	rows, err := s.repo.ListRiskResultsGroupedByChat(ctx, repo.ListRiskResultsGroupedByChatParams{
		ProjectID: *authCtx.ProjectID,
		Cursor:    cursor,
		PageLimit: conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeInvalid, err, "invalid overview window").Log(ctx, s.logger)
	}

	window := riskOverviewWindowParams(from, to)
	counts, err := s.repo.GetRiskOverviewCounts(ctx, repo.GetRiskOverviewCountsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk overview counts").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top users").Log(ctx, s.logger)
	}

	timeSeriesRows, err := s.repo.ListRiskOverviewTimeSeriesFindings(ctx, repo.ListRiskOverviewTimeSeriesFindingsParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview time series findings").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top rules").Log(ctx, s.logger)
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
		MessagesScanned:    counts.MessagesScanned,
		Findings:           counts.Findings,
		FlaggedSessions:    counts.FlaggedSessions,
		ActivePolicies:     counts.ActivePolicies,
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
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by chat").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		cid := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &cid, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.MessageCreatedAt))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

func (s *Service) listResultsByPolicy(ctx context.Context, projectID uuid.UUID, rawPolicyID string, cursor *riskResultsCursor, pageSize int, totalCount int64) (*gen.ListRiskResultsResult, error) {
	policyID, err := uuid.Parse(rawPolicyID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByProjectAndPolicy(ctx, repo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:              projectID,
		RiskPolicyID:           policyID,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results by policy").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.MessageCreatedAt))
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.MessageCreatedAt.Time, ID: row.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

func (s *Service) listResultsByProject(ctx context.Context, projectID uuid.UUID, cursor *riskResultsCursor, pageSize int, totalCount int64, category string, ruleID string, userID string, uniqueMatch bool, fromTime, toTime pgtype.Timestamptz) (*gen.ListRiskResultsResult, error) {
	cursorCreatedAt, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListRiskResultsByProjectFound(ctx, repo.ListRiskResultsByProjectFoundParams{
		ProjectID:              projectID,
		FromTime:               fromTime,
		ToTime:                 toTime,
		Category:               category,
		RuleID:                 ruleID,
		UserID:                 userID,
		UniqueMatch:            uniqueMatch,
		CursorMessageCreatedAt: cursorCreatedAt,
		CursorID:               cursorID,
		PageLimit:              conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk results").Log(ctx, s.logger)
	}
	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		chatID := row.ChatID.String()
		results = append(results, foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, row.ChatMessageID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.MessageCreatedAt))
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
		return nil, oops.E(oops.CodeInvalid, err, "invalid window").Log(ctx, s.logger)
	}
	window := riskOverviewWindowParams(from, to)

	categoryRows, err := s.repo.ListRiskUserCategoryBreakdown(ctx, repo.ListRiskUserCategoryBreakdownParams{
		ProjectID:      *authCtx.ProjectID,
		FromTime:       window.from,
		ToTime:         window.to,
		ExternalUserID: payload.ExternalUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user category breakdown").Log(ctx, s.logger)
	}

	ruleRows, err := s.repo.ListRiskUserRuleBreakdown(ctx, repo.ListRiskUserRuleBreakdownParams{
		ProjectID:      *authCtx.ProjectID,
		FromTime:       window.from,
		ToTime:         window.to,
		ExternalUserID: payload.ExternalUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user rule breakdown").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeInvalid, err, "invalid window").Log(ctx, s.logger)
	}

	window := riskOverviewWindowParams(from, to)
	rows, err := s.repo.ListRiskRulesByCategory(ctx, repo.ListRiskRulesByCategoryParams{
		ProjectID: *authCtx.ProjectID,
		FromTime:  window.from,
		ToTime:    window.to,
		Category:  payload.Category,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list rule breakdown").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	totalMessages, err := s.repo.CountTotalMessages(ctx, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count total messages").Log(ctx, s.logger)
	}

	analyzedMessages, err := s.repo.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         *authCtx.ProjectID,
		RiskPolicyID:      id,
		RiskPolicyVersion: policy.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count analyzed messages").Log(ctx, s.logger)
	}

	findingsCount, err := s.repo.CountFindingsByPolicy(ctx, repo.CountFindingsByPolicyParams{
		ProjectID:         *authCtx.ProjectID,
		RiskPolicyID:      id,
		RiskPolicyVersion: policy.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count findings").Log(ctx, s.logger)
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
	regexPattern := strings.TrimSpace(conv.PtrValOr(payload.Regex, ""))
	severity := payload.Severity
	if severity == "" {
		severity = "medium"
	}
	if err := validateCustomDetectionRule(ruleID, title, regexPattern, severity); err != nil {
		return nil, err
	}
	matchConfig, err := customRuleMatchConfigToStorage(payload.MatchConfig)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid match_config")
	}
	if err := ra.ValidateMatchConfig(matchConfig); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid match_config")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateCustomDetectionRule(ctx, repo.CreateCustomDetectionRuleParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		RuleID:         ruleID,
		Title:          title,
		Description:    description,
		Regex:          pgtype.Text{String: regexPattern, Valid: true},
		MatchConfig:    matchConfig,
		Severity:       severity,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "custom detection rule already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create custom detection rule").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit custom detection rule create").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list custom detection rules").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeNotFound, err, "custom detection rule not found").Log(ctx, s.logger)
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
	regexPattern := strings.TrimSpace(conv.PtrValOr(payload.Regex, ""))
	if err := validateCustomDetectionRuleFields(title, regexPattern, payload.Severity); err != nil {
		return nil, err
	}
	matchConfig, err := customRuleMatchConfigToStorage(payload.MatchConfig)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid match_config")
	}
	if err := ra.ValidateMatchConfig(matchConfig); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid match_config")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).UpdateCustomDetectionRule(ctx, repo.UpdateCustomDetectionRuleParams{
		ID:          id,
		ProjectID:   *authCtx.ProjectID,
		Title:       title,
		Description: description,
		Regex:       pgtype.Text{String: regexPattern, Valid: true},
		MatchConfig: matchConfig,
		Severity:    payload.Severity,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "custom detection rule not found").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit custom detection rule update").Log(ctx, s.logger)
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
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := repo.New(dbtx).DeleteCustomDetectionRule(ctx, repo.DeleteCustomDetectionRuleParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete custom detection rule").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit custom detection rule delete").Log(ctx, s.logger)
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

	suggestion, err := s.suggestCustomRuleViaLLM(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), prompt, payload.ExistingRuleIds)
	if err != nil {
		s.logger.WarnContext(ctx, "openrouter suggestion failed; returning heuristic suggestion", attr.SlogError(err))
		return heuristicCustomRuleSuggestion(prompt, payload.ExistingRuleIds), nil
	}
	return suggestion, nil
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
		RuleID:      ruleID,
		Title:       title,
		Description: strings.TrimSpace(prompt),
		Regex:       "",
		Severity:    "medium",
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

func validateCustomDetectionRule(ruleID, title, regexPattern, severity string) error {
	if !customRuleIDPattern.MatchString(ruleID) {
		return oops.E(oops.CodeInvalid, nil, "rule_id must match custom.[a-z0-9_]+")
	}
	return validateCustomDetectionRuleFields(title, regexPattern, severity)
}

func validateCustomDetectionRuleFields(title, regexPattern, severity string) error {
	if title == "" {
		return oops.E(oops.CodeInvalid, nil, "title must not be empty")
	}
	if _, err := regexp.Compile(regexPattern); err != nil {
		return oops.E(oops.CodeInvalid, err, "regex is invalid")
	}
	if !customRuleSeverityAllow[severity] {
		return oops.E(oops.CodeInvalid, nil, "severity must be one of info, low, medium, high, critical")
	}
	return nil
}

func (s *Service) suggestCustomRuleViaLLM(ctx context.Context, orgID, projectID, userPrompt string, existingIDs []string) (*gen.SuggestCustomDetectionRuleResult, error) {
	systemPrompt := `You are a security-rules assistant for a runtime risk detection product.

Given a single natural-language description of what an operator wants to detect, return a JSON object that the dashboard will use to prefill a "create custom detection rule" form.

Rules:
- "rule_id" must start with the literal prefix "custom." and contain only [a-z0-9_]. Pick a stable, descriptive slug derived from the subject (e.g. "custom.acme_internal_token", "custom.qa_signing_key"). It must NOT appear in the provided existing_rule_ids list.
- "title" is 2-6 words, title case.
- "description" is 1-2 sentences describing what is detected and why it matters. No marketing copy.
- "regex" is an RE2-compatible pattern that matches the described payload. Prefer anchors and character classes that minimise false positives. Do not include leading/trailing slashes.
- "severity" is one of "info", "low", "medium", "high", "critical". Pick based on the leakage cost of the data described — credentials, PII, financial, healthcare are typically high or critical; logging IDs and internal references are typically low or medium.

Output ONLY the JSON object. No prose, no markdown fences.`

	existingList := strings.Join(existingIDs, ", ")
	if existingList == "" {
		existingList = "(none)"
	}
	userMessage := fmt.Sprintf("Operator request: %s\n\nExisting rule ids (avoid colliding): %s", userPrompt, existingList)

	strict := true
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rule_id":     map[string]any{"type": "string", "pattern": "^custom\\.[a-z0-9_]+$"},
			"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
			"description": map[string]any{"type": "string", "minLength": 1, "maxLength": 400},
			"regex":       map[string]any{"type": "string", "minLength": 1, "maxLength": 400},
			"severity":    map[string]any{"type": "string", "enum": []string{"info", "low", "medium", "high", "critical"}},
		},
		"required":             []string{"rule_id", "title", "description", "regex", "severity"},
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
		OrgID:          orgID,
		ProjectID:      projectID,
		Model:          "",
		SystemPrompt:   systemPrompt,
		Prompt:         userMessage,
		Temperature:    &temperature,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
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
		RuleID      string `json:"rule_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Regex       string `json:"regex"`
		Severity    string `json:"severity"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	parsed.RuleID = strings.TrimSpace(parsed.RuleID)
	parsed.Title = strings.TrimSpace(parsed.Title)
	parsed.Description = strings.TrimSpace(parsed.Description)
	parsed.Regex = strings.TrimSpace(parsed.Regex)
	parsed.Severity = strings.ToLower(strings.TrimSpace(parsed.Severity))

	if !strings.HasPrefix(parsed.RuleID, "custom.") || !customRuleIDPattern.MatchString(parsed.RuleID) {
		return nil, fmt.Errorf("model returned invalid rule_id %q", parsed.RuleID)
	}
	if _, err := regexp.Compile(parsed.Regex); err != nil {
		return nil, fmt.Errorf("model returned invalid regex: %w", err)
	}
	if !customRuleSeverityAllow[parsed.Severity] {
		parsed.Severity = "medium"
	}
	if slices.Contains(existingIDs, parsed.RuleID) {
		parsed.RuleID = parsed.RuleID + "_" + time.Now().UTC().Format("20060102150405")
	}

	return &gen.SuggestCustomDetectionRuleResult{
		RuleID:      parsed.RuleID,
		Title:       parsed.Title,
		Description: parsed.Description,
		Regex:       parsed.Regex,
		Severity:    parsed.Severity,
	}, nil
}

// TestDetectionRule runs a single detection rule against pasted sample text
// and returns its matches. The handler dispatches to the same scanners the
// worker uses during chat-message analysis (gitleaks for secrets.*, the
// configured PIIScanner for pii.*, the PromptInjectionScanner for
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
		return s.testPromptInjectionRule(ctx, authCtx.ActiveOrganizationID, text)
	case strings.HasPrefix(ruleID, "custom."):
		return s.testCustomRule(ruleID, conv.PtrValOr(payload.Regex, ""), payload.MatchConfig, text)
	default:
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("This rule has no text-only detector. Playground requires gitleaks/presidio/prompt-injection/custom rule families."),
		}, nil
	}
}

func (s *Service) testGitleaksRule(ctx context.Context, ruleID, text string) (*gen.TestDetectionRuleResult, error) {
	findings, err := ra.ScanWithGitleaks(text)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run gitleaks").Log(ctx, s.logger)
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
	batches, err := s.piiScanner.AnalyzeBatch(ctx, []string{text}, []string{entity}, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run presidio").Log(ctx, s.logger)
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

func (s *Service) testPromptInjectionRule(ctx context.Context, orgID, text string) (*gen.TestDetectionRuleResult, error) {
	if s.piScanner == nil {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("Prompt-injection scanner is not configured on this server."),
		}, nil
	}
	findings, err := s.piScanner.Scan(ctx, text, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run prompt-injection scanner").Log(ctx, s.logger)
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

func (s *Service) testCustomRule(ruleID, pattern string, cfg *types.RiskMatchConfig, text string) (*gen.TestDetectionRuleResult, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" && cfg == nil {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("Custom rules require a regex or match_config. Custom rules are stored client-side, so pass one in the request body."),
		}, nil
	}
	// The playground only has pasted text, so it can simulate content- and
	// user-prompt-targeted conditions but not tool calls or other message parts.
	if cfg != nil && !customRuleTestableFromText(cfg) {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("This rule matches tool calls or other message parts the text playground can't simulate. Save it and run analysis to see matches."),
		}, nil
	}

	raw, err := customRuleMatchConfigToStorage(cfg)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid match_config")
	}
	compiled, err := ra.CompileCustomDetectionRules([]ra.CustomDetectionRule{{
		RuleID:      ruleID,
		Title:       "",
		Description: "Custom rule match",
		MatchConfig: ra.EffectiveMatchConfig(raw, pattern),
	}})
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid custom rule")
	}

	findings := ra.ScanCustomDetectionRules(ra.MessageView{Content: text, Type: message.User, Tools: nil}, compiled)
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

// customRuleTestableFromText reports whether every condition targets a part of
// the message the text playground can stand in for (content or the user
// prompt). Tool-call and other message-part targets cannot be simulated.
func customRuleTestableFromText(cfg *types.RiskMatchConfig) bool {
	for _, c := range cfg.Conditions {
		if c == nil {
			continue
		}
		switch ra.Target(c.Target) {
		case ra.TargetContent, ra.TargetUserPrompt:
		default:
			return false
		}
	}
	return true
}

func findingToMatch(f ra.Finding) *gen.TestDetectionRuleMatch {
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

	return &types.RiskPolicy{
		ID:                   row.ID.String(),
		ProjectID:            row.ProjectID.String(),
		Name:                 row.Name,
		PolicyType:           row.PolicyType,
		Sources:              row.Sources,
		PresidioEntities:     row.PresidioEntities,
		PromptInjectionRules: row.PromptInjectionRules,
		DisabledRules:        row.DisabledRules,
		CustomRuleIds:        row.CustomRuleIds,
		MessageTypes:         row.MessageTypes,
		Enabled:              row.Enabled,
		Action:               row.Action,
		AutoName:             row.AutoName,
		UserMessage:          conv.FromPGText[string](row.UserMessage),
		Prompt:               conv.FromPGText[string](row.Prompt),
		ModelConfig:          unmarshalModelConfig(row.ModelConfig),
		Version:              row.Version,
		CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:      pendingMessages,
		TotalMessages:        totalMessages,
	}, nil
}

// policyRowSnapshot returns a *types.RiskPolicy suitable for audit log
// snapshots. Unlike policyToType it skips the extra DB queries for message
// counts, keeping transactions short. Count fields are set to -1 to indicate
// they were not computed.
func policyRowSnapshot(row repo.RiskPolicy) *types.RiskPolicy {
	return &types.RiskPolicy{
		ID:                   row.ID.String(),
		ProjectID:            row.ProjectID.String(),
		Name:                 row.Name,
		PolicyType:           row.PolicyType,
		Sources:              row.Sources,
		PresidioEntities:     row.PresidioEntities,
		PromptInjectionRules: row.PromptInjectionRules,
		DisabledRules:        row.DisabledRules,
		CustomRuleIds:        row.CustomRuleIds,
		MessageTypes:         row.MessageTypes,
		Enabled:              row.Enabled,
		Action:               row.Action,
		AutoName:             row.AutoName,
		UserMessage:          conv.FromPGText[string](row.UserMessage),
		Prompt:               conv.FromPGText[string](row.Prompt),
		ModelConfig:          unmarshalModelConfig(row.ModelConfig),
		Version:              row.Version,
		CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
		PendingMessages:      -1,
		TotalMessages:        -1,
	}
}

func customDetectionRuleToType(row repo.RiskCustomDetectionRule) *types.RiskCustomDetectionRule {
	return &types.RiskCustomDetectionRule{
		ID:          row.ID.String(),
		RuleID:      row.RuleID,
		Title:       row.Title,
		Description: row.Description,
		Regex:       conv.PtrValOr(conv.FromPGText[string](row.Regex), ""),
		MatchConfig: customRuleMatchConfigFromStorage(row.MatchConfig),
		Severity:    row.Severity,
		CreatedAt:   row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:   row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// customRuleMatchConfigToStorage converts the API match_config into the JSONB
// representation the rule engine evaluates. Returns nil for a nil config so the
// column stays NULL (the rule falls back to its regex). The engine and API
// share the same JSON shape but distinct Go types, so we map explicitly rather
// than re-marshal the Goa type (whose fields lack snake_case json tags).
func customRuleMatchConfigToStorage(in *types.RiskMatchConfig) ([]byte, error) {
	if in == nil {
		return nil, nil
	}
	cfg := ra.MatchConfig{
		Combine:    ra.MatchCombine(conv.PtrValOr(in.Combine, "")),
		Conditions: make([]ra.Condition, 0, len(in.Conditions)),
	}
	for _, c := range in.Conditions {
		if c == nil {
			continue
		}
		cfg.Conditions = append(cfg.Conditions, ra.Condition{
			Target:          ra.Target(c.Target),
			Op:              ra.Op(c.Op),
			Value:           conv.PtrValOr(c.Value, ""),
			Values:          c.Values,
			Path:            conv.PtrValOr(c.Path, ""),
			CaseInsensitive: conv.PtrValOr(c.CaseInsensitive, false),
		})
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal match_config: %w", err)
	}
	return raw, nil
}

// customRuleMatchConfigFromStorage maps stored match_config JSONB back into the
// API type. Returns nil for an empty/NULL column. The bytes were validated on
// write, so a decode error is treated as "no config".
func customRuleMatchConfigFromStorage(raw []byte) *types.RiskMatchConfig {
	if len(raw) == 0 {
		return nil
	}
	var cfg ra.MatchConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	if len(cfg.Conditions) == 0 {
		return nil
	}
	out := &types.RiskMatchConfig{
		Combine:    conv.PtrEmpty(string(cfg.Combine)),
		Conditions: make([]*types.RiskMatchCondition, 0, len(cfg.Conditions)),
	}
	for _, c := range cfg.Conditions {
		out.Conditions = append(out.Conditions, &types.RiskMatchCondition{
			Target:          string(c.Target),
			Op:              string(c.Op),
			Value:           conv.PtrEmpty(c.Value),
			Values:          c.Values,
			Path:            conv.PtrEmpty(c.Path),
			CaseInsensitive: conv.PtrEmpty(c.CaseInsensitive),
		})
	}
	return out
}

func (s *Service) generatePolicyName(ctx context.Context, orgID, projectID string, sources, presidioEntities []string, action string, existingNames []string) string {
	if s.completionClient == nil {
		return s.fallbackPolicyName(sources, action)
	}

	// Policy authors think in *what* is detected, not *how* (gitleaks,
	// presidio). Translate sources to user-facing category labels and
	// scrub library names so the LLM cannot regurgitate them. See AGE-2378.
	categories := sourcesToCategoryLabels(sources)

	prompt := fmt.Sprintf(
		"Generate a short, human-friendly name (2-5 words) for a security policy with these settings:\n"+
			"- Detection categories: %v\n"+
			"- PII entity types: %v\n"+
			"- Action: %s\n"+
			"- Existing policy names to avoid: %v\n\n"+
			"Return ONLY the name, no quotes or explanation. Make it descriptive and distinct from existing names. "+
			"Do not mention internal tool or library names; describe what is detected.",
		categories, presidioEntities, action, existingNames,
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
		return s.fallbackPolicyName(sources, action)
	}

	name := strings.TrimSpace(openrouter.GetText(*response.Message))
	if name == "" {
		return s.fallbackPolicyName(sources, action)
	}

	// Truncate to 100 chars
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}

	return name
}

// sourcesToCategoryLabels maps internal source identifiers (gitleaks,
// presidio, …) to the user-facing detection category they implement.
// Library names are an implementation detail; policy authors think in
// what is detected, not how. See AGE-2378.
func sourcesToCategoryLabels(sources []string) []string {
	out := make([]string, 0, len(sources))
	for _, src := range sources {
		switch src {
		case "gitleaks":
			out = append(out, "Secrets")
		case "presidio":
			out = append(out, "PII")
		case shadowmcp.SourceShadowMCP:
			out = append(out, "Shadow MCP")
		case shadowmcp.SourceDestructiveTool:
			out = append(out, "Destructive Tool")
		case ra.SourceCLIDestructive:
			out = append(out, "Destructive CLI Command")
		case ra.SourcePromptInjection:
			out = append(out, "Prompt Injection")
		}
	}
	return out
}

func (s *Service) fallbackPolicyName(sources []string, action string) string {
	parts := sourcesToCategoryLabels(sources)
	// Singularize the leading "Secrets" label when used in a name like
	// "Secret Blocker" — preserves the previous look of fallback names.
	for i, p := range parts {
		if p == "Secrets" {
			parts[i] = "Secret"
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "Risk")
	}

	actionLabel := "Scanner"
	if action == "block" {
		actionLabel = "Blocker"
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
	case "flag", "block":
		return nil
	default:
		return oops.E(oops.CodeInvalid, nil, "action must be one of: flag, block")
	}
}

func validateSources(sources []string) error {
	for _, src := range sources {
		switch src {
		case "gitleaks", "presidio", shadowmcp.SourceShadowMCP, shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive, ra.SourcePromptInjection:
		default:
			return oops.E(oops.CodeInvalid, nil, "source %q is not a recognized policy source", src)
		}
	}
	return nil
}

func validateSourceAction(sources []string, action string) error {
	if action != "block" {
		return nil
	}
	for _, src := range []string{shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive} {
		if slices.Contains(sources, src) {
			return oops.E(oops.CodeInvalid, nil, "source %q supports flagging only", src)
		}
	}
	return nil
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
	case "standard", "prompt_based":
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
		len(payload.CustomRuleIds) > 0
}

func payloadHasCreatePromptPolicyDetectionConfig(payload *gen.CreateRiskPolicyPayload) bool {
	return len(payload.Sources) > 0 ||
		len(payload.PresidioEntities) > 0 ||
		len(payload.PromptInjectionRules) > 0 ||
		len(payload.DisabledRules) > 0 ||
		len(payload.CustomRuleIds) > 0
}

func createPolicyDetectionField(policyType string, values []string) []string {
	if policyType == "prompt_based" {
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
	id, policyID uuid.UUID, policyVersion int64, chatMessageID uuid.UUID, chatID *string, chatTitle, chatUserID pgtype.Text,
	source string, ruleID, description, match pgtype.Text,
	startPos, endPos pgtype.Int4,
	confidence pgtype.Float8, tags []string, createdAt pgtype.Timestamptz,
) *types.RiskResult {
	return &types.RiskResult{
		ID:            id.String(),
		PolicyID:      policyID.String(),
		PolicyVersion: policyVersion,
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
		CreatedAt:     createdAt.Time.Format(time.RFC3339),
	}
}
