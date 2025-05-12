package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	gen "github.com/speakeasy-api/gram/gen/chat"
	srv "github.com/speakeasy-api/gram/gen/http/chat/server"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/chat/repo"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

var _ gen.Service = (*Service)(nil)

type Service struct {
	openaiAPIKey   string
	auth           *auth.Auth
	repo           *repo.Queries
	tracer         trace.Tracer
	openRouter     openrouter.Provisioner
	logger         *slog.Logger
	proxyTransport http.RoundTripper
}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, openaiAPIKey string, openRouter openrouter.Provisioner) *Service {
	return &Service{
		openaiAPIKey:   openaiAPIKey,
		auth:           auth.New(logger, db, sessions),
		logger:         logger,
		repo:           repo.New(db),
		tracer:         otel.Tracer("github.com/speakeasy-api/gram/internal/chat"),
		openRouter:     openRouter,
		proxyTransport: cleanhttp.DefaultPooledTransport(),
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
	mux.Handle("POST", "/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		service.HandleCompletion(w, r)
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListChats(ctx context.Context, payload *gen.ListChatsPayload) (*gen.ListChatsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	chats, err := s.repo.ListChats(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	result := make([]*gen.ChatOverview, len(chats))
	for i, chat := range chats {
		result[i] = &gen.ChatOverview{
			ID:          chat.ID.String(),
			UserID:      chat.UserID.String,
			Title:       chat.Title.String,
			NumMessages: int(chat.NumMessages),
			CreatedAt:   chat.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:   chat.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.ListChatsResult{
		Chats: result,
	}, nil
}

func (s *Service) LoadChat(ctx context.Context, payload *gen.LoadChatPayload) (*gen.Chat, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	chat, err := s.repo.GetChat(ctx, uuid.MustParse(payload.ID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, err
	}

	messages, err := s.repo.ListChatMessages(ctx, chat.ID)
	if err != nil {
		return nil, err
	}

	resultMessages := make([]*gen.ChatMessage, len(messages))
	for i, msg := range messages {
		toolCalls := string(msg.ToolCalls)
		resultMessages[i] = &gen.ChatMessage{
			ID:           msg.ID.String(),
			Role:         msg.Role,
			Model:        msg.Model.String,
			UserID:       &msg.UserID.String,
			Content:      &msg.Content,
			ToolCalls:    &toolCalls,
			ToolCallID:   &msg.ToolCallID.String,
			FinishReason: &msg.FinishReason.String,
			CreatedAt:    msg.CreatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.Chat{
		ID:          chat.ID.String(),
		Title:       chat.Title.String,
		UserID:      chat.UserID.String,
		NumMessages: len(messages),
		CreatedAt:   chat.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:   chat.UpdatedAt.Time.Format(time.RFC3339),
		Messages:    resultMessages,
	}, nil
}

// HandleCompletion is a proxy to the OpenAI API that logs request and response data.
func (s *Service) HandleCompletion(w http.ResponseWriter, r *http.Request) {
	// Authorize with session or API key
	sc := security.APIKeyScheme{
		Name:           auth.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}

	ctx, err := s.auth.Authorize(r.Context(), r.Header.Get(auth.SessionHeader), &sc)
	if err != nil {
		sc := security.APIKeyScheme{
			Name:           auth.KeySecurityScheme,
			RequiredScopes: []string{"consumer"},
			Scopes:         []string{},
		}
		ctx, err = s.auth.Authorize(r.Context(), r.Header.Get(auth.APIKeyHeader), &sc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	}

	// Authorize with project
	sc = security.APIKeyScheme{
		Name:           auth.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err = s.auth.Authorize(ctx, r.Header.Get(auth.ProjectHeader), &sc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if authCtx.ProjectID == nil {
		http.Error(w, "Unauthorized -- no project ID", http.StatusUnauthorized)
		return
	}

	orgID := ""
	if oid, ok := ctx.Value("organization_id").(string); ok {
		orgID = oid
	}

	userID := ""
	if uid, ok := ctx.Value("user_id").(string); ok {
		userID = uid
	}

	s.logger.InfoContext(ctx, "chat request received",
		slog.String("project_id", authCtx.ProjectID.String()),
		slog.String("org_id", orgID),
		slog.String("user_id", userID))

	// Read the request body
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read request body", slog.String("error", err.Error()))
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var chatRequest OpenAIChatRequest
	if err := json.Unmarshal(reqBody, &chatRequest); err != nil {
		s.logger.ErrorContext(ctx, "failed to unmarshal request body", slog.String("error", err.Error()))
		http.Error(w, "Failed to unmarshal request body", http.StatusBadRequest)
		return
	}

	chatIDHeader := r.Header.Get("Gram-Chat-ID")

	chatID, err := s.startOrResumeChat(ctx, orgID, *authCtx.ProjectID, userID, chatIDHeader, chatRequest)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to start or resume chat", slog.String("error", err.Error()))
		http.Error(w, "Failed to start or resume chat", http.StatusInternalServerError)
		return
	}

	// Create a new reader with the same content for the proxy
	r.Body = io.NopCloser(strings.NewReader(string(reqBody)))

	// Check if this is a streaming request
	isStreaming := chatRequest.Stream

	// Create a custom response writer to capture the response
	//nolint:exhaustruct // the other fields are set during processing
	respCaptor := &responseCaptor{
		ResponseWriter:       w,
		logger:               s.logger,
		ctx:                  ctx,
		isStreaming:          isStreaming,
		chatID:               chatID,
		repo:                 s.repo,
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
	}

	// Set up the proxy to OpenAI
	target, _ := url.Parse(openrouter.OpenRouterBaseURL)
	apiKey, err := s.openRouter.ProvisionAPIKey(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "error getting openrouter api key falling back to openai", slog.String("error", err.Error()))
		// Fallback to OpenAI API key until fully implemented
		target, _ = url.Parse("https://api.openai.com")
		apiKey = s.openaiAPIKey
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

		// For non-streaming responses, we can log the entire body
		if !isStreaming {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				s.logger.ErrorContext(ctx, "failed to read response body", slog.String("error", err.Error()))
				return err
			}

			// Parse and log the complete response
			var chatResponse OpenAIChatResponse
			if err := json.Unmarshal(body, &chatResponse); err == nil {
				if len(chatResponse.Choices) > 0 {
					assistantMsg := chatResponse.Choices[0].Message
					toolCallsJSON, err := json.Marshal(assistantMsg.ToolCalls)
					if err != nil {
						s.logger.ErrorContext(ctx, "failed to marshal tool calls", slog.String("error", err.Error()))
					}

					_, err = s.repo.CreateChatMessage(ctx, []repo.CreateChatMessageParams{{
						ChatID:       chatID,
						Role:         "assistant",
						Model:        conv.ToPGText(chatRequest.Model),
						Content:      assistantMsg.Content,
						MessageID:    conv.ToPGText(chatResponse.ID),
						ToolCallID:   conv.ToPGText(assistantMsg.ToolCallID),
						UserID:       conv.ToPGText(userID),
						ToolCalls:    toolCallsJSON,
						FinishReason: conv.ToPGTextEmpty(""),
					}})
					if err != nil {
						s.logger.ErrorContext(ctx, "failed to store chat message", slog.String("error", err.Error()))
					}
				}
			} else {
				s.logger.InfoContext(ctx, "response body (could not parse)", slog.String("body", string(body)))
			}

			// Restore the body for the client
			resp.Body = io.NopCloser(strings.NewReader(string(body)))
		}

		return nil
	}

	// Serve the proxy through our custom writer
	proxy.ServeHTTP(respCaptor, r)
}

func (s *Service) startOrResumeChat(ctx context.Context, orgID string, projectID uuid.UUID, userID string, chatIDHeader string, request OpenAIChatRequest) (uuid.UUID, error) {
	chatID, err := uuid.Parse(chatIDHeader)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeInvalid, err, "invalid chat ID in header")
	}

	_, err = s.repo.UpsertChat(ctx, repo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userID),
		Title:          conv.ToPGText("New Chat"), // TODO title
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create chat", slog.String("error", err.Error()))
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "failed to create chat")
	}

	// Get the number of already-stored messages so we can insert any new ones
	chatCount, err := s.repo.CountChatMessages(ctx, chatID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get chat history", slog.String("error", err.Error()))
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "failed to get chat history")
	}

	// This shouldn't happen, and also it doesn't really matter if it does, but we error anyway so we can fix it
	if int(chatCount) > len(request.Messages) {
		return uuid.Nil, oops.E(oops.CodeInvalid, nil, "chat history mismatch")
	}

	// If the stored chat history is shorter than the request, insert the missing messages
	// Most of the time, this just serves to store the new message the user just sent
	if int(chatCount) < len(request.Messages) {
		for _, msg := range request.Messages[int(chatCount):] {
			_, err := s.repo.CreateChatMessage(ctx, []repo.CreateChatMessageParams{{
				ChatID:       chatID,
				Role:         msg.Role,
				Model:        conv.ToPGText(request.Model),
				Content:      msg.Content,
				UserID:       conv.ToPGText(userID),
				ToolCallID:   conv.ToPGText(msg.ToolCallID),
				ToolCalls:    nil,
				FinishReason: conv.ToPGTextEmpty(""),
				MessageID:    conv.ToPGTextEmpty(""),
			}})

			if err != nil {
				s.logger.ErrorContext(ctx, "failed to create chat message", slog.String("error", err.Error()))
			}
		}
	}

	return chatID, nil
}

// responseCaptor captures and logs response data
type responseCaptor struct {
	http.ResponseWriter
	//nolint:containedctx // responseCaptor needs to implement io.Writer so its methods cannot accept a context
	ctx                  context.Context
	logger               *slog.Logger
	isStreaming          bool
	chatID               uuid.UUID
	messageContent       *strings.Builder
	messageID            string
	model                string
	isDone               bool
	messageWritten       bool
	finishReason         *string
	repo                 *repo.Queries
	toolCallID           string
	accumulatedToolCalls map[int]ToolCall // Map of index to accumulated tool call data
}

func (r *responseCaptor) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseCaptor) Write(b []byte) (int, error) {
	// If this is a streaming response, parse and collect the chunks
	if r.isStreaming {
		chunkData := string(b)
		for _, line := range strings.Split(chunkData, "\n") {
			r.processLine(line)
		}
	}

	// If we're done, log the message
	if r.isDone && !r.messageWritten {
		// Convert accumulated tool calls to JSON for storage if needed
		var toolCallsJSON []byte
		if len(r.accumulatedToolCalls) > 0 {
			var err error
			toolCallsArr := slices.Collect(maps.Values(r.accumulatedToolCalls))
			toolCallsJSON, err = json.Marshal(toolCallsArr)
			if err != nil {
				r.logger.ErrorContext(r.ctx, "failed to marshal tool calls", slog.String("error", err.Error()))
			}
		}

		// TODO batch insert the messages
		_, err := r.repo.CreateChatMessage(r.ctx, []repo.CreateChatMessageParams{{
			ChatID:       r.chatID,
			MessageID:    conv.ToPGText(r.messageID),
			Role:         "assistant",
			Model:        conv.ToPGText(r.model),
			Content:      r.messageContent.String(),
			ToolCallID:   conv.ToPGText(r.toolCallID),
			ToolCalls:    toolCallsJSON,
			FinishReason: conv.PtrToPGText(r.finishReason),
			UserID:       conv.ToPGTextEmpty(""), // These are agent messages, not user messages
		}})
		if err != nil {
			r.logger.ErrorContext(r.ctx, "failed to store chat message", slog.String("error", err.Error()))
		}
		r.messageWritten = true
	}

	// Forward the bytes to the client
	return r.ResponseWriter.Write(b)
}

func (r *responseCaptor) processLine(line string) {
	if strings.HasPrefix(line, "data: ") {
		data := strings.TrimPrefix(line, "data: ")

		// Check if this is the [DONE] marker
		if strings.TrimSpace(data) == "[DONE]" {
			r.isDone = true
			return
		}

		// Parse the chunk as JSON
		var chunk StreamingChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			r.messageID = chunk.ID
			r.model = chunk.Model

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
							r.accumulatedToolCalls[tc.Index] = ToolCall{
								Index:    tc.Index,
								ID:       tc.ID,
								Type:     tc.Type,
								Function: ToolCallFunction{}, //nolint:exhaustruct // filled in later
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
			r.logger.ErrorContext(r.ctx, "failed to parse streaming chunk", slog.String("error", err.Error()))
		}
	}
}
