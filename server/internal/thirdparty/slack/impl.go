package slack // trigger CI

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/slack/server"
	gen "github.com/speakeasy-api/gram/server/gen/slack"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
	toolset_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Configurations struct {
	GramServerURL     string
	GramSiteURL       string
	SignInRedirectURL string
}

// Service for gram dashboard authentication endpoints
type Service struct {
	tracer              trace.Tracer
	logger              *slog.Logger
	db                  *pgxpool.Pool
	sessions            *sessions.Manager
	enc                 *encryption.Client
	repo                *repo.Queries
	auth                *auth.Auth
	toolsetRepo         *toolset_repo.Queries
	cfg                 *Configurations
	client              *slack_client.SlackClient
	temporal            *temporal.Environment
	watchedThreadsCache cache.TypedCacheObject[types.AppMentionedThreads]
	tokenCache          cache.TypedCacheObject[types.SlackRegistrationToken]
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Client, redisClient *redis.Client, client *slack_client.SlackClient, temporal *temporal.Environment, cfg Configurations) *Service {
	logger = logger.With(attr.SlogComponent("slack"))

	redisCacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	return &Service{
		tracer:              otel.Tracer("github.com/speakeasy-api/gram/server/internal/auth"),
		logger:              logger,
		db:                  db,
		sessions:            sessions,
		enc:                 enc,
		repo:                repo.New(db),
		auth:                auth.New(logger, db, sessions),
		toolsetRepo:         toolset_repo.New(db),
		cfg:                 &cfg,
		client:              client,
		temporal:            temporal,
		watchedThreadsCache: cache.NewTypedObjectCache[types.AppMentionedThreads](logger.With(attr.SlogCacheNamespace("watched_threads")), redisCacheAdapter, cache.SuffixNone),
		tokenCache:          cache.NewTypedObjectCache[types.SlackRegistrationToken](logger.With(attr.SlogCacheNamespace("slack_tokens")), redisCacheAdapter, cache.SuffixNone),
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

	// Registration routes
	o11y.AttachHandler(mux, "GET", "/rpc/slack-apps.getByToken", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.GetByToken).ServeHTTP(w, r)
	})
	o11y.AttachHandler(mux, "POST", "/rpc/slack-apps.register", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.Register).ServeHTTP(w, r)
	})

	o11y.AttachHandler(mux, "GET", "/rpc/slack-apps/{id}/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.SlackAppOAuthCallback).ServeHTTP(w, r)
	})
	o11y.AttachHandler(mux, "POST", "/rpc/slack-apps/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.SlackAppEventHandler).ServeHTTP(w, r)
	})
}

// --- Per-app OAuth callback ---

func (s *Service) SlackAppOAuthCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid app ID").Log(ctx, s.logger)
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" {
		return oops.E(oops.CodeBadRequest, fmt.Errorf("missing code parameter"), "missing code parameter").Log(ctx, s.logger)
	}

	app, err := s.repo.GetSlackAppByID(ctx, appID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "slack app not found").Log(ctx, s.logger)
	}

	if !app.SlackClientID.Valid || !app.SlackClientSecret.Valid {
		return oops.E(oops.CodeBadRequest, fmt.Errorf("slack app not configured"), "slack app missing client credentials").Log(ctx, s.logger)
	}

	decryptedSecret, err := s.enc.Decrypt(app.SlackClientSecret.String)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "decrypt client secret").Log(ctx, s.logger)
	}

	// Use the actual request URL as redirect_uri so it matches what Slack saw
	// during the authorize step (e.g. when proxied through ngrok).
	callbackURL := fmt.Sprintf("%s://%s%s", r.URL.Scheme, r.Host, r.URL.Path)
	if r.URL.Scheme == "" {
		// Behind reverse proxy / TLS termination — reconstruct from headers.
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			scheme = "https"
		}
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		callbackURL = fmt.Sprintf("%s://%s%s", scheme, host, r.URL.Path)
	}
	response, err := s.client.OAuthV2AccessWithCredentials(ctx, code, callbackURL, app.SlackClientID.String, decryptedSecret)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "slack oauth exchange failed").Log(ctx, s.logger)
	}

	encryptedToken, err := s.enc.Encrypt([]byte(response.AccessToken))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encrypt bot token").Log(ctx, s.logger)
	}

	_, err = s.repo.InstallSlackApp(ctx, repo.InstallSlackAppParams{
		ID:             appID,
		SlackBotToken:  conv.ToPGText(encryptedToken),
		SlackTeamID:    conv.ToPGText(response.Team.ID),
		SlackTeamName:  conv.ToPGText(response.Team.Name),
		SlackBotUserID: conv.ToPGText(response.BotUserID),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "install slack app").Log(ctx, s.logger)
	}

	redirectURL := s.cfg.SignInRedirectURL
	if state != "" {
		if parsed, err := url.Parse(state); err == nil && isTrustedRedirect(parsed, s.cfg.GramSiteURL) {
			redirectURL = state
		}
	}
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	return nil
}

// --- Per-app event handler ---

func (s *Service) SlackAppEventHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Buffer body so we can use it for both signature validation and JSON decode
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "read request body").Log(ctx, s.logger)
	}

	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid app ID").Log(ctx, s.logger)
	}

	app, err := s.repo.GetSlackAppByID(ctx, appID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "slack app not found").Log(ctx, s.logger)
	}

	decryptedSigningSecret, err := s.enc.Decrypt(app.SlackSigningSecret.String)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "decrypt signing secret").Log(ctx, s.logger)
	}

	// Restore body for validateSlackEvent which reads r.Body
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if err := validateSlackEvent(r, decryptedSigningSecret); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "request payload failed validation").Log(ctx, s.logger)
	}

	var event types.SlackEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid request payload").Log(ctx, s.logger)
	}

	// Respond to Slack's URL verification challenge
	if event.Type == "url_verification" && event.Challenge != "" {
		w.Header().Set("Content-Type", "text/plain")
		_, err := w.Write([]byte(event.Challenge))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "write slack challenge response").Log(ctx, s.logger)
		}
		return nil
	}

	threadTs := event.Event.ThreadTs
	if threadTs == "" {
		threadTs = event.Event.Ts
	}

	processEvent := false
	switch event.Event.Type {
	case "app_mention":
		if event.Event.Text == "" {
			break
		}
		if err := s.watchedThreadsCache.Store(ctx, types.AppMentionedThreads{
			TeamID:   event.TeamID,
			Channel:  event.Event.Channel,
			ThreadTs: threadTs,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to store watched thread in cache", attr.SlogError(err))
		}
		processEvent = true

	case "message":
		if event.Event.Text == "" {
			break
		}
		if len(event.Authorizations) > 0 && event.Event.User == event.Authorizations[0].UserID {
			break
		}

		if event.Event.ChannelType == "im" {
			processEvent = true
			break
		}

		if len(event.Authorizations) > 0 && strings.HasPrefix(event.Event.Text, fmt.Sprintf("<@%s>", event.Authorizations[0].UserID)) {
			processEvent = false
			break
		}

		if event.Event.ChannelType == "channel" {
			cacheKey := types.AppMentionedThreadsCacheKey(event.TeamID, event.Event.Channel, threadTs)
			if _, err := s.watchedThreadsCache.Get(ctx, cacheKey); err == nil {
				processEvent = true
			}
		}

	default:
		s.logger.InfoContext(ctx, "we do not process this event type", attr.SlogSlackEventType(event.Event.Type))
	}

	if processEvent {
		// React with eyes immediately to acknowledge receipt
		decryptedBotToken, err := s.enc.Decrypt(app.SlackBotToken.String)
		if err != nil {
			s.logger.ErrorContext(ctx, "decrypt bot token for reaction", attr.SlogError(err))
		} else {
			if err := s.client.AddReaction(ctx, decryptedBotToken, slack_client.SlackAddReactionInput{
				ChannelID: event.Event.Channel,
				Timestamp: event.Event.Ts,
				Name:      "eyes",
			}); err != nil {
				s.logger.ErrorContext(ctx, "add eyes reaction to message", attr.SlogError(err))
			}
		}

		slackAccountID := event.Event.User
		if slackAccountID != "" {
			_, err := s.repo.GetSlackRegistrationWithUser(ctx, repo.GetSlackRegistrationWithUserParams{
				SlackAppID:     appID,
				SlackAccountID: slackAccountID,
			})
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return oops.E(oops.CodeUnexpected, err, "check slack registration").Log(ctx, s.logger)
				}

				// No registration — store token in Redis and send ephemeral registration link
				tokenBytes := make([]byte, 16)
				if _, err := rand.Read(tokenBytes); err != nil {
					return oops.E(oops.CodeUnexpected, err, "generate registration token").Log(ctx, s.logger)
				}
				token := hex.EncodeToString(tokenBytes)

				if err := s.tokenCache.Store(ctx, types.SlackRegistrationToken{
					Token:          token,
					SlackAppID:     appID.String(),
					SlackAccountID: slackAccountID,
					ChannelID:      event.Event.Channel,
				}); err != nil {
					return oops.E(oops.CodeUnexpected, err, "cache registration token").Log(ctx, s.logger)
				}

				registerURL := fmt.Sprintf("%s/slack/register?token=%s", s.cfg.GramSiteURL, token)

				decryptedBotToken, err := s.enc.Decrypt(app.SlackBotToken.String)
				if err != nil {
					return oops.E(oops.CodeUnexpected, err, "decrypt bot token for ephemeral").Log(ctx, s.logger)
				}

				if err := s.client.PostEphemeralMessage(ctx, decryptedBotToken, slack_client.SlackPostEphemeralInput{
					ChannelID: event.Event.Channel,
					UserID:    slackAccountID,
					Message:   fmt.Sprintf("To use this bot, please link your Gram account first: <%s|Connect to Gram>", registerURL),
					ThreadTS:  nil, // Intentionally don't post ephemeral messages in threads because they don't show up easily
				}); err != nil {
					s.logger.ErrorContext(ctx, "send ephemeral registration link", attr.SlogError(err))
				}

				w.WriteHeader(http.StatusOK)
				return nil
			}

		}

		event.GramAppID = appID.String()
		if _, err := background.ExecuteProcessSlackEventWorkflow(ctx, s.temporal, background.ProcessSlackWorkflowParams{
			Event: event,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "error kicking off slack event workflow").Log(ctx, s.logger)
		}
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

// --- Slack User Registration ---

type getByTokenToolset struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type getByTokenResponse struct {
	AppName  string              `json:"appName"`
	Toolsets []getByTokenToolset `json:"toolsets"`
	Token    string              `json:"token"`
}

// GetByToken resolves a registration token to a slack app and returns its info.
// This is called by the dashboard when a user visits /slack/register?token={token}.
func (s *Service) GetByToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := r.URL.Query().Get("token")
	if token == "" {
		return oops.E(oops.CodeBadRequest, fmt.Errorf("missing token"), "token is required").Log(ctx, s.logger)
	}

	cached, err := s.tokenCache.Get(ctx, types.SlackTokenCacheKey(token))
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "token not found or expired").Log(ctx, s.logger)
	}

	appID, err := uuid.Parse(cached.SlackAppID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse slack app ID from token").Log(ctx, s.logger)
	}

	app, err := s.repo.GetSlackAppByID(ctx, appID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "slack app not found").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListSlackAppToolsetNames(ctx, app.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list slack app toolsets").Log(ctx, s.logger)
	}

	toolsetResults := make([]getByTokenToolset, 0, len(toolsets))
	for _, ts := range toolsets {
		toolsetResults = append(toolsetResults, getByTokenToolset{
			Name: ts.Name,
			Slug: ts.Slug,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(getByTokenResponse{
		AppName:  app.Name,
		Toolsets: toolsetResults,
		Token:    token,
	}); err != nil {
		return fmt.Errorf("encode getByToken response: %w", err)
	}
	return nil
}

type registerRequest struct {
	Token string `json:"token"`
}

// Register completes the registration flow by linking a Slack account to a Gram user.
// The user must be authenticated via session cookie.
func (s *Service) Register(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	var body registerRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid request body").Log(ctx, s.logger)
	}
	if body.Token == "" {
		return oops.E(oops.CodeBadRequest, fmt.Errorf("missing token"), "token is required").Log(ctx, s.logger)
	}

	// Authenticate via session cookie
	ctx, err := s.sessions.AuthenticateWithCookie(ctx)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authentication required").Log(ctx, s.logger)
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, fmt.Errorf("no auth context"), "authentication required").Log(ctx, s.logger)
	}

	userID, err := uuid.Parse(authCtx.UserID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse user ID").Log(ctx, s.logger)
	}

	cached, err := s.tokenCache.Get(ctx, types.SlackTokenCacheKey(body.Token))
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "token not found or expired").Log(ctx, s.logger)
	}

	slackAppID, err := uuid.Parse(cached.SlackAppID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse slack app ID from token").Log(ctx, s.logger)
	}

	slackApp, err := s.repo.GetSlackAppByID(ctx, slackAppID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "slack app not found").Log(ctx, s.logger)
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, slackApp.ProjectID); err != nil {
		return err
	}

	if _, err := s.repo.CreateSlackRegistration(ctx, repo.CreateSlackRegistrationParams{
		SlackAppID:     slackAppID,
		SlackAccountID: cached.SlackAccountID,
		UserID:         userID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "create slack registration").Log(ctx, s.logger)
	}

	// Consume the token
	if err := s.tokenCache.DeleteByKey(ctx, types.SlackTokenCacheKey(body.Token)); err != nil {
		s.logger.ErrorContext(ctx, "delete consumed registration token", attr.SlogError(err))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		return fmt.Errorf("encode register response: %w", err)
	}
	return nil
}

// --- Slack Apps CRUD ---

func (s *Service) requireEnterprise(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.AccountType != "enterprise" {
		return nil, oops.E(oops.CodeUnauthorized, fmt.Errorf("only available for enterprise accounts"), "only available for enterprise accounts").Log(ctx, s.logger)
	}
	return authCtx, nil
}

const maxAppNameLength = 36

func (s *Service) CreateSlackApp(ctx context.Context, payload *gen.CreateSlackAppPayload) (res *gen.CreateSlackAppResult, err error) {
	authCtx, err := s.requireEnterprise(ctx)
	if err != nil {
		return nil, err
	}

	if len(payload.Name) > maxAppNameLength {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("app name must be %d characters or fewer", maxAppNameLength), "app name too long")
	}

	var iconAssetID uuid.NullUUID
	if payload.IconAssetID != nil {
		parsed, err := uuid.Parse(*payload.IconAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid icon asset ID").Log(ctx, s.logger)
		}
		iconAssetID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	// Validate toolset ownership before creating anything
	toolsetIDs := make([]string, 0, len(payload.ToolsetIds))
	for _, tsID := range payload.ToolsetIds {
		parsed, err := uuid.Parse(tsID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset ID").Log(ctx, s.logger)
		}
		ts, err := s.toolsetRepo.GetToolsetByID(ctx, parsed)
		if err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		if ts.ProjectID != *authCtx.ProjectID {
			return nil, oops.E(oops.CodeNotFound, nil, "toolset not found").Log(ctx, s.logger)
		}
		toolsetIDs = append(toolsetIDs, tsID)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	txRepo := s.repo.WithTx(tx)

	app, err := txRepo.CreateSlackApp(ctx, repo.CreateSlackAppParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           payload.Name,
		SystemPrompt:   conv.PtrToPGText(payload.SystemPrompt),
		IconAssetID:    iconAssetID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeConflict, err, "create slack app").Log(ctx, s.logger)
	}

	for _, tsID := range payload.ToolsetIds {
		parsed, _ := uuid.Parse(tsID) // already validated above
		_, err = txRepo.AddSlackAppToolset(ctx, repo.AddSlackAppToolsetParams{
			SlackAppID: app.ID,
			ToolsetID:  parsed,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "attach toolset to slack app").Log(ctx, s.logger)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit slack app creation").Log(ctx, s.logger)
	}

	redirectURL := s.oauthCallbackURL(app.ID)
	requestURL := s.eventsURL(app.ID)

	appResult := &gen.SlackAppResult{
		ID:            app.ID.String(),
		Name:          app.Name,
		Status:        app.Status,
		SlackClientID: conv.FromPGText[string](app.SlackClientID),
		SlackTeamID:   conv.FromPGText[string](app.SlackTeamID),
		SlackTeamName: conv.FromPGText[string](app.SlackTeamName),
		SystemPrompt:  conv.FromPGText[string](app.SystemPrompt),
		IconAssetID:   conv.FromNullableUUID(app.IconAssetID),
		ToolsetIds:    toolsetIDs,
		RedirectURL:   &redirectURL,
		RequestURL:    &requestURL,
		CreatedAt:     app.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     app.UpdatedAt.Time.Format(time.RFC3339),
	}

	return &gen.CreateSlackAppResult{
		App: appResult,
	}, nil
}

func (s *Service) ListSlackApps(ctx context.Context, payload *gen.ListSlackAppsPayload) (res *gen.ListSlackAppsResult, err error) {
	authCtx, err := s.requireEnterprise(ctx)
	if err != nil {
		return nil, err
	}

	apps, err := s.repo.ListSlackApps(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list slack apps").Log(ctx, s.logger)
	}

	items := make([]*gen.SlackAppResult, 0, len(apps))
	for _, app := range apps {
		toolsets, err := s.repo.ListSlackAppToolsets(ctx, repo.ListSlackAppToolsetsParams{
			SlackAppID: app.ID,
			ProjectID:  *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list slack app toolsets").Log(ctx, s.logger)
		}

		toolsetIDs := make([]string, 0, len(toolsets))
		for _, ts := range toolsets {
			toolsetIDs = append(toolsetIDs, ts.ToolsetID.String())
		}

		items = append(items, &gen.SlackAppResult{
			ID:            app.ID.String(),
			Name:          app.Name,
			Status:        app.Status,
			SlackClientID: conv.FromPGText[string](app.SlackClientID),
			SlackTeamID:   conv.FromPGText[string](app.SlackTeamID),
			SlackTeamName: conv.FromPGText[string](app.SlackTeamName),
			SystemPrompt:  conv.FromPGText[string](app.SystemPrompt),
			IconAssetID:   conv.FromNullableUUID(app.IconAssetID),
			ToolsetIds:    toolsetIDs,
			RedirectURL:   nil,
			RequestURL:    nil,
			CreatedAt:     app.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     app.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListSlackAppsResult{
		Items: items,
	}, nil
}

func (s *Service) GetSlackApp(ctx context.Context, payload *gen.GetSlackAppPayload) (res *gen.SlackAppResult, err error) {
	authCtx, err := s.requireEnterprise(ctx)
	if err != nil {
		return nil, err
	}

	appID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid app ID").Log(ctx, s.logger)
	}

	app, err := s.repo.GetSlackApp(ctx, repo.GetSlackAppParams{
		ID:        appID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "slack app not found").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListSlackAppToolsets(ctx, repo.ListSlackAppToolsetsParams{
		SlackAppID: app.ID,
		ProjectID:  *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list slack app toolsets").Log(ctx, s.logger)
	}

	toolsetIDs := make([]string, 0, len(toolsets))
	for _, ts := range toolsets {
		toolsetIDs = append(toolsetIDs, ts.ToolsetID.String())
	}

	redirectURL := s.oauthCallbackURL(app.ID)
	requestURL := s.eventsURL(app.ID)

	return &gen.SlackAppResult{
		ID:            app.ID.String(),
		Name:          app.Name,
		Status:        app.Status,
		SlackClientID: conv.FromPGText[string](app.SlackClientID),
		SlackTeamID:   conv.FromPGText[string](app.SlackTeamID),
		SlackTeamName: conv.FromPGText[string](app.SlackTeamName),
		SystemPrompt:  conv.FromPGText[string](app.SystemPrompt),
		IconAssetID:   conv.FromNullableUUID(app.IconAssetID),
		ToolsetIds:    toolsetIDs,
		RedirectURL:   &redirectURL,
		RequestURL:    &requestURL,
		CreatedAt:     app.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     app.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) ConfigureSlackApp(ctx context.Context, payload *gen.ConfigureSlackAppPayload) (res *gen.SlackAppResult, err error) {
	authCtx, err := s.requireEnterprise(ctx)
	if err != nil {
		return nil, err
	}

	appID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid app ID").Log(ctx, s.logger)
	}

	encClientSecret, err := s.enc.Encrypt([]byte(payload.SlackClientSecret))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt client secret").Log(ctx, s.logger)
	}

	encSigningSecret, err := s.enc.Encrypt([]byte(payload.SlackSigningSecret))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt signing secret").Log(ctx, s.logger)
	}

	app, err := s.repo.ConfigureSlackApp(ctx, repo.ConfigureSlackAppParams{
		ID:                 appID,
		ProjectID:          *authCtx.ProjectID,
		SlackClientID:      conv.ToPGText(payload.SlackClientID),
		SlackClientSecret:  conv.ToPGText(encClientSecret),
		SlackSigningSecret: conv.ToPGText(encSigningSecret),
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "configure slack app").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListSlackAppToolsets(ctx, repo.ListSlackAppToolsetsParams{
		SlackAppID: app.ID,
		ProjectID:  *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list slack app toolsets").Log(ctx, s.logger)
	}

	toolsetIDs := make([]string, 0, len(toolsets))
	for _, ts := range toolsets {
		toolsetIDs = append(toolsetIDs, ts.ToolsetID.String())
	}

	return &gen.SlackAppResult{
		ID:            app.ID.String(),
		Name:          app.Name,
		Status:        app.Status,
		SlackClientID: conv.FromPGText[string](app.SlackClientID),
		SlackTeamID:   conv.FromPGText[string](app.SlackTeamID),
		SlackTeamName: conv.FromPGText[string](app.SlackTeamName),
		SystemPrompt:  conv.FromPGText[string](app.SystemPrompt),
		IconAssetID:   conv.FromNullableUUID(app.IconAssetID),
		ToolsetIds:    toolsetIDs,
		RedirectURL:   nil,
		RequestURL:    nil,
		CreatedAt:     app.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     app.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) UpdateSlackApp(ctx context.Context, payload *gen.UpdateSlackAppPayload) (res *gen.SlackAppResult, err error) {
	authCtx, err := s.requireEnterprise(ctx)
	if err != nil {
		return nil, err
	}

	if payload.Name != nil && len(*payload.Name) > maxAppNameLength {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("app name must be %d characters or fewer", maxAppNameLength), "app name too long")
	}

	appID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid app ID").Log(ctx, s.logger)
	}

	var iconAssetID uuid.NullUUID
	if payload.IconAssetID != nil {
		parsed, err := uuid.Parse(*payload.IconAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid icon asset ID").Log(ctx, s.logger)
		}
		iconAssetID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	app, err := s.repo.UpdateSlackApp(ctx, repo.UpdateSlackAppParams{
		ID:           appID,
		ProjectID:    *authCtx.ProjectID,
		Name:         conv.PtrToPGText(payload.Name),
		SystemPrompt: conv.PtrToPGText(payload.SystemPrompt),
		IconAssetID:  iconAssetID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "update slack app").Log(ctx, s.logger)
	}

	toolsets, err := s.repo.ListSlackAppToolsets(ctx, repo.ListSlackAppToolsetsParams{
		SlackAppID: app.ID,
		ProjectID:  *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list slack app toolsets").Log(ctx, s.logger)
	}

	toolsetIDs := make([]string, 0, len(toolsets))
	for _, ts := range toolsets {
		toolsetIDs = append(toolsetIDs, ts.ToolsetID.String())
	}

	return &gen.SlackAppResult{
		ID:            app.ID.String(),
		Name:          app.Name,
		Status:        app.Status,
		SlackClientID: conv.FromPGText[string](app.SlackClientID),
		SlackTeamID:   conv.FromPGText[string](app.SlackTeamID),
		SlackTeamName: conv.FromPGText[string](app.SlackTeamName),
		SystemPrompt:  conv.FromPGText[string](app.SystemPrompt),
		IconAssetID:   conv.FromNullableUUID(app.IconAssetID),
		ToolsetIds:    toolsetIDs,
		RedirectURL:   nil,
		RequestURL:    nil,
		CreatedAt:     app.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     app.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) DeleteSlackApp(ctx context.Context, payload *gen.DeleteSlackAppPayload) (err error) {
	authCtx, err := s.requireEnterprise(ctx)
	if err != nil {
		return err
	}

	appID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid app ID").Log(ctx, s.logger)
	}

	err = s.repo.SoftDeleteSlackApp(ctx, repo.SoftDeleteSlackAppParams{
		ID:        appID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete slack app").Log(ctx, s.logger)
	}

	return nil
}

// isTrustedRedirect checks that a redirect URL points to the same host as the
// Gram site, or to localhost (for local development).
func isTrustedRedirect(u *url.URL, gramSiteURL string) bool {
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}
	if server, err := url.Parse(gramSiteURL); err == nil {
		return u.Hostname() == server.Hostname()
	}
	return false
}

func (s *Service) oauthCallbackURL(appID uuid.UUID) string {
	return fmt.Sprintf("%s/rpc/slack-apps/%s/oauth/callback", s.cfg.GramServerURL, appID.String())
}

func (s *Service) eventsURL(appID uuid.UUID) string {
	return fmt.Sprintf("%s/rpc/slack-apps/%s/events", s.cfg.GramServerURL, appID.String())
}

// --- Auth ---

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// validateSlackEvent validates the Slack request signature and timestamp.
// This follows slacks recommended standards https://api.slack.com/authentication/verifying-requests-from-slack
func validateSlackEvent(r *http.Request, signingSecret string) error {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	if timestamp == "" {
		return fmt.Errorf("missing X-Slack-Request-Timestamp header")
	}
	sig := r.Header.Get("X-Slack-Signature")
	if sig == "" {
		return fmt.Errorf("missing X-Slack-Signature header")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if math.Abs(float64(time.Now().Unix()-ts)) > 60*5 {
		return fmt.Errorf("request timestamp is too old or too far in the future")
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	// Restore the body for further reading
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes)))

	baseString := "v0:" + timestamp + ":" + string(bodyBytes)

	h := hmac.New(sha256.New, []byte(signingSecret))
	h.Write([]byte(baseString))
	computed := h.Sum(nil)
	computedSig := "v0=" + hex.EncodeToString(computed)

	if !hmac.Equal([]byte(computedSig), []byte(sig)) {
		return fmt.Errorf("invalid Slack signature")
	}
	return nil
}
