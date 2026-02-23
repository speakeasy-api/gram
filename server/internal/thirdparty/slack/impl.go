package slack

import (
	"bytes"
	"context"
	"crypto/hmac"
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

	"github.com/google/uuid"
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
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
	toolset_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Configurations struct {
	GramServerURL      string
	SignInRedirectURL  string
	SlackAppInstallURL string
	SlackSigningSecret string
}

// Service for gram dashboard authentication endpoints
// Service for gram dashboard authentication endpoints
type Service struct {
	tracer              trace.Tracer
	logger              *slog.Logger
	db                  *pgxpool.Pool
	sessions            *sessions.Manager
	enc                 *encryption.Client
	repo                *repo.Queries
	auth                *auth.Auth
	toolset             *toolset_repo.Queries
	cfg                 *Configurations
	client              *slack_client.SlackClient
	temporal            *temporal.Environment
	watchedThreadsCache cache.TypedCacheObject[types.AppMentionedThreads]
}

func SlackInstallURL(env string) string {
	switch env {
	case "prod":
		return "https://slack.com/oauth/v2/authorize?client_id=2519256324743.8891175217264&scope=app_mentions:read,channels:history,channels:join,channels:manage,channels:read,channels:write.invites,chat:write,chat:write.customize,chat:write.public,groups:history,groups:read,groups:write,groups:write.invites,im:history,im:read,im:write,mpim:history,mpim:read,mpim:write,reminders:read,reminders:write,usergroups:read,usergroups:write,users.profile:read,users:read,users:read.email,users:write,reactions:read,reactions:write,groups:write.topic,channels:write.topic&user_scope="
	default:
		return "https://slack.com/oauth/v2/authorize?client_id=2519256324743.8884952287878&scope=app_mentions:read,channels:history,channels:join,channels:manage,channels:read,channels:write.invites,chat:write,chat:write.customize,chat:write.public,groups:history,groups:read,groups:write,groups:write.invites,im:history,im:read,im:write,mpim:history,mpim:read,mpim:write,reminders:read,reminders:write,usergroups:read,usergroups:write,users.profile:read,users:read,users:read.email,users:write,reactions:read,reactions:write,groups:write.topic,channels:write.topic&user_scope="
	}
}

func SlackClientID(env string) string {
	switch env {
	case "prod":
		return "2519256324743.8891175217264"
	default:
		return "2519256324743.8884952287878"
	}
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Client, redisClient *redis.Client, client *slack_client.SlackClient, temporal *temporal.Environment, cfg Configurations) *Service {
	logger = logger.With(attr.SlogComponent("slack"))

	return &Service{
		tracer:              otel.Tracer("github.com/speakeasy-api/gram/server/internal/auth"),
		logger:              logger,
		db:                  db,
		sessions:            sessions,
		enc:                 enc,
		repo:                repo.New(db),
		auth:                auth.New(logger, db, sessions),
		toolset:             toolset_repo.New(db),
		cfg:                 &cfg,
		client:              client,
		temporal:            temporal,
		watchedThreadsCache: cache.NewTypedObjectCache[types.AppMentionedThreads](logger.With(attr.SlogCacheNamespace("watched_threads")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
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
	// payloads may end up being polymorphic defining this outside of goa
	o11y.AttachHandler(mux, "POST", "/rpc/slack.events", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.SlackEventHandler).ServeHTTP(w, r)
	})
}

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	returnURL := s.cfg.SignInRedirectURL
	redirectWithError := func(returnURL string, err error) (*gen.CallbackResult, error) {
		s.logger.ErrorContext(ctx, "slack auth error", attr.SlogError(err))
		return &gen.CallbackResult{
			Location: fmt.Sprintf("%s?slack_error=%s", returnURL, err.Error()),
		}, nil
	}
	stateValues, err := url.ParseQuery(payload.State)
	if err != nil {
		return redirectWithError(returnURL, err)
	}

	//TODO: Check organization and project relationship with exported utility
	projectID := stateValues.Get("project_id")
	organizationID := stateValues.Get("organization_id")
	returnURL = stateValues.Get("return_url")
	if returnURL == "" {
		returnURL = s.cfg.SignInRedirectURL
	}

	initialRedirectURI := fmt.Sprintf("%s/rpc/slack.callback", s.cfg.GramServerURL)

	response, err := s.client.OAuthV2Access(ctx, payload.Code, initialRedirectURI)
	if err != nil {
		return redirectWithError(returnURL, err)
	}

	toolsets, err := s.toolset.ListToolsetsByProject(ctx, uuid.MustParse(projectID))
	if err != nil {
		return redirectWithError(returnURL, errors.New("invalid project"))
	}

	defaultToolsetSlug := ""
	if len(toolsets) > 0 {
		defaultToolsetSlug = toolsets[0].Slug
	}

	encrypedSlackToken, err := s.enc.Encrypt([]byte(response.AccessToken))
	if err != nil {
		return redirectWithError(returnURL, err)
	}

	_, err = s.repo.CreateSlackAppConnection(ctx, repo.CreateSlackAppConnectionParams{
		OrganizationID:     organizationID,
		ProjectID:          uuid.MustParse(projectID),
		SlackTeamID:        response.Team.ID,
		SlackTeamName:      response.Team.Name,
		AccessToken:        encrypedSlackToken,
		DefaultToolsetSlug: conv.ToPGTextEmpty(defaultToolsetSlug),
	})
	if err != nil {
		return redirectWithError(returnURL, errors.New("this slack workspace is already linked to a gram project"))
	}

	return &gen.CallbackResult{
		Location: returnURL,
	}, nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	redirectURI := fmt.Sprintf("%s/rpc/slack.callback", s.cfg.GramServerURL)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.AccountType != "enterprise" {
		return nil, oops.E(oops.CodeUnauthorized, fmt.Errorf("only available for enterprise accounts"), "only available for enterprise accounts").Log(ctx, s.logger)
	}

	state := url.Values{}
	state.Set("project_id", authCtx.ProjectID.String())
	state.Set("organization_id", authCtx.ActiveOrganizationID)
	if payload.ReturnURL != nil {
		state.Set("return_url", *payload.ReturnURL)
	}

	installURL, err := url.Parse(s.cfg.SlackAppInstallURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to delete Slack app connection").Log(ctx, s.logger)
	}

	query := installURL.Query()
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state.Encode())
	installURL.RawQuery = query.Encode()

	return &gen.LoginResult{
		Location: installURL.String(),
	}, nil
}

func (s *Service) GetSlackConnection(ctx context.Context, payload *gen.GetSlackConnectionPayload) (res *gen.GetSlackConnectionResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	connection, err := s.repo.GetSlackAppConnection(ctx, repo.GetSlackAppConnectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "failed to get Slack app connection").Log(ctx, s.logger)
	}

	return &gen.GetSlackConnectionResult{
		SlackTeamName:      connection.SlackTeamName,
		SlackTeamID:        connection.SlackTeamID,
		DefaultToolsetSlug: connection.DefaultToolsetSlug.String,
		CreatedAt:          connection.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:          connection.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) DeleteSlackConnection(ctx context.Context, payload *gen.DeleteSlackConnectionPayload) (err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	err = s.repo.DeleteSlackAppConnection(ctx, repo.DeleteSlackAppConnectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete Slack app connection").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) UpdateSlackConnection(ctx context.Context, payload *gen.UpdateSlackConnectionPayload) (res *gen.GetSlackConnectionResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	sanitizedSlug := conv.ToLower(payload.DefaultToolsetSlug)

	// Ensure the toolset exists for the given slug and project
	if _, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(sanitizedSlug), nil); err != nil {
		return nil, err
	}

	result, err := s.repo.UpdateSlackAppConnection(ctx, repo.UpdateSlackAppConnectionParams{
		DefaultToolsetSlug: conv.ToPGText(sanitizedSlug),
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update Slack app connection").Log(ctx, s.logger)
	}

	return &gen.GetSlackConnectionResult{
		SlackTeamName:      result.SlackTeamName,
		SlackTeamID:        result.SlackTeamID,
		DefaultToolsetSlug: result.DefaultToolsetSlug.String,
		CreatedAt:          result.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:          result.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) SlackEventHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	if err := validateSlackEvent(r, s.cfg.SlackSigningSecret); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "request payload failed validation").Log(ctx, s.logger)
	}

	var event types.SlackEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid request payload").Log(ctx, s.logger)
	}

	// Respond to Slack's URL verification challenge
	if event.Type == "url_verification" && event.Challenge != "" {
		w.Header().Set("Content-Type", "text/plain")
		_, err := w.Write([]byte(event.Challenge))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to write slack response").Log(ctx, s.logger)
		}
		return nil
	}

	_, err := s.repo.GetSlackAppConnectionByTeamID(ctx, event.TeamID)
	if err != nil {
		s.logger.InfoContext(ctx, "skipping an event with no slack app connection", attr.SlogSlackTeamID(event.TeamID))
		w.WriteHeader(http.StatusOK)
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
			s.logger.ErrorContext(ctx, "failed to store user info in cache", attr.SlogError(err))
		}
		processEvent = true

	case "message":
		if event.Event.Text == "" {
			break
		}
		// Ignore messages from the bot itself
		if event.Event.User == event.Authorizations[0].UserID {
			break
		}

		if event.Event.ChannelType == "im" {
			processEvent = true
			break
		}

		// This will be processed by app_mention, slack sends duplicate event
		if strings.HasPrefix(event.Event.Text, fmt.Sprintf("<@%s>", event.Authorizations[0].UserID)) {
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
		if _, err := background.ExecuteProcessSlackEventWorkflow(ctx, s.temporal, background.ProcessSlackWorkflowParams{
			Event: event,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "error kicking off slack event workflow").Log(ctx, s.logger)
		}
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

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
