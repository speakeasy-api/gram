package networkingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/network_ingress/server"
	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

// ProviderTailscale is the only overlay network provider supported today.
// Allowed values are validated here rather than with database CHECK
// constraints so new providers can land without a migration.
const ProviderTailscale = "tailscale"

const (
	credentialKindAuthKey     = "auth_key"
	credentialKindOAuthClient = "oauth_client" //nolint:gosec // discriminator value, not a secret
)

// defaultTags is the tag set advertised when the caller does not supply one.
// Tailscale requires tagged nodes for OAuth-minted keys; tag:gram matches the
// setup instructions shown in the dashboard.
var defaultTags = []string{"tag:gram"}

var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	auth            *auth.Auth
	authz           *authz.Engine
	productFeatures *productfeatures.Client
	enc             *encryption.Client
	audit           *audit.Logger
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	features *productfeatures.Client,
	enc *encryption.Client,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("network_ingress"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/networkingress"),
		logger:          logger,
		db:              db,
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		productFeatures: features,
		enc:             enc,
		audit:           auditLogger,
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

// authorize applies the shared per-method gate: an authenticated org context,
// the required RBAC scope, and the network_ingress product feature.
func (s *Service) authorize(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureNetworkIngress)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error checking network ingress entitlement").LogError(ctx, s.logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeForbidden, nil, "private network ingress is not enabled for this organization")
	}

	return authCtx, nil
}

func (s *Service) GetIngress(ctx context.Context, _ *gen.GetIngressPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	ingress, err := repo.New(s.db).GetNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization").LogError(ctx, s.logger)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get network ingress for organization").LogError(ctx, s.logger)
	}

	return mv.BuildNetworkIngressView(ingress), nil
}

func (s *Service) CreateIngress(ctx context.Context, payload *gen.CreateIngressPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	provider := conv.PtrValOr(payload.Provider, ProviderTailscale)
	if provider != ProviderTailscale {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported provider: only 'tailscale' is available").LogError(ctx, s.logger)
	}

	hostname := conv.PtrValOr(payload.Hostname, "gram-mcp")
	if err := validateHostname(hostname); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid hostname").LogError(ctx, s.logger)
	}

	tags := payload.Tags
	if tags == nil {
		tags = defaultTags
	}
	if err := validateTags(tags); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid tags").LogError(ctx, s.logger)
	}

	cred, err := s.encryptCredential(payload.AuthKey, payload.OauthClientID, payload.OauthClientSecret)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin network ingress creation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	ingress, err := repo.New(dbtx).CreateNetworkIngress(ctx, repo.CreateNetworkIngressParams{
		OrganizationID:       authCtx.ActiveOrganizationID,
		Provider:             provider,
		Hostname:             hostname,
		CredentialKind:       cred.kind,
		AuthKeyEnc:           cred.authKeyEnc,
		OauthClientID:        cred.oauthClientID,
		OauthClientSecretEnc: cred.oauthClientSecretEnc,
		Tags:                 tags,
		PrivateNetworkOnly:   conv.PtrValOr(payload.PrivateNetworkOnly, false),
		IdentityRequired:     conv.PtrValOr(payload.IdentityRequired, true),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "organization already has a network ingress").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create network ingress").LogError(ctx, s.logger)
	}

	if err := s.audit.LogNetworkIngressCreate(ctx, dbtx, audit.LogNetworkIngressCreateEvent{
		OrganizationID:    authCtx.ActiveOrganizationID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		NetworkIngressURN: urn.NewNetworkIngress(ingress.ID),
		Hostname:          ingress.Hostname,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log network ingress creation").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit network ingress creation").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "network ingress created", attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	return mv.BuildNetworkIngressView(ingress), nil
}

func (s *Service) UpdateIngress(ctx context.Context, payload *gen.UpdateIngressPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	if payload.Hostname == nil && payload.Enabled == nil && payload.PrivateNetworkOnly == nil && payload.IdentityRequired == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide at least one network ingress setting to update").LogError(ctx, s.logger)
	}
	if payload.Hostname != nil {
		if err := validateHostname(*payload.Hostname); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid hostname").LogError(ctx, s.logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin network ingress update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repository := repo.New(dbtx)
	before, err := repository.LockNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization").LogError(ctx, s.logger)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress for update").LogError(ctx, s.logger)
	}
	beforeView := mv.BuildNetworkIngressView(before)

	ingress, err := repository.UpdateNetworkIngressSettings(ctx, repo.UpdateNetworkIngressSettingsParams{
		UpdateHostname:           payload.Hostname != nil,
		Hostname:                 conv.PtrValOr(payload.Hostname, ""),
		UpdateEnabled:            payload.Enabled != nil,
		Enabled:                  conv.PtrValOr(payload.Enabled, false),
		UpdatePrivateNetworkOnly: payload.PrivateNetworkOnly != nil,
		PrivateNetworkOnly:       conv.PtrValOr(payload.PrivateNetworkOnly, false),
		UpdateIdentityRequired:   payload.IdentityRequired != nil,
		IdentityRequired:         conv.PtrValOr(payload.IdentityRequired, false),
		OrganizationID:           authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update network ingress settings").LogError(ctx, s.logger)
	}
	afterView := mv.BuildNetworkIngressView(ingress)

	if err := s.audit.LogNetworkIngressUpdate(ctx, dbtx, audit.LogNetworkIngressUpdateEvent{
		OrganizationID:               authCtx.ActiveOrganizationID,
		Actor:                        urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:             authCtx.Email,
		ActorSlug:                    nil,
		NetworkIngressURN:            urn.NewNetworkIngress(ingress.ID),
		Hostname:                     ingress.Hostname,
		NetworkIngressSnapshotBefore: beforeView,
		NetworkIngressSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log network ingress update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit network ingress update").LogError(ctx, s.logger)
	}

	return afterView, nil
}

func (s *Service) RotateCredentials(ctx context.Context, payload *gen.RotateCredentialsPayload) (*gen.NetworkIngress, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	cred, err := s.encryptCredential(payload.AuthKey, payload.OauthClientID, payload.OauthClientSecret)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin network ingress credential rotation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repository := repo.New(dbtx)
	if _, err := repository.LockNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "no network ingress found for organization").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock network ingress for credential rotation").LogError(ctx, s.logger)
	}

	ingress, err := repository.RotateNetworkIngressCredentials(ctx, repo.RotateNetworkIngressCredentialsParams{
		CredentialKind:       cred.kind,
		AuthKeyEnc:           cred.authKeyEnc,
		OauthClientID:        cred.oauthClientID,
		OauthClientSecretEnc: cred.oauthClientSecretEnc,
		OrganizationID:       authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "rotate network ingress credentials").LogError(ctx, s.logger)
	}

	if err := s.audit.LogNetworkIngressRotateCredentials(ctx, dbtx, audit.LogNetworkIngressRotateCredentialsEvent{
		OrganizationID:    authCtx.ActiveOrganizationID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		NetworkIngressURN: urn.NewNetworkIngress(ingress.ID),
		Hostname:          ingress.Hostname,
		CredentialKind:    cred.kind,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log network ingress credential rotation").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit network ingress credential rotation").LogError(ctx, s.logger)
	}

	return mv.BuildNetworkIngressView(ingress), nil
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

	repository := repo.New(dbtx)
	ingress, err := repository.LockNetworkIngressByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "no network ingress found for organization").LogError(ctx, s.logger)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock network ingress for deletion").LogError(ctx, s.logger)
	}

	if err := repository.DeleteNetworkIngress(ctx, authCtx.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete network ingress").LogError(ctx, s.logger)
	}

	if err := s.audit.LogNetworkIngressDelete(ctx, dbtx, audit.LogNetworkIngressDeleteEvent{
		OrganizationID:    authCtx.ActiveOrganizationID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		NetworkIngressURN: urn.NewNetworkIngress(ingress.ID),
		Hostname:          ingress.Hostname,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log network ingress deletion").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit network ingress deletion").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "network ingress deleted", attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	return nil
}

// encryptedCredential is the storage form of a validated credential: exactly
// one mode populated, secrets already AES-256-GCM ciphertext.
type encryptedCredential struct {
	kind                 string
	authKeyEnc           pgtype.Text
	oauthClientID        pgtype.Text
	oauthClientSecretEnc pgtype.Text
}

func (s *Service) encryptCredential(authKey, oauthClientID, oauthClientSecret *string) (encryptedCredential, error) {
	var zero encryptedCredential

	hasKey := authKey != nil && *authKey != ""
	hasClientID := oauthClientID != nil && *oauthClientID != ""
	hasClientSecret := oauthClientSecret != nil && *oauthClientSecret != ""

	switch {
	case hasKey && (hasClientID || hasClientSecret):
		return zero, errors.New("provide exactly one credential mode: auth_key, or oauth_client_id with oauth_client_secret")
	case hasKey:
		keyEnc, err := s.enc.Encrypt([]byte(*authKey))
		if err != nil {
			return zero, fmt.Errorf("encrypt auth key: %w", err)
		}
		return encryptedCredential{
			kind:                 credentialKindAuthKey,
			authKeyEnc:           conv.ToPGText(keyEnc),
			oauthClientID:        conv.ToPGTextEmpty(""),
			oauthClientSecretEnc: conv.ToPGTextEmpty(""),
		}, nil
	case hasClientID && hasClientSecret:
		secretEnc, err := s.enc.Encrypt([]byte(*oauthClientSecret))
		if err != nil {
			return zero, fmt.Errorf("encrypt oauth client secret: %w", err)
		}
		return encryptedCredential{
			kind:                 credentialKindOAuthClient,
			authKeyEnc:           conv.ToPGTextEmpty(""),
			oauthClientID:        conv.ToPGText(*oauthClientID),
			oauthClientSecretEnc: conv.ToPGText(secretEnc),
		}, nil
	default:
		return zero, errors.New("provide an auth_key, or an oauth_client_id with an oauth_client_secret")
	}
}

func validateHostname(hostname string) error {
	if !hostnamePattern.MatchString(hostname) {
		return fmt.Errorf("hostname must be a DNS label (lowercase letters, digits, hyphens; max 63 chars): %q", hostname)
	}
	return nil
}

func validateTags(tags []string) error {
	if len(tags) > 16 {
		return errors.New("at most 16 tags are allowed")
	}
	for _, tag := range tags {
		rest, ok := strings.CutPrefix(tag, "tag:")
		if !ok || rest == "" {
			return fmt.Errorf("tags must have the form tag:<name>: %q", tag)
		}
	}
	return nil
}
