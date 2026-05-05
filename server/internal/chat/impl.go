package chat

import (
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
	"strings"
	"time"

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
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// ChatResolutionAnalyzer schedules async chat resolution analysis.
type ChatResolutionAnalyzer interface {
	ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID, apiKeyID string) error
}

type Service struct {
	auth             *auth.Auth
	db               *pgxpool.Pool
	repo             *repo.Queries
	tracer           trace.Tracer
	openRouter       openrouter.Provisioner
	completionClient openrouter.CompletionClient
	logger           *slog.Logger
	sessions         *sessions.Manager
	chatSessions     *chatsessions.Manager
	assistantTokens  *assistanttokens.Manager
	assetStorage     assets.BlobStore
	posthog          *posthog.Posthog
	telemetryService *telemetry.Service
	billingRepo      billing.Repository
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	chatSessions *chatsessions.Manager,
	openRouter openrouter.Provisioner,
	completionClient openrouter.CompletionClient,
	posthog *posthog.Posthog,
	telemetryService *telemetry.Service,
	assetStorage assets.BlobStore,
	authzEngine *authz.Engine,
	assistantTokens *assistanttokens.Manager,
	billingRepo billing.Repository,
) *Service {
	logger = logger.With(attr.SlogComponent("chat"))

	return &Service{
		auth:             auth.New(logger, db, sessions, authzEngine),
		db:               db,
		sessions:         sessions,
		chatSessions:     chatSessions,
		assistantTokens:  assistantTokens,
		logger:           logger,
		repo:             repo.New(db),
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/chat"),
		openRouter:       openRouter,
		completionClient: completionClient,
		assetStorage:     assetStorage,
		posthog:          posthog,
		telemetryService: telemetryService,
		billingRepo:      billingRepo,
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

	result := make([]*gen.ChatOverview, 0)
	var userInfo *sessions.CachedUserInfo
	var err error
	if authCtx.SessionID != nil {
		userInfo, _, err = s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
		}
	}

	// If we have an external user ID, always only list chats for that external user
	if authCtx.ExternalUserID != "" {
		chats, err := s.repo.ListChatsForExternalUser(ctx, repo.ListChatsForExternalUserParams{
			ProjectID:      *authCtx.ProjectID,
			ExternalUserID: conv.ToPGText(authCtx.ExternalUserID),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list chats").Log(ctx, s.logger)
		}

		for _, chat := range chats {
			lastMessageTimestamp := chat.CreatedAt.Time.Format(time.RFC3339)
			if chat.LastMessageTimestamp.Valid {
				lastMessageTimestamp = chat.LastMessageTimestamp.Time.Format(time.RFC3339)
			}
			result = append(result, &gen.ChatOverview{
				ID:                   chat.ID.String(),
				UserID:               nil,
				ExternalUserID:       &chat.ExternalUserID.String,
				Source:               conv.FromPGText[string](chat.Source),
				Title:                chat.Title.String,
				NumMessages:          int(chat.NumMessages),
				CreatedAt:            chat.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:            chat.UpdatedAt.Time.Format(time.RFC3339),
				LastMessageTimestamp: lastMessageTimestamp,
				TotalInputTokens:     nil,
				TotalOutputTokens:    nil,
				TotalTokens:          nil,
				TotalCost:            nil,
			})
		}

		// Enrich with metrics from ClickHouse
		if err := s.enrichChatsWithMetrics(ctx, authCtx.ProjectID.String(), result); err != nil {
			s.logger.WarnContext(ctx, "failed to enrich chats with metrics", attr.SlogError(err))
		}

		return &gen.ListChatsResult{Chats: result}, nil
	}

	// if the user is Admin, we list chat for a whole project
	if userInfo != nil && userInfo.Admin {
		chats, err := s.repo.ListAllChats(ctx, *authCtx.ProjectID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list chats").Log(ctx, s.logger)
		}

		for _, chat := range chats {
			lastMessageTimestamp := chat.CreatedAt.Time.Format(time.RFC3339)
			if chat.LastMessageTimestamp.Valid {
				lastMessageTimestamp = chat.LastMessageTimestamp.Time.Format(time.RFC3339)
			}
			result = append(result, &gen.ChatOverview{
				ID:                   chat.ID.String(),
				UserID:               &chat.UserID.String,
				ExternalUserID:       nil,
				Source:               conv.FromPGText[string](chat.Source),
				Title:                chat.Title.String,
				NumMessages:          int(chat.NumMessages),
				CreatedAt:            chat.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:            chat.UpdatedAt.Time.Format(time.RFC3339),
				LastMessageTimestamp: lastMessageTimestamp,
				TotalInputTokens:     nil,
				TotalOutputTokens:    nil,
				TotalTokens:          nil,
				TotalCost:            nil,
			})
		}

		// Enrich with metrics from ClickHouse
		if err := s.enrichChatsWithMetrics(ctx, authCtx.ProjectID.String(), result); err != nil {
			s.logger.WarnContext(ctx, "failed to enrich chats with metrics", attr.SlogError(err))
		}

		return &gen.ListChatsResult{Chats: result}, nil
	}

	// at this point if there's no UserID in authCtx then the request is unauthorized
	if authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	chats, err := s.repo.ListChatsForUser(ctx, repo.ListChatsForUserParams{
		ProjectID: *authCtx.ProjectID,
		UserID:    conv.ToPGText(authCtx.UserID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list chats").Log(ctx, s.logger)
	}

	for _, chat := range chats {
		lastMessageTimestamp := chat.CreatedAt.Time.Format(time.RFC3339)
		if chat.LastMessageTimestamp.Valid {
			lastMessageTimestamp = chat.LastMessageTimestamp.Time.Format(time.RFC3339)
		}
		result = append(result, &gen.ChatOverview{
			ID:                   chat.ID.String(),
			UserID:               &chat.UserID.String,
			ExternalUserID:       nil,
			Source:               conv.FromPGText[string](chat.Source),
			Title:                chat.Title.String,
			NumMessages:          int(chat.NumMessages),
			CreatedAt:            chat.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:            chat.UpdatedAt.Time.Format(time.RFC3339),
			LastMessageTimestamp: lastMessageTimestamp,
			TotalInputTokens:     nil,
			TotalOutputTokens:    nil,
			TotalTokens:          nil,
			TotalCost:            nil,
		})
	}

	// Enrich with metrics from ClickHouse
	if err := s.enrichChatsWithMetrics(ctx, authCtx.ProjectID.String(), result); err != nil {
		s.logger.WarnContext(ctx, "failed to enrich chats with metrics", attr.SlogError(err))
	}

	return &gen.ListChatsResult{Chats: result}, nil
}

func (s *Service) ListChatsWithResolutions(ctx context.Context, payload *gen.ListChatsWithResolutionsPayload) (*gen.ListChatsWithResolutionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Check if logs are enabled for this organization
	if err := s.telemetryService.CheckLogsEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, fmt.Errorf("checking logs enabled: %w", err)
	}

	// If an external user ID is set, restrict to only their chats
	// This prevents chat-session users from viewing other users' chats
	if authCtx.ExternalUserID != "" {
		if payload.ExternalUserID == nil || *payload.ExternalUserID != authCtx.ExternalUserID {
			// Force filter to their own external user ID
			payload.ExternalUserID = &authCtx.ExternalUserID
		}
	}

	// Set up pagination with safe int32 conversion
	limit := payload.Limit
	if limit == 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := payload.Offset

	// Convert optional filter parameters (use empty string for SQL NULL check)
	search := conv.PtrValOr(payload.Search, "")
	externalUserID := conv.PtrValOr(payload.ExternalUserID, "")
	resolutionStatus := conv.PtrValOr(payload.ResolutionStatus, "")

	// Parse time filters
	var fromTime, toTime pgtype.Timestamptz
	if payload.From != nil {
		t, err := time.Parse(time.RFC3339, *payload.From)
		if err == nil {
			fromTime = conv.ToPGTimestamptz(t)
		}
	}
	if payload.To != nil {
		t, err := time.Parse(time.RFC3339, *payload.To)
		if err == nil {
			toTime = conv.ToPGTimestamptz(t)
		}
	}

	// Get total count (before pagination)
	totalCount, err := s.repo.CountChatsWithResolutions(ctx, repo.CountChatsWithResolutionsParams{
		ProjectID:        *authCtx.ProjectID,
		Search:           search,
		ExternalUserID:   externalUserID,
		FromTime:         fromTime,
		ToTime:           toTime,
		ResolutionStatus: resolutionStatus,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to count chats").Log(ctx, s.logger)
	}

	// Query database - returns denormalized rows (one row per chat+resolution combination)
	rows, err := s.repo.ListChatsWithResolutions(ctx, repo.ListChatsWithResolutionsParams{
		ProjectID:        *authCtx.ProjectID,
		Search:           search,
		ExternalUserID:   externalUserID,
		FromTime:         fromTime,
		ToTime:           toTime,
		ResolutionStatus: resolutionStatus,
		SortBy:           payload.SortBy,
		SortOrder:        payload.SortOrder,
		PageLimit:        int32(limit),
		PageOffset:       int32(offset), //nolint:gosec // offset is controlled by client pagination
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list chats with resolutions").Log(ctx, s.logger)
	}

	// Group denormalized rows by chat_id
	chatMap := make(map[string]*gen.ChatOverviewWithResolutions)
	chatOrder := make([]string, 0) // Preserve order

	for _, row := range rows {
		chatID := row.ChatID.String()

		// If this is the first row for this chat, create the chat entry
		if _, exists := chatMap[chatID]; !exists {
			lastMessageTimestamp := row.CreatedAt.Time.Format(time.RFC3339)
			if row.LastMessageTimestamp.Valid {
				lastMessageTimestamp = row.LastMessageTimestamp.Time.Format(time.RFC3339)
			}
			chatMap[chatID] = &gen.ChatOverviewWithResolutions{
				ID:                   chatID,
				Title:                row.Title.String,
				UserID:               conv.FromPGText[string](row.UserID),
				ExternalUserID:       conv.FromPGText[string](row.ExternalUserID),
				Source:               conv.FromPGText[string](row.Source),
				NumMessages:          int(row.NumMessages),
				CreatedAt:            row.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:            row.UpdatedAt.Time.Format(time.RFC3339),
				LastMessageTimestamp: lastMessageTimestamp,
				Resolutions:          make([]*gen.ChatResolution, 0),
				TotalInputTokens:     nil,
				TotalOutputTokens:    nil,
				TotalTokens:          nil,
				TotalCost:            nil,
			}
			chatOrder = append(chatOrder, chatID)
		}

		// Add resolution to this chat (if one exists - ResolutionID can be NULL from LEFT JOIN)
		if row.ResolutionID.Valid {
			// Convert message_ids from interface{} to []string
			var messageIDs []string
			if row.MessageIds != nil {
				// PostgreSQL array comes back as []interface{} containing uuid.UUID values
				if msgIDsSlice, ok := row.MessageIds.([]any); ok {
					messageIDs = make([]string, len(msgIDsSlice))
					for i, msgID := range msgIDsSlice {
						if uid, ok := msgID.(uuid.UUID); ok {
							messageIDs[i] = uid.String()
						}
					}
				}
			}

			resolution := &gen.ChatResolution{
				ID:              row.ResolutionID.UUID.String(),
				UserGoal:        row.UserGoal.String,
				Resolution:      row.Resolution.String,
				ResolutionNotes: row.ResolutionNotes.String,
				Score:           int(row.Score.Int32),
				CreatedAt:       row.ResolutionCreatedAt.Time.Format(time.RFC3339),
				MessageIds:      messageIDs,
			}
			chatMap[chatID].Resolutions = append(chatMap[chatID].Resolutions, resolution)
		}
	}

	// Convert map to ordered slice
	chats := make([]*gen.ChatOverviewWithResolutions, len(chatOrder))
	for i, chatID := range chatOrder {
		chats[i] = chatMap[chatID]
	}

	// Enrich with metrics from ClickHouse
	if err := s.enrichChatsWithResolutionsWithMetrics(ctx, authCtx.ProjectID.String(), chats); err != nil {
		s.logger.WarnContext(ctx, "failed to enrich chats with metrics", attr.SlogError(err))
	}

	return &gen.ListChatsWithResolutionsResult{
		Chats: chats,
		Total: int(totalCount),
	}, nil
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").Log(ctx, s.logger)
	}

	// older chat_messages may not have project_id in the model, but it will always exist on the chat
	if chat.ProjectID != *authCtx.ProjectID {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// If this isn't coming from the dashboard, make sure the external user ID matches
	if authCtx.SessionID == nil {
		if chat.ExternalUserID.String != "" && chat.ExternalUserID.String != authCtx.ExternalUserID {
			return nil, oops.C(oops.CodeUnauthorized)
		}
	}

	messages, err := s.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    chat.ID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat messages").Log(ctx, s.logger)
	}

	resultMessages := make([]*gen.ChatMessage, len(messages))
	for i, msg := range messages {
		toolCalls := string(msg.ToolCalls)
		resultMessages[i] = &gen.ChatMessage{
			ID:             msg.ID.String(),
			Role:           msg.Role,
			Model:          msg.Model.String,
			UserID:         &msg.UserID.String,
			ExternalUserID: &msg.ExternalUserID.String,
			Content:        s.loadMessageContent(ctx, msg),
			ToolCalls:      &toolCalls,
			ToolCallID:     &msg.ToolCallID.String,
			FinishReason:   &msg.FinishReason.String,
			CreatedAt:      msg.CreatedAt.Time.Format(time.RFC3339),
			Generation:     int(msg.Generation),
		}
	}

	// Infer source from the most recent message with a source
	var source *string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Source.Valid && messages[i].Source.String != "" {
			s := messages[i].Source.String
			source = &s
			break
		}
	}

	// Get last message timestamp from the most recent message, or fall back to
	// the chat's own created_at so the response always carries a valid datetime.
	lastMessageTimestamp := chat.CreatedAt.Time.Format(time.RFC3339)
	if len(messages) > 0 {
		lastMessageTimestamp = messages[len(messages)-1].CreatedAt.Time.Format(time.RFC3339)
	}

	result := &gen.Chat{
		ID:                   chat.ID.String(),
		Title:                chat.Title.String,
		UserID:               &chat.UserID.String,
		ExternalUserID:       &chat.ExternalUserID.String,
		Source:               source,
		NumMessages:          len(messages),
		CreatedAt:            chat.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            chat.UpdatedAt.Time.Format(time.RFC3339),
		LastMessageTimestamp: lastMessageTimestamp,
		Messages:             resultMessages,
		TotalInputTokens:     nil,
		TotalOutputTokens:    nil,
		TotalTokens:          nil,
		TotalCost:            nil,
	}

	// Enrich with metrics from ClickHouse
	if err := s.enrichChatWithMetrics(ctx, authCtx.ProjectID.String(), result); err != nil {
		s.logger.WarnContext(ctx, "failed to enrich chat with metrics", attr.SlogError(err))
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
		return oops.C(oops.CodeInsufficientCredits).Log(
			ctx, s.logger,
			attr.SlogOrganizationID(orgID),
		)
	}

	return nil
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

	eventProperties := map[string]any{
		"action":            "chat_request_received",
		"organization_slug": authCtx.OrganizationSlug,
		"project_slug":      *authCtx.ProjectSlug,
		"success":           false,
		"source":            metadata.Source,
		"user_agent":        metadata.UserAgent,
		"origin":            metadata.Origin,
	}

	source := billing.ModelUsageSource(metadata.Source)
	if source == "" {
		source = billing.ModelUsageSourcePlayground
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
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, s.logger)
	}

	// Create a new reader with the same content for the proxy
	r.Body = io.NopCloser(strings.NewReader(string(reqBody)))

	var chatRequest openrouter.OpenAIChatRequest
	if err := json.Unmarshal(reqBody, &chatRequest); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse request body").Log(ctx, s.logger)
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
			).Log(ctx, s.logger)
		}
	}

	chatIDHeader := r.Header.Get("Gram-Chat-ID")

	eventProperties["model"] = chatRequest.Model
	eventProperties["chat_id"] = chatIDHeader

	chatID := uuid.Nil
	if chatIDHeader != "" {
		chatID, err = uuid.Parse(chatIDHeader)
		if err != nil {
			return oops.E(oops.CodeInvalid, err, "invalid chat ID").Log(ctx, s.logger)
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

	completionReq := openrouter.CompletionRequest{
		OrgID:          orgID,
		ProjectID:      authCtx.ProjectID.String(),
		Messages:       chatRequest.Messages,
		Tools:          chatRequest.Tools,
		Temperature:    &temp,
		Model:          chatRequest.Model,
		Stream:         false,
		UsageSource:    source,
		ChatID:         chatID,
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
		NormalizeOutboundMessages: r.URL.Query().Get("unstable_normalizeOutboundMessages") == "1",
	}

	isStreaming := chatRequest.Stream
	if isStreaming {
		// The streamingResponseReader automatically parses SSE and triggers capture/tracking on close
		streamBody, err := s.completionClient.GetCompletionStream(ctx, completionReq)
		if err != nil {
			return oops.E(oops.CodeGatewayError, err, "get completion stream").Log(ctx, s.logger)
		}
		defer o11y.NoLogDefer(func() error { return streamBody.Close() })

		// Set response headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Copy stream directly to response writer
		// UnifiedClient's streamingResponseReader handles SSE parsing and message capture
		flusher, canFlush := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, readErr := streamBody.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					s.logger.ErrorContext(ctx, "stream write error", attr.SlogError(writeErr))
					return oops.E(oops.CodeGatewayError, writeErr, "stream write failed").Log(ctx, s.logger)
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					s.logger.ErrorContext(ctx, "stream read error", attr.SlogError(readErr))
				}
				break
			}
		}

		eventProperties["success"] = true
		return nil
	}

	/**
	 * Non-Streaming
	 */
	response, err := s.completionClient.GetCompletion(ctx, completionReq)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "completion failed").Log(ctx, s.logger)
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
		Usage: &response.Usage,
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openAIResp); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode response").Log(ctx, s.logger)
	}

	eventProperties["success"] = true
	return nil
}

func (s *Service) CreditUsage(ctx context.Context, payload *gen.CreditUsagePayload) (*gen.CreditUsageResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	creditsUsed, creditLimit, err := s.openRouter.GetCreditsUsed(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get credit usage").Log(ctx, s.logger)
	}

	return &gen.CreditUsageResult{
		CreditsUsed:    creditsUsed,
		MonthlyCredits: creditLimit,
	}, nil
}

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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").Log(ctx, s.logger)
	}

	if chat.ProjectID != *authCtx.ProjectID {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Return current title from DB. Title generation happens asynchronously via
	// Temporal after first completion; title will be available on next list()/fetch().
	title := "New Chat"
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
		return oops.E(oops.CodeBadRequest, err, "invalid chat id").Log(ctx, s.logger)
	}

	err = s.repo.SoftDeleteChat(ctx, repo.SoftDeleteChatParams{
		ID:        chatID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft delete chat").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list messages").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to store feedback").Log(ctx, s.logger)
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
func (s *Service) loadMessageContent(ctx context.Context, msg repo.ChatMessage) json.RawMessage {
	content, _ := json.Marshal(msg.Content)

	// 1. Try ContentRaw first (inline JSON for small messages)
	if len(msg.ContentRaw) > 0 {
		return msg.ContentRaw
	}

	// 2. Try fetching from asset storage
	if msg.ContentAssetUrl.Valid && msg.ContentAssetUrl.String != "" {
		assetURL, err := url.Parse(msg.ContentAssetUrl.String)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to parse message content asset URL",
				attr.SlogError(err),
				attr.SlogChatID(msg.ChatID.String()),
			)
			return content
		}

		reader, err := s.assetStorage.Read(ctx, assetURL)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to open message content from asset storage",
				attr.SlogError(err),
				attr.SlogChatID(msg.ChatID.String()),
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
				attr.SlogChatID(msg.ChatID.String()),
			)
			return content
		}

		return data
	}

	// 3. Fallback to plain text content
	return content
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

	results := make([]uploadResult, len(rows))

	// Upload all messages to asset storage in parallel.
	// We don't use errgroup's error propagation here because we want to
	// continue even if some uploads fail - we'll record the errors and
	// still insert the messages with their plain text content.
	var wg errgroup.Group
	for i, row := range rows {
		wg.Go(func() error {
			// Marshal the message content to JSON.
			jsonData, err := openrouter.GetContentJSON(row.content)
			if err != nil {
				results[i] = uploadResult{assetURL: "", jsonData: nil, err: fmt.Errorf("marshal message content: %w", err)}
				return nil // Don't abort other uploads
			}

			// Compute SHA256 hash for the content-addressable path.
			hash := sha256.Sum256(jsonData)
			hashHex := hex.EncodeToString(hash[:])

			// Build asset path: <project_id>/chats/<chat_id>/<sha256_hex>.json
			assetPath := path.Join(row.projectID.String(), "chats", row.chatID.String(), hashHex+".json")

			// Upload to asset storage.
			writer, assetURL, err := assetStorage.Write(ctx, assetPath, "application/json", int64(len(jsonData)))
			if err != nil {
				results[i] = uploadResult{assetURL: "", jsonData: jsonData, err: fmt.Errorf("create asset writer: %w", err)}
				return nil
			}

			if _, err := io.Copy(writer, bytes.NewReader(jsonData)); err != nil {
				_ = writer.Close()
				results[i] = uploadResult{assetURL: "", jsonData: jsonData, err: fmt.Errorf("write asset content: %w", err)}
				return nil
			}

			if err := writer.Close(); err != nil {
				results[i] = uploadResult{assetURL: "", jsonData: jsonData, err: fmt.Errorf("finalize asset upload: %w", err)}
				return nil
			}

			results[i] = uploadResult{
				assetURL: assetURL.String(),
				jsonData: jsonData,
				err:      nil,
			}
			return nil
		})
	}

	_ = wg.Wait() // Always succeeds since goroutines don't return errors

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
		return oops.E(oops.CodeUnexpected, err, "failed to insert chat messages").Log(ctx, logger)
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

// enrichChatsWithResolutionsWithMetrics fetches token and cost metrics from ClickHouse and adds them to chat overviews with resolutions.
// This is a best-effort operation - if metrics can't be fetched, chats are returned with zero values.
func (s *Service) enrichChatsWithResolutionsWithMetrics(ctx context.Context, projectID string, chats []*gen.ChatOverviewWithResolutions) error {
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
