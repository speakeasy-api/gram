// Package orgencryptionapi implements the orgEncryption management service.
// It lives apart from package orgencryption so features that encrypt can
// import the domain package without the session and authorization stack.
package orgencryptionapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/org_encryption/server"
	gen "github.com/speakeasy-api/gram/server/gen/org_encryption"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// verifyRatePerMin and verifyRateBurst bound how often one organization can
	// probe a key. Each probe is two KMS operations billed to the key's owner;
	// the bound stops a runaway client without refusing an operator working
	// through a few keys.
	verifyRatePerMin = 10
	verifyRateBurst  = 10

	// detailMaxLen bounds the provider text a probe echoes back. The full error
	// is in the log.
	detailMaxLen = 300
)

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	auth            *auth.Auth
	authz           *authz.Engine
	audit           *audit.Logger
	provisioner     *orgencryption.Provisioner
	productFeatures *productfeatures.Client
	verifyLimiter   *ratelimit.Limiter
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// NewService builds the service. provisioner may be nil when organization
// encryption is not configured; every method then reports it unavailable.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	provisioner *orgencryption.Provisioner,
	productFeatures *productfeatures.Client,
	verifyStore ratelimit.Store,
) *Service {
	logger = logger.With(attr.SlogComponent("orgencryptionapi"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/orgencryptionapi"),
		logger:          logger,
		db:              db,
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		audit:           auditLogger,
		provisioner:     provisioner,
		productFeatures: productFeatures,
		verifyLimiter: ratelimit.New(verifyStore, "org-encryption-key-verify",
			ratelimit.PerMinute(verifyRatePerMin).WithBurst(verifyRateBurst),
			ratelimit.WithMetrics(meterProvider)),
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

// requireOrgAccess enforces the RBAC scope and the customer-managed keys
// entitlement, the same one that gates signing keys, and that organization
// encryption is configured.
func (s *Service) requireOrgAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}

	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureCustomerManagedEncryptionKeys)
	if err != nil {
		return nil, logger, oops.E(oops.CodeUnexpected, err, "error checking customer managed keys entitlement").LogError(ctx, logger)
	}
	if !enabled {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "customer-managed encryption keys are not enabled for this organization")
	}

	if s.provisioner == nil {
		return nil, logger, oops.E(oops.CodeUnavailable, nil, "organization encryption is not available").LogWarn(ctx, logger)
	}

	return authCtx, logger, nil
}

func actorFor(authCtx *contextvalues.AuthContext) orgencryption.Actor {
	return orgencryption.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: authCtx.Email}
}

// domainError maps an orgencryption error onto the response a caller acts on.
func domainError(ctx context.Context, logger *slog.Logger, err error) error {
	switch {
	case errors.Is(err, orgencryption.ErrNotProvisioned):
		return oops.E(oops.CodeNotFound, err, "organization encryption is not set up yet").LogInfo(ctx, logger)
	case errors.Is(err, orgencryption.ErrUnknownUseCase):
		return oops.E(oops.CodeBadRequest, err, "unknown use case").LogInfo(ctx, logger)
	case errors.Is(err, orgencryption.ErrInvalidKey):
		return oops.E(oops.CodeBadRequest, err, "%s", conv.TruncateDetail(err.Error(), detailMaxLen)).LogInfo(ctx, logger)
	case errors.Is(err, orgencryption.ErrInvalidState):
		return oops.E(oops.CodeConflict, err, "%s", conv.TruncateDetail(err.Error(), detailMaxLen)).LogInfo(ctx, logger)
	case errors.Is(err, orgencryption.ErrConcurrentChange):
		return oops.E(oops.CodeConflict, err, "the encryption configuration changed while this request ran; try again").LogInfo(ctx, logger)
	case errors.Is(err, orgencryption.ErrSuspended):
		return oops.E(oops.CodeFailedPrecondition, err, "organization encryption is suspended; fix the key and resume").LogInfo(ctx, logger)
	case errors.Is(err, orgencryption.ErrKeyUnavailable):
		return oops.E(oops.CodeFailedPrecondition, err, "the current key could not unwrap the organization's data keys: %s", conv.TruncateDetail(err.Error(), detailMaxLen)).LogWarn(ctx, logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "error updating organization encryption").LogError(ctx, logger)
	}
}

func (s *Service) GetConfig(ctx context.Context, _ *gen.GetConfigPayload) (*gen.OrgEncryptionConfig, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	config, err := repo.New(s.db).GetOrganizationEncryptionConfig(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, domainError(ctx, logger, orgencryption.ErrNotProvisioned)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading organization encryption").LogError(ctx, logger)
	}

	return mv.BuildOrgEncryptionConfigView(config), nil
}

func (s *Service) ListKeys(ctx context.Context, _ *gen.ListKeysPayload) (*gen.ListOrgEncryptionKeysResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListEncryptionExternalKeys(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing encryption keys").LogError(ctx, logger)
	}

	keys := make([]*gen.OrgEncryptionKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, mv.BuildOrgEncryptionKeyView(row.ExternalKey, row.ResourceName, row.Managed))
	}

	return &gen.ListOrgEncryptionKeysResult{Keys: keys}, nil
}

func (s *Service) CreateGcpKey(ctx context.Context, payload *gen.CreateGcpKeyPayload) (*gen.OrgEncryptionKey, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogInfo(ctx, logger)
	}
	resourceName := strings.TrimSpace(payload.ResourceName)
	if err := gcpkms.ValidateKeyName(resourceName); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "resource_name must be a crypto key without a version: projects/<p>/locations/<l>/keyRings/<r>/cryptoKeys/<k>").LogInfo(ctx, logger)
	}
	credentialID, err := uuid.Parse(payload.ExternalCredentialID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external credential id").LogInfo(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating encryption key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	credential, err := q.GetOrganizationGcpIamCredentialForKey(ctx, repo.GetOrganizationGcpIamCredentialForKeyParams{
		ID:             credentialID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, nil, "gcp_iam external credential not found").LogInfo(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading external credential").LogError(ctx, logger)
	}
	// An exempted credential acts as Speakeasy's own project; it can never
	// reach an organization's key.
	if credential.SkipProjectVerification {
		return nil, oops.E(oops.CodeBadRequest, nil, "a credential exempted from project verification cannot back an organization key").LogInfo(ctx, logger)
	}

	key, err := q.CreateCustomerEncryptionExternalKey(ctx, repo.CreateCustomerEncryptionExternalKeyParams{
		OrganizationID:         conv.ToPGText(authCtx.ActiveOrganizationID),
		ExternalCredentialID:   credentialID,
		Algorithm:              gcpkms.SymmetricAlgorithm,
		Name:                   name,
		CustomerGrantReference: conv.PtrToPGTextTrimmed(payload.CustomerGrantReference),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating encryption key").LogError(ctx, logger)
	}
	if _, err := q.CreateEncryptionGcpKmsKey(ctx, repo.CreateEncryptionGcpKmsKeyParams{ResourceName: resourceName, ExternalKeyID: key.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating encryption key").LogError(ctx, logger)
	}

	if err := s.audit.LogGcpKmsKeyCreate(ctx, dbtx, audit.LogGcpKmsKeyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewGcpKmsKey(key.ID),
		KeyName:          key.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error logging encryption key creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating encryption key").LogError(ctx, logger)
	}

	return mv.BuildOrgEncryptionKeyView(key, resourceName, false), nil
}

func (s *Service) VerifyKey(ctx context.Context, payload *gen.VerifyKeyPayload) (*gen.VerifyOrgEncryptionKeyResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogInfo(ctx, logger)
	}

	// Best effort: a limiter outage allows the probe rather than blocking key
	// management on a Redis blip.
	switch res, limitErr := s.verifyLimiter.Allow(ctx, authCtx.ActiveOrganizationID); {
	case limitErr != nil:
		logger.WarnContext(ctx, "encryption key verify rate limiter unavailable, allowing", attr.SlogError(limitErr))
	case !res.Allowed:
		return nil, oops.E(oops.CodeRateLimitExceeded, nil, "key verification rate limit exceeded, try again shortly")
	}

	if _, err := repo.New(s.db).GetEncryptionExternalKey(ctx, repo.GetEncryptionExternalKeyParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "encryption key not found").LogInfo(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error loading encryption key").LogError(ctx, logger)
	}

	result := s.provisioner.VerifyKey(ctx, authCtx.ActiveOrganizationID, id)
	if !result.Verified {
		logger.InfoContext(ctx, "encryption key verification did not pass", attr.SlogOutcome(string(result.Reason)))
		return &gen.VerifyOrgEncryptionKeyResult{
			Verified:        false,
			KeyProbeOutcome: probeOutcome(result.Reason),
			Detail:          new(conv.TruncateDetail(result.Detail, detailMaxLen)),
		}, nil
	}

	return &gen.VerifyOrgEncryptionKeyResult{Verified: true, KeyProbeOutcome: string(gcpkms.ReasonVerified), Detail: nil}, nil
}

// probeOutcome maps a probe reason onto the API enum, folding reasons only
// signing produces into unexpected.
func probeOutcome(reason gcpkms.VerifyReason) string {
	switch reason {
	case gcpkms.ReasonVerified,
		orgencryption.ProbeOutcomeCredentialUnusable,
		gcpkms.ReasonInvalidResourceName,
		gcpkms.ReasonKeyNotFound,
		gcpkms.ReasonPermissionDenied,
		gcpkms.ReasonKeyUnusable,
		gcpkms.ReasonRoundTripFailed,
		gcpkms.ReasonUnavailable:
		return string(reason)
	default:
		return string(gcpkms.ReasonUnexpected)
	}
}

func (s *Service) DeleteKey(ctx context.Context, payload *gen.DeleteKeyPayload) error {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key id").LogInfo(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting encryption key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	locked, err := q.LockEncryptionExternalKeyForDelete(ctx, repo.LockEncryptionExternalKeyForDeleteParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting encryption key").LogError(ctx, logger)
	}
	if locked.Managed {
		return oops.E(oops.CodeForbidden, nil, "Speakeasy-managed encryption keys cannot be deleted")
	}

	protects, err := q.ExternalKeyProtectsOrganizationData(ctx, repo.ExternalKeyProtectsOrganizationDataParams{OrganizationID: authCtx.ActiveOrganizationID, ExternalKeyID: id})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error checking encryption key use").LogError(ctx, logger)
	}
	if protects {
		return oops.E(oops.CodeConflict, nil, "this key still protects the organization's data; switch to another key and complete the switch first")
	}

	deleted, err := q.SoftDeleteEncryptionExternalKey(ctx, repo.SoftDeleteEncryptionExternalKeyParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting encryption key").LogError(ctx, logger)
	}

	if err := s.audit.LogGcpKmsKeyDelete(ctx, dbtx, audit.LogGcpKmsKeyDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewGcpKmsKey(deleted.ID),
		KeyName:          deleted.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error logging encryption key deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting encryption key").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) SwitchKey(ctx context.Context, payload *gen.SwitchKeyPayload) (*gen.OrgEncryptionConfig, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ExternalKeyID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogInfo(ctx, logger)
	}

	key, err := repo.New(s.db).GetEncryptionExternalKey(ctx, repo.GetEncryptionExternalKeyParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "encryption key not found").LogInfo(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading encryption key").LogError(ctx, logger)
	}
	// Returning to a Speakeasy-managed key is a staff-assisted offboarding
	// flow, not a customer action.
	if key.Managed {
		return nil, oops.E(oops.CodeBadRequest, nil, "only customer-managed keys can be switched to").LogInfo(ctx, logger)
	}

	config, err := s.provisioner.SwitchKey(ctx, authCtx.ActiveOrganizationID, id, actorFor(authCtx))
	if err != nil {
		return nil, domainError(ctx, logger, err)
	}

	return mv.BuildOrgEncryptionConfigView(config), nil
}

func (s *Service) CompleteSwitch(ctx context.Context, _ *gen.CompleteSwitchPayload) (*gen.OrgEncryptionConfig, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	config, err := s.provisioner.CompleteSwitch(ctx, authCtx.ActiveOrganizationID, actorFor(authCtx))
	if err != nil {
		return nil, domainError(ctx, logger, err)
	}

	return mv.BuildOrgEncryptionConfigView(config), nil
}

func (s *Service) RollbackSwitch(ctx context.Context, _ *gen.RollbackSwitchPayload) (*gen.OrgEncryptionConfig, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	config, err := s.provisioner.RollbackSwitch(ctx, authCtx.ActiveOrganizationID, actorFor(authCtx))
	if err != nil {
		return nil, domainError(ctx, logger, err)
	}

	return mv.BuildOrgEncryptionConfigView(config), nil
}

func (s *Service) RotateDataKey(ctx context.Context, payload *gen.RotateDataKeyPayload) (*gen.OrgEncryptionConfig, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	useCase, err := orgencryption.ParseUseCase(payload.UseCase)
	if err != nil {
		return nil, domainError(ctx, logger, err)
	}

	if err := s.provisioner.RotateDataKey(ctx, authCtx.ActiveOrganizationID, useCase, actorFor(authCtx)); err != nil {
		return nil, domainError(ctx, logger, err)
	}

	return s.configAfterChange(ctx, logger, authCtx.ActiveOrganizationID)
}

func (s *Service) Resume(ctx context.Context, _ *gen.ResumePayload) (*gen.OrgEncryptionConfig, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	if err := s.provisioner.Resume(ctx, authCtx.ActiveOrganizationID, actorFor(authCtx)); err != nil {
		return nil, domainError(ctx, logger, err)
	}

	return s.configAfterChange(ctx, logger, authCtx.ActiveOrganizationID)
}

// configAfterChange reads the configuration a committed change produced. It
// repeats no authorization, so a mutation that succeeded is never reported as
// failed and retried.
func (s *Service) configAfterChange(ctx context.Context, logger *slog.Logger, organizationID string) (*gen.OrgEncryptionConfig, error) {
	config, err := repo.New(s.db).GetOrganizationEncryptionConfig(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "the change was applied but the configuration could not be read").LogError(ctx, logger)
	}

	return mv.BuildOrgEncryptionConfigView(config), nil
}
