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
	"github.com/cenkalti/backoff/v5"
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
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
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
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tooldisposition"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/risk/analysisstatus"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	shadowadmission "github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/stokens"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

const sessionQuarantineCircuitDeleteAttempts = 3

// RiskAnalysisSignaler signals the per-project risk analysis coordinator
// workflow and reports its run state for the Watchdog "last analysed" badge.
type RiskAnalysisSignaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
	analysisstatus.Describer
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
	tracer                       trace.Tracer
	logger                       *slog.Logger
	db                           *pgxpool.Pool
	repo                         *repo.Queries
	policies                     *policycore.Core
	platformToolsets             map[string]platformtools.Toolset
	exclusions                   *exclusioncore.Core
	auth                         *auth.Auth
	authz                        *authz.Engine
	signaler                     RiskAnalysisSignaler
	reconciler                   RiskExclusionReconciler
	resultsCleaner               RiskPolicyResultsCleaner
	completionClient             openrouter.CompletionClient
	shadowMCPClient              *shadowmcp.Client
	reconcileShadowMCPPolicyURLs ShadowMCPPolicyURLReconciler
	shadowMCPInventoryURLLookup  ShadowMCPInventoryURLLookup
	audit                        *audit.Logger
	// cache backs the rpbr2 policy-bypass request links: the link generator
	// stores request state here and CreateRiskPolicyBypassRequest reads it
	// back. Must be the same backing store the link generator uses.
	cache     cache.Cache
	jwtSecret string
	// approvalIntake routes a redeemed shadow-MCP block link into the MCP
	// approval workflow instead of a bypass request. Optional: nil keeps the
	// legacy bypass flow.
	approvalIntake ShadowMCPApprovalIntake
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
	riskRecorder    *metering.RiskRecorder
	// stokenCodec counts prepared rule-playground input.
	stokenCodec *stokens.Codec

	// approvals holds, lists and decides tool approval requests.
	approvals *approval.Service
	// publicationRequests records a plugin republish in the transaction that
	// adds the approvals server to the Default plugin.
	publicationRequests plugins.PublicationRequests
	// pluginPublish enqueues that republish after commit. Nil when plugin
	// publishing is not configured.
	pluginPublish PluginPublishFunc
	// celEng is the shared CEL env, injected at construction; used to compile
	// and validate scope/detection expressions. nil in the lightweight observer.
	celEng         *celenv.Engine
	builtinPresets *presetlib.Library
	// promptJudge replays an inline guardrail against a chat session for the
	// policy-eval workbench (EvaluatePromptGuardrail). It is the same LLM judge
	// the realtime scanner uses. Optional: when nil the eval endpoint returns
	// un-matched verdicts (judge unavailable).
	promptJudge promptpolicy.Evaluator
	// findingsCH reads the ClickHouse risk_findings table for the project-wide
	// listing, reveal, overview and signals.
	findingsCH      *chrepo.Queries
	findingEvidence *MCPFindingEvidenceStore
	// assetStorage reads chat content part assets for the ClickHouse reveal
	// path, the same store the batch analysis activity hydrates parts from.
	// Optional: when nil, content-part findings are not reconstructible.
	assetStorage blobio.Reader
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
	riskRecorder *metering.RiskRecorder,
) chat.MessageObserver {
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),

		logger:                       logger.With(attr.SlogComponent("risk")),
		db:                           db,
		repo:                         repo.New(db),
		policies:                     policycore.New(db),
		platformToolsets:             nil,
		exclusions:                   exclusioncore.New(db),
		auth:                         nil,
		authz:                        nil,
		signaler:                     signaler,
		reconciler:                   nil,
		resultsCleaner:               nil,
		completionClient:             nil,
		shadowMCPClient:              nil,
		reconcileShadowMCPPolicyURLs: nil,
		shadowMCPInventoryURLLookup:  nil,
		audit:                        auditLogger,
		cache:                        nil,
		jwtSecret:                    "",
		approvalIntake:               nil,
		approvals:                    nil,
		piiScanner:                   nil,
		piScanner:                    nil,
		gitleaksScanner:              nil,
		riskRecorder:                 riskRecorder,
		stokenCodec:                  stokens.NewCodec(),
		flags:                        nil,
		celEng:                       nil,
		builtinPresets:               nil,
		promptJudge:                  nil,
		findingsCH:                   nil,
		findingEvidence:              nil,
		assetStorage:                 nil,
		publicationRequests:          plugins.PublicationRequests{Enabled: false},
		pluginPublish:                nil,
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
	approvalIntake ShadowMCPApprovalIntake,
	piiScanner ra.PIIScanner,
	piScanner *promptinjection.Scanner,
	flags feature.Provider,
	celEng *celenv.Engine,
	builtinPresets *presetlib.Library,
	promptJudge promptpolicy.Evaluator,
	reconcileShadowMCPPolicyURLs ShadowMCPPolicyURLReconciler,
	shadowMCPInventoryURLLookup ShadowMCPInventoryURLLookup,
	findingEvidence *MCPFindingEvidenceStore,
	findingsCH *chrepo.Queries,
	assetStorage blobio.Reader,
	riskRecorder *metering.RiskRecorder,
	platformToolsets map[string]platformtools.Toolset,
	approvals *approval.Service,
) *Service {
	logger = logger.With(attr.SlogComponent("risk"))

	var policyCacheInvalidator policycore.PolicyCacheInvalidator
	if shadowMCPClient != nil {
		policyCacheInvalidator = shadowMCPClient
	}

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		policies: policycore.NewWithToolAnnotations(db, tooldisposition.New(logger, db, cacheImpl), policycore.MutationDependencies{
			Transactor:       db,
			Auditor:          policyMutationAuditor{logger: auditLogger},
			Approvals:        approvalIntake,
			ReconcileURLs:    policycore.ReconcilePolicyURLs(reconcileShadowMCPPolicyURLs),
			Signaler:         signaler,
			CacheInvalidator: policyCacheInvalidator,
		}),
		exclusions: exclusioncore.New(db, exclusioncore.MutationDependencies{
			Transactor:  db,
			Auditor:     exclusionMutationAuditor{logger: auditLogger},
			AfterCommit: newExclusionAfterCommit(logger, reconciler),
			Redactor:    exclusioncore.NewRedactor(jwtSecret),
		}),
		auth:                         auth.New(logger, db, sessions, authzEngine),
		authz:                        authzEngine,
		signaler:                     signaler,
		reconciler:                   reconciler,
		resultsCleaner:               resultsCleaner,
		completionClient:             completionClient,
		shadowMCPClient:              shadowMCPClient,
		reconcileShadowMCPPolicyURLs: reconcileShadowMCPPolicyURLs,
		shadowMCPInventoryURLLookup:  shadowMCPInventoryURLLookup,
		audit:                        auditLogger,
		cache:                        cacheImpl,
		jwtSecret:                    jwtSecret,
		approvalIntake:               approvalIntake,
		piiScanner:                   piiScanner,
		piScanner:                    piScanner,
		gitleaksScanner:              gitleaks.NewScanner(),
		flags:                        flags,
		celEng:                       celEng,
		builtinPresets:               builtinPresets,
		promptJudge:                  promptJudge,
		findingsCH:                   findingsCH,
		findingEvidence:              findingEvidence,
		assetStorage:                 assetStorage,
		riskRecorder:                 riskRecorder,
		platformToolsets:             platformToolsets,
		stokenCodec:                  stokens.NewCodec(),
		approvals:                    approvals,
		publicationRequests:          plugins.PublicationRequests{Enabled: false},
		pluginPublish:                nil,
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
	ctx, err := s.auth.Authorize(ctx, key, schema)
	if err != nil {
		return ctx, err
	}
	// Bypass requests bind to a human requester; agent-principal keys never qualify.
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return ctx, oops.C(oops.CodeForbidden)
	}
	return ctx, nil
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
		if sources == nil && action == policycore.ActionRequireApproval {
			// require_approval holds calls by MCP scope alone, with no detectors.
			sources = []string{}
		}
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
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	shadowMCPDisposition := conv.PtrValOr(payload.ShadowMcpDisposition, "")
	if err := validateShadowMCPDisposition(shadowMCPDisposition, sources, action); err != nil {
		return nil, err
	}

	var shadowMCPAllowedURLs []string
	if payload.ShadowMcpAllowedUrls != nil {
		shadowMCPAllowedURLs, err = validateShadowMCPAllowedURLs(ctx, s.shadowMCPInventoryURLLookup, *authCtx.ProjectID, enabled, sources, action, shadowMCPDisposition, payload.ShadowMcpAllowedUrls)
		if err != nil {
			return nil, err
		}
	}

	var shadowMCPBlockedURLs []string
	if payload.ShadowMcpBlockedUrls != nil {
		shadowMCPBlockedURLs, err = validateShadowMCPBlockedURLs(shadowMCPDisposition, payload.ShadowMcpBlockedUrls)
		if err != nil {
			return nil, err
		}
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
		} else if shadowName := shadowMCPPolicyAutoName(sources, action, existingNames); shadowName != "" {
			name = shadowName
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
	if len(payload.DetectionScopes) > 0 {
		specs, err := validateDetectionScopes(s.celEng, payload.DetectionScopes)
		if err != nil {
			return nil, err
		}
		analyzerConfig, err = ra.WithDetectionScopes(analyzerConfig, specs)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build analyzer config").LogError(ctx, s.logger)
		}
	}

	mcpScope, mcpScopeJSON, err := s.normalizeMCPScope(ctx, *authCtx.ProjectID, payload.McpScope)
	if err != nil {
		return nil, err
	}
	if err := policycore.ValidateMCPScope(mcpScope, sources, action); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	if action == policycore.ActionRequireApproval && !s.toolApprovalsEnabled(ctx, authCtx) {
		return nil, oops.E(oops.CodeForbidden, nil, "tool approvals are not enabled for this organization")
	}
	approvalConfig := approvalConfigFromPayload(payload.ApprovalConfig)
	if err := policycore.ValidateApprovalPolicy(action, policyType, sources, payload.CustomRuleIds, approvalConfig); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	if err := s.validateApprovers(ctx, authCtx.ActiveOrganizationID, approvalConfig); err != nil {
		return nil, err
	}
	approvalConfigJSON, err := policycore.MarshalApprovalConfig(approvalConfig)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode approval config").LogError(ctx, s.logger)
	}

	result, setup, err := s.createPolicy(ctx, authCtx, policycore.CreateMutation{
		Params: repo.CreateRiskPolicyParams{
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
			McpScope:             mcpScopeJSON,
			Enabled:              enabled,
			Action:               action,
			AudienceType:         audienceType,
			ShadowMcpDisposition: conv.ToPGTextEmpty(shadowMCPDisposition),
			AutoName:             autoName,
			UserMessage:          conv.PtrToPGTextEmpty(payload.UserMessage),
			Prompt:               prompt,
			ModelConfig:          modelConfig,
			ApprovalConfig:       approvalConfigJSON,
			Score:                pgtype.Float8{Float64: payload.Score, Valid: true},
		},
		AudiencePrincipals: audiencePrincipals,
		AllowedURLs:        shadowMCPAllowedURLs,
		AllowedURLsSet:     payload.ShadowMcpAllowedUrls != nil,
		BlockedURLs:        shadowMCPBlockedURLs,
		BlockedURLsSet:     payload.ShadowMcpBlockedUrls != nil,
		Actor: policycore.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			DisplayName: authCtx.Email,
			Slug:        nil,
		},
	})
	if err != nil {
		return nil, err
	}
	return s.policyWithSetup(ctx, result.Row, setup)
}

func (s *Service) ListRiskPolicies(ctx context.Context, payload *gen.ListRiskPoliciesPayload) (*gen.ListRiskPoliciesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	policies, err := s.policies.List(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policies").LogError(ctx, s.logger)
	}

	result := make([]*types.RiskPolicy, 0, len(policies))
	for _, policy := range policies {
		result = append(result, policycore.ToGoa(policy))
	}
	return &gen.ListRiskPoliciesResult{Policies: result}, nil
}

func (s *Service) ListMCPPlatformToolsets(ctx context.Context, _ *gen.ListMCPPlatformToolsetsPayload) (*gen.ListMCPPlatformToolsetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	toolsets := make([]*gen.RiskMCPPlatformToolset, 0, len(s.platformToolsets))
	for slug, toolset := range s.platformToolsets {
		tools := make([]*gen.RiskMCPPlatformTool, 0, len(toolset.Tools))
		for _, tool := range toolset.Tools {
			if tool.Executor == nil {
				continue
			}
			descriptor := tool.Executor.Descriptor()
			tools = append(tools, &gen.RiskMCPPlatformTool{
				Name:        descriptor.Name,
				Annotations: descriptor.Annotations,
			})
		}
		slices.SortFunc(tools, func(a, b *gen.RiskMCPPlatformTool) int {
			return cmp.Compare(a.Name, b.Name)
		})
		toolsets = append(toolsets, &gen.RiskMCPPlatformToolset{
			ID:    platformtools.PlatformToolsetID(slug).String(),
			Slug:  slug,
			Name:  platformtools.PlatformToolsetName(slug),
			Tools: tools,
		})
	}
	slices.SortFunc(toolsets, func(a, b *gen.RiskMCPPlatformToolset) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return &gen.ListMCPPlatformToolsetsResult{Toolsets: toolsets}, nil
}

func (s *Service) ListRiskPoliciesForMcpServer(ctx context.Context, payload *gen.ListRiskPoliciesForMcpServerPayload) (*gen.ListRiskPoliciesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}
	target := policycore.MCPTarget{
		ServerID:        serverID,
		ToolName:        conv.PtrValOr(payload.ToolName, ""),
		ToolAnnotations: nil,
		PlatformToolset: false,
		Principal:       nil,
	}
	if toolset, ok := s.platformToolsetByID(serverID); ok {
		target.PlatformToolset = true
		target.ToolAnnotations = platformToolAnnotations(toolset, target.ToolName)
	}
	policies, err := s.policies.ListEnabledForMCP(
		ctx,
		authCtx.ActiveOrganizationID,
		*authCtx.ProjectID,
		target,
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policies for MCP server").LogError(ctx, s.logger)
	}

	result := make([]*types.RiskPolicy, 0, len(policies))
	for _, policy := range policies {
		result = append(result, policycore.ToGoa(policy))
	}
	return &gen.ListRiskPoliciesResult{Policies: result}, nil
}

func (s *Service) ListSessionQuarantines(ctx context.Context, _ *gen.ListSessionQuarantinesPayload) (*gen.ListSessionQuarantinesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListActiveSessionQuarantines(ctx, repo.ListActiveSessionQuarantinesParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list session quarantines").LogError(ctx, s.logger)
	}

	quarantines := make([]*gen.SessionQuarantine, 0, len(rows))
	for _, row := range rows {
		quarantines = append(quarantines, buildSessionQuarantine(row))
	}
	return &gen.ListSessionQuarantinesResult{Quarantines: quarantines}, nil
}

func (s *Service) ReleaseSessionQuarantine(ctx context.Context, payload *gen.ReleaseSessionQuarantinePayload) (*gen.SessionQuarantine, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid session quarantine id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "open transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).ReleaseSessionQuarantine(ctx, repo.ReleaseSessionQuarantineParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ReleasedBy:     conv.ToPGTextEmpty(authCtx.UserID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "release session quarantine").LogError(ctx, s.logger)
	}

	riskPolicyID := ""
	if row.RiskPolicyID.Valid {
		riskPolicyID = row.RiskPolicyID.UUID.String()
	}
	if err := s.audit.LogSessionQuarantineRelease(ctx, dbtx, audit.LogSessionQuarantineEvent{
		OrganizationID:       row.OrganizationID,
		ProjectID:            row.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		SessionQuarantineURN: urn.NewSessionQuarantine(row.ID),
		RiskPolicyName:       row.RiskPolicyName,
		Metadata: audit.SessionQuarantineMetadata{
			SessionID:      row.SessionID,
			RiskPolicyID:   riskPolicyID,
			RiskPolicyName: row.RiskPolicyName,
			UserID:         row.UserID,
			Reason:         row.Reason,
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit session quarantine release").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit session quarantine release").LogError(ctx, s.logger)
	}

	s.clearSessionQuarantineCircuit(ctx, row)

	return buildSessionQuarantine(row), nil
}

func (s *Service) clearSessionQuarantineCircuit(ctx context.Context, row repo.SessionQuarantine) {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 25 * time.Millisecond
	exp.MaxInterval = 100 * time.Millisecond
	exp.RandomizationFactor = 0

	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		if err := sessionquarantine.Delete(ctx, s.cache, row.OrganizationID, row.ProjectID.String(), row.SessionID); err != nil {
			return struct{}{}, fmt.Errorf("delete session quarantine circuit: %w", err)
		}
		return struct{}{}, nil
	}, backoff.WithBackOff(exp), backoff.WithMaxTries(sessionQuarantineCircuitDeleteAttempts))
	if err != nil {
		s.logger.ErrorContext(ctx, "clear released session quarantine circuit",
			attr.SlogError(err),
			attr.SlogOrganizationID(row.OrganizationID),
			attr.SlogProjectID(row.ProjectID.String()),
		)
	}
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

	policy, err := s.policies.Get(ctx, *authCtx.ProjectID, id)
	if err != nil {
		if errors.Is(err, policycore.ErrLoadPolicy) {
			return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load risk policy").LogError(ctx, s.logger)
	}

	return policycore.ToGoa(policy), nil
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

	mcpScope := policycore.Project(current, nil).MCPScope
	mcpScopeJSON := current.McpScope
	if payload.McpScope != nil {
		mcpScope, mcpScopeJSON, err = s.normalizeMCPScope(ctx, *authCtx.ProjectID, payload.McpScope)
		if err != nil {
			return nil, err
		}
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
	// Omit to preserve; send (possibly empty, to clear) to replace.
	if payload.DetectionScopes != nil {
		specs, err := validateDetectionScopes(s.celEng, payload.DetectionScopes)
		if err != nil {
			return nil, err
		}
		updated, err := ra.WithDetectionScopes(analyzerConfig, specs)
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
	if err := policycore.ValidateMCPScope(mcpScope, sources, action); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	if err := validateSourceAction(sources, action); err != nil {
		return nil, err
	}
	if action == policycore.ActionRequireApproval && current.Action != policycore.ActionRequireApproval && !s.toolApprovalsEnabled(ctx, authCtx) {
		return nil, oops.E(oops.CodeForbidden, nil, "tool approvals are not enabled for this organization")
	}
	approvalConfig := policycore.Project(current, nil).ApprovalConfig
	if payload.ApprovalConfig != nil {
		approvalConfig = approvalConfigFromPayload(payload.ApprovalConfig)
	}
	if action != policycore.ActionRequireApproval && payload.ApprovalConfig == nil {
		// Moving off require_approval drops the approvers it no longer uses.
		approvalConfig = nil
	}
	if err := policycore.ValidateApprovalPolicy(action, current.PolicyType, sources, customRuleIds, approvalConfig); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	if payload.ApprovalConfig != nil {
		if err := s.validateApprovers(ctx, authCtx.ActiveOrganizationID, approvalConfig); err != nil {
			return nil, err
		}
	}
	approvalConfigJSON, err := policycore.MarshalApprovalConfig(approvalConfig)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode approval config").LogError(ctx, s.logger)
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
		audiencePrincipalURNs, err = s.policies.AudiencePrincipalURNs(ctx, authCtx.ActiveOrganizationID, current.ID.String())
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
	// A blocking shadow MCP policy is one that detects shadow_mcp and denies;
	// its disposition says whether the server list it carries is a set of
	// allowed exceptions (block_all) or a set of blocked servers (allow_all).
	currentDisposition := shadowmcp.EffectiveDisposition(current.ShadowMcpDisposition, current.Sources, current.Action)
	effectiveDisposition := shadowmcp.EffectiveDisposition(current.ShadowMcpDisposition, sources, action)
	// This edit retires the blocking posture: shadow_mcp left the sources, the
	// action left block, or both.
	retiresBlockingPosture := currentDisposition != "" && effectiveDisposition == ""
	// An allow_all policy is its blocked-server list — outside the blocking
	// posture there is nothing for that list to deny, and the policy would sit
	// in the project's single blocking-policy slot enforcing nothing. A
	// block_all policy's list is only a set of exceptions to a default deny,
	// so it can retire the posture and give the exceptions up.
	if retiresBlockingPosture && currentDisposition == ShadowMCPDispositionAllowAll {
		return nil, oops.E(oops.CodeInvalid, nil, "%s", ShadowMCPRetiredAllowAllMessage(current.Sources, current.Action, sources, action))
	}

	if payload.ShadowMcpDisposition != nil {
		if effectiveDisposition == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "shadow_mcp_disposition only applies to a policy that detects shadow_mcp and denies, and this update leaves it %s", shadowMCPPolicyShape(sources, action))
		}
		if *payload.ShadowMcpDisposition != effectiveDisposition {
			return nil, oops.E(oops.CodeInvalid, nil, "a shadow mcp policy's disposition is chosen when it is created and cannot be edited: this policy is %q and cannot become %q. Delete it and create a %q policy instead", effectiveDisposition, *payload.ShadowMcpDisposition, *payload.ShadowMcpDisposition)
		}
	}

	var shadowMCPAllowedURLs []string
	allowedURLsSet := payload.ShadowMcpAllowedUrls != nil
	audienceUpdateRequested := payload.AudienceType != nil || payload.AudiencePrincipalUrns != nil
	if payload.ShadowMcpAllowedUrls != nil {
		shadowMCPAllowedURLs, err = validateShadowMCPAllowedURLs(ctx, s.shadowMCPInventoryURLLookup, *authCtx.ProjectID, enabled, sources, action, effectiveDisposition, payload.ShadowMcpAllowedUrls)
		if err != nil {
			return nil, err
		}
	}
	// Retiring the posture retires the allowed-server exceptions with it,
	// whether or not the caller thought to send an empty list. Leaving the
	// bypass grants behind would silently reinstate them the next time the
	// policy blocks again. The empty slice is load-bearing: a nil desired set
	// means "preserve" to the grant reconciler.
	if retiresBlockingPosture {
		shadowMCPAllowedURLs, allowedURLsSet = []string{}, true
	}

	var shadowMCPBlockedURLs []string
	if payload.ShadowMcpBlockedUrls != nil {
		shadowMCPBlockedURLs, err = validateShadowMCPBlockedURLs(effectiveDisposition, payload.ShadowMcpBlockedUrls)
		if err != nil {
			return nil, err
		}
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
		} else if shadowName := shadowMCPPolicyAutoName(sources, action, existingNames); shadowName != "" {
			name = shadowName
		} else {
			customRuleTitles := s.customRuleTitlesForIDs(ctx, *authCtx.ProjectID, customRuleIds)
			name = s.generatePolicyName(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sources, presidioEntities, customRuleTitles, action, existingNames)
		}
	}

	if err := validatePolicyName(name); err != nil {
		return nil, err
	}

	result, setup, err := s.updatePolicy(ctx, authCtx, policycore.UpdateMutation{
		Current: current,
		Params: repo.UpdateRiskPolicyParams{
			ID:                   id,
			ProjectID:            *authCtx.ProjectID,
			Name:                 name,
			Sources:              sources,
			PresidioEntities:     presidioEntities,
			AnalyzerConfig:       analyzerConfig,
			PromptInjectionRules: promptInjectionRules,
			DisabledRules:        disabledRules,
			CustomRuleIds:        customRuleIds,
			McpScope:             mcpScopeJSON,
			Enabled:              enabled,
			Action:               action,
			AudienceType:         audienceType,
			AutoName:             autoName,
			UserMessage:          userMessage,
			Prompt:               prompt,
			ModelConfig:          modelConfig,
			ApprovalConfig:       approvalConfigJSON,
			Score:                conv.PtrToPGFloat8(payload.Score),
		},
		AudiencePrincipals:   audiencePrincipals,
		AudienceChanged:      audienceUpdateRequested,
		AudienceDelta:        nil,
		AllowedURLs:          shadowMCPAllowedURLs,
		AllowedURLsSet:       allowedURLsSet,
		BlockedURLs:          shadowMCPBlockedURLs,
		BlockedURLsSet:       payload.ShadowMcpBlockedUrls != nil,
		EffectiveDisposition: effectiveDisposition,
		SupersedeDecisions:   payload.SupersedeDecisions != nil && *payload.SupersedeDecisions,
		ValidateLocked:       nil,
		Actor: policycore.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			DisplayName: authCtx.Email,
			Slug:        nil,
		},
	})
	if err != nil {
		return nil, err
	}
	return s.policyWithSetup(ctx, result.Row, setup)
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

	if err := shadowadmission.LockProject(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock shadow mcp admission project").LogError(ctx, s.logger)
	}
	q := repo.New(dbtx)
	if err := q.LockRiskExclusionMutations(ctx, authCtx.ProjectID.String()); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock risk exclusion mutations").LogError(ctx, s.logger)
	}
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

// ListRiskResults serves the dashboard's risk results listing. It requires
// org:admin and never returns raw `match`/`spans` content: ClickHouse rows
// carry only the ingest-time match_redacted, and the plaintext is revealed
// per finding through UnmaskRiskResult.
func (s *Service) ListRiskResults(ctx context.Context, payload *gen.ListRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	raw, err := s.listRiskResultsRaw(ctx, payload)
	if err != nil {
		return nil, err
	}

	for _, r := range raw.Results {
		if r.ChatTitle != nil {
			r.ChatTitle = new(authz.RedactSessionTitle(ctx, *r.ChatTitle, r.ChatID))
		}
	}

	return raw, nil
}

func parseOptionalMCPServerID(raw *string) (string, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return "", oops.E(oops.CodeInvalid, err, "invalid MCP server ID")
	}
	return id.String(), nil
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
	mcpServerID, err := parseOptionalMCPServerID(payload.McpServerID)
	if err != nil {
		return nil, err
	}
	chatID := ""
	if payload.ChatID != nil && strings.TrimSpace(*payload.ChatID) != "" {
		id, err := uuid.Parse(*payload.ChatID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid chat ID")
		}
		chatID = id.String()
	}
	resultIDInput := payload.ResultID
	if resultIDInput != nil && strings.TrimSpace(*resultIDInput) == "" {
		resultIDInput = nil
	}
	resultID, err := conv.PtrToNullUUID(resultIDInput)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid result ID")
	}
	executionID, ok := ParseExecutionIDFilter(conv.PtrValOr(payload.ExecutionID, ""))
	if !ok {
		return nil, oops.E(oops.CodeInvalid, nil, "invalid execution ID")
	}

	// A policy filter narrows the pushdown alongside the other filters, so
	// combinations like policy_id + rule_id are honored.
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

	var from, to *time.Time
	if fromTime.Valid {
		from = &fromTime.Time
	}
	if toTime.Valid {
		to = &toTime.Time
	}
	return s.listResultsByProjectFromClickHouse(ctx, authCtx, cursor, pageSize, policyID, mcpServerID, chatID, resultID, executionID, category, ruleID, userID, payload.ExternalUserIds, uniqueMatch, nonAssistant, assistantID, from, to)
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
		McpServerID:      payload.McpServerID,
		ResultID:         nil,
		ExecutionID:      nil,
		Category:         payload.Category,
		RuleID:           payload.RuleID,
		// The agent surface lists its own project's findings; it has no
		// identity to narrow to.
		ExternalUserIds: nil,
		UserID:          payload.UserID,
		UniqueMatch:     payload.UniqueMatch,
		NonAssistant:    payload.NonAssistant,
		AssistantID:     payload.AssistantID,
		From:            payload.From,
		To:              payload.To,
		Cursor:          payload.Cursor,
		Limit:           payload.Limit,
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
		if r.ChatTitle != nil {
			r.ChatTitle = new(authz.RedactSessionTitle(ctx, *r.ChatTitle, r.ChatID))
		}
		redacted = append(redacted, redactRiskResult(r, authCtx.ActiveOrganizationID))
	}

	return &gen.ListRiskResultsForAgentResult{
		Results:    redacted,
		TotalCount: base.TotalCount,
		NextCursor: base.NextCursor,
	}, nil
}

// UnmaskRiskResult returns the plaintext match for one risk result. Chat-backed
// findings reconstruct it from chat data; MCP findings read encrypted evidence.
// Both require chat:read for the result's chat, and successful reveals are
// audited as discrete access events.
func (s *Service) UnmaskRiskResult(ctx context.Context, payload *gen.UnmaskRiskResultPayload) (*gen.RiskUnmaskResultResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := authz.CheckSensitiveContentAccess(ctx); err != nil {
		return nil, fmt.Errorf("authorize risk match reveal: %w", err)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.C(oops.CodeInvalid)
	}

	// Resolve against ClickHouse first so mediation_surface selects the
	// chat-reconstruction or encrypted MCP-evidence path.
	return s.unmaskRiskResultFromClickHouse(ctx, authCtx, id)
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
	ruleID := conv.PtrValOrEmpty(r.RuleID, "")
	matchRedacted := redactMatch(r.Source, ruleID, r.Match, orgID)
	// ClickHouse-served rows arrive with no raw match and the ingest-time
	// redaction already stamped; pass it through instead of deriving
	// `<redacted len=0>` from the nil match.
	if r.Match == nil && r.MatchRedacted != nil {
		matchRedacted = *r.MatchRedacted
	}

	var spansRedacted []*types.RiskSpanRedacted
	if len(r.Spans) > 0 {
		spansRedacted = make([]*types.RiskSpanRedacted, 0, len(r.Spans))
		for _, sp := range r.Spans {
			match := sp.Match
			spansRedacted = append(spansRedacted, &types.RiskSpanRedacted{
				MatchRedacted: redactMatch(r.Source, ruleID, &match, orgID),
				Field:         sp.Field,
				Path:          sp.Path,
				PositionKnown: sp.StartPos != nil && sp.EndPos != nil,
			})
		}
	}

	return &types.RiskResultRedacted{
		ID:                 r.ID,
		PolicyID:           r.PolicyID,
		PolicyVersion:      r.PolicyVersion,
		ExecutionID:        r.ExecutionID,
		McpServerID:        r.McpServerID,
		MetaMcpServerID:    r.MetaMcpServerID,
		ToolsetID:          r.ToolsetID,
		ToolName:           r.ToolName,
		Phase:              r.Phase,
		MediationSurface:   r.MediationSurface,
		McpMethod:          r.McpMethod,
		PrincipalKind:      r.PrincipalKind,
		IdentityStamped:    r.IdentityStamped,
		EnforcementOutcome: r.EnforcementOutcome,
		ChatMessageID:      r.ChatMessageID,
		ChatContentPartID:  r.ChatContentPartID,
		ChatID:             r.ChatID,
		ChatTitle:          r.ChatTitle,
		UserID:             r.UserID,
		Source:             r.Source,
		RuleID:             r.RuleID,
		Description:        r.Description,
		MatchRedacted:      matchRedacted,
		PositionKnown:      r.StartPos != nil && r.EndPos != nil,
		Confidence:         r.Confidence,
		Tags:               r.Tags,
		SpansRedacted:      spansRedacted,
		CreatedAt:          r.CreatedAt,
	}
}

// RedactMatchAll encodes a match value as `<redacted len=N sha=XXXXXXXX>`,
// redacting every source with no passthrough. An empty match collapses to
// `<redacted len=0>` without a sha component so the absence of a finding payload
// is distinguishable from a real hash.
//
// The hash is salted by orgID with a NUL separator so two different orgs holding
// the same secret produce different fingerprints — defense in depth against any
// future surface that crosses an org boundary. Within an org the fingerprint
// stays deterministic so agents can still dedupe.
//
// This is the canonical redaction for the ClickHouse analytics store
// (match_redacted), where no source may store plaintext. The API-facing
// redactMatch wraps it for its non-passthrough sources, and the risk_findings
// backfill (server/cmd/tools/migrations) calls it directly, so the two never
// drift in salt layout, prefix, or sha truncation.
func RedactMatchAll(match string, orgID string) string {
	if match == "" {
		return "<redacted len=0>"
	}
	var buf []byte
	buf = append(buf, orgID...)
	buf = append(buf, 0x00)
	buf = append(buf, match...)
	sum := sha256.Sum256(buf)
	return fmt.Sprintf("<redacted len=%d sha=%s>", len(match), hex.EncodeToString(sum[:4]))
}

// redactMatch is the API-facing redaction: like RedactMatchAll, except certain
// findings pass through verbatim because the matched value is an identifier, not
// a secret:
//   - shadow_mcp / account_identity: an MCP server URL or account email IS the
//     report.
//   - an AWS access key id (the gitleaks aws-access-token rule): an identifier,
//     non-sensitive (AWS logs it in CloudTrail). Its paired secret access key
//     and session token still redact like any other secret.
func redactMatch(source, ruleID string, match *string, orgID string) string {
	if match == nil || *match == "" {
		return "<redacted len=0>"
	}
	if source == shadowmcp.SourceShadowMCP || source == ra.SourceAccountIdentity {
		return *match
	}
	// Scoped to the gitleaks source so only the built-in aws-access-token rule
	// gets the carve-out; another source reusing this rule id must not bypass
	// redaction if its match is a secret.
	if source == gitleaks.Source && ruleID == gitleaks.AccessKeyIDRuleID {
		return *match
	}
	return RedactMatchAll(*match, orgID)
}

func (s *Service) ListRiskResultsByChat(ctx context.Context, payload *gen.ListRiskResultsByChatPayload) (*gen.ListRiskResultsByChatResult, error) {
	result, err := s.listRiskResultsByChatRaw(ctx, payload)
	if err != nil {
		return nil, err
	}
	for _, chat := range result.Chats {
		if chat.ChatTitle != nil {
			chat.ChatTitle = new(authz.RedactSessionTitle(ctx, *chat.ChatTitle, new(chat.ChatID)))
		}
	}
	return result, nil
}

func (s *Service) listRiskResultsByChatRaw(ctx context.Context, payload *gen.ListRiskResultsByChatPayload) (*gen.ListRiskResultsByChatResult, error) {
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

	return s.listRiskResultsByChatFromClickHouse(ctx, authCtx, cursor, resolvePageSize(payload.Limit))
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

	if s.findingsCH == nil {
		return nil, oops.E(oops.CodeNotImplemented, nil, "risk overview requires the ClickHouse findings store").LogError(ctx, s.logger)
	}

	return s.getRiskOverviewFromClickHouse(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, from, to)
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

// topCategoriesFromCounts ranks per-category totals and drops empty ones.
func topCategoriesFromCounts(counts map[string]int64, limit int) []*gen.RiskOverviewCategory {
	if limit <= 0 {
		return nil
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

func (s *Service) ListRiskCategories(ctx context.Context, payload *gen.ListRiskCategoriesPayload) (*gen.RiskCategoriesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	defs := categories.All()
	out := make([]*gen.RiskCategoryDefinition, 0, len(defs))
	// The definition list carries trailing scanner-source fallback entries for
	// Classify precedence (duplicate secrets/pii keys); the API surface is one
	// entry per category, keeping the canonical (first) definition.
	seen := make(map[categories.Category]bool, len(defs))
	for _, def := range defs {
		if seen[def.Category] {
			continue
		}
		seen[def.Category] = true
		ruleIDs := def.RuleIDs
		if ruleIDs == nil {
			ruleIDs = []string{}
		}
		rec, ok := recommendedscopes.For(def.Category)
		if !ok {
			rec = recommendedscopes.Recommendation{
				Category:     def.Category,
				ScopeInclude: "",
				ScopeExempt:  "",
				Rationale:    "",
				Applicable:   true,
			}
		}
		out = append(out, &gen.RiskCategoryDefinition{
			Key:                        string(def.Category),
			Label:                      def.Label,
			Description:                def.Description,
			Icon:                       def.Icon,
			Source:                     def.Source,
			RuleIds:                    ruleIDs,
			RuleIDPrefix:               def.RulePrefix,
			RecommendedScopeInclude:    rec.ScopeInclude,
			RecommendedScopeExempt:     rec.ScopeExempt,
			RecommendedScopeRationale:  rec.Rationale,
			RecommendedScopeApplicable: rec.Applicable,
		})
	}
	return &gen.RiskCategoriesResult{
		Categories:               out,
		RecommendedScopesVersion: recommendedscopes.Version,
	}, nil
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

	policyIDs, err := s.enabledPolicyIDs(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve enabled risk policies").LogError(ctx, s.logger)
	}
	if len(policyIDs) == 0 {
		return &gen.RiskUserBreakdownResult{
			From:           from.UTC().Format(time.RFC3339),
			To:             to.UTC().Format(time.RFC3339),
			ExternalUserID: payload.ExternalUserID,
			Findings:       0,
			Categories:     []*gen.RiskOverviewCategory{},
			Rules:          []*gen.RiskRuleBreakdownEntry{},
		}, nil
	}
	if s.findingsCH == nil {
		return nil, oops.E(oops.CodeNotImplemented, nil, "risk user breakdown requires the ClickHouse findings store").LogError(ctx, s.logger)
	}

	chWindow := chrepo.RiskOverviewWindowParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		From:           from,
		To:             to,
		MCPServerID:    "",
	}
	categoryRows, err := s.findingsCH.ListRiskUserCategoryBreakdown(ctx, chWindow, payload.ExternalUserID, policyIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user category breakdown").LogError(ctx, s.logger)
	}

	ruleRows, err := s.findingsCH.ListRiskUserRuleBreakdown(ctx, chWindow, payload.ExternalUserID, policyIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user rule breakdown").LogError(ctx, s.logger)
	}

	categories := make([]*gen.RiskOverviewCategory, 0, len(categoryRows))
	var total int64
	for _, row := range categoryRows {
		findings := safeCount(row.Findings)
		categories = append(categories, &gen.RiskOverviewCategory{
			Category: row.Category,
			Findings: findings,
		})
		total += findings
	}

	rules := make([]*gen.RiskRuleBreakdownEntry, 0, len(ruleRows))
	for _, row := range ruleRows {
		rules = append(rules, &gen.RiskRuleBreakdownEntry{
			RuleID:   row.RuleID,
			Source:   row.Source,
			Findings: safeCount(row.Findings),
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

	prompt := strings.TrimSpace(conv.PtrValOr(payload.Prompt, ""))

	// Resolve ids server-side to reject foreign-tenant or stale finding state.
	var findings []exclusionSuggestionFinding
	if len(payload.FindingIds) > 0 {
		ids := make([]uuid.UUID, 0, len(payload.FindingIds))
		seen := make(map[uuid.UUID]struct{}, len(payload.FindingIds))
		for _, raw := range payload.FindingIds {
			id, err := uuid.Parse(raw)
			if err != nil {
				return nil, oops.E(oops.CodeInvalid, err, "invalid finding id %q", raw)
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		var err error
		findings, err = s.exclusionSuggestionFindings(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, ids)
		if err != nil {
			return nil, err
		}
	}

	if prompt == "" && len(findings) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "prompt or finding_ids is required")
	}

	if s.completionClient == nil {
		s.logger.WarnContext(ctx, "completion client not configured; returning heuristic exclusion suggestion")
		return heuristicExclusionSuggestion(prompt, findings), nil
	}

	suggestion, err := s.suggestExclusionViaLLM(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, conv.PtrValOr(authCtx.Email, ""), prompt, findings, payload.KnownRuleIds)
	if err != nil {
		s.logger.WarnContext(ctx, "openrouter exclusion suggestion failed; returning heuristic suggestion", attr.SlogError(err))
		return heuristicExclusionSuggestion(prompt, findings), nil
	}
	return suggestion, nil
}

// exclusionSuggestionFinding carries only non-sensitive finding metadata. The
// matched value is never read here: it would bypass the chat:read gate and
// audit trail of risk.unmaskResult.
type exclusionSuggestionFinding struct {
	RuleID string
	Source string
}

func (s *Service) exclusionSuggestionFindings(ctx context.Context, organizationID string, projectID uuid.UUID, ids []uuid.UUID) ([]exclusionSuggestionFinding, error) {
	rows, err := s.findingsCH.ListRiskFindingsForUnmask(ctx, chrepo.ListRiskFindingsForUnmaskParams{
		OrganizationID: organizationID,
		ProjectID:      projectID.String(),
		IDs:            ids,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk findings by ids").LogError(ctx, s.logger)
	}
	byID := make(map[uuid.UUID]*chrepo.RiskFindingUnmaskRow, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}

	findings := make([]exclusionSuggestionFinding, 0, len(ids))
	for _, id := range ids {
		row := byID[id]
		if row == nil {
			return nil, oops.E(oops.CodeInvalid, nil, "one or more finding ids are unavailable")
		}
		findings = append(findings, exclusionSuggestionFinding{
			RuleID: row.RuleID,
			Source: row.Source,
		})
	}
	return findings, nil
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
// unavailable. With a prompt, treats it as the literal value to suppress, since
// the operator typed and is knowingly disclosing that value. With only a
// findings batch, never surfaces a finding's raw matched value: that would
// bypass the audited risk.unmaskResult path. Falls back to a rule_id- or
// source-scoped exclusion built only from non-sensitive metadata.
func heuristicExclusionSuggestion(prompt string, findings []exclusionSuggestionFinding) *gen.SuggestExclusionResult {
	if prompt != "" {
		return exclusionSuggestionResult("exact", strings.TrimSpace(prompt), "", "")
	}
	if len(findings) > 0 {
		return exclusionSuggestionResultFromFindingMetadata(findings)
	}
	return exclusionSuggestionResult("exact", "", "", "")
}

// exclusionSuggestionResultFromFindingMetadata derives a coarse exclusion from
// rule_id and source only. Prefers rule_id when every finding shares one.
func exclusionSuggestionResultFromFindingMetadata(findings []exclusionSuggestionFinding) *gen.SuggestExclusionResult {
	firstRuleID := findings[0].RuleID
	sameRuleID := firstRuleID != ""
	firstSource := findings[0].Source
	sameSource := firstSource != ""
	for _, finding := range findings[1:] {
		sameRuleID = sameRuleID && finding.RuleID == firstRuleID
		sameSource = sameSource && finding.Source == firstSource
	}
	if sameRuleID {
		return exclusionSuggestionResult("rule_id", firstRuleID, "", "")
	}
	if sameSource {
		return exclusionSuggestionResult("source", firstSource, "", "")
	}
	if firstRuleID != "" {
		return exclusionSuggestionResult("rule_id", firstRuleID, "", "")
	}
	return exclusionSuggestionResult("source", firstSource, "", "")
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
	if err := policycore.ValidateCustomRuleIDs(ids); err != nil {
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
}

// validateDetectionScopes adapts generated Goa values to the transport-neutral
// policy core while retaining the existing API error shape.
func validateDetectionScopes(eng *celenv.Engine, specs []*types.RiskDetectionScope) ([]ra.DetectionScopeConfig, error) {
	inputs := make([]*policycore.DetectionScopeInput, 0, len(specs))
	for _, spec := range specs {
		if spec == nil {
			inputs = append(inputs, nil)
			continue
		}
		inputs = append(inputs, &policycore.DetectionScopeInput{
			Category:     spec.Category,
			ScopeInclude: spec.ScopeInclude,
			ScopeExempt:  spec.ScopeExempt,
		})
	}
	out, err := policycore.ValidateDetectionScopes(eng, inputs)
	if err != nil {
		if validationErr, ok := errors.AsType[*policycore.ValidationError](err); ok {
			return nil, oops.E(oops.CodeInvalid, validationErr.Cause, "%s", validationErr.Message)
		}
		return nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return out, nil
}

func (s *Service) normalizeMCPScope(
	ctx context.Context,
	projectID uuid.UUID,
	input *types.RiskMCPScope,
) (*policycore.MCPScope, []byte, error) {
	var coreInput *policycore.MCPScopeInput
	if input != nil {
		coreInput = &policycore.MCPScopeInput{
			AllServers:      input.AllServers,
			ToolAnnotations: input.ToolAnnotations,
			Servers:         make([]*policycore.MCPServerScopeInput, 0, len(input.Servers)),
		}
		for _, server := range input.Servers {
			if server == nil {
				coreInput.Servers = append(coreInput.Servers, nil)
				continue
			}
			coreInput.Servers = append(coreInput.Servers, &policycore.MCPServerScopeInput{
				MCPServerID: server.McpServerID,
				Tools:       server.Tools,
			})
		}
	}

	scope, err := policycore.NormalizeMCPScope(coreInput)
	if err != nil {
		return nil, nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	if scope == nil {
		return nil, nil, nil
	}

	serverIDs := make([]uuid.UUID, 0, len(scope.Servers))
	for _, server := range scope.Servers {
		serverIDs = append(serverIDs, server.MCPServerID)
	}
	projectServerIDs, err := s.repo.ListRiskPolicyMCPScopeServerIDs(ctx, repo.ListRiskPolicyMCPScopeServerIDsParams{
		ProjectID:    projectID,
		McpServerIds: serverIDs,
	})
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "validate MCP policy scope").LogError(ctx, s.logger)
	}
	if err := policycore.ValidateMCPScopeOwnership(scope, projectServerIDs, s.platformToolsetIDs()); err != nil {
		return nil, nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "encode MCP policy scope").LogError(ctx, s.logger)
	}
	return scope, raw, nil
}

func (s *Service) platformToolsetIDs() []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(s.platformToolsets))
	for slug := range s.platformToolsets {
		ids = append(ids, platformtools.PlatformToolsetID(slug))
	}
	return ids
}

func (s *Service) platformToolsetByID(id uuid.UUID) (platformtools.Toolset, bool) {
	for slug, toolset := range s.platformToolsets {
		if platformtools.PlatformToolsetID(slug) == id {
			return toolset, true
		}
	}
	return platformtools.Toolset{
		Slug:  "",
		Tools: nil,
	}, false
}

func platformToolAnnotations(toolset platformtools.Toolset, toolName string) *types.ToolAnnotations {
	for _, tool := range toolset.Tools {
		if tool.Executor == nil {
			continue
		}
		descriptor := tool.Executor.Descriptor()
		if descriptor.Name == toolName {
			return descriptor.Annotations
		}
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
- kind — message type string (user_message, assistant_message, tool_request, tool_response, prompt_attachment). Usually unnecessary because the body fields are already auto-scoped.

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
		MaxTokens:    nil,
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
		UserID:                 userID,
		ExternalUserID:         "",
		UserEmail:              userEmail,
		HTTPMetadata:           nil,
		JSONSchema:             &jsonSchema,
		Reasoning:              nil,
		DisableResponseHealing: false,
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

// errExclusionSuggestionInvalid marks a completion that parsed but failed
// semantic validation (bad match_type, regex that does not compile as RE2,
// empty value) — the one failure class where feeding the error back gives
// the model something it can act on, so it is worth one corrective retry.
// Transport failures, empty completions, and unparseable JSON stay outside
// it: the retry prompt carries only the error text, not the model's raw
// output, so a parse-level failure cannot self-correct and the second call
// would be wasted.
var errExclusionSuggestionInvalid = errors.New("invalid exclusion suggestion")

func (s *Service) suggestExclusionViaLLM(ctx context.Context, orgID, projectID, userID, userEmail, userPrompt string, findings []exclusionSuggestionFinding, knownRuleIDs []string) (*gen.SuggestExclusionResult, error) {
	systemPrompt := `You are a security-rules assistant for a runtime risk detection product.

Given a natural-language description of findings an operator wants to stop flagging, a batch of example findings they selected, or both, return a JSON object the dashboard uses to prefill a "create exclusion" form. An exclusion suppresses matching findings retroactively and going forward.

A finding carries the id of the rule that flagged it ("rule_id", e.g. "pii.email_address", "secret.aws_access_token", "custom.acme_token") and the detector source ("source", e.g. "gitleaks", "presidio", "prompt_injection", "custom"). When given a batch of example findings, their actual matched text is deliberately withheld from you: it's sensitive (a detected secret or PII value), and the operator hasn't reviewed or disclosed it to this request. Reason only from what the findings share by rule_id and source and suggest a "rule_id" or "source" match_type that covers them, never "exact"/"regex"/"entity_type" from batch findings alone. If the operator's own natural-language request separately names a specific value, that value came from them directly and may be used for "exact"/"regex"/"entity_type".

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

	var messageParts []string
	if userPrompt != "" {
		messageParts = append(messageParts, fmt.Sprintf("Operator request: %s", userPrompt))
	}
	if len(findings) > 0 {
		// Matched values never cross this boundary; see the system prompt.
		var b strings.Builder
		b.WriteString("Example findings the operator selected:\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "- rule_id=%q source=%q\n", f.RuleID, f.Source)
		}
		messageParts = append(messageParts, strings.TrimRight(b.String(), "\n"))
	}
	messageParts = append(messageParts, fmt.Sprintf("Known rule ids: %s", knownList))
	userMessage := strings.Join(messageParts, "\n\n")

	strict := false
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"match_type":     map[string]any{"type": "string", "enum": []string{"exact", "regex", "rule_id", "source", "entity_type"}},
			"match_value":    map[string]any{"type": "string", "minLength": 1, "maxLength": exclusioncore.RegexMaxLength},
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

	result, err := s.requestExclusionSuggestion(ctx, orgID, projectID, userID, userEmail, systemPrompt, userMessage, &jsonSchema)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, errExclusionSuggestionInvalid) {
		return nil, err
	}

	// One corrective retry with the validation error fed back. Without it, a
	// model slip (a lookahead in an otherwise fine regex, say) drops the
	// caller onto the heuristic fallback — an exact match on the operator's
	// entire prompt text — which reads as the AI failing outright.
	retryMessage := fmt.Sprintf("%s\n\nYour previous suggestion was rejected: %v\nReturn a corrected JSON object that fixes this.", userMessage, err)
	result, retryErr := s.requestExclusionSuggestion(ctx, orgID, projectID, userID, userEmail, systemPrompt, retryMessage, &jsonSchema)
	if retryErr != nil {
		return nil, fmt.Errorf("retry exclusion suggestion: %w (first attempt: %w)", retryErr, err)
	}
	return result, nil
}

// requestExclusionSuggestion performs a single completion round trip and
// validates the response with the same gate the create/update exclusion
// handlers use. Validation failures wrap errExclusionSuggestionInvalid.
func (s *Service) requestExclusionSuggestion(ctx context.Context, orgID, projectID, userID, userEmail, systemPrompt, userMessage string, jsonSchema *or.ChatJSONSchemaConfig) (*gen.SuggestExclusionResult, error) {
	suggestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	temperature := 0.2
	response, err := s.completionClient.GetObjectCompletion(suggestCtx, openrouter.ObjectCompletionRequest{
		MaxTokens:    nil,
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
		UserID:                 userID,
		ExternalUserID:         "",
		UserEmail:              userEmail,
		HTTPMetadata:           nil,
		JSONSchema:             jsonSchema,
		Reasoning:              nil,
		DisableResponseHealing: false,
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
		return nil, fmt.Errorf("%w: model returned invalid match_type %q", errExclusionSuggestionInvalid, parsed.MatchType)
	}
	// Same gate the create/update exclusion handlers apply: non-empty value,
	// and a regex must compile (RE2) and fit the length cap.
	if err := validateExclusionMatchValue(parsed.MatchType, parsed.MatchValue); err != nil {
		return nil, fmt.Errorf("%w: model returned invalid match_value: %w", errExclusionSuggestionInvalid, err)
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
	provenance := metering.RiskProvenance{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		RiskPolicyID:           uuid.Nil,
		RiskPolicyVersion:      0,
		PolicyLinkReason:       "detection_rule_playground",
		ChatID:                 uuid.Nil,
		ExternalConversationID: "",
		ChatMessageID:          uuid.Nil,
		ContentPartID:          uuid.Nil,
		MessageLinkReason:      "playground_content_not_persisted",
		OperationID:            uuid.NewString(),
		ExecutionPath:          "detection_rule_playground",
		RequestID:              "",
		MessageType:            message.User,
		HookSource:             "",
		UserID:                 authCtx.UserID,
		ToolCallID:             "",
		ToolName:               "",
		Model:                  "",
		Provider:               "",
	}
	text := payload.Text
	if ruleID == "" || text == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "rule_id and text are required")
	}

	switch {
	case strings.HasPrefix(ruleID, "secret."):
		return s.testGitleaksRule(ctx, ruleID, text, provenance)
	case strings.HasPrefix(ruleID, "pii."):
		return s.testPresidioRule(ctx, ruleID, text, provenance)
	case ruleID == "prompt_injection.default" || strings.HasPrefix(ruleID, "prompt_injection."):
		return s.testPromptInjectionRule(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), text, provenance)
	case strings.HasPrefix(ruleID, "custom."):
		return s.testCustomRule(ctx, ruleID, conv.PtrValOr(payload.DetectionExpr, ""), text, provenance)
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
	// GetChat is project-scoped and filters soft-deleted chats, so a chat in
	// another project is indistinguishable from one that does not exist.
	chatRepo := chatrepo.New(s.db)
	chatRow, err := chatRepo.GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "chat not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load chat").LogError(ctx, s.logger)
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

	occurredAt := time.Now().UTC()
	evaluation, err := ra.EvalPromptGuardrail(
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

	verdicts := evaluation.Verdicts

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
		s.recordContentScan(ctx, metering.RiskPromptPolicy(), metering.RiskProvenance{
			OrganizationID:         orgID,
			ProjectID:              projectID,
			RiskPolicyID:           uuid.Nil,
			RiskPolicyVersion:      0,
			PolicyLinkReason:       "guardrail_evaluation",
			ChatID:                 chatID,
			ExternalConversationID: "",
			ChatMessageID:          row.ID,
			ContentPartID:          uuid.Nil,
			MessageLinkReason:      "",
			OperationID:            uuid.NewString(),
			ExecutionPath:          "guardrail_evaluation",
			RequestID:              "",
			MessageType:            v.Type,
			HookSource:             "",
			UserID:                 conv.FromPGTextOrEmpty[string](chatRow.UserID),
			ToolCallID:             "",
			ToolName:               v.ToolName,
			Model:                  v.Model,
			Provider:               v.Provider,
		}, scanners.Result{
			Findings:  nil,
			STokens:   v.STokens,
			Completed: v.Completed,
		}, occurredAt)
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
		ChatID:              chatID.String(),
		Flagged:             flagged,
		JudgedCount:         len(out),
		InScopeMessageCount: evaluation.InScopeMessageCount,
		MessageLimitHit:     evaluation.MessageLimitHit,
		TotalCostUsd:        totalCostUSD,
		TotalLatencyMs:      totalLatencyMs,
		Verdicts:            out,
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

func (s *Service) testGitleaksRule(ctx context.Context, ruleID, text string, provenance metering.RiskProvenance) (*gen.TestDetectionRuleResult, error) {
	occurredAt := time.Now().UTC()
	result, err := s.gitleaksScanner.Scan(ctx, text)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run gitleaks").LogError(ctx, s.logger)
	}
	s.recordContentScan(ctx, metering.RiskGitleaks(), provenance, result, occurredAt)
	matches := make([]*gen.TestDetectionRuleMatch, 0, len(result.Findings))
	for _, f := range result.Findings {
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

func (s *Service) testPresidioRule(ctx context.Context, ruleID, text string, provenance metering.RiskProvenance) (*gen.TestDetectionRuleResult, error) {
	if s.piiScanner == nil {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("PII scanner is not configured on this server."),
		}, nil
	}
	entity := strings.ToUpper(strings.TrimPrefix(ruleID, "pii."))
	// No policy context here; apply the default threshold (0 resolves to it).
	occurredAt := time.Now().UTC()
	batches, err := s.piiScanner.AnalyzeBatch(ctx, []string{text}, []string{entity}, ra.DefaultPresidioScoreThreshold, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run presidio").LogError(ctx, s.logger)
	}
	if len(batches) > 0 {
		s.recordContentScan(ctx, metering.RiskPresidio(), provenance, batches[0], occurredAt)
	}
	matches := make([]*gen.TestDetectionRuleMatch, 0)
	if len(batches) > 0 {
		for _, f := range batches[0].Findings {
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

func (s *Service) testPromptInjectionRule(ctx context.Context, orgID, projectID, text string, provenance metering.RiskProvenance) (*gen.TestDetectionRuleResult, error) {
	if s.piScanner == nil {
		return &gen.TestDetectionRuleResult{
			Matches:   nil,
			Supported: false,
			Reason:    new("Prompt-injection scanner is not configured on this server."),
		}, nil
	}
	occurredAt := time.Now().UTC()
	result, verdict, err := s.piScanner.ScanWithVerdict(ctx, text, orgID, projectID, provenance.UserID, judgemessage.New(message.User, "", text))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "run prompt-injection scanner").LogError(ctx, s.logger)
	}
	provenance.Model = verdict.Model
	provenance.Provider = verdict.Provider
	s.recordContentScan(ctx, metering.RiskPromptInjection(), provenance, result, occurredAt)
	matches := make([]*gen.TestDetectionRuleMatch, 0, len(result.Findings))
	for _, f := range result.Findings {
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

func (s *Service) testCustomRule(ctx context.Context, ruleID, detectionExpr, text string, provenance metering.RiskProvenance) (*gen.TestDetectionRuleResult, error) {
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

	occurredAt := time.Now().UTC()
	findings, err := ra.ScanCELRules(eng, ra.MessageView{Content: text, Type: message.User, Tools: []ra.ToolView{}}, compiled)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "evaluate detection rule")
	}
	stokenCount, err := s.stokenCodec.Count(ctx, text)
	if err == nil {
		s.recordContentScan(ctx, metering.RiskCustomRules(), provenance, scanners.Result{
			Findings:  findings,
			STokens:   int64(stokenCount),
			Completed: true,
		}, occurredAt)
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

func (s *Service) recordContentScan(ctx context.Context, definition metering.Definition, provenance metering.RiskProvenance, result scanners.Result, occurredAt time.Time) {
	if !result.Completed {
		return
	}
	go func() {
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.riskRecorder.Record(recordCtx, definition, provenance, result.STokens, occurredAt); err != nil {
			s.logger.ErrorContext(recordCtx, "record content scan usage", attr.SlogError(err))
		}
	}()
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

// policyToType converts an already-loaded database row to the API type,
// enriching it through the shared transport-neutral policy core.
func (s *Service) policyToType(ctx context.Context, row repo.RiskPolicy) (*types.RiskPolicy, error) {
	policy, err := s.policies.ProjectWithAudience(ctx, row)
	if err != nil {
		return nil, fmt.Errorf("project risk policy: %w", err)
	}
	return policycore.ToGoa(policy), nil
}

func buildSessionQuarantine(row repo.SessionQuarantine) *gen.SessionQuarantine {
	var riskPolicyID *string
	if row.RiskPolicyID.Valid {
		riskPolicyID = conv.PtrEmpty(row.RiskPolicyID.UUID.String())
	}
	var releasedAt *string
	if row.ReleasedAt.Valid {
		releasedAt = conv.PtrEmpty(row.ReleasedAt.Time.Format(time.RFC3339))
	}
	return &gen.SessionQuarantine{
		ID:             row.ID.String(),
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID.String(),
		SessionID:      row.SessionID,
		RiskPolicyID:   riskPolicyID,
		RiskPolicyName: row.RiskPolicyName,
		UserID:         row.UserID,
		Reason:         row.Reason,
		CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
		ReleasedAt:     releasedAt,
		ReleasedBy:     conv.FromPGText[string](row.ReleasedBy),
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
		MaxTokens: nil,
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageUser(prompt),
		},
		Tools:                     nil,
		ToolChoice:                nil,
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
		WebSearch:                 nil,
		DisableResponseHealing:    false,
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
	if len(parts) == 0 && action == policycore.ActionRequireApproval {
		parts = append(parts, "Tool")
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
	case "quarantine":
		actionLabel = "Quarantiner"
	case policycore.ActionRequireApproval:
		actionLabel = "Approval"
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
		MaxTokens: nil,
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageUser(namePrompt),
		},
		Tools:                     nil,
		ToolChoice:                nil,
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
		WebSearch:                 nil,
		DisableResponseHealing:    false,
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
	if err := policycore.ValidateAction(action); err != nil {
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
}

func validateSources(sources []string) error {
	if err := policycore.ValidateSources(sources); err != nil {
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
}

func validateSourceAction(sources []string, action string) error {
	if err := policycore.ValidateSourceAction(sources, action); err != nil {
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
}

func validateApprovedEmailDomains(domains []string) ([]string, error) {
	out, err := policycore.NormalizeApprovedEmailDomains(domains)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return out, nil
}

func validatePolicyName(name string) error {
	if err := policycore.ValidateName(name); err != nil {
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
}

func validatePolicyType(policyType string) error {
	if err := policycore.ValidatePolicyType(policyType); err != nil {
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
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
	Temperature *float64 `json:"temperature,omitempty"`
	FailOpen    *bool    `json:"fail_open,omitempty"`
}

// validateApprovers checks that every approver names a user or role in the
// organization, so a typo cannot hold calls for an approver nobody can be.
func (s *Service) validateApprovers(ctx context.Context, organizationID string, config *policycore.ApprovalConfig) error {
	if config == nil {
		return nil
	}
	for _, approver := range config.Approvers {
		principal, err := urn.ParsePrincipal(approver)
		if err != nil {
			return oops.E(oops.CodeInvalid, err, "invalid approver %q", approver)
		}
		err = authz.ValidatePrincipal(ctx, s.db, organizationID, principal)
		if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
			return oops.E(oops.CodeInvalid, err, "approver %q is not a user or role in this organization", approver)
		}
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "check approver %q", approver).LogError(ctx, s.logger)
		}
	}
	return nil
}

// approvalConfigFromPayload converts an API approval config to its domain
// form. A nil config stays nil.
func approvalConfigFromPayload(c *types.RiskPolicyApprovalConfig) *policycore.ApprovalConfig {
	if c == nil {
		return nil
	}
	return &policycore.ApprovalConfig{Approvers: c.Approvers}
}

// marshalModelConfig encodes an API model config into the JSONB column value.
// Returns nil (SQL NULL) when no config was supplied.
func marshalModelConfig(mc *types.RiskPolicyModelConfig) ([]byte, error) {
	if mc == nil {
		return nil, nil
	}
	raw, err := json.Marshal(promptModelConfig{
		Temperature: mc.Temperature,
		FailOpen:    mc.FailOpen,
	})
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid model_config")
	}
	return raw, nil
}

// fallbackPromptPolicyName is used when the LLM naming call is unavailable
// or fails. It must not just truncate the raw prompt to some length: the
// dashboard's policy table renders the prompt itself (truncated to 60 runes)
// right next to the name, so a name that's merely a differently-truncated
// prefix of the same text reads as pointless duplication. Prefixing it and
// excerpting much shorter (breaking on a word boundary, not mid-word) keeps
// it visually distinct at a glance, even though it's still deterministic
// and prompt-derived (unlike the LLM path, this can't produce a genuine
// summary).
func fallbackPromptPolicyName(prompt string, existing []string) string {
	base := strings.TrimSpace(strings.Join(strings.Fields(prompt), " "))
	const maxExcerptRunes = 30
	if r := []rune(base); len(r) > maxExcerptRunes {
		excerpt := string(r[:maxExcerptRunes])
		if i := strings.LastIndex(excerpt, " "); i > 0 {
			excerpt = excerpt[:i]
		}
		base = excerpt + "…"
	}
	if base == "" {
		return promptPolicyNameFromBase("Prompt Policy", existing)
	}
	return promptPolicyNameFromBase("Prompt Policy: "+base, existing)
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

// toolApprovalsEnabled reports whether the organization may create
// require_approval policies. Errors fail closed.
func (s *Service) toolApprovalsEnabled(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if s.flags == nil {
		return false
	}
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagToolApprovals, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "tool approvals flag check failed; treating as disabled",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
	}
	return evaluation == feature.EvaluationEnabled
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
