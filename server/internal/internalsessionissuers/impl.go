package internalsessionissuers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/http/internal_session_issuers/server"
	gensvc "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	enableRatePerMin = 10
	enableRateBurst  = 10
)

// Manager provisions and maintains issuers; the identity provider connections
// provisioner implements it with its KMS machinery.
type Manager interface {
	EnsureEMAIssuer(ctx context.Context, organizationID string) (uuid.UUID, error)
	DisableEMAIssuer(ctx context.Context, organizationID string) error
	RotateEMAIssuer(ctx context.Context, organizationID, activeKid string) error
	RotateEMAIssuerCompromised(ctx context.Context, organizationID string) error
	SweepEMAIssuerKeys(ctx context.Context, organizationID string) error
}

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	metrics     *metrics
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	gate        *Gate
	manager     Manager
	mintLimiter *ratelimit.Limiter
}

var (
	_ gensvc.Service = (*Service)(nil)
	_ gensvc.Auther  = (*Service)(nil)
)

// NewService builds the management API. manager is nil where the deployment
// can't provision issuers; reads still work.
func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, gate *Gate, manager Manager, mintStore ratelimit.Store) *Service {
	logger = logger.With(attr.SlogComponent("internalsessionissuers"))
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/internalsessionissuers"),
		logger:  logger,
		metrics: newMetrics(logger, meterProvider),
		db:      db,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		gate:    gate,
		manager: manager,
		mintLimiter: ratelimit.New(mintStore, "internal-session-issuer-mint",
			ratelimit.PerMinute(enableRatePerMin).WithBurst(enableRateBurst),
			ratelimit.WithMetrics(meterProvider)),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gensvc.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	gen.Mount(mux, gen.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// access checks RBAC on the active organization and reads its issuer status.
func (s *Service) access(ctx context.Context, scope authz.Scope) (string, *slog.Logger, Status, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return "", s.logger, Status{}, oops.C(oops.CodeUnauthorized)
	}
	organizationID := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}); err != nil {
		return "", logger, Status{}, err
	}
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return "", logger, Status{}, oops.E(oops.CodeUnavailable, err, "issuer availability could not be determined").LogError(ctx, logger)
	}
	return organizationID, logger, status, nil
}

func (s *Service) Get(ctx context.Context, _ *gensvc.GetPayload) (*gensvc.InternalSessionIssuer, error) {
	organizationID, logger, status, err := s.access(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	s.sweep(ctx, logger, organizationID, status)
	return s.view(ctx, logger, organizationID)
}

func (s *Service) SetEnabled(ctx context.Context, payload *gensvc.SetEnabledPayload) (*gensvc.InternalSessionIssuer, error) {
	organizationID, logger, status, err := s.access(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "issuers are not configured on this deployment").LogError(ctx, logger)
	}
	if !payload.Enabled {
		// Turning signing off never depends on the rollout or entitlement.
		err := s.manager.DisableEMAIssuer(ctx, organizationID)
		s.metrics.record(ctx, opDisable, err)
		if err != nil {
			return nil, s.operationError(ctx, logger, err, "could not disable the issuer")
		}
		return s.view(ctx, logger, organizationID)
	}
	switch {
	case !status.RolledOut:
		return nil, oops.E(oops.CodeForbidden, nil, "issuers are not available for this organization yet")
	case !status.Entitled:
		return nil, oops.E(oops.CodeForbidden, nil, "issuers require single sign-on for this organization")
	}
	if status.Issuer == nil {
		if err := s.allowMint(ctx, logger, organizationID); err != nil {
			return nil, err
		}
	}
	_, err = s.manager.EnsureEMAIssuer(ctx, organizationID)
	s.metrics.record(ctx, opEnable, err)
	if err != nil {
		return nil, s.operationError(ctx, logger, err, "could not enable the issuer")
	}
	return s.view(ctx, logger, organizationID)
}

func (s *Service) Rotate(ctx context.Context, payload *gensvc.RotatePayload) (*gensvc.InternalSessionIssuer, error) {
	organizationID, logger, status, err := s.access(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "issuers are not configured on this deployment").LogError(ctx, logger)
	}
	if status.Issuer == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "the issuer is not enabled")
	}
	if payload.Compromised {
		// The compromise path is never rate limited or gated on entitlement.
		err := s.manager.RotateEMAIssuerCompromised(ctx, organizationID)
		s.metrics.record(ctx, opRotateCompromised, err)
		if err != nil {
			return nil, s.operationError(ctx, logger, err, "could not replace the issuer's keys")
		}
		return s.view(ctx, logger, organizationID)
	}
	if payload.ActiveKid == nil || *payload.ActiveKid == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "active_kid is required unless compromised is set")
	}
	if status.State != StateEnabled && status.State != StateSigningPaused {
		return nil, oops.E(oops.CodeForbidden, nil, "routine rotation needs an enabled issuer")
	}
	if err := s.allowMint(ctx, logger, organizationID); err != nil {
		return nil, err
	}
	err = s.manager.RotateEMAIssuer(ctx, organizationID, *payload.ActiveKid)
	s.metrics.record(ctx, opRotate, err)
	if err != nil && !isPending(err) {
		return nil, s.operationError(ctx, logger, err, "could not rotate the issuer's key")
	}
	return s.view(ctx, logger, organizationID)
}

func isPending(err error) bool {
	_, ok := errors.AsType[*RotationPendingError](err)
	return ok
}

func (s *Service) allowMint(ctx context.Context, logger *slog.Logger, organizationID string) error {
	switch res, limitErr := s.mintLimiter.Allow(ctx, organizationID); {
	case limitErr != nil:
		logger.WarnContext(ctx, "issuer rate limiter unavailable, allowing", attr.SlogError(limitErr))
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "issuer key rate limit exceeded, try again shortly")
	}
	return nil
}

// sweep retires expired keys opportunistically; the scheduled sweep is the backstop.
func (s *Service) sweep(ctx context.Context, logger *slog.Logger, organizationID string, status Status) {
	if s.manager == nil || status.Issuer == nil {
		return
	}
	err := s.manager.SweepEMAIssuerKeys(ctx, organizationID)
	s.metrics.record(ctx, opSweep, err)
	if err != nil {
		logger.WarnContext(ctx, "failed to sweep expired issuer keys", attr.SlogError(err))
	}
}

func (s *Service) operationError(ctx context.Context, logger *slog.Logger, err error, msg string) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return oops.E(oops.CodeNotFound, err, "the issuer is not enabled")
	case errors.Is(err, ErrUnknownKid):
		return oops.E(oops.CodeConflict, err, "active_kid was never published by this issuer; read the issuer again")
	case errors.Is(err, ErrRotationConflict):
		return oops.E(oops.CodeConflict, err, "the active key changed; read the issuer again")
	case errors.Is(err, ErrMisconfigured):
		logger.ErrorContext(ctx, "platform issuer signing is misconfigured", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "%s: issuer signing is misconfigured on this deployment; contact support", msg)
	default:
		return oops.E(oops.CodeUnavailable, err, "%s; retry shortly", msg).LogError(ctx, logger)
	}
}

// view is the public state of an organization's issuer: never signing material.
func (s *Service) view(ctx context.Context, logger *slog.Logger, organizationID string) (*gensvc.InternalSessionIssuer, error) {
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "issuer availability could not be determined").LogError(ctx, logger)
	}
	view := &gensvc.InternalSessionIssuer{State: string(status.State), Issuer: nil, JwksURI: nil, Keys: []*gensvc.InternalSessionIssuerKey{}, RotationReadyAt: nil}
	if status.Issuer == nil {
		return view, nil
	}
	issuer, published, err := PublicKeys(ctx, s.db, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the issuer's keys").LogError(ctx, logger)
	}
	jwksURI, err := JWKSURI(issuer)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "stored issuer is not a URL").LogError(ctx, logger)
	}
	keys, err := repo.New(s.db).ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: status.Issuer.JsonWebKeySetID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the issuer's keys").LogError(ctx, logger)
	}
	live := make(map[string]bool, len(published.Keys))
	for _, key := range published.Keys {
		live[key.KeyID] = true
	}
	for _, key := range keys {
		if !live[key.Kid] {
			continue
		}
		view.Keys = append(view.Keys, &gensvc.InternalSessionIssuerKey{Kid: key.Kid, State: key.State})
		if key.State == "pending" && key.UpdatedAt.Valid && key.UpdatedAt.InfinityModifier == pgtype.Finite {
			view.RotationReadyAt = new(key.UpdatedAt.Time.Add(ActivationDelay).UTC().Format(time.RFC3339))
		}
	}
	view.Issuer = &issuer
	view.JwksURI = &jwksURI
	return view, nil
}
