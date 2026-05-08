// Package usersessions implements the management API services that surface
// user_session_issuer / user_session_client / user_session_consent /
// user_session resources. The four Goa services are authored under
// server/design/usersession{issuers,clients,consents}/ and
// server/design/usersessions/; a single Go package owns their shared
// implementation, dependencies, and lifecycle.
package usersessions

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	clientssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_clients/server"
	consentssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_consents/server"
	issuerssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_issuers/server"
	sessionssrv "github.com/speakeasy-api/gram/server/gen/http/user_sessions/server"
	clientsgen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	consentsgen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// Service implements all four Goa services. The split into four design
// packages keeps the management-API surface logically grouped while a single
// Service struct lets handlers share dependencies.
type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	auth         *auth.Auth
	authz        *authz.Engine
	chatSessions *chatsessions.Manager
}

var (
	_ issuersgen.Service  = (*Service)(nil)
	_ issuersgen.Auther   = (*Service)(nil)
	_ clientsgen.Service  = (*Service)(nil)
	_ clientsgen.Auther   = (*Service)(nil)
	_ consentsgen.Service = (*Service)(nil)
	_ consentsgen.Auther  = (*Service)(nil)
	_ sessionsgen.Service = (*Service)(nil)
	_ sessionsgen.Auther  = (*Service)(nil)
)

// NewService constructs a Service ready to be Attached against each of the
// four user_session* Goa services. chatSessionsManager is used by the
// userSessions revoke handler to push revoked jtis into the revocation cache.
func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, chatSessionsManager *chatsessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("usersessions"))

	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/usersessions"),
		logger:       logger,
		db:           db,
		auth:         auth.New(logger, db, sessionManager, authzEngine),
		authz:        authzEngine,
		chatSessions: chatSessionsManager,
	}
}

// Attach wires every Goa service this package backs onto the shared mux:
// userSessionIssuers, userSessionClients, userSessionConsents, userSessions.
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

	consentEndpoints := consentsgen.NewEndpoints(service)
	for _, m := range mw {
		consentEndpoints.Use(m)
	}
	consentssrv.Mount(mux, consentssrv.New(consentEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

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
