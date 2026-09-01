package identityapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/identity/server"
	gen "github.com/speakeasy-api/gram/server/gen/identity"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/identity"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Service exposes identity resolution: the mapping from any one identifier a
// dashboard surface holds to every identifier the same subject's activity is
// recorded under.
type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	auth     *auth.Auth
	authz    *authz.Engine
	resolver *identity.Resolver
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("identity"))

	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/identityapi"),
		logger:   logger,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		resolver: identity.NewResolver(logger, db),
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// Resolve maps an identity URN to the subject it names. Org read is the gate:
// the subject is an org member and the caller is one too. Note the response
// carries more than access.listMembers does — directory attributes, group
// memberships and the linked AI-account addresses — so raising this gate is
// the lever if those are ever judged admin-only.
func (s *Service) Resolve(ctx context.Context, payload *gen.ResolvePayload) (*gen.IdentityModel, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require org read: %w", err)
	}

	subjectURN, err := urn.ParseIdentity(payload.Urn)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid identity urn")
	}

	resolved, err := s.resolver.Resolve(ctx, authCtx.ActiveOrganizationID, subjectURN)
	switch {
	case errors.Is(err, identity.ErrAgentUnsupported):
		return nil, oops.E(oops.CodeNotImplemented, err, "agent identities are not supported yet")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve identity").LogError(ctx, s.logger)
	}

	return mv.BuildIdentityView(resolved), nil
}
