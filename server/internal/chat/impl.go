package chat

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/sync/errgroup"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	gen "github.com/speakeasy-api/gram/server/gen/chat"
	srv "github.com/speakeasy-api/gram/server/gen/http/chat/server"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

type Service struct {
	auth             *auth.Auth
	authz            *authz.Engine
	db               *pgxpool.Pool
	repo             *repo.Queries
	tracer           trace.Tracer
	openRouter       openrouter.Provisioner
	completionClient openrouter.CompletionClient
	contextWindow    *openrouter.ContextWindowResolver
	logger           *slog.Logger
	sessions         *sessions.Manager
	chatSessions     *chatsessions.Manager
	assistantTokens  *assistanttokens.Manager
	assetStorage     assets.BlobStore
	posthog          *posthog.Posthog
	telemetryService *telemetry.Service
	billingRepo      billing.Repository
	audit            *audit.Logger
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	chatSessions *chatsessions.Manager,
	openRouter openrouter.Provisioner,
	completionClient openrouter.CompletionClient,
	contextWindow *openrouter.ContextWindowResolver,
	posthog *posthog.Posthog,
	telemetryService *telemetry.Service,
	assetStorage assets.BlobStore,
	authzEngine *authz.Engine,
	assistantTokens *assistanttokens.Manager,
	billingRepo billing.Repository,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("chat"))

	return &Service{
		auth:             auth.New(logger, db, sessions, authzEngine),
		authz:            authzEngine,
		db:               db,
		sessions:         sessions,
		chatSessions:     chatSessions,
		assistantTokens:  assistantTokens,
		logger:           logger,
		repo:             repo.New(db),
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/chat"),
		openRouter:       openRouter,
		completionClient: completionClient,
		contextWindow:    contextWindow,
		assetStorage:     assetStorage,
		posthog:          posthog,
		telemetryService: telemetryService,
		billingRepo:      billingRepo,
		audit:            auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))

	server := srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	srv.Mount(mux, server)

	o11y.AttachHandler(mux, "POST", "/chat/completions", oops.ErrHandle(service.logger, service.HandleCompletion).ServeHTTP)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) JWTAuth(ctx context.Context, token string, schema *security.JWTScheme) (context.Context, error) {
	return s.chatSessions.Authorize(ctx, token)
}

// directAuthorize performs authentication and authorization for chat requests.
// It tries session auth first, then API key auth, then chat session token as fallback.
// It also validates the project header and ensures ProjectID is present.
func (s *Service) directAuthorize(ctx context.Context, r *http.Request) (context.Context, *contextvalues.AuthContext, error) {
	if token := r.Header.Get("Authorization"); token != "" {
		authorizedCtx, _, err := s.assistantTokens.Authorize(ctx, token)
		if err == nil {
			authCtx, ok := contextvalues.GetAuthContext(authorizedCtx)
			if !ok || authCtx == nil || authCtx.ProjectID == nil {
				return authorizedCtx, nil, oops.C(oops.CodeUnauthorized)
			}
			return authorizedCtx, authCtx, nil
		}
	}

	// Try session auth first
	sc := security.APIKeyScheme{
		Name:           constants.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}

	authorizedCtx, err := s.auth.Authorize(ctx, r.Header.Get(constants.SessionHeader), &sc)

	// Try API key auth if session auth fails
	if err != nil {
		sc := security.APIKeyScheme{
			Name:           constants.KeySecurityScheme,
			RequiredScopes: []string{"chat"},
			Scopes:         []string{},
		}
		authorizedCtx, err = s.auth.Authorize(ctx, r.Header.Get(constants.APIKeyHeader), &sc)
	}

	// Try Chat Sessions auth if API key auth fails
	if err != nil {
		token := r.Header.Get(constants.ChatSessionsTokenHeader)
		authorizedCtx, err = s.chatSessions.Authorize(ctx, token)
		if err != nil {
			return authorizedCtx, nil, oops.E(oops.CodeUnauthorized, err, "unauthorized access")
		}
	}

	// Authorize with project
	sc = security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	authorizedCtx, err = s.auth.Authorize(authorizedCtx, r.Header.Get(constants.ProjectHeader), &sc)
	if err != nil {
		return authorizedCtx, nil, oops.E(oops.CodeUnauthorized, err, "unauthorized access")
	}

	authCtx, ok := contextvalues.GetAuthContext(authorizedCtx)
	if !ok {
		return authorizedCtx, nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return authorizedCtx, nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized: project id is required")
	}

	return authorizedCtx, authCtx, nil
}

func (s *Service) ListChats(ctx context.Context, payload *gen.ListChatsPayload) (*gen.ListChatsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	var fromTime, toTime pgtype.Timestamptz
	if payload.From != nil {
		t, err := time.Parse(time.RFC3339, *payload.From)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid from timestamp").LogError(ctx, s.logger)
		}
		fromTime = pgtype.Timestamptz{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
	}
	if payload.To != nil {
		t, err := time.Parse(time.RFC3339, *payload.To)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid to timestamp").LogError(ctx, s.logger)
		}
		toTime = pgtype.Timestamptz{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
	}

	search := conv.PtrValOr(payload.Search, "")
	assistantID := conv.PtrValOr(payload.AssistantID, "")
	hasRiskFilter := conv.PtrValOr(payload.HasRisk, "")
	// -1 is the "no threshold" sentinel: the queries short-circuit to "show all"
	// on a negative bound. A real bound N keeps chats with at least N findings
	// (inclusive), matching the "Min risk score" control.
	minRiskScore := int32(-1)
	if payload.MinRiskScore != nil {
		minRiskScore = conv.SafeInt32(*payload.MinRiskScore)
	}

	// Visibility scoping: callers holding an unrestricted chat:read grant and the
	// managed-assistant runtime see all project sessions (optionally narrowed by
	// an explicit external user id); everyone else is restricted to their own
	// sessions.
	externalUserID, userID, err := s.chatVisibilityScope(ctx, authCtx, payload.ExternalUserID)
	if err != nil {
		return nil, err
	}

	baseParams := repo.CountChatsParams{
		ProjectID:      *authCtx.ProjectID,
		ExternalUserID: externalUserID,
		UserID:         userID,
		FromTime:       fromTime,
		ToTime:         toTime,
		Search:         search,
		AssistantID:    assistantID,
		HasRiskFilter:  hasRiskFilter,
		MinRiskScore:   minRiskScore,
		Pinned:         conv.PtrValOr(payload.Pinned, ""),
		Sources:        parseSourceFilter(conv.PtrValOr(payload.Source, "")),
	}

	total, err := s.repo.CountChats(ctx, baseParams)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count chats").LogError(ctx, s.logger)
	}

	rows, err := s.repo.ListChats(ctx, repo.ListChatsParams{
		ProjectID:      baseParams.ProjectID,
		ExternalUserID: baseParams.ExternalUserID,
		UserID:         baseParams.UserID,
		FromTime:       baseParams.FromTime,
		ToTime:         baseParams.ToTime,
		Search:         baseParams.Search,
		AssistantID:    baseParams.AssistantID,
		HasRiskFilter:  baseParams.HasRiskFilter,
		MinRiskScore:   baseParams.MinRiskScore,
		Pinned:         baseParams.Pinned,
		Sources:        baseParams.Sources,
		SortBy:         payload.SortBy,
		SortOrder:      payload.SortOrder,
		PageLimit:      conv.SafeInt32(payload.Limit),
		PageOffset:     conv.SafeInt32(payload.Offset),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list chats").LogError(ctx, s.logger)
	}

	result := make([]*gen.ChatOverview, 0, len(rows))
	for _, row := range rows {
		lastMessageTimestamp := row.CreatedAt.Time.Format(time.RFC3339)
		if row.LastMessageTimestamp.Valid {
			lastMessageTimestamp = row.LastMessageTimestamp.Time.Format(time.RFC3339)
		}
		riskCount := int(row.RiskFindingsCount)
		result = append(result, &gen.ChatOverview{
			ID:                   row.ID.String(),
			UserID:               conv.FromPGText[string](row.UserID),
			ExternalUserID:       conv.FromPGText[string](row.ExternalUserID),
			Source:               conv.FromPGText[string](row.Source),
			Title:                row.Title.String,
			NumMessages:          int(row.NumMessages),
			CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
			LastMessageTimestamp: lastMessageTimestamp,
			RiskFindingsCount:    &riskCount,
			TotalInputTokens:     nil,
			TotalOutputTokens:    nil,
			TotalTokens:          nil,
			TotalCost:            nil,
		})
	}

	if err := s.enrichChatsWithMetrics(ctx, authCtx.ProjectID.String(), result); err != nil {
		s.logger.WarnContext(ctx, "failed to enrich chats with metrics", attr.SlogError(err))
	}

	return &gen.ListChatsResult{Chats: result, Total: int(total)}, nil
}

// logChatAccess records an audit entry that a dashboard user opened a chat
// session transcript. It is written with the pool directly (no surrounding
// transaction) because it describes a read, not a mutation.
func (s *Service) logChatAccess(ctx context.Context, authCtx *contextvalues.AuthContext, chat repo.Chat) error {
	if err := s.audit.LogChatSessionAccess(ctx, s.db, audit.LogChatSessionAccessEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        chat.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ChatSessionURN:   urn.NewChatSession(chat.ID),
		ChatTitle:        chat.Title.String,
		OwnerUserID:      chat.UserID.String,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record chat access audit log").LogError(ctx, s.logger)
	}
	return nil
}

// ListSources returns the distinct agent sources present in the project's chats
// so the dashboard can populate the agent-type filter from real data instead of
// a hardcoded catalog. Honors the same visibility scoping as ListChats.
func (s *Service) ListSources(ctx context.Context, payload *gen.ListSourcesPayload) (*gen.ListSourcesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	externalUserID, userID, err := s.chatVisibilityScope(ctx, authCtx, nil)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.ListChatSources(ctx, repo.ListChatSourcesParams{
		ProjectID:      *authCtx.ProjectID,
		ExternalUserID: externalUserID,
		UserID:         userID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list chat sources").LogError(ctx, s.logger)
	}

	sources := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Valid {
			sources = append(sources, row.String)
		}
	}

	return &gen.ListSourcesResult{Sources: sources}, nil
}

// parseSourceFilter splits the comma-separated `source` filter into the list of
// exact source strings matched against each chat's inferred source. It always
// returns a non-nil slice so the no-filter case sends an empty text[]
// (cardinality 0 disables the filter) rather than SQL NULL, which would drop
// every row.
func parseSourceFilter(source string) []string {
	seen := make(map[string]struct{})
	sources := []string{}
	for raw := range strings.SplitSeq(source, ",") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		sources = append(sources, s)
	}
	return sources
}

// chatVisibilityScope resolves the (external_user_id, user_id) scoping shared by
// the chat listing endpoints. Callers holding an unrestricted chat:read grant
// (only admins do) and the managed-assistant runtime see all project sessions
// (optionally narrowed by an explicit external user id); everyone else is
// restricted to their own sessions. Both empty means "all chats in the project".
// Visibility is never a hard gate on the route — when the chat:read check can't
// be made we fall back to own-sessions rather than failing.
func (s *Service) chatVisibilityScope(ctx context.Context, authCtx *contextvalues.AuthContext, payloadExternalUserID *string) (string, string, error) {
	// An assistant principal is set only on the assistant runtime path and only
	// the managed-assistant platform toolset surfaces chat tools, so treat it as
	// admin-equivalent for project-wide visibility.
	_, isAssistantCall := contextvalues.GetAssistantPrincipal(ctx)

	// Whether the caller sees all project sessions or only their own is decided by
	// the chat:read RBAC scope. Only admins hold a chat:read grant; members hold
	// none and fall through to own-session visibility (filtered by user_id in SQL,
	// which keeps the list paginated and its `total` correct). Members still read
	// their own sessions; the per-session chat.load route grants that via
	// owner-matching, not via chat:read.
	//
	// When RBAC is not enforced for the org we must NOT fall through to "see all"
	// — Require short-circuits to allow when enforcement is off, so check
	// ShouldEnforce explicitly and treat the disabled case as constrained.
	canReadAllSessions := false
	if enforce, err := s.authz.ShouldEnforce(ctx); err != nil {
		s.logger.WarnContext(ctx, "could not determine RBAC enforcement for chat visibility; showing own sessions", attr.SlogError(err))
	} else if enforce {
		err := s.authz.Require(ctx, authz.ChatReadCheck(authCtx.ProjectID.String()))
		var shareableErr *oops.ShareableError
		switch {
		case err == nil:
			canReadAllSessions = true
		case errors.As(err, &shareableErr) && shareableErr.Code == oops.CodeForbidden:
			// Forbidden simply means the caller can only read their own sessions.
		default:
			// Any other error is unexpected; log it but still serve own sessions
			// rather than failing the listing.
			s.logger.WarnContext(ctx, "chat:read visibility check failed for chat listing; showing own sessions", attr.SlogError(err))
		}
	}

	switch {
	case authCtx.ExternalUserID != "":
		return authCtx.ExternalUserID, "", nil
	case canReadAllSessions, isAssistantCall:
		return conv.PtrValOr(payloadExternalUserID, ""), "", nil
	default:
		if authCtx.UserID == "" {
			return "", "", oops.C(oops.CodeUnauthorized)
		}
		return "", authCtx.UserID, nil
	}
}

func (s *Service) LoadChat(ctx context.Context, payload *gen.LoadChatPayload) (*gen.Chat, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	chatID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat ID")
	}

	chat, err := s.repo.GetChat(ctx, chatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").LogError(ctx, s.logger)
	}

	// older chat_messages may not have project_id in the model, but it will always exist on the chat
	if chat.ProjectID != *authCtx.ProjectID {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Off-dashboard callers must match the chat owner unless they're the
	// managed-assistant runtime (see ListChats).
	_, isAssistantCall := contextvalues.GetAssistantPrincipal(ctx)
	if authCtx.SessionID == nil {
		if !isAssistantCall {
			if chat.ExternalUserID.String != "" && chat.ExternalUserID.String != authCtx.ExternalUserID {
				return nil, oops.C(oops.CodeUnauthorized)
			}
		}
	}

	// Gate dashboard access on chat:read. The check is a no-op unless RBAC is
	// enforced for the org (enterprise + feature flag + session). Members can
	// always read sessions they own, so bypass the scope check for the owner;
	// reading anyone else's session requires an unrestricted chat:read grant,
	// which only admins hold. The managed-assistant runtime is exempt — it
	// consumes transcripts programmatically, not as a reviewer.
	isOwner := authCtx.UserID != "" && chat.UserID.Valid && chat.UserID.String == authCtx.UserID
	if !isAssistantCall && !isOwner {
		if err := s.authz.Require(ctx, authz.ChatReadCheck(chat.ID.String())); err != nil {
			return nil, err
		}
	}

	// Record dashboard session-opens in the audit log. Scroll pagination
	// (before_seq/after_seq) reuses the same open and is not re-logged; only
	// session-authenticated (dashboard) reads are recorded, since chat-token,
	// external-user, and assistant reads are the owner/runtime consuming their
	// own transcript rather than a reviewer accessing a session.
	if authCtx.SessionID != nil && payload.BeforeSeq == nil && payload.AfterSeq == nil {
		if err := s.logChatAccess(ctx, authCtx, chat); err != nil {
			return nil, err
		}
	}

	maxGeneration, err := s.repo.GetMaxGenerationForChat(ctx, chat.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat generation").LogError(ctx, s.logger)
	}

	generation := maxGeneration
	if payload.Generation != nil {
		requested := *payload.Generation
		if requested < 0 || int64(requested) > int64(maxGeneration) {
			return nil, oops.E(oops.CodeInvalid, nil, "generation out of range")
		}
		generation = int32(requested) //nolint:gosec // bounded by maxGeneration above
	}

	limit := payload.Limit
	if limit < 1 {
		limit = defaultLoadChatLimit
	}
	if limit > maxLoadChatLimit {
		limit = maxLoadChatLimit
	}

	var (
		resultMessages []*gen.ChatMessage
		hasMoreBefore  bool
		hasMoreAfter   bool
		riskSegments   []*gen.RiskSegment
		matchSegments  []*gen.RiskSegment
		// latestPageRows holds the repo rows of the initial newest page so we can
		// infer the chat source from them; only populated on that first request.
		latestPageRows []repo.ChatMessage
	)

	// query enables the search-windowed view; it's mutually exclusive with the
	// risk-only view. Trim so a whitespace-only query is treated as no query.
	queryStr := ""
	if payload.Query != nil {
		queryStr = strings.TrimSpace(*payload.Query)
	}
	if queryStr != "" && payload.RiskOnly {
		return nil, oops.E(oops.CodeInvalid, nil, "query and risk_only are mutually exclusive")
	}

	// The initial request (latest generation, no cursors, not risk-only, not a
	// search) is the only one that carries source inference and ClickHouse/Claude
	// enrichment: the dashboard consumes those once from the first page, and they
	// depend on the most recent messages which that page contains.
	isInitialLatest := generation == maxGeneration &&
		payload.BeforeSeq == nil && payload.AfterSeq == nil &&
		!payload.RiskOnly && queryStr == ""

	switch {
	case payload.RiskOnly:
		rows, err := s.repo.ListRiskWindowedMessages(ctx, repo.ListRiskWindowedMessagesParams{
			ContextSize: riskContextWindow,
			ProjectID:   *authCtx.ProjectID,
			ChatID:      chat.ID,
			Generation:  generation,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load risk-windowed messages").LogError(ctx, s.logger)
		}
		resultMessages = make([]*gen.ChatMessage, len(rows))
		for i := range rows {
			r := rows[i]
			toolCalls := string(r.ToolCalls)
			// is_risk marks the flagged messages themselves (vs the surrounding
			// context windowed in around them) so the dashboard can filter to
			// findings without the server exposing internal seq positions.
			isRisk := r.IsRisk
			resultMessages[i] = &gen.ChatMessage{
				ID:             r.ID.String(),
				Seq:            r.Seq,
				IsRisk:         &isRisk,
				Role:           r.Role,
				Model:          r.Model.String,
				UserID:         &r.UserID.String,
				ExternalUserID: &r.ExternalUserID.String,
				Content:        s.loadMessageContentFields(ctx, r.ChatID, r.Content, r.ContentRaw, r.ContentAssetUrl),
				ToolCalls:      &toolCalls,
				ToolCallID:     &r.ToolCallID.String,
				FinishReason:   &r.FinishReason.String,
				PromptID:       conv.FromPGText[string](r.MessageID),
				CreatedAt:      r.CreatedAt.Time.Format(time.RFC3339),
				Generation:     int(r.Generation),
			}
		}
		riskSegments = buildRiskSegments(rows)
		if len(riskSegments) > 0 {
			hasMoreBefore = riskSegments[0].HasMoreBefore
			hasMoreAfter = riskSegments[len(riskSegments)-1].HasMoreAfter
		}

	case queryStr != "":
		// Search view: messages matching the query plus a fixed context window,
		// grouped into contiguous segments — same windowing as risk-only. Cursors
		// are ignored on this initial request; the dashboard expands segments with
		// plain before_seq/after_seq follow-ups. Message construction mirrors the
		// risk_only branch above.
		rows, err := s.repo.ListSearchWindowedMessages(ctx, repo.ListSearchWindowedMessagesParams{
			ContextSize: searchContextWindow,
			ProjectID:   *authCtx.ProjectID,
			ChatID:      chat.ID,
			Generation:  generation,
			Query:       queryStr,
			MatchLimit:  searchMatchLimit,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load search-windowed messages").LogError(ctx, s.logger)
		}
		resultMessages = make([]*gen.ChatMessage, len(rows))
		for i := range rows {
			r := rows[i]
			toolCalls := string(r.ToolCalls)
			resultMessages[i] = &gen.ChatMessage{
				ID:             r.ID.String(),
				Seq:            r.Seq,
				IsRisk:         nil, // is_risk is a risk_only-mode signal
				Role:           r.Role,
				Model:          r.Model.String,
				UserID:         &r.UserID.String,
				ExternalUserID: &r.ExternalUserID.String,
				Content:        s.loadMessageContentFields(ctx, r.ChatID, r.Content, r.ContentRaw, r.ContentAssetUrl),
				ToolCalls:      &toolCalls,
				ToolCallID:     &r.ToolCallID.String,
				FinishReason:   &r.FinishReason.String,
				PromptID:       conv.FromPGText[string](r.MessageID),
				CreatedAt:      r.CreatedAt.Time.Format(time.RFC3339),
				Generation:     int(r.Generation),
			}
		}
		matchSegments = buildSearchSegments(rows)
		if len(matchSegments) > 0 {
			hasMoreBefore = matchSegments[0].HasMoreBefore
			hasMoreAfter = matchSegments[len(matchSegments)-1].HasMoreAfter
		}

	case payload.AfterSeq != nil:
		// Scroll down: messages newer than the cursor, oldest first. Fetch one
		// extra row to detect whether still-newer messages remain.
		rows, err := s.repo.ListChatMessagesAfterPage(ctx, repo.ListChatMessagesAfterPageParams{
			ChatID:     chat.ID,
			ProjectID:  *authCtx.ProjectID,
			Generation: generation,
			AfterSeq:   pgtype.Int8{Int64: *payload.AfterSeq, Valid: true},
			Lim:        int32(limit + 1),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat messages").LogError(ctx, s.logger)
		}
		if len(rows) > limit {
			hasMoreAfter = true
			rows = rows[:limit]
		}
		// We paged forward from an existing anchor, so older messages exist.
		hasMoreBefore = true
		resultMessages = s.buildGenMessages(ctx, rows)

	case payload.FromStart && payload.BeforeSeq == nil:
		// Start of the thread: oldest page, ascending. A NULL cursor returns from
		// the very beginning. Fetch one extra row to detect whether newer messages
		// remain. (before_seq takes precedence per the design, hence the guard.)
		rows, err := s.repo.ListChatMessagesAfterPage(ctx, repo.ListChatMessagesAfterPageParams{
			ChatID:     chat.ID,
			ProjectID:  *authCtx.ProjectID,
			Generation: generation,
			AfterSeq:   pgtype.Int8{Int64: 0, Valid: false}, // null → oldest page
			Lim:        int32(limit + 1),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat messages").LogError(ctx, s.logger)
		}
		if len(rows) > limit {
			hasMoreAfter = true
			rows = rows[:limit]
		}
		// We're at the start of the thread, so nothing older remains.
		hasMoreBefore = false
		// This is still an initial (cursorless, latest-generation) load, so it
		// carries source inference + ClickHouse enrichment like the newest page.
		if isInitialLatest {
			latestPageRows = rows
		}
		resultMessages = s.buildGenMessages(ctx, rows)

	default:
		// Initial newest page (no cursor) or scroll up via before_seq. Query DESC
		// so LIMIT keeps the most recent rows, fetch one extra to detect more, then
		// reverse to ascending for display.
		var beforeSeq pgtype.Int8
		if payload.BeforeSeq != nil {
			beforeSeq = pgtype.Int8{Int64: *payload.BeforeSeq, Valid: true}
		}
		rows, err := s.repo.ListChatMessagesBeforePage(ctx, repo.ListChatMessagesBeforePageParams{
			ChatID:     chat.ID,
			ProjectID:  *authCtx.ProjectID,
			Generation: generation,
			BeforeSeq:  beforeSeq,
			Lim:        int32(limit + 1),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat messages").LogError(ctx, s.logger)
		}
		if len(rows) > limit {
			hasMoreBefore = true
			rows = rows[:limit]
		}
		slices.Reverse(rows)
		// A before_seq request pages backward from an anchor, so newer messages exist.
		hasMoreAfter = payload.BeforeSeq != nil
		if isInitialLatest {
			latestPageRows = rows
		}
		resultMessages = s.buildGenMessages(ctx, rows)
	}

	// Chat-wide aggregates (count + most recent message timestamp) are computed
	// from a single cheap query so every paginated response carries the chat's
	// real totals regardless of which page was requested.
	stats, err := s.repo.GetChatMessageStats(ctx, repo.GetChatMessageStatsParams{
		ChatID:    chat.ID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat message stats").LogError(ctx, s.logger)
	}

	lastMessageTimestamp := chat.CreatedAt.Time.Format(time.RFC3339)
	if stats.LastMessageAt.Valid {
		lastMessageTimestamp = stats.LastMessageAt.Time.Format(time.RFC3339)
	}

	// Whole-generation trace-entry totals so the detail sheet's filter bar can
	// show real counts even though messages are paginated. Scoped to the loaded
	// generation to stay consistent with the (also generation-scoped) transcript
	// and risk-windowed view.
	totals, err := s.repo.GetChatEntryTotals(ctx, repo.GetChatEntryTotalsParams{
		ChatID:     chat.ID,
		ProjectID:  *authCtx.ProjectID,
		Generation: generation,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat entry totals").LogError(ctx, s.logger)
	}

	var source *string
	if isInitialLatest {
		for i := len(latestPageRows) - 1; i >= 0; i-- {
			if latestPageRows[i].Source.Valid && latestPageRows[i].Source.String != "" {
				v := latestPageRows[i].Source.String
				source = &v
				break
			}
		}
	}

	result := &gen.Chat{
		ID:                   chat.ID.String(),
		Title:                chat.Title.String,
		UserID:               &chat.UserID.String,
		ExternalUserID:       &chat.ExternalUserID.String,
		Source:               source,
		NumMessages:          int(stats.Total),
		CreatedAt:            chat.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            chat.UpdatedAt.Time.Format(time.RFC3339),
		LastMessageTimestamp: lastMessageTimestamp,
		RiskFindingsCount:    nil,
		Messages:             resultMessages,
		Generation:           int(generation),
		MaxGeneration:        int(maxGeneration),
		HasMoreBefore:        hasMoreBefore,
		HasMoreAfter:         hasMoreAfter,
		RiskSegments:         riskSegments,
		MatchSegments:        matchSegments,
		Totals: &gen.ChatTotals{
			Total:             totals.Total,
			UserMessages:      totals.UserMessages,
			AssistantMessages: totals.AssistantMessages,
			ToolCalls:         totals.ToolCalls,
			ToolResults:       totals.ToolResults,
			RiskOnly:          totals.RiskFindings,
		},
		TotalInputTokens:  nil,
		TotalOutputTokens: nil,
		TotalTokens:       nil,
		TotalCost:         nil,
		AgentUsage:        nil,
	}

	if isInitialLatest {
		if err := s.enrichChatWithMetrics(ctx, authCtx.ProjectID.String(), result); err != nil {
			s.logger.WarnContext(ctx, "failed to enrich chat with metrics", attr.SlogError(err))
		}
		if err := s.enrichChatWithClaudeTurnUsage(ctx, authCtx.ProjectID.String(), result); err != nil {
			s.logger.WarnContext(ctx, "failed to enrich chat with Claude turn usage", attr.SlogError(err))
		}
	}

	return result, nil
}

// httpMetadata contains HTTP request metadata to be stored with chat messages.
type httpMetadata struct {
	Origin    string
	UserAgent string
	IPAddress string
	Source    string
}

// extractHTTPMetadata extracts metadata from the HTTP request.
func extractHTTPMetadata(r *http.Request) httpMetadata {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}

	userAgent := r.Header.Get("User-Agent")

	ipAddress := r.Header.Get("X-Forwarded-For")
	if ipAddress == "" {
		ipAddress = r.Header.Get("X-Real-IP")
	}
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
	}

	source := r.Header.Get(constants.HeaderSource)

	return httpMetadata{
		Origin:    origin,
		UserAgent: userAgent,
		IPAddress: ipAddress,
		Source:    source,
	}
}

// checkCreditBalance rejects the request when the org has consumed its granted
// credits. Reads only the cached period usage so the gate stays cheap; on
// cache miss we fail open and rely on the OpenRouter per-key monthly limit as
// the hard backstop. Speakeasy-internal orgs (specialLimitOrgs) bypass.
//
// Phase 0: only enforce the hard gate on free-tier orgs. Pro/enterprise stay
// bounded by the OpenRouter monthly key cap (creditsAccountTypeMap) until the
// two limit sources are unified — see AGE-2122.
func (s *Service) checkCreditBalance(ctx context.Context, orgID, accountType string) error {
	if openrouter.IsSpecialLimitOrg(orgID) {
		return nil
	}

	if accountType != string(billing.TierBase) && accountType != "" {
		return nil
	}

	pu, err := s.billingRepo.GetStoredPeriodUsage(ctx, orgID)
	if err != nil {
		s.logger.WarnContext(ctx, "credit balance cache miss; allowing request",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return nil
	}

	if pu.IncludedCredits > 0 && pu.Credits >= pu.IncludedCredits {
		return oops.C(oops.CodeInsufficientCredits).LogError(
			ctx, s.logger,
			attr.SlogOrganizationID(orgID),
		)
	}

	return nil
}

// historyCorruptedMarker rides on /chat/completions 4xx response bodies and
// survives the runner's "provider error: <body>" wrap so the assistant
// runtime can detect upstream history rejection without sniffing each
// provider's evolving wording. Detection is exposed via IsHistoryCorrupted.
const historyCorruptedMarker = "history corrupted: upstream provider rejected the replayed transcript"

// IsHistoryCorrupted reports whether err carries the upstream-history-rejected
// marker stamped by HandleCompletion on a 400/422 from OpenRouter. Callers
// (assistant runtime) self-heal by trimming history and retrying.
func IsHistoryCorrupted(err error) bool {
	return err != nil && strings.Contains(err.Error(), historyCorruptedMarker)
}

// classifyCompletionError maps an upstream OpenRouter error returned by the
// completion client into the oops code that should flow back to the caller
// (and through the runner to the assistant runtime).
func (s *Service) classifyCompletionError(ctx context.Context, label string, err error) error {
	switch {
	case openrouter.IsInsufficientCredits(err):
		return oops.C(oops.CodeInsufficientCredits).LogError(ctx, s.logger)
	case openrouter.IsHistoryCorruptionCandidate(err):
		return oops.E(oops.CodeInvalid, err, historyCorruptedMarker).LogError(ctx, s.logger)
	default:
		return oops.E(oops.CodeGatewayError, err, "%s", label).LogError(ctx, s.logger)
	}
}

// HandleCompletion is a proxy to the OpenAI API that logs request and response data.
func (s *Service) HandleCompletion(w http.ResponseWriter, r *http.Request) error {
	ctx, authCtx, err := s.directAuthorize(r.Context(), r)
	if err != nil {
		return err
	}

	if err := s.checkCreditBalance(ctx, authCtx.ActiveOrganizationID, authCtx.AccountType); err != nil {
		return err
	}

	// Extract HTTP metadata for message tracking
	metadata := extractHTTPMetadata(r)

	orgID := authCtx.ActiveOrganizationID
	userID := authCtx.UserID
	source := billing.ModelUsageSource(metadata.Source)
	if source == "assistant" {
		source = billing.ModelUsageSourceAssistants
	}
	if source == "" {
		source = billing.ModelUsageSourcePlayground
	}
	sourceName := string(source)

	eventProperties := map[string]any{
		"action":            "chat_request_received",
		"organization_slug": authCtx.OrganizationSlug,
		"project_slug":      *authCtx.ProjectSlug,
		"success":           false,
		"source":            sourceName,
		"user_agent":        metadata.UserAgent,
		"origin":            metadata.Origin,
	}

	defer func() {
		if err := s.posthog.CaptureEvent(ctx, "elements_event", authCtx.ActiveOrganizationID, eventProperties); err != nil {
			s.logger.ErrorContext(ctx, "failed to capture elements event", attr.SlogError(err))
		}
	}()

	slogArgs := []any{
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogOrganizationID(orgID),
		attr.SlogOrganizationSlug(authCtx.OrganizationSlug),
		attr.SlogUserID(userID),
		attr.SlogOrganizationAccountType(authCtx.AccountType),
	}

	if authCtx.ProjectSlug != nil {
		slogArgs = append(slogArgs, attr.SlogProjectSlug(*authCtx.ProjectSlug))
	}

	s.logger.InfoContext(ctx, "chat request received",
		slogArgs...)

	// Read the request body
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").LogError(ctx, s.logger)
	}

	// Create a new reader with the same content for the proxy
	r.Body = io.NopCloser(strings.NewReader(string(reqBody)))

	var chatRequest openrouter.OpenAIChatRequest
	if err := json.Unmarshal(reqBody, &chatRequest); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse request body").LogError(ctx, s.logger)
	}

	// Defense-in-depth: reject any single tool-result message larger than the
	// per-tool byte cap. The client is expected to truncate oversized tool
	// results before sending, but if it doesn't, a huge payload here will
	// waste upstream tokens and typically trip the provider's own context
	// limit with an opaque 400. Failing fast here keeps the error clean.
	const maxToolMessageBytes = 200 * 1024
	for i, m := range chatRequest.Messages {
		if openrouter.GetRole(m) != "tool" {
			continue
		}
		content, err := openrouter.GetContentJSON(m)
		if err != nil {
			continue // other validation will catch malformed messages
		}
		if len(content) > maxToolMessageBytes {
			return oops.E(
				oops.CodeRequestTooLarge,
				nil,
				"tool message %d exceeds %d bytes; truncate tool output client-side",
				i, maxToolMessageBytes,
			).LogError(ctx, s.logger)
		}
	}

	chatIDHeader := r.Header.Get("Gram-Chat-ID")

	eventProperties["model"] = chatRequest.Model
	eventProperties["chat_id"] = chatIDHeader

	chatID := uuid.Nil
	if chatIDHeader != "" {
		chatID, err = uuid.Parse(chatIDHeader)
		if err != nil {
			return oops.E(oops.CodeInvalid, err, "invalid chat ID").LogError(ctx, s.logger)
		}
	}

	// Non-streaming: Use UnifiedClient
	temp := float64(chatRequest.Temperature)

	// Extract JSON schema from response_format if present
	// This enables structured output mode (e.g., for generateObject in AI SDK)
	var jsonSchema *or.ChatJSONSchemaConfig
	if chatRequest.ResponseFormat != nil && chatRequest.ResponseFormat.ChatFormatJSONSchemaConfig != nil {
		schema := chatRequest.ResponseFormat.ChatFormatJSONSchemaConfig.GetJSONSchema()
		jsonSchema = &schema
	}

	toolNames := make([]string, 0, len(chatRequest.Tools))
	for _, t := range chatRequest.Tools {
		if t.Function != nil {
			toolNames = append(toolNames, t.Function.Name)
		}
	}
	s.logger.DebugContext(ctx, "chat completions tools forwarded",
		attr.SlogChatModel(chatRequest.Model),
		attr.SlogChatToolCount(len(toolNames)),
		attr.SlogChatToolNames(toolNames),
	)

	// The runner's compactor sends `Gram-Skip-Capture: 1` so its
	// "summarise this transcript" turn does not persist as divergence on
	// the user's chat; zero the ChatID so the capture strategy (which
	// keys off ChatID) treats the call as anonymous.
	//
	// TODO(daniel): the header is client-trustable today and any caller
	// with /chat/completions access can suppress persistence. Acceptable
	// while the assistant runner is the sole producer; gate behind an
	// assistant-principal check before any non-assistant caller adopts
	// it.
	completionChatID := chatID
	if r.Header.Get("Gram-Skip-Capture") == "1" {
		completionChatID = uuid.Nil
	}
	reasoning := chatRequest.Reasoning
	if reasoning == nil {
		reasoning = &openrouter.Reasoning{Effort: "none", MaxTokens: nil, Exclude: nil, Enabled: nil}
	}

	completionReq := openrouter.CompletionRequest{
		OrgID:          orgID,
		ProjectID:      authCtx.ProjectID.String(),
		Messages:       chatRequest.Messages,
		Tools:          chatRequest.Tools,
		Temperature:    &temp,
		Model:          chatRequest.Model,
		Stream:         false,
		UsageSource:    source,
		ChatID:         completionChatID,
		UserID:         userID,
		ExternalUserID: authCtx.ExternalUserID,
		UserEmail:      conv.PtrValOr(authCtx.Email, ""),
		HTTPMetadata: &openrouter.HTTPMetadata{
			Origin:    metadata.Origin,
			UserAgent: metadata.UserAgent,
			IPAddress: metadata.IPAddress,
		},
		APIKeyID:                  authCtx.APIKeyID,
		JSONSchema:                jsonSchema,
		Reasoning:                 reasoning,
		CacheControl:              chatRequest.CacheControl,
		NormalizeOutboundMessages: r.URL.Query().Get("unstable_normalizeOutboundMessages") == "1",
	}

	// Opt-in: callers must pass includeContextWindow=1 to receive the
	// gram_metadata.context_window decoration. When off, the resolver
	// (and its OpenRouter round trip on cache miss) is never called.
	getContextWindow := func() int { return 0 }
	if r.URL.Query().Get("includeContextWindow") == "1" {
		resolved := sync.OnceValue(func() int {
			return s.resolveContextWindow(ctx, completionReq.Model)
		})
		go resolved()
		getContextWindow = resolved
	}

	isStreaming := chatRequest.Stream
	if isStreaming {
		streamBody, err := s.completionClient.GetCompletionStream(ctx, completionReq)
		if err != nil {
			return s.classifyCompletionError(ctx, "get completion stream", err)
		}
		defer o11y.NoLogDefer(func() error { return streamBody.Close() })

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		if err := s.streamCompletion(ctx, w, streamBody, getContextWindow); err != nil {
			return err
		}

		eventProperties["success"] = true
		return nil
	}

	/**
	 * Non-Streaming
	 */
	response, err := s.completionClient.GetCompletion(ctx, completionReq)
	if err != nil {
		return s.classifyCompletionError(ctx, "completion failed", err)
	}

	var gramMetadata *openrouter.GramMetadata
	if cw := getContextWindow(); cw > 0 {
		gramMetadata = &openrouter.GramMetadata{ContextWindow: cw}
	}

	// Build OpenAI-compatible response
	openAIResp := openrouter.OpenAIChatResponse{
		ID:      response.MessageID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   response.Model,
		Choices: []struct {
			Message      or.ChatMessages `json:"message"`
			FinishReason string          `json:"finish_reason"`
		}{
			{
				Message:      *response.Message,
				FinishReason: conv.PtrValOr(response.FinishReason, "stop"),
			},
		},
		Usage:        &response.Usage,
		GramMetadata: gramMetadata,
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openAIResp); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode response").LogError(ctx, s.logger)
	}

	eventProperties["success"] = true
	return nil
}

func (s *Service) resolveContextWindow(ctx context.Context, requestedModel string) int {
	model := requestedModel
	if model == "" {
		model = openrouter.DefaultChatModel
	}
	if resolved := openrouter.ResolveModel(model); resolved != "" {
		model = resolved
	}

	tokens, err := s.contextWindow.Resolve(ctx, model)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve model context window", attr.SlogError(err), attr.SlogGenAIRequestModel(model))
		return 0
	}
	return tokens
}

func (s *Service) streamCompletion(ctx context.Context, w http.ResponseWriter, src io.Reader, getContextWindow func() int) error {
	flusher, canFlush := w.(http.Flusher)
	br := bufio.NewReader(src)

	var event strings.Builder
	injected := false

	flush := func() error {
		text := event.String()
		event.Reset()

		if !injected {
			if rewritten, ok := maybeInjectContextWindow(text, getContextWindow); ok {
				text = rewritten
				injected = true
			}
		}

		if _, err := io.WriteString(w, text); err != nil {
			return oops.E(oops.CodeGatewayError, err, "stream write failed").LogError(ctx, s.logger)
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	for {
		line, err := br.ReadString('\n')
		if line != "" {
			isBlank := line == "\n" || line == "\r\n"
			if isBlank && event.Len() == 0 {
				// Skip leading/inter-event blank lines so we don't emit empty events.
			} else {
				event.WriteString(line)
				if isBlank {
					if flushErr := flush(); flushErr != nil {
						return flushErr
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if event.Len() > 0 {
					if flushErr := flush(); flushErr != nil {
						return flushErr
					}
				}
				return nil
			}
			s.logger.ErrorContext(ctx, "stream read error", attr.SlogError(err))
			return oops.E(oops.CodeGatewayError, err, "stream read failed").LogError(ctx, s.logger)
		}
	}
}

func maybeInjectContextWindow(eventText string, getContextWindow func() int) (string, bool) {
	dataLine, payload, ok := extractDataPayload(eventText)
	if !ok || payload == "" || payload == "[DONE]" {
		return eventText, false
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		return eventText, false
	}

	if !isFinalFrame(obj) {
		return eventText, false
	}

	cw := getContextWindow()
	if cw <= 0 {
		return eventText, false
	}

	metadata, err := json.Marshal(openrouter.GramMetadata{ContextWindow: cw})
	if err != nil {
		return eventText, false
	}
	obj["gram_metadata"] = metadata

	rewritten, err := json.Marshal(obj)
	if err != nil {
		return eventText, false
	}

	return strings.Replace(eventText, dataLine, "data: "+string(rewritten), 1), true
}

// isFinalFrame reports whether an SSE chunk is OpenRouter's metadata/usage
// frame — the trailing data event that carries the `usage` block before
// `[DONE]`. We inject Gram metadata next to OpenRouter's so the two travel
// together rather than landing on the earlier finish_reason chunk.
func isFinalFrame(obj map[string]json.RawMessage) bool {
	usage, ok := obj["usage"]
	return ok && len(usage) > 0 && string(usage) != "null"
}

// extractDataPayload returns the original `data: <payload>` line and its
// trimmed payload. OpenRouter does not emit multi-line data frames so the
// first match wins.
func extractDataPayload(eventText string) (line string, payload string, ok bool) {
	const prefix = "data: "
	rest := eventText
	for rest != "" {
		end := strings.IndexByte(rest, '\n')
		var raw string
		if end < 0 {
			raw = rest
			rest = ""
		} else {
			raw = rest[:end]
			rest = rest[end+1:]
		}
		trimmed := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(trimmed, prefix) {
			return trimmed, trimmed[len(prefix):], true
		}
	}
	return "", "", false
}

func (s *Service) CreditUsage(ctx context.Context, payload *gen.CreditUsagePayload) (*gen.CreditUsageResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	creditsUsed, creditLimit, err := s.openRouter.GetCreditsUsed(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get credit usage").LogError(ctx, s.logger)
	}

	return &gen.CreditUsageResult{
		CreditsUsed:    creditsUsed,
		MonthlyCredits: creditLimit,
	}, nil
}

// maxChatTitleLength bounds a manually set chat title. Kept in sync with the
// MaxLength(200) validation on the generateTitle design payload.
const maxChatTitleLength = 200

func (s *Service) GenerateTitle(ctx context.Context, payload *gen.GenerateTitlePayload) (*gen.GenerateTitleResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	chatID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat ID")
	}

	// Load the chat to verify access
	chat, err := s.repo.GetChat(ctx, chatID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").LogError(ctx, s.logger)
	}

	if chat.ProjectID != *authCtx.ProjectID {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Write path: a manual rename. A non-empty title is pinned (auto-generation
	// skips it); an empty title clears the manual flag and re-enables auto-naming.
	if payload.Title != nil {
		// Mirrors the MaxLength(200) transport validation so the bound also holds
		// for any non-HTTP caller. Goa's MaxLength counts runes, so we do too —
		// using byte length here would wrongly reject valid multi-byte titles.
		if titleLen := utf8.RuneCountInString(*payload.Title); titleLen > maxChatTitleLength {
			return nil, oops.E(oops.CodeInvalid, fmt.Errorf("title length %d exceeds max %d", titleLen, maxChatTitleLength), "chat title is too long")
		}

		trimmed := strings.TrimSpace(*payload.Title)

		var newTitle pgtype.Text
		manual := false
		if trimmed != "" {
			newTitle = conv.PtrToPGText(&trimmed)
			manual = true
		}

		if err := s.repo.RenameChat(ctx, repo.RenameChatParams{
			Title:            newTitle,
			TitleManuallySet: manual,
			ID:               chatID,
			ProjectID:        *authCtx.ProjectID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to rename chat").LogError(ctx, s.logger)
		}

		return &gen.GenerateTitleResult{Title: trimmed}, nil
	}

	// Read path: return the current title from DB. Title generation happens
	// asynchronously via Temporal after first completion; the title will be
	// available on the next list()/fetch().
	title := DefaultChatTitle
	if chat.Title.Valid && chat.Title.String != "" {
		title = chat.Title.String
	}
	return &gen.GenerateTitleResult{Title: title}, nil
}

func (s *Service) DeleteChat(ctx context.Context, payload *gen.DeleteChatPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	chatID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid chat id").LogError(ctx, s.logger)
	}

	// SoftDeleteChat deletes the chat unless it backs a live assistant thread, and
	// reports the disposition in one statement (no racy re-query). A live-thread
	// chat reloads its conversation every turn, so a soft-deleted backing chat
	// would wedge the thread — refuse with a conflict. A no-op that isn't
	// thread-backed (chat absent / already deleted / other project) is a success,
	// matching the prior project-scoped behavior.
	res, err := s.repo.SoftDeleteChat(ctx, repo.SoftDeleteChatParams{
		ID:        chatID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft delete chat").LogError(ctx, s.logger)
	}
	if !res.Deleted && res.BacksLiveThread {
		return oops.E(oops.CodeConflict, nil, "cannot delete a chat that backs an assistant thread").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) SetPinned(ctx context.Context, payload *gen.SetPinnedPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	chatID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid chat id").LogError(ctx, s.logger)
	}

	// Load the chat to verify access before mutating it.
	chat, err := s.repo.GetChat(ctx, chatID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.C(oops.CodeNotFound)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load chat").LogError(ctx, s.logger)
	}

	if chat.ProjectID != *authCtx.ProjectID {
		return oops.C(oops.CodeUnauthorized)
	}

	// Off-dashboard callers must match the chat owner unless they're the
	// managed-assistant runtime (see LoadChat).
	if authCtx.SessionID == nil {
		if _, isAssistantCall := contextvalues.GetAssistantPrincipal(ctx); !isAssistantCall {
			if chat.ExternalUserID.String != "" && chat.ExternalUserID.String != authCtx.ExternalUserID {
				return oops.C(oops.CodeUnauthorized)
			}
		}
	}

	if err := s.repo.SetChatPinned(ctx, repo.SetChatPinnedParams{
		Pinned:    payload.Pinned,
		ID:        chatID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "set chat pinned").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) SubmitFeedback(ctx context.Context, payload *gen.SubmitFeedbackPayload) (*gen.SubmitFeedbackResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	chatID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat ID")
	}

	// Load the chat to verify access
	chat, err := s.repo.GetChat(ctx, chatID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").LogError(ctx, s.logger)
	}

	if chat.ProjectID != *authCtx.ProjectID {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Validate feedback value
	if payload.Feedback != "success" && payload.Feedback != "failure" {
		return nil, oops.E(oops.CodeInvalid, nil, "feedback must be 'success' or 'failure'")
	}

	messages, err := s.repo.ListLatestGenerationChatMessages(ctx, repo.ListLatestGenerationChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list messages").LogError(ctx, s.logger)
	}

	var lastMessageID uuid.NullUUID
	if len(messages) > 0 {
		lastMessageID = uuid.NullUUID{UUID: messages[len(messages)-1].ID, Valid: true}
	} else {
		return nil, oops.E(oops.CodeInvalid, nil, "no messages found for chat")
	}

	// Insert user feedback
	_, err = s.repo.InsertUserFeedback(ctx, repo.InsertUserFeedbackParams{
		ProjectID:           *authCtx.ProjectID,
		ChatID:              chatID,
		MessageID:           lastMessageID.UUID,
		UserResolution:      payload.Feedback,
		UserResolutionNotes: conv.ToPGTextEmpty(""),
		ChatResolutionID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to store feedback").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "user feedback submitted",
		attr.SlogChatID(chatID.String()),
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogOutcome(payload.Feedback),
	)

	return &gen.SubmitFeedbackResult{Success: true}, nil
}

// loadMessageContent retrieves the full message content using the precedence:
// 1. ContentRaw (inline JSON for messages ≤128 KiB)
// 2. ContentAssetUrl (fetch from asset storage)
// 3. Content (plain text fallback)
// loadMessageContentFields resolves a message's content from inline JSON, asset
// storage, or plain-text fallback. It takes the individual columns rather than a
// row struct so callers holding different row shapes (e.g. the risk-windowed
// query rows) can reuse it.
func (s *Service) loadMessageContentFields(ctx context.Context, chatID uuid.UUID, plainContent string, contentRaw []byte, contentAssetURL pgtype.Text) json.RawMessage {
	content, _ := json.Marshal(plainContent)

	// 1. Try ContentRaw first (inline JSON for small messages)
	if len(contentRaw) > 0 {
		return contentRaw
	}

	// 2. Try fetching from asset storage
	if contentAssetURL.Valid && contentAssetURL.String != "" {
		assetURL, err := url.Parse(contentAssetURL.String)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to parse message content asset URL",
				attr.SlogError(err),
				attr.SlogChatID(chatID.String()),
			)
			return content
		}

		reader, err := s.assetStorage.Read(ctx, assetURL)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to open message content from asset storage",
				attr.SlogError(err),
				attr.SlogChatID(chatID.String()),
			)
			return content
		}
		defer func() { _ = reader.Close() }()

		// Limit read size to prevent memory issues
		limitedReader := io.LimitReader(reader, maxAssetReadSize)
		data, err := io.ReadAll(limitedReader)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to read message content from asset storage",
				attr.SlogError(err),
				attr.SlogChatID(chatID.String()),
			)
			return content
		}

		return data
	}

	// 3. Fallback to plain text content
	return content
}

// buildGenMessages converts a page of repo rows (ascending by seq) to API messages.
func (s *Service) buildGenMessages(ctx context.Context, rows []repo.ChatMessage) []*gen.ChatMessage {
	out := make([]*gen.ChatMessage, len(rows))
	for i := range rows {
		out[i] = s.buildGenMessage(ctx, rows[i])
	}
	return out
}

func (s *Service) buildGenMessage(ctx context.Context, m repo.ChatMessage) *gen.ChatMessage {
	toolCalls := string(m.ToolCalls)
	return &gen.ChatMessage{
		ID:             m.ID.String(),
		Seq:            m.Seq,
		IsRisk:         nil, // is_risk is a risk_only-mode signal
		Role:           m.Role,
		Model:          m.Model.String,
		UserID:         &m.UserID.String,
		ExternalUserID: &m.ExternalUserID.String,
		Content:        s.loadMessageContentFields(ctx, m.ChatID, m.Content, m.ContentRaw, m.ContentAssetUrl),
		ToolCalls:      &toolCalls,
		ToolCallID:     &m.ToolCallID.String,
		FinishReason:   &m.FinishReason.String,
		PromptID:       conv.FromPGText[string](m.MessageID),
		CreatedAt:      m.CreatedAt.Time.Format(time.RFC3339),
		Generation:     int(m.Generation),
	}
}

// buildRiskSegments folds the risk-windowed rows (ascending by seq, each
// carrying its 1-based ordinal rn within the generation and the generation
// total) into contiguous segments. A break in rn starts a new segment;
// has_more_before/after mark whether earlier/later messages remain to expand.
// foldWindowSegments folds windowed rows (ordinal rn ascending; contiguous rn
// within a segment) into [first_seq,last_seq] segments carrying edge has_more
// flags. Shared by the risk-only and query-search windowed views, which return
// different row types but identical (rn, total, seq) windowing columns.
func foldWindowSegments(n int, at func(i int) (rn, total, seq int64)) []*gen.RiskSegment {
	var segments []*gen.RiskSegment
	var cur *gen.RiskSegment
	var prevRn int64
	for i := range n {
		rn, total, seq := at(i)
		if cur == nil || rn != prevRn+1 {
			cur = &gen.RiskSegment{
				FirstSeq:      seq,
				LastSeq:       seq,
				HasMoreBefore: rn > 1,
				HasMoreAfter:  rn < total,
			}
			segments = append(segments, cur)
		} else {
			cur.LastSeq = seq
			cur.HasMoreAfter = rn < total
		}
		prevRn = rn
	}
	return segments
}

func buildRiskSegments(rows []repo.ListRiskWindowedMessagesRow) []*gen.RiskSegment {
	return foldWindowSegments(len(rows), func(i int) (int64, int64, int64) {
		return rows[i].Rn, rows[i].Total, rows[i].Seq
	})
}

func buildSearchSegments(rows []repo.ListSearchWindowedMessagesRow) []*gen.RiskSegment {
	return foldWindowSegments(len(rows), func(i int) (int64, int64, int64) {
		return rows[i].Rn, rows[i].Total, rows[i].Seq
	})
}

type chatMessageRow struct {
	projectID      uuid.UUID
	chatID         uuid.UUID
	userID         string
	externalUserID string
	messageID      string
	toolCallID     string

	role         string
	model        string
	content      or.ChatMessages
	finishReason *string
	// toolCalls is the replay JSONB blob written to chat_messages.tool_calls.
	// The content hash is computed from a typed projection instead of these raw
	// bytes so equivalent tool calls can arrive through different wire shapes.
	toolCalls        []byte
	promptTokens     int64
	completionTokens int64
	totalTokens      int64

	generation int32

	metadata httpMetadata
}

const (
	// maxInlineContentSize is the maximum size of message content that will be
	// stored inline in the database. Messages larger than this will only have
	// their content stored in the asset storage.
	maxInlineContentSize = 128 * 1024 // 128 KiB

	// maxAssetReadSize is the maximum size of message content that will be
	// read from asset storage to prevent memory issues.
	maxAssetReadSize = 20 * 1024 * 1024 // 20 MiB

	// defaultLoadChatLimit / maxLoadChatLimit bound the keyset page size for
	// loadChat. Mirrors the Default/Maximum in the Goa design; clamped again here
	// so direct (non-Goa) callers can't request an unbounded page.
	defaultLoadChatLimit = 50
	maxLoadChatLimit     = 200

	// riskContextWindow is how many surrounding messages (by ordinal position) to
	// include on each side of a risk finding in the risk-only view.
	riskContextWindow = 5

	// searchContextWindow is how many surrounding messages (by ordinal position)
	// to include on each side of a query match in the search-windowed view.
	searchContextWindow = 5

	// searchMatchLimit caps how many seed matches the search-windowed view returns
	// (earliest first by ordinal), bounding the response on broad queries.
	searchMatchLimit = 200

	// maxConcurrentChatAssetWork bounds parallelism for the per-batch marshal
	// and asset-upload phases in storeMessages, capping goroutines, memory,
	// and outbound connections for arbitrarily large batches.
	maxConcurrentChatAssetWork = 32
)

func storeMessages(ctx context.Context, logger *slog.Logger, tx repo.DBTX, assetStorage assets.BlobStore, rows []chatMessageRow) error {
	if len(rows) == 0 {
		return nil
	}

	// uploadResult holds the result of uploading a single message to asset storage.
	type uploadResult struct {
		assetURL string
		jsonData []byte
		err      error // non-nil if upload failed
	}

	// Phase 1: marshal + hash + path in parallel. Asset paths are
	// content-addressable (sha256 of jsonData), so the path also serves as
	// the dedup key in Phase 2.
	type rowPrep struct {
		jsonData []byte
		path     string
		err      error
	}
	preps := make([]rowPrep, len(rows))
	var marshalWg errgroup.Group
	marshalWg.SetLimit(maxConcurrentChatAssetWork)
	for i, row := range rows {
		marshalWg.Go(func() error {
			jsonData, err := openrouter.GetContentJSON(row.content)
			if err != nil {
				preps[i] = rowPrep{jsonData: nil, path: "", err: fmt.Errorf("marshal message content: %w", err)}
				return nil
			}
			hash := sha256.Sum256(jsonData)
			hashHex := hex.EncodeToString(hash[:])
			assetPath := path.Join(row.projectID.String(), "chats", row.chatID.String(), hashHex+".json")
			preps[i] = rowPrep{jsonData: jsonData, path: assetPath, err: nil}
			return nil
		})
	}
	if err := marshalWg.Wait(); err != nil {
		// Goroutines record per-row failures into preps[i].err and return nil,
		// so a non-nil error here signals an unexpected future change. Log and
		// continue with whatever was prepared.
		logger.ErrorContext(ctx, "chat asset marshal phase reported unexpected error", attr.SlogError(err))
	}

	// Phase 2: dedup by asset path so duplicate-content rows dispatch a
	// single upload. Without this, concurrent writers race to the same GCS
	// object and hit the per-object 1-write/sec rate limit.
	leaders := make(map[string]int, len(rows)) // assetPath -> first row index that dispatches the upload
	for i, prep := range preps {
		if prep.err != nil {
			continue
		}
		if _, ok := leaders[prep.path]; !ok {
			leaders[prep.path] = i
		}
	}

	results := make([]uploadResult, len(rows))

	// Phase 3: upload deduplicated leaders to asset storage in parallel. The
	// BlobStore.Write contract attaches a "create only if absent" precondition
	// on GCS so cross-batch races resolve as idempotent no-ops. We don't use
	// errgroup's error propagation because we want to continue even if some
	// uploads fail — we'll record the errors and still insert the messages
	// with their plain text content.
	var uploadWg errgroup.Group
	uploadWg.SetLimit(maxConcurrentChatAssetWork)
	for assetPath, leader := range leaders {
		uploadWg.Go(func() error {
			prep := preps[leader]

			writer, assetURL, err := assetStorage.Write(ctx, assetPath, "application/json", int64(len(prep.jsonData)))
			if err != nil {
				results[leader] = uploadResult{assetURL: "", jsonData: prep.jsonData, err: fmt.Errorf("create asset writer: %w", err)}
				return nil
			}

			if _, err := io.Copy(writer, bytes.NewReader(prep.jsonData)); err != nil {
				_ = writer.Close()
				results[leader] = uploadResult{assetURL: "", jsonData: prep.jsonData, err: fmt.Errorf("write asset content: %w", err)}
				return nil
			}

			if err := writer.Close(); err != nil {
				results[leader] = uploadResult{assetURL: "", jsonData: prep.jsonData, err: fmt.Errorf("finalize asset upload: %w", err)}
				return nil
			}

			results[leader] = uploadResult{
				assetURL: assetURL.String(),
				jsonData: prep.jsonData,
				err:      nil,
			}
			return nil
		})
	}

	if err := uploadWg.Wait(); err != nil {
		// Goroutines record per-row failures into results[i].err and return
		// nil, so a non-nil error here signals an unexpected future change.
		// Log and continue with whatever was uploaded.
		logger.ErrorContext(ctx, "chat asset upload phase reported unexpected error", attr.SlogError(err))
	}

	// Fan leader results back out to follower rows and record marshal errors.
	for i := range rows {
		prep := preps[i]
		if prep.err != nil {
			results[i] = uploadResult{assetURL: "", jsonData: nil, err: prep.err}
			continue
		}
		if leader, ok := leaders[prep.path]; ok && leader != i {
			res := results[leader]
			results[i] = uploadResult{assetURL: res.assetURL, jsonData: prep.jsonData, err: res.err}
		}
	}

	// Build database params from upload results.
	dbrows := make([]repo.CreateChatMessageParams, len(rows))
	for i, row := range rows {
		res := results[i]

		// Log storage errors but continue - we'll still store the message with plain text.
		var storageError pgtype.Text
		if res.err != nil {
			logger.ErrorContext(ctx, "failed to upload message to asset storage",
				attr.SlogError(res.err),
				attr.SlogChatID(row.chatID.String()),
				attr.SlogProjectID(row.projectID.String()),
			)
			storageError = conv.ToPGText(res.err.Error())
		}

		// Only store content inline if upload succeeded and it's within the size threshold.
		var contentRaw []byte
		if res.err == nil && len(res.jsonData) <= maxInlineContentSize {
			contentRaw = res.jsonData
		}

		dbrows[i] = repo.CreateChatMessageParams{
			ChatID:           row.chatID,
			ProjectID:        row.projectID,
			Role:             row.role,
			Content:          openrouter.GetText(row.content),
			ContentRaw:       contentRaw,
			ContentAssetUrl:  conv.ToPGText(res.assetURL),
			StorageError:     storageError,
			Model:            conv.ToPGText(row.model),
			MessageID:        conv.ToPGText(row.messageID),
			ToolCallID:       conv.ToPGText(row.toolCallID),
			UserID:           conv.ToPGText(row.userID),
			ExternalUserID:   conv.ToPGText(row.externalUserID),
			FinishReason:     conv.PtrToPGText(row.finishReason),
			ToolCalls:        row.toolCalls,
			PromptTokens:     row.promptTokens,
			CompletionTokens: row.completionTokens,
			TotalTokens:      row.totalTokens,
			Origin:           conv.ToPGText(row.metadata.Origin),
			UserAgent:        conv.ToPGText(row.metadata.UserAgent),
			IpAddress:        conv.ToPGText(row.metadata.IPAddress),
			Source:           conv.ToPGText(row.metadata.Source),
			ContentHash:      nil,
			Generation:       row.generation,
		}
	}

	// Batch insert all messages.
	crepo := repo.New(tx)
	if _, err := crepo.CreateChatMessage(ctx, dbrows); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to insert chat messages").LogError(ctx, logger)
	}

	return nil
}

// enrichChatsWithMetrics fetches token and cost metrics from ClickHouse and adds them to chat overviews.
// This is a best-effort operation - if metrics can't be fetched, chats are returned with zero values.
func (s *Service) enrichChatsWithMetrics(ctx context.Context, projectID string, chats []*gen.ChatOverview) error {
	if len(chats) == 0 {
		return nil
	}

	// Check if telemetry service is available
	if s.telemetryService == nil {
		return nil
	}

	// Extract chat IDs
	chatIDs := make([]string, len(chats))
	for i, chat := range chats {
		chatIDs[i] = chat.ID
	}

	// Fetch metrics from ClickHouse
	metricsMap, err := s.telemetryService.GetChatMetricsByIDs(ctx, projectID, chatIDs)
	if err != nil {
		return fmt.Errorf("get chat metrics from ClickHouse: %w", err)
	}

	// Enrich each chat with its metrics
	for _, chat := range chats {
		if metrics, found := metricsMap[chat.ID]; found {
			chat.TotalInputTokens = &metrics.TotalInputTokens
			chat.TotalOutputTokens = &metrics.TotalOutputTokens
			chat.TotalTokens = &metrics.TotalTokens
			chat.TotalCost = &metrics.TotalCost
		}
	}

	return nil
}

// enrichChatWithMetrics fetches token and cost metrics from ClickHouse and adds them to a single chat.
// This is a best-effort operation - if metrics can't be fetched, the chat is returned with zero values.
func (s *Service) enrichChatWithMetrics(ctx context.Context, projectID string, chat *gen.Chat) error {
	// Check if telemetry service is available
	if s.telemetryService == nil {
		return nil
	}

	// Fetch metrics from ClickHouse
	metricsMap, err := s.telemetryService.GetChatMetricsByIDs(ctx, projectID, []string{chat.ID})
	if err != nil {
		return fmt.Errorf("get chat metrics from ClickHouse: %w", err)
	}

	// Enrich chat with its metrics
	if metrics, found := metricsMap[chat.ID]; found {
		chat.TotalInputTokens = &metrics.TotalInputTokens
		chat.TotalOutputTokens = &metrics.TotalOutputTokens
		chat.TotalTokens = &metrics.TotalTokens
		chat.TotalCost = &metrics.TotalCost
	}

	return nil
}

// enrichChatWithClaudeTurnUsage fetches per-turn Claude Code usage from ClickHouse
// and attaches it to chat.load. This is best-effort: missing ClickHouse data
// simply leaves the optional agent usage payload empty.
func (s *Service) enrichChatWithClaudeTurnUsage(ctx context.Context, projectID string, chat *gen.Chat) error {
	if s.telemetryService == nil {
		return nil
	}

	usageMap, err := s.telemetryService.GetClaudeTurnUsageByChatIDs(ctx, projectID, []string{chat.ID})
	if err != nil {
		return fmt.Errorf("get Claude turn usage from ClickHouse: %w", err)
	}
	toolUsageMap, err := s.telemetryService.GetClaudeToolUsageByChatIDs(ctx, projectID, []string{chat.ID})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to enrich chat with Claude tool usage", attr.SlogError(err))
		toolUsageMap = nil
	}

	turns := usageMap[chat.ID]
	toolUsageRows := toolUsageMap[chat.ID]
	if len(turns) == 0 && len(toolUsageRows) == 0 {
		return nil
	}

	apiTurns := make([]*gen.ClaudeTurnUsage, 0, len(turns))
	for _, turn := range turns {
		apiTurns = append(apiTurns, &gen.ClaudeTurnUsage{
			PromptID:            turn.PromptID,
			StartTimeUnixNano:   strconv.FormatInt(turn.StartTimeUnixNano, 10),
			EndTimeUnixNano:     strconv.FormatInt(turn.EndTimeUnixNano, 10),
			RequestCount:        clampUint64ToInt64(turn.RequestCount),
			InputTokens:         turn.InputTokens,
			OutputTokens:        turn.OutputTokens,
			CacheReadTokens:     turn.CacheReadTokens,
			CacheCreationTokens: turn.CacheCreationTokens,
			TotalTokens:         turn.TotalTokens,
			CostUsd:             turn.CostUSD,
			CostMicros:          turn.CostMicros,
			Models:              turn.Models,
			QuerySources:        turn.QuerySources,
		})
	}
	apiTools := make([]*gen.ClaudeToolUsage, 0, len(toolUsageRows))
	for _, tool := range toolUsageRows {
		apiTools = append(apiTools, &gen.ClaudeToolUsage{
			ToolUseID:       tool.ToolUseID,
			PromptID:        tool.PromptID,
			ToolName:        tool.ToolName,
			InputSizeBytes:  tool.InputSizeBytes,
			ResultSizeBytes: tool.ResultSizeBytes,
		})
	}

	chat.AgentUsage = &gen.AgentUsage{
		Type: "claude",
		Claude: &gen.ClaudeAgentUsage{
			Turns: apiTurns,
			Tools: apiTools,
		},
	}

	return nil
}

func clampUint64ToInt64(value uint64) int64 {
	const maxInt64 = ^uint64(0) >> 1
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}
