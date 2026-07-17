package agent

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	srv "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	serverURL string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// NewService constructs the agent service.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	serverURL string,
) *Service {
	logger = logger.With(attr.SlogComponent("agent"))
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/agent"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		serverURL: serverURL,
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

// GetPlugins returns every plugin in the published projects of the caller's
// org. Per-principal assignment scoping is intentionally disabled for now (see
// DNO-239): every org member receives every published-project plugin. The
// resolved email will drive principal resolution again (user:/role: lookups)
// once RBAC-backed assignment management ships.
//
// The enrolled user is resolved differently per credential type, because who
// the key belongs to differs:
//   - Org install key (`agent` scope): the key owner is whoever minted the org
//     token in the dashboard (an admin), NOT the enrolled developer. Attribution
//     comes from the vouched `email` param the MDM profile supplies — required
//     here. This is the zero-touch MDM path where the developer never signs in.
//   - Per-user key (`agent_user` only): the key owner IS the enrolled developer
//     (minted by token-exchange or manual enrollment), so attribution is the key
//     owner and the vouched param is ignored.
func (s *Service) GetPlugins(ctx context.Context, payload *gen.GetPluginsPayload) (*gen.GetPluginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// An org install key carries the `agent` scope; a per-user key carries only
	// `agent_user` (agent implies agent_user, never the reverse — see
	// auth.effectiveScopes), so the presence of `agent` distinguishes them.
	isInstallKey := slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String())

	var enrolledEmail string
	if isInstallKey {
		// Org key: the owner is not the developer, so we must be vouched an email.
		if payload.Email == nil || *payload.Email == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "email is required when authenticating with an org-scoped agent install key")
		}
		enrolledEmail = conv.NormalizeEmail(*payload.Email)
	} else if authCtx.Email != nil {
		// Per-user key: the owner is the enrolled developer.
		enrolledEmail = conv.NormalizeEmail(*authCtx.Email)
	}
	if enrolledEmail != "" {
		if _, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + enrolledEmail); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
		}
	}

	rows, err := s.repo.GetAgentPluginSet(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin set").LogError(ctx, s.logger)
	}

	base := strings.TrimRight(s.serverURL, "/")
	marketplaceURL := func(token string) string {
		return base + marketplace.RoutePrefix + token + ".git"
	}

	return mv.BuildAgentPluginsView(rows, marketplaceURL), nil
}
