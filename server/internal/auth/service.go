package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/projects"
	"github.com/speakeasy-api/gram/internal/sessions"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/auth"
	srv "github.com/speakeasy-api/gram/gen/http/auth/server"
)

type Service struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Sessions
	projects *projects.Service
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, sessions: sessions.New(), projects: projects.NewService(logger, db)}
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
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, session.UserID)
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
		session.ActiveOrganizationID = *payload.OrganizationID
	}

	if err := s.sessions.UpdateSession(ctx, *session); err != nil {
		return nil, err
	}

	return &gen.SwitchScopesResult{
		SessionToken:  session.ID,
		SessionCookie: session.ID,
	}, nil
}

func (s *Service) Logout(ctx context.Context, payload *gen.LogoutPayload) (res *gen.LogoutResult, err error) {
	// Clears cookie and invalidates session
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}
	if err := s.sessions.ClearSession(ctx, *session); err != nil {
		return nil, err
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}
func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, session.UserID)
	if err != nil {
		return nil, err
	}

	// Fully unpack the userInfo object
	organizations := make([]*gen.Organization, len(userInfo.Organizations))
	for i, org := range userInfo.Organizations {
		projectRows, err := s.projects.GetProjectsOrCreateDefault(ctx, org.OrganizationID)
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
		SessionToken:         session.ID,
		SessionCookie:        session.ID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		UserID:               userInfo.UserID,
		UserEmail:            userInfo.Email,
		Organizations:        organizations,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}
