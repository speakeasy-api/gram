package internalsessionissuers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/http/internal_session_issuers/server"
	gensvc "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
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

// Manager provisions and maintains issuers with KMS; the identity provider
// connections provisioner implements it. Disabling and containing a
// compromise never go through it.
type Manager interface {
	EnsureEMAIssuer(ctx context.Context, organizationID string) (uuid.UUID, error)
	RotateEMAIssuer(ctx context.Context, organizationID, activeKid string) error
	ProvisionEMAIssuerKey(ctx context.Context, organizationID string) error
	CleanupEMAIssuerKMS(ctx context.Context, organizationID string) (cleaned, pending int, err error)
}

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	metrics     *metrics
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	gate        *Gate
	lifecycle   *Lifecycle
	manager     Manager
	tokenPurger EMAIssuerTokenPurger
	mintLimiter *ratelimit.Limiter
}

var (
	_ gensvc.Service = (*Service)(nil)
	_ gensvc.Auther  = (*Service)(nil)
)

// NewService builds the management API. manager is nil where the deployment
// can't provision issuers; reads, disabling and revocation still work.
func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger, gate *Gate, manager Manager, mintStore ratelimit.Store) *Service {
	logger = logger.With(attr.SlogComponent("internalsessionissuers"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/internalsessionissuers"),
		logger:      logger,
		metrics:     newMetrics(logger, meterProvider),
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		gate:        gate,
		lifecycle:   NewLifecycle(db, auditLogger),
		manager:     manager,
		tokenPurger: nil,
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

// authorize checks RBAC on the active organization; a mutation additionally
// refuses support and impersonated sessions and non-principal API keys. It
// reads no flags or domains, so containment works whatever their providers do.
func (s *Service) authorize(ctx context.Context, scope authz.Scope, mutation bool) (string, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return "", s.logger, oops.C(oops.CodeUnauthorized)
	}
	organizationID := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}); err != nil {
		return "", logger, err
	}
	if mutation {
		if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
			return "", logger, oops.E(oops.CodeForbidden, nil, "the issuer cannot be changed with an API key").LogError(ctx, logger)
		}
		if contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
			return "", logger, oops.E(oops.CodeForbidden, nil, "the issuer cannot be changed from a support or impersonated session").LogError(ctx, logger)
		}
	}
	return organizationID, logger, nil
}

// access authorizes and reads the issuer status for paths that depend on it.
func (s *Service) access(ctx context.Context, scope authz.Scope, mutation bool) (string, *slog.Logger, Status, error) {
	organizationID, logger, err := s.authorize(ctx, scope, mutation)
	if err != nil {
		return "", logger, Status{}, err
	}
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return "", logger, Status{}, oops.E(oops.CodeUnavailable, err, "issuer availability could not be determined").LogError(ctx, logger)
	}
	return organizationID, logger, status, nil
}

func (s *Service) Get(ctx context.Context, _ *gensvc.GetPayload) (*gensvc.InternalSessionIssuer, error) {
	organizationID, logger, err := s.authorize(ctx, authz.ScopeOrgRead, false)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, logger, organizationID)
}

// TODO(AIM-551): expose allowed sign-in methods and Microsoft tenants once their columns reach main.
func (s *Service) SetEnabled(ctx context.Context, payload *gensvc.SetEnabledPayload) (*gensvc.InternalSessionIssuer, error) {
	if !payload.Enabled {
		return s.disable(ctx)
	}
	organizationID, logger, status, err := s.access(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "issuers are not configured on this deployment").LogError(ctx, logger)
	}
	switch {
	case !status.RolledOut:
		return nil, oops.E(oops.CodeForbidden, nil, "issuers are not available for this organization yet")
	case !status.HasVerifiedDomain:
		return nil, oops.E(oops.CodeForbidden, nil, "issuers require a verified domain for this organization")
	}
	if (status.State == StateSetupRequired || status.State == StateDisabled) && !s.gate.CanEnable(organizationID, status) {
		return nil, oops.E(oops.CodeUnavailable, nil, "the issuer cannot be enabled on this deployment")
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

// disable turns signing off with RBAC and the database alone.
func (s *Service) disable(ctx context.Context) (*gensvc.InternalSessionIssuer, error) {
	organizationID, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	err = s.lifecycle.SetDisabled(ctx, organizationID, true)
	s.metrics.record(ctx, opDisable, err)
	if err != nil {
		return nil, s.operationError(ctx, logger, err, "could not disable the issuer")
	}
	s.purgeIssuerTokens(ctx, organizationID)
	return s.view(ctx, logger, organizationID)
}

func (s *Service) Rotate(ctx context.Context, payload *gensvc.RotatePayload) (*gensvc.InternalSessionIssuer, error) {
	if payload.Compromised {
		return s.rotateCompromised(ctx, payload.ActiveKid)
	}
	organizationID, logger, status, err := s.access(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "issuers are not configured on this deployment").LogError(ctx, logger)
	}
	if status.Issuer == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "the issuer is not enabled")
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

// rotateCompromised contains first: one transaction revokes every key and
// withdraws it from the key set, needing only RBAC and the database. KMS
// cleanup and the replacement key follow as separate steps; a failure in
// either leaves the issuer contained, and the response reports what is left.
func (s *Service) rotateCompromised(ctx context.Context, activeKid *string) (*gensvc.InternalSessionIssuer, error) {
	organizationID, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	containment, err := s.lifecycle.Contain(ctx, organizationID, activeKid)
	if err != nil {
		s.metrics.record(ctx, opRotateCompromised, err)
		return nil, s.operationError(ctx, logger, err, "could not revoke the issuer's keys")
	}
	if len(containment.Revoked) > 0 {
		logger.WarnContext(ctx, "revoked every internal session issuer key after a reported compromise", attr.SlogValueInt(len(containment.Revoked)))
	}
	s.purgeIssuerTokens(ctx, organizationID)
	if s.manager == nil {
		s.metrics.record(ctx, opRotateCompromised, errNoManager)
		return withReplacementError(s.view(ctx, logger, organizationID))
	}
	var errs []error
	if _, _, err := s.manager.CleanupEMAIssuerKMS(ctx, organizationID); err != nil {
		logger.ErrorContext(ctx, "revoked issuer keys still await kms cleanup; the sweep retries it", attr.SlogError(err))
		errs = append(errs, err)
	}
	// Containment never depends on these, but a replacement key is only worth
	// minting for an issuer the organization can still use.
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "could not check whether to replace the contained issuer key; it stays keyless", attr.SlogError(err))
		s.metrics.record(ctx, opRotateCompromised, errors.Join(append(errs, err)...))
		return withReplacementError(s.view(ctx, logger, organizationID))
	}
	if !status.RolledOut || !status.HasVerifiedDomain {
		s.metrics.record(ctx, opRotateCompromised, errors.Join(errs...))
		return s.view(ctx, logger, organizationID)
	}
	replaceErr := s.replace(ctx, logger, organizationID)
	s.metrics.record(ctx, opRotateCompromised, errors.Join(append(errs, replaceErr)...))
	if replaceErr != nil {
		return withReplacementError(s.view(ctx, logger, organizationID))
	}
	return s.view(ctx, logger, organizationID)
}

// withReplacementError marks a contained issuer whose replacement key isn't
// active, so callers don't read containment alone as a finished replacement.
func withReplacementError(view *gensvc.InternalSessionIssuer, err error) (*gensvc.InternalSessionIssuer, error) {
	if err == nil && view != nil && view.State == string(StateKeyless) {
		message := "The compromised keys are withdrawn, but a replacement key couldn't be set up yet. Try again."
		view.ReplacementError = &message
	}
	return view, err
}

// errNoManager marks a containment that could not continue past revocation.
var errNoManager = errors.New("internalsessionissuers: no key manager on this deployment")

// replace activates a fresh key on a keyless issuer. Revocation is never
// limited, but minting is: each mint needs a newly revoked active key and
// still counts against the organization's mint limit.
func (s *Service) replace(ctx context.Context, logger *slog.Logger, organizationID string) error {
	issuer, err := repo.New(s.db).GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("read issuer: %w", err)
	}
	active, err := ActiveKey(ctx, s.db, organizationID, issuer.JsonWebKeySetID)
	if err != nil || active != nil {
		return err
	}
	if err := s.allowMint(ctx, logger, organizationID); err != nil {
		return fmt.Errorf("replacement key deferred: %w", err)
	}
	if err := s.manager.ProvisionEMAIssuerKey(ctx, organizationID); err != nil {
		logger.ErrorContext(ctx, "issuer stays keyless; replacement key could not be activated", attr.SlogError(err))
		return fmt.Errorf("activate replacement key: %w", err)
	}
	return nil
}

func isRefused(err error) bool {
	_, ok := errors.AsType[*SigningRefusedError](err)
	return ok
}

func isPending(err error) bool {
	_, ok := errors.AsType[*RotationPendingError](err)
	return ok
}

func (s *Service) allowMint(ctx context.Context, logger *slog.Logger, organizationID string) error {
	// Minting creates KMS keys, so it fails closed; revocation never comes here.
	switch res, limitErr := s.mintLimiter.Allow(ctx, organizationID); {
	case limitErr != nil:
		return oops.E(oops.CodeUnavailable, limitErr, "issuer key rate limit could not be checked, try again shortly").LogError(ctx, logger)
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "issuer key rate limit exceeded, try again shortly")
	}
	return nil
}

func (s *Service) operationError(ctx context.Context, logger *slog.Logger, err error, msg string) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return oops.E(oops.CodeNotFound, err, "the issuer is not enabled")
	case errors.Is(err, ErrActiveKidRequired):
		return oops.E(oops.CodeBadRequest, err, "active_kid is required while the issuer has an active key")
	case errors.Is(err, ErrUnknownKid):
		return oops.E(oops.CodeConflict, err, "active_kid was never published by this issuer; read the issuer again")
	case isRefused(err):
		return oops.E(oops.CodeForbidden, err, "%s: the issuer is not enabled for signing", msg)
	case errors.Is(err, ErrRotationConflict):
		return oops.E(oops.CodeConflict, err, "the active key changed; read the issuer again")
	case errors.Is(err, ErrMisconfigured):
		logger.ErrorContext(ctx, "platform issuer signing is misconfigured", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "%s: issuer signing is misconfigured on this deployment; contact support", msg)
	default:
		return oops.E(oops.CodeUnavailable, err, "%s; retry shortly", msg).LogError(ctx, logger)
	}
}

// view is the public state of an organization's issuer: never signing
// material. It falls back to database-only state when the gate's providers
// fail, so a containment response never depends on them.
func (s *Service) view(ctx context.Context, logger *slog.Logger, organizationID string) (*gensvc.InternalSessionIssuer, error) {
	view := &gensvc.InternalSessionIssuer{State: string(StateUnavailable), Issuer: nil, JwksURI: nil, Keys: []*gensvc.InternalSessionIssuerKey{}, RotationReadyAt: nil, KmsCleanupPending: 0, CanEnable: false, ReplacementError: nil}
	q := repo.New(s.db)
	row, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		row.ID = uuid.Nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the issuer").LogError(ctx, logger)
	}
	status, err := s.gate.Status(ctx, organizationID)
	switch {
	case err == nil:
		view.State = string(status.State)
		view.CanEnable = s.gate.CanEnable(organizationID, status)
	case row.ID == uuid.Nil:
		// Nothing to disable or contain, so a provider outage reads as not offered.
		logger.WarnContext(ctx, "issuer availability could not be determined; reporting unavailable", attr.SlogError(err))
		return view, nil
	default:
		logger.WarnContext(ctx, "issuer status unavailable, reporting database state", attr.SlogError(err))
		view.State = string(s.databaseState(ctx, organizationID, row))
	}
	if row.ID == uuid.Nil {
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
	keys, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: row.JsonWebKeySetID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the issuer's keys").LogError(ctx, logger)
	}
	pending, err := q.ListPendingEMAIssuerKMSCleanup(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the issuer's key cleanup").LogError(ctx, logger)
	}
	view.KmsCleanupPending = len(pending)
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
			view.RotationReadyAt = new(key.UpdatedAt.Time.Add(ActivationDelay).UTC().Format(time.RFC3339Nano))
		}
	}
	view.Issuer = &issuer
	view.JwksURI = &jwksURI
	return view, nil
}

// databaseState is the issuer state the database alone shows. With the
// gate's providers failing, the signer refuses, so live issuers read paused.
func (s *Service) databaseState(ctx context.Context, organizationID string, row repo.InternalSessionIssuer) State {
	if row.DisabledAt.Valid {
		return StateDisabled
	}
	if active, err := ActiveKey(ctx, s.db, organizationID, row.JsonWebKeySetID); err == nil && active == nil {
		return StateKeyless
	}
	return StateSigningPaused
}
