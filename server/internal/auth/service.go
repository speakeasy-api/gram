package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/projects"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/auth"
	srv "github.com/speakeasy-api/gram/gen/http/auth/server"
)

// Service for gram dashboard authentication endpoints
type Service struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Sessions
	projects *projects.Service
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, sessions: sessions.NewSessionAuth(logger), projects: projects.NewService(logger, db)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) Callback(context.Context, *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	// TODO: Exchange sharedToken with speakeasy backend for user information OIDC. Token will either be a JWT userID or another short term redis backed token
	// TODO: Populate an auth session from that information, redirect back to gram
	// TODO: This will call GET api.speakeasy.com/v1/gram/info/{userID}
	return &gen.CallbackResult{}, nil
}

func (s *Service) SwitchScopes(ctx context.Context, payload *gen.SwitchScopesPayload) (res *gen.SwitchScopesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, err
	}

	if payload.OrganizationID != nil {
		orgFound := false
		for _, org := range userInfo.Organizations {
			if org.OrganizationID == *payload.OrganizationID {
				orgFound = true
				break
			}
		}
		if !orgFound {
			return nil, errors.New("organization not found")
		}
		authCtx.ActiveOrganizationID = *payload.OrganizationID
	}

	if err := s.sessions.UpdateSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, err
	}

	return &gen.SwitchScopesResult{
		SessionToken:  *authCtx.SessionID,
		SessionCookie: *authCtx.SessionID,
	}, nil
}

func (s *Service) Logout(ctx context.Context, payload *gen.LogoutPayload) (res *gen.LogoutResult, err error) {
	// Clears cookie and invalidates session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}
	if err := s.sessions.ClearSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, err
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}
func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, err
	}

	// Fully unpack the userInfo object
	organizations := make([]*gen.Organization, len(userInfo.Organizations))
	for i, org := range userInfo.Organizations {
		// TODO: Not the cleanest but a temporary measue while in POC phase.
		// This may actually be bettter executed from elsewhere
		projectRows, err := s.projects.GetProjectsOrSetuptDefaults(ctx, org.OrganizationID)
		if err != nil {
			return nil, err
		}
		var orgProjects []*gen.Project
		for _, project := range projectRows {
			orgProjects = append(orgProjects, &gen.Project{
				ProjectID:   project.ID.String(),
				ProjectName: project.Name,
				ProjectSlug: project.Slug,
			})
		}

		organizations[i] = &gen.Organization{
			OrganizationID:   org.OrganizationID,
			OrganizationName: org.OrganizationName,
			OrganizationSlug: org.OrganizationSlug,
			AccountType:      org.AccountType,
			Projects:         orgProjects,
		}
	}

	return &gen.InfoResult{
		SessionToken:         *authCtx.SessionID,
		SessionCookie:        *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               userInfo.UserID,
		UserEmail:            userInfo.Email,
		Organizations:        organizations,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}
