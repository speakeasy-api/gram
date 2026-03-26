package teams

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/teams/server"
	gen "github.com/speakeasy-api/gram/server/gen/teams"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	sessions *sessions.Manager
	auth     *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func NewService(logger *slog.Logger, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("teams"))

	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/server/internal/teams"),
		logger:   logger,
		sessions: sessions,
		auth:     auth.New(logger, nil, sessions),
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

func (s *Service) ListMembers(ctx context.Context, payload *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	// TODO: implement using WorkOS API
	return nil, oops.E(oops.CodeUnexpected, nil, "not implemented")
}

func (s *Service) InviteMember(ctx context.Context, payload *gen.InviteMemberPayload) (*gen.InviteMemberResult, error) {
	// TODO: implement using WorkOS API
	return nil, oops.E(oops.CodeUnexpected, nil, "not implemented")
}

func (s *Service) ListInvites(ctx context.Context, payload *gen.ListInvitesPayload) (*gen.ListInvitesResult, error) {
	// TODO: implement using WorkOS API
	return nil, oops.E(oops.CodeUnexpected, nil, "not implemented")
}

func (s *Service) CancelInvite(ctx context.Context, payload *gen.CancelInvitePayload) error {
	// TODO: implement using WorkOS API
	return oops.E(oops.CodeUnexpected, nil, "not implemented")
}

func (s *Service) ResendInvite(ctx context.Context, payload *gen.ResendInvitePayload) (*gen.ResendInviteResult, error) {
	// TODO: implement using WorkOS API
	return nil, oops.E(oops.CodeUnexpected, nil, "not implemented")
}

func (s *Service) GetInviteInfo(ctx context.Context, payload *gen.GetInviteInfoPayload) (*gen.InviteInfoResult, error) {
	// TODO: implement using WorkOS API
	return nil, oops.E(oops.CodeUnexpected, nil, "not implemented")
}

func (s *Service) RemoveMember(ctx context.Context, payload *gen.RemoveMemberPayload) error {
	// TODO: implement using WorkOS API
	return oops.E(oops.CodeUnexpected, nil, "not implemented")
}
