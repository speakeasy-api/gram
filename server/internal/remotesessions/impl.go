// Package remotesessions implements the management API services that surface
// remote_session_issuer / remote_session_client / remote_session resources —
// Gram-as-OAuth-Client configuration and the upstream sessions Gram is
// holding on a principal's behalf. A single Go package owns three Goa
// services' shared implementation, dependencies, and lifecycle.
package remotesessions

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	clientssrv "github.com/speakeasy-api/gram/server/gen/http/remote_session_clients/server"
	issuerssrv "github.com/speakeasy-api/gram/server/gen/http/remote_session_issuers/server"
	sessionssrv "github.com/speakeasy-api/gram/server/gen/http/remote_sessions/server"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	enc         *encryption.Client
	policy      *guardian.Policy
	auditLogger *audit.Logger
}

var (
	_ issuersgen.Service  = (*Service)(nil)
	_ issuersgen.Auther   = (*Service)(nil)
	_ clientsgen.Service  = (*Service)(nil)
	_ clientsgen.Auther   = (*Service)(nil)
	_ sessionsgen.Service = (*Service)(nil)
	_ sessionsgen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, enc *encryption.Client, policy *guardian.Policy, auditLogger *audit.Logger) *Service {
	logger = logger.With(attr.SlogComponent("remotesessions"))

	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotesessions"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessionManager, authzEngine),
		authz:       authzEngine,
		enc:         enc,
		policy:      policy,
		auditLogger: auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	mw := []func(goa.Endpoint) goa.Endpoint{
		middleware.MapErrors(),
		middleware.TraceMethods(service.tracer),
	}

	issuerEndpoints := issuersgen.NewEndpoints(service)
	for _, m := range mw {
		issuerEndpoints.Use(m)
	}
	issuerssrv.Mount(mux, issuerssrv.New(issuerEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

	clientEndpoints := clientsgen.NewEndpoints(service)
	for _, m := range mw {
		clientEndpoints.Use(m)
	}
	clientssrv.Mount(mux, clientssrv.New(clientEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

	sessionEndpoints := sessionsgen.NewEndpoints(service)
	for _, m := range mw {
		sessionEndpoints.Use(m)
	}
	sessionssrv.Mount(mux, sessionssrv.New(sessionEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
