package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	srv "github.com/speakeasy-api/gram/server/gen/http/chat/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// FallbackModelUsageTracker schedules fallback model usage tracking when the inline call fails.
type FallbackModelUsageTracker interface {
	ScheduleFallbackModelUsageTracking(ctx context.Context, generationID, orgID, projectID string, source billing.ModelUsageSource, chatID string) error
}

// ChatTitleGenerator schedules async chat title generation.
type ChatTitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID string) error
}

type Service struct {
	auth                 *auth.Auth
	repo                 *repo.Queries
	tracer               trace.Tracer
	openRouter           openrouter.Provisioner
	chatClient           *openrouter.ChatClient
	logger               *slog.Logger
	sessions             *sessions.Manager
	chatSessions         *chatsessions.Manager
	proxyTransport       http.RoundTripper
	fallbackUsageTracker FallbackModelUsageTracker
	chatTitleGenerator   ChatTitleGenerator
	posthog              *posthog.Posthog
}

func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	chatSessions *chatsessions.Manager,
	openRouter openrouter.Provisioner,
	chatClient *openrouter.ChatClient,
	fallbackUsageTracker FallbackModelUsageTracker,
	chatTitleGenerator ChatTitleGenerator,
	posthog *posthog.Posthog,
) *Service {
	logger = logger.With(attr.SlogComponent("chat"))

	return &Service{
		auth:                 auth.New(logger, db, sessions),
		sessions:             sessions,
		chatSessions:         chatSessions,
		logger:               logger,
		repo:                 repo.New(db),
		tracer:               otel.Tracer("github.com/speakeasy-api/gram/server/internal/chat"),
		openRouter:           openRouter,
		chatClient:           chatClient,
		proxyTransport:       cleanhttp.DefaultPooledTransport(),
		fallbackUsageTracker: fallbackUsageTracker,
		chatTitleGenerator:   chatTitleGenerator,
		posthog:              posthog,
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
			result = append(result, &gen.ChatOverview{
				ID:             chat.ID.String(),
				UserID:         nil,
				ExternalUserID: &chat.ExternalUserID.String,
				Title:          chat.Title.String,
				NumMessages:    int(chat.NumMessages),
				CreatedAt:      chat.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:      chat.UpdatedAt.Time.Format(time.RFC3339),
			})
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
			result = append(result, &gen.ChatOverview{
				ID:             chat.ID.String(),
				UserID:         &chat.UserID.String,
				ExternalUserID: nil,
				Title:          chat.Title.String,
				NumMessages:    int(chat.NumMessages),
				CreatedAt:      chat.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:      chat.UpdatedAt.Time.Format(time.RFC3339),
			})
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
		result = append(result, &gen.ChatOverview{
			ID:             chat.ID.String(),
			UserID:         &chat.UserID.String,
			ExternalUserID: nil,
			Title:          chat.Title.String,
			NumMessages:    int(chat.NumMessages),
			CreatedAt:      chat.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      chat.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListChatsResult{Chats: result}, nil
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load chat").Log(ctx, s.logger)
	}

	// older chat_messages may not have project_id in the model, but it will always exist on the chat
	if chat.ProjectID != *authCtx.ProjectID {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if chat.ExternalUserID.String != "" && chat.ExternalUserID.String != authCtx.ExternalUserID {
		return nil, oops.C(oops.CodeUnauthorized)
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
			Content:        &msg.Content,
			ToolCalls:      &toolCalls,
			ToolCallID:     &msg.ToolCallID.String,
			FinishReason:   &msg.FinishReason.String,
			CreatedAt:      msg.CreatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.Chat{
		ID:             chat.ID.String(),
		Title:          chat.Title.String,
		UserID:         &chat.UserID.String,
		ExternalUserID: &chat.ExternalUserID.String,
		NumMessages:    len(messages),
		CreatedAt:      chat.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      chat.UpdatedAt.Time.Format(time.RFC3339),
		Messages:       resultMessages,
	}, nil
}

// HandleCompletion is a proxy to the OpenAI API that logs request and response data.
func (s *Service) HandleCompletion(w http.ResponseWriter, r *http.Request) error {
	ctx, authCtx, err := s.directAuthorize(r.Context(), r)
	if err != nil {
		return err
	}

	orgID := authCtx.ActiveOrganizationID
	userID := authCtx.UserID

	eventProperties := map[string]any{
		"action":            "chat_request_received",
		"organization_slug": authCtx.OrganizationSlug,
		"project_slug":      *authCtx.ProjectSlug,
		"success":           false,
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

	// Validate that the model is in the allowlist
	if !openrouter.IsModelAllowed(chatRequest.Model) {
		return oops.E(oops.CodeBadRequest, nil, "model %s is not allowed", chatRequest.Model).Log(ctx, s.logger)
	}

	chatIDHeader := r.Header.Get("Gram-Chat-ID")

	eventProperties["model"] = chatRequest.Model
	eventProperties["chat_id"] = chatIDHeader

	respCaptor := w

	if chatIDHeader != "" {
		chatResult, err := s.startOrResumeChat(ctx, orgID, *authCtx.ProjectID, userID, authCtx.ExternalUserID, chatIDHeader, chatRequest)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to start or resume chat").Log(ctx, s.logger)
		}

		// Check if this is a streaming request
		isStreaming := chatRequest.Stream

		// Create a custom response writer to capture the response
		respCaptor = &responseCaptor{
			ResponseWriter:       w,
			logger:               s.logger,
			ctx:                  ctx,
			isStreaming:          isStreaming,
			orgID:                orgID,
			chatID:               chatResult.ChatID,
			projectID:            *authCtx.ProjectID,
			repo:                 s.repo,
			messageContent:       &strings.Builder{},
			lineBuf:              &strings.Builder{},
			accumulatedToolCalls: make(map[int]openrouter.ToolCall),
			messageID:            "",
			model:                "",
			isDone:               false,
			messageWritten:       false,
			finishReason:         nil,
			toolCallID:           "",
			usage: openrouter.Usage{
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			},
			usageSet:           false,
			isFirstMessage:     chatResult.IsFirstMessage,
			chatTitleGenerator: s.chatTitleGenerator,
		}
	}

	// Set up the proxy to OpenRouter
	target, err := url.Parse(openrouter.OpenRouterBaseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error parsing openrouter url").Log(ctx, s.logger)
	}

	apiKey, err := s.openRouter.ProvisionAPIKey(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "error provisioning openrouter api key").Log(ctx, s.logger)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = s.proxyTransport
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
		// Safely join /api (openrouter base path) + /v1/chat/completions
		req.URL.Path = path.Join("/", target.Path, "v1/chat/completions")

		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Handle CORS headers
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Remove any existing CORS headers
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")

		// TODO: Store chat history for non-streaming requests

		return nil
	}

	// Defer executes when HandleCompletion returns, which happens after proxy.ServeHTTP completes
	// (whether normally or due to client disconnection)
	defer func() {
		if respCaptorWithTracking, ok := respCaptor.(*responseCaptor); ok && respCaptorWithTracking.messageID != "" {
			go func() {
				if err := s.openRouter.TriggerModelUsageTracking(
					context.WithoutCancel(ctx),
					respCaptorWithTracking.messageID,
					orgID,
					authCtx.ProjectID.String(),
					billing.ModelUsageSourceChat,
					respCaptorWithTracking.chatID.String(),
				); err != nil {
					// Only schedule fallback for 404 errors (generation not found yet)
					if errors.Is(err, openrouter.ErrGenerationNotFound) {
						s.logger.WarnContext(ctx, "generation not found, scheduling fallback tracking",
							attr.SlogError(err),
							attr.SlogOrganizationID(orgID),
						)
						if s.fallbackUsageTracker != nil {
							if scheduleErr := s.fallbackUsageTracker.ScheduleFallbackModelUsageTracking(
								context.WithoutCancel(ctx),
								respCaptorWithTracking.messageID,
								orgID,
								authCtx.ProjectID.String(),
								billing.ModelUsageSourceChat,
								respCaptorWithTracking.chatID.String(),
							); scheduleErr != nil {
								s.logger.ErrorContext(ctx, "failed to schedule fallback model usage tracking",
									attr.SlogError(scheduleErr),
									attr.SlogOrganizationID(orgID),
								)
							}
						}
					} else {
						s.logger.ErrorContext(ctx, "failed to track model usage",
							attr.SlogError(err),
							attr.SlogOrganizationID(orgID),
						)
					}
				}
			}()
		} else {
			msg := "no message ID"
			if respCaptorWithTracking != nil {
				msg += "; model: " + respCaptorWithTracking.model
				msg += "; org ID: " + respCaptorWithTracking.orgID
				msg += "; project ID: " + respCaptorWithTracking.projectID.String()
				msg += "; chat ID: " + respCaptorWithTracking.chatID.String()
				msg += fmt.Sprintf("; isStreaming: %t", respCaptorWithTracking.isStreaming)
			}
			s.logger.ErrorContext(ctx, "failed to track model usage", attr.SlogError(errors.New(msg)))
		}
	}()

	proxy.ServeHTTP(respCaptor, r)

	eventProperties["success"] = true

	return nil
}

func (s *Service) CreditUsage(ctx context.Context, payload *gen.CreditUsagePayload) (*gen.CreditUsageResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.SessionID == nil {
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
	case errors.Is(err, sql.ErrNoRows):
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

// startOrResumeChatResult contains the result of starting or resuming a chat.
type startOrResumeChatResult struct {
	ChatID         uuid.UUID
	IsFirstMessage bool // True if this is the first assistant response for this chat
}

func (s *Service) startOrResumeChat(ctx context.Context, orgID string, projectID uuid.UUID, userID string, externalUserID string, chatIDHeader string, request openrouter.OpenAIChatRequest) (*startOrResumeChatResult, error) {
	chatID, err := uuid.Parse(chatIDHeader)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid chat ID in header")
	}

	// Create chat with placeholder title - title generation happens via the generateTitle RPC
	_, err = s.repo.UpsertChat(ctx, repo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(externalUserID),
		Title:          conv.ToPGText("New Chat"),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create chat", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create chat")
	}

	// Get the number of already-stored messages so we can insert any new ones
	chatCount, err := s.repo.CountChatMessages(ctx, chatID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get chat history", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get chat history")
	}

	// This shouldn't happen, and also it doesn't really matter if it does, but we error anyway so we can fix it
	if int(chatCount) > len(request.Messages) {
		return nil, oops.E(oops.CodeInvalid, nil, "chat history mismatch")
	}

	// If the stored chat history is shorter than the request, insert the missing messages
	// Most of the time, this just serves to store the new message the user just sent
	if int(chatCount) < len(request.Messages) {
		for _, msg := range request.Messages[int(chatCount):] {
			_, err := s.repo.CreateChatMessage(ctx, []repo.CreateChatMessageParams{{
				ChatID:           chatID,
				ProjectID:        projectID,
				Role:             msg.Role,
				Model:            conv.ToPGText(request.Model),
				Content:          msg.Content,
				UserID:           conv.ToPGText(userID),
				ExternalUserID:   conv.ToPGText(externalUserID),
				ToolCallID:       conv.ToPGText(msg.ToolCallID),
				ToolCalls:        nil,
				FinishReason:     conv.ToPGTextEmpty(""),
				MessageID:        conv.ToPGTextEmpty(""),
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			}})

			if err != nil {
				s.logger.ErrorContext(ctx, "failed to create chat message", attr.SlogError(err))
			}
		}
	}

	// This is the first message if there are no existing messages in the chat
	isFirstMessage := chatCount == 0

	return &startOrResumeChatResult{
		ChatID:         chatID,
		IsFirstMessage: isFirstMessage,
	}, nil
}

// responseCaptor captures and logs response data
type responseCaptor struct {
	http.ResponseWriter
	//nolint:containedctx // responseCaptor needs to implement io.Writer so its methods cannot accept a context
	ctx                  context.Context
	logger               *slog.Logger
	isStreaming          bool
	orgID                string
	chatID               uuid.UUID
	projectID            uuid.UUID
	messageContent       *strings.Builder
	lineBuf              *strings.Builder // Buffer for accumulating partial SSE lines across Write calls
	messageID            string
	model                string
	isDone               bool
	usageSet             bool
	messageWritten       bool
	finishReason         *string
	repo                 *repo.Queries
	toolCallID           string
	accumulatedToolCalls map[int]openrouter.ToolCall // Map of index to accumulated tool call data
	usage                openrouter.Usage
	// Title generation
	isFirstMessage     bool
	chatTitleGenerator ChatTitleGenerator
}

func (r *responseCaptor) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher to ensure SSE streaming data is sent immediately.
// Without this, the reverse proxy may buffer chunks, breaking stream parsing on the client.
func (r *responseCaptor) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseCaptor) Write(b []byte) (int, error) {
	// If this is a streaming response, parse and collect the chunks
	if r.isStreaming {
		isDoneBeforeLineProcess := r.isDone

		// Append new data to the line buffer
		r.lineBuf.Write(b)
		bufData := r.lineBuf.String()

		// Find the last newline - everything before it is complete lines we can process
		lastNewline := strings.LastIndex(bufData, "\n")
		if lastNewline >= 0 {
			// Process all complete lines
			completeLines := bufData[:lastNewline]
			for _, line := range strings.Split(completeLines, "\n") {
				r.processLine(line)
			}

			// Keep the remainder (partial line) in the buffer for the next Write call
			r.lineBuf.Reset()
			if lastNewline < len(bufData)-1 {
				r.lineBuf.WriteString(bufData[lastNewline+1:])
			}
		}

		// Log if we are unexpectedly not receiving usage data on the next message after
		// Could be a sign of a parsing issue
		if isDoneBeforeLineProcess && !r.usageSet && r.usage.TotalTokens == 0 {
			r.logger.ErrorContext(r.ctx, fmt.Sprintf("streaming response finished without usage data for chat message: %s", r.chatID.String()))
		}
	}

	// If we're done, log the message
	// openrouter streams the usage data after the finish_reason, it's important we wait for that
	if r.isDone && r.usageSet && !r.messageWritten {
		// Process any remaining buffered line that didn't end with \n
		if r.lineBuf.Len() > 0 {
			r.processLine(r.lineBuf.String())
			r.lineBuf.Reset()
		}

		// Convert accumulated tool calls to JSON for storage if needed
		var toolCallsJSON []byte
		if len(r.accumulatedToolCalls) > 0 {
			var err error
			toolCallsArr := slices.Collect(maps.Values(r.accumulatedToolCalls))
			toolCallsJSON, err = json.Marshal(toolCallsArr)
			if err != nil {
				r.logger.ErrorContext(r.ctx, "failed to marshal tool calls", attr.SlogError(err))
			}
		}

		// TODO batch insert the messages
		_, err := r.repo.CreateChatMessage(r.ctx, []repo.CreateChatMessageParams{{
			ChatID:           r.chatID,
			ProjectID:        r.projectID,
			MessageID:        conv.ToPGText(r.messageID),
			Role:             "assistant",
			Model:            conv.ToPGText(r.model),
			Content:          r.messageContent.String(),
			ToolCallID:       conv.ToPGText(r.toolCallID),
			ToolCalls:        toolCallsJSON,
			PromptTokens:     int64(r.usage.PromptTokens),
			CompletionTokens: int64(r.usage.CompletionTokens),
			TotalTokens:      int64(r.usage.TotalTokens),
			FinishReason:     conv.PtrToPGText(r.finishReason),
			UserID:           conv.ToPGTextEmpty(""), // These are agent messages, not user messages
			ExternalUserID:   conv.ToPGTextEmpty(""), // These are agent messages, not user messages
		}})
		if err != nil {
			r.logger.ErrorContext(r.ctx, "failed to store chat message", attr.SlogError(err))
		}
		r.messageWritten = true

		// Use WithoutCancel to ensure the workflow is scheduled even if the HTTP request is cancelled.
		if r.isFirstMessage && r.chatTitleGenerator != nil {
			if err := r.chatTitleGenerator.ScheduleChatTitleGeneration(
				context.WithoutCancel(r.ctx),
				r.chatID.String(),
				r.orgID,
			); err != nil {
				r.logger.WarnContext(r.ctx, "failed to schedule chat title generation", attr.SlogError(err))
			}
		}
	}

	n, err := r.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("failed to write completion response: %w", err)
	}

	return n, nil
}

func (r *responseCaptor) processLine(line string) {
	if strings.HasPrefix(line, "data: ") {
		data := strings.TrimPrefix(line, "data: ")

		// Check if this is the [DONE] marker
		if strings.TrimSpace(data) == "[DONE]" {
			r.isDone = true
			r.usageSet = true
			return
		}

		// Parse the chunk as JSON
		var chunk openrouter.StreamingChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			// Capture ID from the first chunk only
			if r.messageID == "" && chunk.ID != "" {
				r.messageID = chunk.ID
			}
			r.model = chunk.Model

			if chunk.Usage != nil {
				r.usage = *chunk.Usage
				r.usageSet = true
			}

			// Process each choice in the chunk
			for _, choice := range chunk.Choices {
				// Append any content to our message
				r.messageContent.WriteString(choice.Delta.Content)

				// Process tool calls if present
				if len(choice.Delta.ToolCalls) > 0 {
					// Process each tool call
					for _, tc := range choice.Delta.ToolCalls {
						r.toolCallID = tc.ID // TODO: is there ever more than one tool call in a chunk?

						if _, ok := r.accumulatedToolCalls[tc.Index]; !ok {
							r.accumulatedToolCalls[tc.Index] = openrouter.ToolCall{
								Index: tc.Index,
								ID:    tc.ID,
								Type:  tc.Type,
								Function: openrouter.ToolCallFunction{
									Name:      "",
									Arguments: "",
								},
							}
						}

						// Accumulate function name if provided
						if tc.Function.Name != "" {
							c := r.accumulatedToolCalls[tc.Index]
							c.Function.Name = tc.Function.Name
							r.accumulatedToolCalls[tc.Index] = c
						}

						// Accumulate function arguments if provided
						if tc.Function.Arguments != "" {
							c := r.accumulatedToolCalls[tc.Index]
							c.Function.Arguments += tc.Function.Arguments
							r.accumulatedToolCalls[tc.Index] = c
						}
					}
				}

				// If we have a finish reason, the message is complete
				if choice.FinishReason != nil {
					r.finishReason = choice.FinishReason
					r.isDone = true
				}
			}
		} else {
			r.logger.ErrorContext(r.ctx, "failed to parse streaming chunk", attr.SlogError(err))
		}
	}
}
