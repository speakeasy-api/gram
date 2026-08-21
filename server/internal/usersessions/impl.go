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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	clientssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_clients/server"
	consentssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_consents/server"
	issuerssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_issuers/server"
	cimdclientssrv "github.com/speakeasy-api/gram/server/gen/http/user_session_issuers_cimd_clients/server"
	sessionssrv "github.com/speakeasy-api/gram/server/gen/http/user_sessions/server"
	clientsgen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	consentsgen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	cimdclientsgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
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
	chatSessions TokenRevoker
	audit        *audit.Logger
	// signer mints the user-session JWT returned by mintUserSession. Same
	// signer the /mcp/{slug}/token handler uses, so the resulting JWTs
	// validate through the runtime gateway by the existing user-session
	// path with no special casing.
	signer *Signer
	// serverURL is the public base URL used to stamp the JWT issuer claim
	// on mintUserSession output. Matches the issuer URL /token would emit.
	serverURL string
	// cimdResolver backs the VerifyURL handler, which probes an
	// operator-supplied CIMD document URL on demand. Guardian-backed, so the
	// probe cannot be turned into an SSRF primitive against internal
	// addresses. Its attempts land on the same cimd.fetch.* instruments as
	// authorize-time resolutions — a probe genuinely is a resolve attempt —
	// but note this is now an on-demand button rather than the one-shot
	// create-time probe those instruments were sized for, so the origin
	// attribute is tenant-driven at whatever rate the caller chooses.
	cimdResolver *cimd.Resolver

	// revoker cascades a user-session revoke into the subject's upstream
	// grants. A revoked session whose provider tokens keep working is only
	// half a revocation, so the two are driven together.
	revoker *remotesessions.UpstreamRevoker

	// verifyLimiter bounds the VerifyURL handler, keyed per project. That
	// endpoint is the only one that makes Gram issue an outbound request to
	// a caller-chosen host, and the resolver's guardian client carries no
	// resilience layer of its own (WithResilience is opt-in and unused
	// here), so without this a project:write holder could loop it into a
	// scanner wearing Gram's egress IPs, or pin goroutines against a
	// slowloris host for fetchTimeout apiece.
	verifyLimiter *ratelimit.Limiter
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
// userSessions and userSessionClients revoke handlers to push revoked jtis
// into the revocation cache; it is held as a TokenRevoker so tests can
// substitute a failing revoker.
// signer + serverURL drive mintUserSession; pass an empty serverURL to
// disable that handler (it will 503 on call — used in tests that don't
// need the surface).
func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, chatSessionsManager TokenRevoker, authzEngine *authz.Engine, auditLogger *audit.Logger, guardianPolicy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, enc *encryption.Client, signer *Signer, serverURL string, verifyStore ratelimit.Store) *Service {
	logger = logger.With(attr.SlogComponent("usersessions"))

	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/usersessions"),
		logger:       logger,
		db:           db,
		auth:         auth.New(logger, db, sessionManager, authzEngine),
		authz:        authzEngine,
		chatSessions: chatSessionsManager,
		audit:        auditLogger,
		signer:       signer,
		serverURL:    serverURL,
		cimdResolver: cimd.NewResolver(guardianPolicy, meterProvider, logger),
		revoker:      remotesessions.NewUpstreamRevoker(logger, tracerProvider, meterProvider, db, enc, guardianPolicy, tunnels),
		verifyLimiter: ratelimit.New(verifyStore, "cimd-url-verify",
			ratelimit.PerMinute(verifyRatePerMin).WithBurst(verifyRateBurst),
			ratelimit.WithMetrics(meterProvider)),
	}
}

// Attach wires every Goa service this package backs onto the shared mux:
// userSessionIssuers, userSessionIssuersCimdClients, userSessionClients,
// userSessionConsents, userSessions.
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

	cimdClientEndpoints := cimdclientsgen.NewEndpoints(service)
	for _, m := range mw {
		cimdClientEndpoints.Use(m)
	}
	cimdclientssrv.Mount(mux, cimdclientssrv.New(cimdClientEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

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

	// Tombstone for the retired OAuth proxy token endpoint: answers
	// invalid_grant so a client holding a proxy refresh token re-authorizes
	// against the user_session_issuer these endpoints serve.
	//
	// Removable once no client still exchanges a pre-migration proxy refresh
	// token here. Two sessions still rely on the signal as of 2026-08-07 and are
	// expected to be resolved within the week; revisit and drop this after
	// ~2026-08-14.
	attachRetiredProxy(mux, service)
}

// APIKeyAuth implements goa Auther for every Goa service this package backs;
// each generated package treats it as the same method.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
