package hooks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"

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
	toolsetCache       cache.TypedCacheObject[mv.ToolsetBaseContents]
	temporalEnv        *tenv.Environment
	repo               *repo.Queries
	productFeatures    ProductFeaturesClient
	chatTitleGenerator ChatTitleGenerator
	writer             *chat.ChatMessageWriter
}

// SessionMetadata contains validated session information from the Logs endpoint
type SessionMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
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
	writer *chat.ChatMessageWriter,
) *Service {
	return &Service{
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hooks"),
		logger:             logger.With(attr.SlogComponent("hooks")),
		db:                 db,
		telemetryLogger:    telemetryLogger,
		auth:               auth.New(logger, db, sessionsMgr, authz),
		authz:              authz,
		cache:              cacheAdapter,
		toolsetCache:       cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheAdapter, cache.SuffixNone),
		temporalEnv:        temporalEnv,
		repo:               repo.New(db),
		productFeatures:    pfClient,
		chatTitleGenerator: chatTitleGenerator,
		writer:             writer,
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
		srv.New(endpoints, mux, claudeRequestDecoder, goahttp.ResponseEncoder, nil, nil),
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

// blockShadowMCPEnabled reports whether the FeatureBlockShadowMCP gate is on
// for the given org. A nil productFeatures client (test setups) or any lookup
// failure returns false so hooks default to permissive behaviour.
func (s *Service) blockShadowMCPEnabled(ctx context.Context, orgID string) bool {
	if s.productFeatures == nil || orgID == "" {
		return false
	}
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, orgID, productfeatures.FeatureBlockShadowMCP)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to check block_shadow_mcp feature; defaulting to off",
			attr.SlogError(err),
		)
		return false
	}
	return enabled
}

// validateGramToolsetCall enforces that a Gram-hosted tool call carries the
// required "x-gram-toolset-id" property in its input, that the referenced
// toolset exists in the calling organization, and that the toolset contains a
// tool whose post-variation name matches toolName. Returns (reason, true) when
// the call must be denied. The reason is wrapped with a user-facing prefix so
// the underlying detail is preserved without leaking implementation jargon.
func (s *Service) validateGramToolsetCall(
	ctx context.Context,
	toolInput any,
	toolName string,
	orgID string,
) (string, bool) {
	deny := func(detail string) (string, bool) {
		return fmt.Sprintf("MCP server not managed through Speakeasy (%s)", detail), true
	}

	inputMap, ok := toolInput.(map[string]any)
	if !ok {
		return deny(fmt.Sprintf("missing required %q property in tool input", xGramToolsetIDField))
	}
	rawID, ok := inputMap[xGramToolsetIDField].(string)
	if !ok || rawID == "" {
		return deny(fmt.Sprintf("missing required %q property in tool input", xGramToolsetIDField))
	}
	toolsetID, err := uuid.Parse(rawID)
	if err != nil {
		return deny(fmt.Sprintf("invalid %q value: not a UUID", xGramToolsetIDField))
	}

	toolsetRow, err := tsr.New(s.db).GetToolsetByIDAndOrganization(ctx, tsr.GetToolsetByIDAndOrganizationParams{
		ID:             toolsetID,
		OrganizationID: orgID,
	})
	if err != nil {
		return deny(fmt.Sprintf("toolset %s not found in this organization", toolsetID))
	}

	if toolName == "" {
		return deny("tool call missing tool name")
	}

	described, err := mv.DescribeToolset(
		ctx,
		s.logger,
		s.db,
		mv.ProjectID(toolsetRow.ProjectID),
		mv.ToolsetSlug(toolsetRow.Slug),
		&s.toolsetCache,
	)
	if err != nil {
		return deny(fmt.Sprintf("failed to load toolset %s", toolsetID))
	}

	for _, tool := range described.Tools {
		base, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		if base.Name == toolName {
			return "", false
		}
	}

	return deny(fmt.Sprintf("tool %q is not part of toolset %s", toolName, toolsetID))
}
