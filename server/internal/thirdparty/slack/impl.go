package slack

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/slack/server"
	gen "github.com/speakeasy-api/gram/gen/slack"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/repo"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

type Configurations struct {
	GramServerURL        string
	SignInRedirectURL    string
	SlackAppInstallURL   string
	SlackAppClientID     string
	SlackAppClientSecret string
	SlackSigningSecret   string
}

// Service for gram dashboard authentication endpoints
// Service for gram dashboard authentication endpoints
type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Manager
	enc      *encryption.Encryption
	repo     *repo.Queries
	auth     *auth.Auth
	toolset  *toolsets.Toolsets
	cfg      *Configurations
	client   *SlackClient
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

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption, cfg Configurations) *Service {
	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/internal/auth"),
		logger:   logger,
		db:       db,
		sessions: sessions,
		enc:      enc,
		repo:     repo.New(db),
		auth:     auth.New(logger, db, sessions),
		toolset:  toolsets.NewToolsets(db),
		cfg:      &cfg,
		client:   NewSlackClient(cfg.SlackAppClientID, cfg.SlackAppClientSecret),
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

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	redirectWithError := func(err error) (*gen.CallbackResult, error) {
		s.logger.ErrorContext(ctx, "slack auth error", slog.String("error", err.Error()))
		return &gen.CallbackResult{
			Location: fmt.Sprintf("%s?slack_error=%s", s.cfg.SignInRedirectURL, err.Error()),
		}, nil
	}
	stateValues, err := url.ParseQuery(payload.State)
	if err != nil {
		return redirectWithError(err)
	}

	//TODO: Check organization and project relationship with exported utility
	projectID := stateValues.Get("project_id")
	organizationID := stateValues.Get("organization_id")

	initialRedirectURI := fmt.Sprintf("%s/rpc/slack.callback", s.cfg.GramServerURL)

	response, err := s.client.OAuthV2Access(ctx, payload.Code, initialRedirectURI)
	if err != nil {
		return redirectWithError(err)
	}

	encrypedSlackToken, err := s.enc.Encrypt([]byte(response.AccessToken))
	if err != nil {
		return redirectWithError(err)
	}

	_, err = s.repo.CreateSlackAppConnection(ctx, repo.CreateSlackAppConnectionParams{
		OrganizationID:     organizationID,
		ProjectID:          uuid.MustParse(projectID),
		SlackTeamID:        response.Team.ID,
		SlackTeamName:      response.Team.Name,
		AccessToken:        encrypedSlackToken,
		DefaultToolsetSlug: conv.ToPGTextEmpty(""),
	})
	if err != nil {
		return redirectWithError(err)
	}

	return &gen.CallbackResult{
		Location: s.cfg.SignInRedirectURL,
	}, nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	redirectURI := fmt.Sprintf("%s/rpc/slack.callback", s.cfg.GramServerURL)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	state := url.Values{}
	state.Set("project_id", authCtx.ProjectID.String())
	state.Set("organization_id", authCtx.ActiveOrganizationID)

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
	if _, err := s.toolset.LoadToolsetDetails(ctx, sanitizedSlug, *authCtx.ProjectID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load toolset details").Log(ctx, s.logger)
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
