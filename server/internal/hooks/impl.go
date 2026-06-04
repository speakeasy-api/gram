package hooks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/policyaccess"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	srv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
)

type Service struct {
	tracer             trace.Tracer
	logger             *slog.Logger
	db                 *pgxpool.Pool
	telemetryLogger    *telemetry.Logger
	auth               *auth.Auth
	authz              *authz.Engine
	cache              cache.Cache
	temporalEnv        *tenv.Environment
	repo               *repo.Queries
	productFeatures    ProductFeaturesClient
	chatTitleGenerator ChatTitleGenerator
	riskScanner        risk.RiskScanner
	shadowMCPClient    *shadowmcp.Client
	writer             *chat.ChatMessageWriter
	siteURL            *url.URL
	jwtSecret          string
}

// SessionMetadata contains validated session information from the Logs endpoint
type SessionMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	UserID      string
	ClaudeOrgID string
	GramOrgID   string
	ProjectID   string
}

// HookSpecificOutput is the structure for hook-specific output in responses
type HookSpecificOutput struct {
	HookEventName            *string `json:"hookEventName,omitempty"`
	AdditionalContext        *string `json:"additionalContext,omitempty"`
	PermissionDecision       *string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason *string `json:"permissionDecisionReason,omitempty"`
}

// ProductFeaturesClient checks whether product features are enabled for an org.
type ProductFeaturesClient interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// ChatTitleGenerator schedules async chat title generation.
type ChatTitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	tracerProvider trace.TracerProvider,
	telemetryLogger *telemetry.Logger,
	sessionsMgr *sessions.Manager,
	cacheAdapter cache.Cache,
	completionsClient openrouter.CompletionClient,
	temporalEnv *tenv.Environment,
	authz *authz.Engine,
	pfClient ProductFeaturesClient,
	chatTitleGenerator ChatTitleGenerator,
	riskScanner risk.RiskScanner,
	shadowMCPClient *shadowmcp.Client,
	writer *chat.ChatMessageWriter,
	siteURL *url.URL,
	jwtSecret string,
) *Service {
	return &Service{
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hooks"),
		logger:             logger.With(attr.SlogComponent("hooks")),
		db:                 db,
		telemetryLogger:    telemetryLogger,
		auth:               auth.New(logger, db, sessionsMgr, authz),
		authz:              authz,
		cache:              cacheAdapter,
		temporalEnv:        temporalEnv,
		repo:               repo.New(db),
		productFeatures:    pfClient,
		chatTitleGenerator: chatTitleGenerator,
		riskScanner:        riskScanner,
		shadowMCPClient:    shadowMCPClient,
		writer:             writer,
		siteURL:            siteURL,
		jwtSecret:          jwtSecret,
	}
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, newHooksRequestDecoder(service.logger), goahttp.ResponseEncoder, nil, nil),
	)
	AttachServerNames(mux, service)
}

// generateTraceID generates a W3C-compliant trace ID (32 hex characters)
func generateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashToolCallIDToTraceID converts a tool call ID (e.g., toolu_01SsRreQbJuFTsZS9ZszkzNR)
// into a W3C-compliant 32-character hex trace ID using SHA256 hashing
func hashToolCallIDToTraceID(toolCallID string) string {
	hash := sha256.Sum256([]byte(toolCallID))
	// Take first 16 bytes (128 bits) of the hash to create a 32-hex-char trace ID
	return hex.EncodeToString(hash[:16])
}

// generateSpanID generates a W3C-compliant span ID (16 hex characters)
func generateSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// withAuthContext returns a child logger with gram.org.id and gram.project.id
// attached when an AuthContext is present on ctx. Falls through to logger
// unchanged when there's no auth context — callers can use the result
// regardless and the unauthenticated path still emits a (less attributed)
// log line.
func (s *Service) withAuthContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return logger
	}
	logger = logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	if authCtx.ProjectID != nil {
		logger = logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	}
	return logger
}

// lookupShadowMCPBlockingPolicy returns the first enabled shadow_mcp policy
// with action=block for the given project, or nil when no such policy exists.
// A nil scanner (test setups) or lookup failure returns nil so the hook falls
// back to permissive behaviour. Flag-action policies are intentionally ignored
// here — they surface as findings via the batch scanner instead of denying at
// the hook layer.
func (s *Service) lookupShadowMCPBlockingPolicy(ctx context.Context, orgID, userID, projectID string) *risk.ShadowMCPPolicy {
	if s.riskScanner == nil || projectID == "" {
		return nil
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}
	policy, err := s.riskScanner.LookupShadowMCPBlockingPolicy(ctx, pid)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to look up shadow_mcp policy; defaulting to off",
			attr.SlogError(err),
		)
		return nil
	}
	if policy == nil {
		return nil
	}
	// Audience + bypass gate: a targeted policy only applies to in-audience
	// principals, and any principal holding a risk_policy:bypass grant for it is
	// exempt. Out-of-audience / bypassed callers get a nil policy (no block).
	// Anonymous / unresolvable callers always have the policy applied (fail-safe).
	if !s.policyAppliesToCaller(ctx, orgID, userID, policy.ID, policy.AudienceType) {
		return nil
	}
	return policy
}

// policyAppliesToCaller implements the realtime AUDIENCE rule for risk policies
// (RFC §2.1): everyone-tier always applies; a targeted policy applies only to
// in-audience principals; any failure to positively resolve a known in-org
// caller collapses to "applies" so an error can never silently skip
// enforcement. Bypass is NOT checked here — it is per-server and is evaluated at
// the block site (callerBypassesPolicy) once the target server is known.
//
// Takes the caller identity explicitly because on the hook path authCtx.UserID
// is empty (API-key auth); the resolved Gram user id lives in the OTEL-seeded
// session metadata (claude metadata.UserID, from resolveUserByEmail). Passing ""
// for userID/orgID collapses to the anonymous branch -> always scan.
func (s *Service) policyAppliesToCaller(ctx context.Context, orgID, userID, policyID, audienceType string) bool {
	everyoneTier := audienceType != risk.AudienceTypeTargeted

	if userID == "" || orgID == "" {
		return true
	}

	principals, err := authz.ResolveUserPrincipals(ctx, s.db, orgID, userID)
	if err != nil {
		// Cross-org / non-member / resolution error -> anonymous -> always scan.
		return true
	}

	audience, err := authz.ListGrantsForResource(ctx, s.db, orgID, authz.ScopeRiskPolicyEvaluate, policyID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load policy audience; applying policy",
			attr.SlogError(err),
		)
		return true
	}

	return risk.InAudience(principals, everyoneTier, audience)
}

// recordPolicyAccessRequest upserts a pending policy_access_request for a block,
// so the admin approvals queue is populated. The pending unique index dedups
// repeat blocks for the same (org, requester, policy, server) onto one row. A
// block IS the access request — "review blocked resource access requests".
// Best-effort: failures are logged, never block the hook response.
func (s *Service) recordPolicyAccessRequest(ctx context.Context, metadata SessionMetadata, policyID, serverURL string) {
	if metadata.GramOrgID == "" || metadata.ProjectID == "" {
		return
	}
	if _, err := policyaccess.RecordRequest(ctx, s.db, policyaccess.RecordRequestParams{
		OrganizationID:  metadata.GramOrgID,
		ProjectID:       metadata.ProjectID,
		PolicyID:        policyID,
		Target:          policyaccess.ShadowMCPServerTarget(serverURL),
		RequesterUserID: metadata.UserID,
		RequesterEmail:  metadata.UserEmail,
		Note:            "",
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to record policy access request", attr.SlogError(err))
	}
}

// callerBypassesPolicy reports whether the caller holds a risk_policy:bypass
// grant exempting them from this policy for the given server URL host. A bypass
// grant with no server_url selector key exempts the whole policy (every server);
// one with a server_url matches only that host. This is the unified model: the
// thing being unblocked is always the policy, narrowed by an optional server_url
// caveat. Resolution failure -> not bypassed (the policy still blocks; the
// caller can request access).
func (s *Service) callerBypassesPolicy(ctx context.Context, orgID, userID, policyID, serverURL string) bool {
	if orgID == "" || userID == "" {
		return false
	}
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, orgID, userID)
	if err != nil {
		return false
	}
	bypass, err := authz.ListGrantsForResource(ctx, s.db, orgID, authz.ScopeRiskPolicyBypass, policyID)
	if err != nil {
		return false
	}
	check := authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindRiskPolicy,
		authz.SelectorKeyResourceID:   policyID,
	}
	if serverURL != "" {
		check[authz.SelectorKeyServerURL] = serverURL
	}
	return risk.IsBypassed(principals, bypass, check)
}
