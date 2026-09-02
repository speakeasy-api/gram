package networkingress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/network_ingress/server"
	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const ProviderTailscale = "tailscale"

var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type ReconcileSignaler interface {
	SignalNetworkIngress(ctx context.Context, ingressID uuid.UUID) error
}

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	auth      *auth.Auth
	authz     *authz.Engine
	enc       *encryption.Client
	audit     *audit.Logger
	admission *ExpansionAdmission
	signaler  ReconcileSignaler
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, enc *encryption.Client, auditLogger *audit.Logger, admission *ExpansionAdmission, signaler ReconcileSignaler) *Service {
	logger = logger.With(attr.SlogComponent("network_ingress"))
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/networkingress"),
		logger:    logger,
		db:        db,
		auth:      auth.New(logger, db, sessions, authzEngine),
		authz:     authzEngine,
		enc:       enc,
		audit:     auditLogger,
		admission: admission,
		signaler:  signaler,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) authorize(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

func (s *Service) requireExpansion(ctx context.Context, organizationID string) error {
	if s.admission == nil {
		return oops.E(oops.CodeForbidden, nil, "private network ingress is not enabled for this organization")
	}
	if err := s.admission.CheckExpansion(ctx, organizationID); err != nil {
		return oops.E(oops.CodeForbidden, err, "private network ingress is not enabled for this organization")
	}
	return nil
}

func (s *Service) GetIngress(ctx context.Context, _ *gen.GetIngressPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	ingress, err := repo.New(s.db).GetNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get network ingress").LogError(ctx, s.logger)
	}
	return mv.BuildNetworkIngressView(ingress), nil
}

type tailscaleCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (s *Service) encryptCredentials(clientID, clientSecret string) (pgtype.Text, error) {
	if clientID == "" || clientSecret == "" {
		return pgtype.Text{}, fmt.Errorf("OAuth client ID and secret are required")
	}
	plaintext, err := json.Marshal(tailscaleCredentials{ClientID: clientID, ClientSecret: clientSecret})
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("encode provider credentials: %w", err)
	}
	ciphertext, err := s.enc.Encrypt(plaintext)
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("encrypt provider credentials: %w", err)
	}
	return conv.ToPGText(ciphertext), nil
}

func validateHostname(hostname string) error {
	if !hostnamePattern.MatchString(hostname) {
		return fmt.Errorf("hostname must be a lowercase DNS label of at most 63 characters")
	}
	return nil
}

func (s *Service) CreateIngress(ctx context.Context, payload *gen.CreateIngressPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.requireExpansion(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	if payload.Provider != ProviderTailscale {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported network ingress provider")
	}
	if err := validateHostname(payload.Hostname); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	credentials, err := s.encryptCredentials(payload.OauthClientID, payload.OauthClientSecret)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid provider credentials")
	}

	ingressID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate network ingress id").LogError(ctx, s.logger)
	}
	resources, err := NewResourceNames(ingressID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "derive provider resource identities").LogError(ctx, s.logger)
	}
	providerResources, err := resources.Marshal()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize provider resource identities").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin network ingress creation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, s.logger)
	}
	if _, err := queries.GetNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID); err == nil {
		return nil, oops.E(oops.CodeConflict, nil, "organization already has a network ingress")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "check existing network ingress").LogError(ctx, s.logger)
	}
	if _, err := queries.GetPendingDeletedNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID); err == nil {
		return nil, oops.E(oops.CodeConflict, nil, "previous network ingress cleanup is still pending")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "check pending network ingress cleanup").LogError(ctx, s.logger)
	}

	namespaceKind := "platform"
	customDomainID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if domain, domainErr := customdomainsrepo.New(dbtx).LockCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID); domainErr == nil {
		namespaceKind = "custom_domain"
		customDomainID = uuid.NullUUID{UUID: domain.ID, Valid: true}
	} else if !errors.Is(domainErr, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, domainErr, "lock custom domain namespace").LogError(ctx, s.logger)
	}

	ingress, err := queries.CreateNetworkIngress(ctx, repo.CreateNetworkIngressParams{
		ID: ingressID, OrganizationID: authCtx.ActiveOrganizationID, Provider: payload.Provider,
		Hostname: payload.Hostname, EndpointNamespaceKind: namespaceKind, CustomDomainID: customDomainID,
		Enabled: true, IdentityRequired: conv.PtrValOr(payload.IdentityRequired, false), CredentialsEncrypted: credentials,
		AttestorNamespace: resources.Namespace, AttestorServiceAccount: resources.AttestorServiceAccount, ProviderResources: providerResources,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "organization already has a network ingress")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create network ingress").LogError(ctx, s.logger)
	}
	view := mv.BuildNetworkIngressView(ingress)
	if err := s.audit.LogNetworkIngressCreate(ctx, dbtx, audit.LogNetworkIngressCreateEvent{NetworkIngressEventBase: s.auditBase(authCtx, ingress), Snapshot: view}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record network ingress creation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit network ingress creation").LogError(ctx, s.logger)
	}
	s.signalAfterCommit(ctx, ingress.ID)
	return view, nil
}

func (s *Service) UpdateIngress(ctx context.Context, payload *gen.UpdateIngressPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if payload.Hostname == nil && payload.Enabled == nil && payload.IdentityRequired == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide at least one ingress setting")
	}
	if payload.Hostname != nil {
		if err := validateHostname(*payload.Hostname); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
		}
	}
	// Explicit disable-only remains available after entitlement or rollout removal.
	disableOnly := payload.Enabled != nil && !*payload.Enabled && payload.Hostname == nil && payload.IdentityRequired == nil
	if !disableOnly {
		if err := s.requireExpansion(ctx, authCtx.ActiveOrganizationID); err != nil {
			return nil, err
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin network ingress update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, s.logger)
	}
	before, err := queries.LockNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress").LogError(ctx, s.logger)
	}
	after, err := queries.UpdateNetworkIngressSettings(ctx, repo.UpdateNetworkIngressSettingsParams{
		UpdateHostname: payload.Hostname != nil, Hostname: conv.PtrValOr(payload.Hostname, ""),
		UpdateEnabled: payload.Enabled != nil, Enabled: conv.PtrValOr(payload.Enabled, false),
		UpdateIdentityRequired: payload.IdentityRequired != nil, IdentityRequired: conv.PtrValOr(payload.IdentityRequired, false),
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update network ingress").LogError(ctx, s.logger)
	}
	beforeView, afterView := mv.BuildNetworkIngressView(before), mv.BuildNetworkIngressView(after)
	if err := s.audit.LogNetworkIngressUpdate(ctx, dbtx, audit.LogNetworkIngressUpdateEvent{NetworkIngressEventBase: s.auditBase(authCtx, after), Before: beforeView, After: afterView}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record network ingress update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit network ingress update").LogError(ctx, s.logger)
	}
	s.signalAfterCommit(ctx, after.ID)
	return afterView, nil
}

func (s *Service) RotateCredentials(ctx context.Context, payload *gen.RotateCredentialsPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.requireExpansion(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	credentials, err := s.encryptCredentials(payload.OauthClientID, payload.OauthClientSecret)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid provider credentials")
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin credential rotation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, s.logger)
	}
	if _, err := queries.LockNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID); errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization")
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress").LogError(ctx, s.logger)
	}
	ingress, err := queries.RotateNetworkIngressCredentials(ctx, repo.RotateNetworkIngressCredentialsParams{CredentialsEncrypted: credentials, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "rotate network ingress credentials").LogError(ctx, s.logger)
	}
	if err := s.audit.LogNetworkIngressRotateCredentials(ctx, dbtx, audit.LogNetworkIngressRotateCredentialsEvent{NetworkIngressEventBase: s.auditBase(authCtx, ingress), Provider: ingress.Provider}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record credential rotation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit credential rotation").LogError(ctx, s.logger)
	}
	s.signalAfterCommit(ctx, ingress.ID)
	return mv.BuildNetworkIngressView(ingress), nil
}

func (s *Service) GetDeleteImpact(ctx context.Context, _ *gen.GetDeleteImpactPayload) (*gen.NetworkIngressDeleteImpact, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	if _, err := repo.New(s.db).GetNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID); errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization")
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get network ingress").LogError(ctx, s.logger)
	}
	impact, err := repo.New(s.db).CountNetworkIngressDeleteImpact(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count network ingress delete impact").LogError(ctx, s.logger)
	}
	return &gen.NetworkIngressDeleteImpact{McpServersDual: impact.McpServersDual, McpServersPrivateOnly: impact.McpServersPrivateOnly, MetaMcpServersDual: impact.MetaMcpServersDual, MetaMcpServersPrivateOnly: impact.MetaMcpServersPrivateOnly}, nil
}

func (s *Service) DeleteIngress(ctx context.Context, _ *gen.DeleteIngressPayload) error {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin network ingress deletion").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, s.logger)
	}
	ingress, err := queries.LockNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		pending, pendingErr := queries.GetPendingDeletedNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
		if errors.Is(pendingErr, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "no network ingress found for organization")
		}
		if pendingErr != nil {
			return oops.E(oops.CodeUnexpected, pendingErr, "find pending network ingress cleanup").LogError(ctx, s.logger)
		}
		if err := dbtx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "commit cleanup retry").LogError(ctx, s.logger)
		}
		s.signalAfterCommit(ctx, pending.ID)
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock network ingress").LogError(ctx, s.logger)
	}
	deleted, err := queries.SoftDeleteNetworkIngress(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete network ingress").LogError(ctx, s.logger)
	}
	if err := s.audit.LogNetworkIngressDelete(ctx, dbtx, audit.LogNetworkIngressDeleteEvent{NetworkIngressEventBase: s.auditBase(authCtx, ingress)}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record network ingress deletion").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit network ingress deletion").LogError(ctx, s.logger)
	}
	s.signalAfterCommit(ctx, deleted.ID)
	return nil
}

func (s *Service) CheckHealth(ctx context.Context, _ *gen.CheckHealthPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	ingress, err := repo.New(s.db).GetNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get network ingress").LogError(ctx, s.logger)
	}
	if s.signaler == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "network ingress health reconciliation is unavailable")
	}
	if err := s.signaler.SignalNetworkIngress(ctx, ingress.ID); err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "network ingress health reconciliation is unavailable").LogError(ctx, s.logger)
	}
	refreshed, err := repo.New(s.db).GetNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload network ingress health").LogError(ctx, s.logger)
	}
	return mv.BuildNetworkIngressView(refreshed), nil
}

func (s *Service) auditBase(authCtx *contextvalues.AuthContext, ingress repo.NetworkIngress) audit.NetworkIngressEventBase {
	return audit.NetworkIngressEventBase{OrganizationID: authCtx.ActiveOrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, NetworkIngressURN: urn.NewNetworkIngress(ingress.ID), Hostname: ingress.Hostname}
}

func (s *Service) signalAfterCommit(ctx context.Context, ingressID uuid.UUID) {
	if s.signaler == nil {
		return
	}
	if err := s.signaler.SignalNetworkIngress(context.WithoutCancel(ctx), ingressID); err != nil {
		s.logger.WarnContext(ctx, "failed to signal network ingress reconciliation", attr.SlogError(err), attr.SlogNetworkIngressID(ingressID.String()))
	}
}
