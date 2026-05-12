// Package remotesessions implements the management API services that surface
// remote_session_issuer / remote_session_client / remote_session resources —
// Gram-as-OAuth-Client configuration and the upstream sessions Gram is
// holding on a principal's behalf. Per the spike (§3.1, §6.2), three Goa
// services are authored under server/design/remotesession{issuers,clients}/
// and server/design/remotesessions/, but a single Go package owns their
// shared implementation, dependencies, and lifecycle.
//
// All method bodies are stubbed to oops.CodeNotImplemented; real logic lands
// in tickets #9-#11.
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
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// Service implements all three Goa services. The split into three design
// packages keeps the management-API surface logically grouped while a single
// Service struct lets handlers share dependencies.
type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
}

var (
	_ issuersgen.Service  = (*Service)(nil)
	_ issuersgen.Auther   = (*Service)(nil)
	_ clientsgen.Service  = (*Service)(nil)
	_ clientsgen.Auther   = (*Service)(nil)
	_ sessionsgen.Service = (*Service)(nil)
	_ sessionsgen.Auther  = (*Service)(nil)
)

// NewService constructs a Service ready to be Attached against each of the
// three remote_session* Goa services.
func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("remotesessions"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotesessions"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessionManager, authzEngine),
		authz:  authzEngine,
	}
}

// Attach wires every Goa service this package backs onto the shared mux:
// remoteSessionIssuers, remoteSessionClients, remoteSessions.
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

// APIKeyAuth implements goa Auther for every Goa service this package backs;
// each generated package treats it as the same method.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
