package auth

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
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
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, sessions: sessions.New()}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) AuthCallback(context.Context, *gen.AuthCallbackPayload) (res *gen.AuthCallbackResult, err error) {
	return &gen.AuthCallbackResult{}, nil
}

func (s *Service) AuthSwitchScopes(context.Context, *gen.AuthSwitchScopesPayload) (res *gen.AuthSwitchScopesResult, err error) {
	return &gen.AuthSwitchScopesResult{}, nil
}

func (s *Service) AuthLogout(context.Context) (res *gen.AuthLogoutResult, err error) {
	return &gen.AuthLogoutResult{GramSession: ""}, nil
}

func (s *Service) AuthInfo(context.Context, *gen.AuthInfoPayload) (res *gen.AuthInfoResult, err error) {
	return &gen.AuthInfoResult{}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}
