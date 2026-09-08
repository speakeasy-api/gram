package externalcredentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	adminecsrv "github.com/speakeasy-api/gram/server/gen/http/admin_external_credentials/server"
	srv "github.com/speakeasy-api/gram/server/gen/http/external_credentials/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// verifyRatePerMin and verifyRateBurst bound how often one organization can run
// the verify probe. Verify makes an authenticated outbound call using Gram's own
// identity against a caller-supplied service account, so an unbounded endpoint
// would double as an oracle for which service accounts Gram can impersonate.
const (
	verifyRatePerMin = 10
	verifyRateBurst  = 5
)

// impersonationRole is the IAM role a customer must grant Gram's own service
// account on the service account they want Gram to impersonate.
const impersonationRole = "roles/iam.serviceAccountTokenCreator"

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	auth            *auth.Auth
	authz           *authz.Engine
	audit           *audit.Logger
	gcpIdentity     *gcpauth.Identity
	productFeatures *productfeatures.Client
	verifyLimiter   *ratelimit.Limiter
}

var (
	_ gen.Service        = (*Service)(nil)
	_ gen.Auther         = (*Service)(nil)
	_ adminecgen.Service = (*Service)(nil)
	_ adminecgen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	gcpIdentity *gcpauth.Identity,
	productFeatures *productfeatures.Client,
	verifyStore ratelimit.Store,
) *Service {
	logger = logger.With(attr.SlogComponent("externalcredentials"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalcredentials"),
		logger:          logger,
		db:              db,
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		audit:           auditLogger,
		gcpIdentity:     gcpIdentity,
		productFeatures: productFeatures,
		verifyLimiter: ratelimit.New(verifyStore, "external-credential-verify",
			ratelimit.PerMinute(verifyRatePerMin).WithBurst(verifyRateBurst),
			ratelimit.WithMetrics(meterProvider)),
	}
}

// requireOrgAccess resolves the auth context and enforces both gates every
// organization-tier handler needs: the RBAC scope and the customer-managed-keys
// entitlement. Routing all of them through one function keeps the entitlement
// from being reachable by an endpoint that simply forgot to check it.
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

	return authCtx, logger, nil
}

// priorTarget is a credential's impersonation state as already stored. Update
// passes the row it is about to replace; create passes the zero value, having
// nothing to carry forward.
type priorTarget struct {
	// ImpersonateServiceAccount is the target the stored row names.
	ImpersonateServiceAccount string

	// SkipProjectVerification is the stored exemption from the own-project
	// refusal.
	SkipProjectVerification bool
}

// impersonationDecision is what one write's screening settled: the target to
// store, and whether the row carries the exemption from the own-project refusal.
type impersonationDecision struct {
	// Target is the trimmed service account the row should name.
	Target string

	// Exempt is what the row's skip_project_verification column should hold.
	Exempt bool

	// NewGrant reports that this write hands the credential an exemption it did
	// not already hold for this same service account. It drives the log, which
	// is the only durable record of the grant, so it tracks the pair the
	// exemption is pinned to rather than the column alone: re-pointing an
	// already-exempt credential at a second service account is a new grant even
	// though the column does not change.
	NewGrant bool
}

// resolveOrgImpersonationTarget validates the service account an organization
// credential wants Gram to impersonate, returning the trimmed target and
// whether the row should record an exemption from the own-project refusal.
//
// Two callers may name a service account in Gram's own project. A platform
// administrator may, which is how Speakeasy staff dogfood the feature against
// an internal service account. And anyone may keep one a platform administrator
// already approved, so long as they submit it unchanged: the update payload
// replaces every column, so the dashboard resubmits the target even when the
// operator only renamed the credential, and without this an exempted credential
// could never be edited again by the organization that owns it.
//
// Carrying the exemption forward does not let it be laundered onto a different
// identity, because it is pinned to the target rather than to the credential.
// Naming any other service account in Gram's own project is screened as an
// ordinary caller and refused, so the edit path cannot be used to probe which
// internal service accounts Gram can impersonate. Moving the target away and
// back refuses too: the intervening write records no exemption.
func (s *Service) resolveOrgImpersonationTarget(ctx context.Context, logger *slog.Logger, raw string, isPlatformAdmin bool, prior priorTarget) (impersonationDecision, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return impersonationDecision{}, oops.E(oops.CodeBadRequest, nil, "impersonate_service_account is required").LogError(ctx, logger)
	}

	kind, reason, err := s.gcpIdentity.ImpersonationTargetProblem(ctx, logger, target)
	if err != nil {
		return impersonationDecision{}, oops.E(oops.CodeUnexpected, err, "cannot validate impersonate_service_account right now, try again shortly").LogError(ctx, logger)
	}

	switch kind {
	case gcpauth.TargetOK:
		return impersonationDecision{Target: target, Exempt: false, NewGrant: false}, nil

	case gcpauth.TargetOwnProject:
		// Compared case-insensitively so re-submitting the same account with
		// different capitalization is not read as naming a new one, which would
		// re-impose the refusal on an edit that changed nothing.
		carried := prior.SkipProjectVerification && strings.EqualFold(target, strings.TrimSpace(prior.ImpersonateServiceAccount))
		if !isPlatformAdmin && !carried {
			return impersonationDecision{}, oops.E(oops.CodeBadRequest, nil, "%s; if you need this, contact Speakeasy support", reason).LogError(ctx, logger)
		}

		if carried {
			// Persist the spelling that was approved rather than the one just
			// submitted. The two differ only in case, and the caller may not
			// change this target at all, so writing back their capitalization
			// would let an edit that is required to be a no-op rewrite the
			// identity the credential authenticates as.
			return impersonationDecision{Target: strings.TrimSpace(prior.ImpersonateServiceAccount), Exempt: true, NewGrant: false}, nil
		}

		return impersonationDecision{Target: target, Exempt: true, NewGrant: true}, nil

	case gcpauth.TargetMalformed:
		return impersonationDecision{}, oops.E(oops.CodeBadRequest, nil, "%s", reason).LogError(ctx, logger)

	default:
		return impersonationDecision{}, oops.E(oops.CodeUnexpected, nil, "cannot validate impersonate_service_account right now, try again shortly").LogError(ctx, logger)
	}
}

// logExemptionGranted records a credential gaining the exemption from the
// own-project refusal. Only the transition is logged, not every write that
// carries an existing exemption forward, so the grant stands out from the
// re-saves that follow it.
//
// This log is the only durable record of the grant. The exemption is
// deliberately absent from the audit snapshot and from every API surface,
// because putting it there would show an organization a decision it cannot make
// or undo. That leaves the actor and the organization it was exercised in
// visible only here.
func logExemptionGranted(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, credentialID uuid.UUID, target string) {
	logger.InfoContext(ctx, "platform admin exempted a gcp iam credential from own-project screening",
		attr.SlogUserID(authCtx.UserID),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogExternalCredentialID(credentialID.String()),
		attr.SlogGCPImpersonateServiceAccount(target),
	)
}

// verifyDetailMaxLen bounds the provider text a failed probe echoes back. The
// untruncated error is always in the log.
const verifyDetailMaxLen = 300

func Attach(mux goahttp.Muxer, service *Service) {
	mw := []func(goa.Endpoint) goa.Endpoint{
		middleware.MapErrors(),
		middleware.TraceMethods(service.tracer),
	}

	endpoints := gen.NewEndpoints(service)
	for _, m := range mw {
		endpoints.Use(m)
	}
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

	// The platform-admin (adminExternalCredentials) surface shares this Service
	// struct; its handlers live in adminhandlers.go and gate on the platform-admin
	// flag rather than an org RBAC scope.
	adminEndpoints := adminecgen.NewEndpoints(service)
	for _, m := range mw {
		adminEndpoints.Use(m)
	}
	adminecsrv.Mount(mux, adminecsrv.New(adminEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CreateAwsIamCredential(ctx context.Context, payload *gen.CreateAwsIamCredentialPayload) (*gen.AwsIamCredential, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	cols, err := s.resolveAwsColumns(ctx, logger, awsCredentialInput{
		assumeRoleArn: payload.AssumeRoleArn,
		oidcAudience:  payload.OidcAudience,
		oidcSubject:   payload.OidcSubject,
		stsRegion:     payload.StsRegion,
	}, pgtype.Text{String: "", Valid: false})
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	ec, err := q.CreateExternalCredential(ctx, repo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       "aws_iam",
		Name:           name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external credential").LogError(ctx, logger)
	}

	aws, err := q.CreateAwsIamCredential(ctx, repo.CreateAwsIamCredentialParams{
		ExternalCredentialID: ec.ID,
		AssumeRoleArn:        cols.AssumeRoleArn,
		ExternalID:           cols.ExternalID,
		OidcAudience:         cols.OidcAudience,
		OidcSubject:          cols.OidcSubject,
		StsRegion:            cols.StsRegion,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating aws iam credential").LogError(ctx, logger)
	}

	if err := s.audit.LogAwsIamCredentialCreate(ctx, dbtx, audit.LogAwsIamCredentialCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		CredentialURN:    urn.NewAwsIamCredential(ec.ID),
		CredentialName:   ec.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording aws iam credential creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external credential").LogError(ctx, logger)
	}

	return mv.BuildAwsIamCredentialView(ec, aws), nil
}

func (s *Service) UpdateAwsIamCredential(ctx context.Context, payload *gen.UpdateAwsIamCredentialPayload) (*gen.AwsIamCredential, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	current, err := q.GetAwsIamCredential(ctx, repo.GetAwsIamCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "aws iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading aws iam credential").LogError(ctx, logger)
	}

	cols, err := s.resolveAwsColumns(ctx, logger, awsCredentialInput{
		assumeRoleArn: payload.AssumeRoleArn,
		oidcAudience:  payload.OidcAudience,
		oidcSubject:   payload.OidcSubject,
		stsRegion:     payload.StsRegion,
	}, current.AwsIamCredential.ExternalID)
	if err != nil {
		return nil, err
	}

	ec, err := q.UpdateExternalCredential(ctx, repo.UpdateExternalCredentialParams{
		Name:           name,
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "aws iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external credential").LogError(ctx, logger)
	}

	aws, err := q.UpdateAwsIamCredential(ctx, repo.UpdateAwsIamCredentialParams{
		AssumeRoleArn:        cols.AssumeRoleArn,
		ExternalID:           cols.ExternalID,
		OidcAudience:         cols.OidcAudience,
		OidcSubject:          cols.OidcSubject,
		StsRegion:            cols.StsRegion,
		ExternalCredentialID: id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating aws iam credential").LogError(ctx, logger)
	}

	if err := s.audit.LogAwsIamCredentialUpdate(ctx, dbtx, audit.LogAwsIamCredentialUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		ProjectID:                uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		CredentialURN:            urn.NewAwsIamCredential(ec.ID),
		CredentialName:           ec.Name,
		CredentialSnapshotBefore: awsSnapshot(current.AwsIamCredential, current.ExternalCredential.Name),
		CredentialSnapshotAfter:  awsSnapshot(aws, ec.Name),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording aws iam credential update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external credential").LogError(ctx, logger)
	}

	return mv.BuildAwsIamCredentialView(ec, aws), nil
}

func (s *Service) CreateGcpIamCredential(ctx context.Context, payload *gen.CreateGcpIamCredentialPayload) (*gen.GcpIamCredential, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	// The organization tier is impersonation-only, so the WIF columns are never
	// written here and resolveGcpColumns (which infers between all three modes)
	// does not apply.
	decision, err := s.resolveOrgImpersonationTarget(ctx, logger, payload.ImpersonateServiceAccount, authCtx.IsAdmin, priorTarget{
		ImpersonateServiceAccount: "",
		SkipProjectVerification:   false,
	})
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	ec, err := q.CreateExternalCredential(ctx, repo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       "gcp_iam",
		Name:           name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external credential").LogError(ctx, logger)
	}

	gcp, err := q.CreateGcpIamCredential(ctx, repo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: conv.ToPGText(decision.Target),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   decision.Exempt,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating gcp iam credential").LogError(ctx, logger)
	}

	if err := s.audit.LogGcpIamCredentialCreate(ctx, dbtx, audit.LogGcpIamCredentialCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		CredentialURN:    urn.NewGcpIamCredential(ec.ID),
		CredentialName:   ec.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording gcp iam credential creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external credential").LogError(ctx, logger)
	}

	if decision.NewGrant {
		logExemptionGranted(ctx, logger, authCtx, ec.ID, decision.Target)
	}

	return mv.BuildGcpIamCredentialView(ec, gcp), nil
}

func (s *Service) UpdateGcpIamCredential(ctx context.Context, payload *gen.UpdateGcpIamCredentialPayload) (*gen.GcpIamCredential, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	current, err := q.GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading gcp iam credential").LogError(ctx, logger)
	}

	// Screened after the row it may carry an exemption from has been read, since
	// the decision depends on that row's target. The read takes no row lock and
	// runs at READ COMMITTED, so a concurrent write can still land between it and
	// the update below; that cannot produce an exemption for an unapproved
	// service account, because carrying one forward demands an exact target
	// match, but two racing edits can leave the later one's exemption standing.
	//
	// The screening resolves an ambient identity, which is memoized for the
	// process lifetime, so this holds the transaction open across a network call
	// at most once per process.
	decision, err := s.resolveOrgImpersonationTarget(ctx, logger, payload.ImpersonateServiceAccount, authCtx.IsAdmin, priorTarget{
		ImpersonateServiceAccount: current.GcpIamCredential.ImpersonateServiceAccount.String,
		SkipProjectVerification:   current.GcpIamCredential.SkipProjectVerification,
	})
	if err != nil {
		return nil, err
	}

	ec, err := q.UpdateExternalCredential(ctx, repo.UpdateExternalCredentialParams{
		Name:           name,
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external credential").LogError(ctx, logger)
	}

	// UpdateGcpIamCredential replaces every subtype column, so this also clears
	// any WIF columns a credential carried from before the organization tier
	// became impersonation-only. That is the intended convergence, and it is safe
	// precisely because impersonation is the only mode the form can express —
	// there is no field the caller could omit and silently lose.
	gcp, err := q.UpdateGcpIamCredential(ctx, repo.UpdateGcpIamCredentialParams{
		ImpersonateServiceAccount: conv.ToPGText(decision.Target),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   decision.Exempt,
		ExternalCredentialID:      id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating gcp iam credential").LogError(ctx, logger)
	}

	if err := s.audit.LogGcpIamCredentialUpdate(ctx, dbtx, audit.LogGcpIamCredentialUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		ProjectID:                uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		CredentialURN:            urn.NewGcpIamCredential(ec.ID),
		CredentialName:           ec.Name,
		CredentialSnapshotBefore: gcpSnapshot(current.GcpIamCredential, current.ExternalCredential.Name),
		CredentialSnapshotAfter:  gcpSnapshot(gcp, ec.Name),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording gcp iam credential update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external credential").LogError(ctx, logger)
	}

	if decision.NewGrant {
		logExemptionGranted(ctx, logger, authCtx, id, decision.Target)
	}

	return mv.BuildGcpIamCredentialView(ec, gcp), nil
}

func (s *Service) ListExternalCredentials(ctx context.Context, payload *gen.ListExternalCredentialsPayload) (*gen.ListExternalCredentialsResult, error) {
	provider := pgtype.Text{String: "", Valid: false}
	if payload.Provider != nil {
		provider = conv.ToPGText(*payload.Provider)
	}

	return s.listCredentials(ctx, provider)
}

func (s *Service) ListAwsIamCredentials(ctx context.Context, payload *gen.ListAwsIamCredentialsPayload) (*gen.ListExternalCredentialsResult, error) {
	return s.listCredentials(ctx, conv.ToPGText("aws_iam"))
}

func (s *Service) ListGcpIamCredentials(ctx context.Context, payload *gen.ListGcpIamCredentialsPayload) (*gen.ListExternalCredentialsResult, error) {
	return s.listCredentials(ctx, conv.ToPGText("gcp_iam"))
}

// listCredentials returns the org's credential summaries, optionally filtered to
// a single provider (invalid pgtype.Text = no filter).
func (s *Service) listCredentials(ctx context.Context, provider pgtype.Text) (*gen.ListExternalCredentialsResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListExternalCredentials(ctx, repo.ListExternalCredentialsParams{
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       provider,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing external credentials").LogError(ctx, logger)
	}

	return &gen.ListExternalCredentialsResult{
		Credentials: mv.BuildExternalCredentialSummaryListView(rows),
	}, nil
}

func (s *Service) GetAwsIamCredential(ctx context.Context, payload *gen.GetAwsIamCredentialPayload) (*gen.AwsIamCredential, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetAwsIamCredential(ctx, repo.GetAwsIamCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "aws iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading aws iam credential").LogError(ctx, logger)
	}

	return mv.BuildAwsIamCredentialView(row.ExternalCredential, row.AwsIamCredential), nil
}

func (s *Service) GetGcpIamCredential(ctx context.Context, payload *gen.GetGcpIamCredentialPayload) (*gen.GcpIamCredential, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading gcp iam credential").LogError(ctx, logger)
	}

	return mv.BuildGcpIamCredentialView(row.ExternalCredential, row.GcpIamCredential), nil
}

// VerifyGcpIamCredential probes that Gram can impersonate the service account the
// credential names. Unlike the platform equivalent this is a real authorization
// check rather than a "who am I": impersonation only succeeds when the customer
// has granted Gram's service account roles/iam.serviceAccountTokenCreator on the
// target. The probe is ephemeral — nothing is persisted — and a resolution
// failure is a reportable outcome (verified=false), not a request error.
func (s *Service) VerifyGcpIamCredential(ctx context.Context, payload *gen.VerifyGcpIamCredentialPayload) (*gen.VerifyCredentialResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	// A limiter outage is not a throttle: verify is a read-only probe, so degrade
	// to allowing rather than blocking the organization on a Redis blip.
	switch res, limitErr := s.verifyLimiter.Allow(ctx, authCtx.ActiveOrganizationID); {
	case limitErr != nil:
		logger.WarnContext(ctx, "external credential verify rate limiter unavailable, allowing", attr.SlogError(limitErr))
	case !res.Allowed:
		return nil, oops.E(oops.CodeRateLimitExceeded, nil, "verify rate limit exceeded, try again shortly")
	}

	row, err := repo.New(s.db).GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading gcp iam credential").LogError(ctx, logger)
	}

	// Rows written before this tier became impersonation-only can name no target,
	// or name one alongside Workload Identity Federation columns. Neither can be
	// probed honestly: an empty target would resolve Gram's own ambient identity
	// and report success, and a WIF row's real resolution mode is WIF (which
	// gcpauth reports as unsupported), so probing its impersonation hop in
	// isolation would claim the credential works when nothing else can use it.
	target := row.GcpIamCredential.ImpersonateServiceAccount.String
	switch {
	case target == "":
		return &gen.VerifyCredentialResult{
			Verified:  false,
			Principal: nil,
			Detail:    conv.PtrEmpty("this credential names no service account to impersonate; edit it to set one"),
		}, nil
	case row.GcpIamCredential.WifPoolID.Valid || row.GcpIamCredential.WifProviderID.Valid || row.GcpIamCredential.WifProjectNumber.Valid:
		return &gen.VerifyCredentialResult{
			Verified:  false,
			Principal: conv.PtrEmpty(target),
			Detail:    conv.PtrEmpty("this credential still uses Workload Identity Federation, which cannot be verified; save it again to convert it to impersonation"),
		}, nil
	}

	// Re-screen the stored target. The write-time guard was added with this
	// endpoint, so rows created earlier were never screened and would otherwise
	// make verify an oracle for which service accounts in Gram's own project Gram
	// can impersonate. A screening the server cannot evaluate is an error rather
	// than an unverified result: reporting "not verified" would blame the
	// customer's configuration for a fault on Gram's side.
	//
	// A row a platform administrator exempted is forgiven the own-project
	// refusal, so probing it reports what it can actually do rather than a
	// refusal no edit through this API can clear.
	kind, reason, err := s.gcpIdentity.ImpersonationTargetProblem(ctx, logger, target)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cannot verify this credential right now, try again shortly").LogError(ctx, logger)
	}
	exempted := kind == gcpauth.TargetOwnProject && row.GcpIamCredential.SkipProjectVerification
	if kind != gcpauth.TargetOK && !exempted {
		return &gen.VerifyCredentialResult{
			Verified:  false,
			Principal: conv.PtrEmpty(target),
			Detail:    conv.PtrEmpty(reason),
		}, nil
	}

	principal, resolveErr := s.gcpIdentity.ResolvePrincipal(ctx, gcpauth.Credential{
		ImpersonateServiceAccount: target,
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	})
	if resolveErr != nil {
		logger.InfoContext(ctx, "gcp iam credential verify probe did not resolve", attr.SlogError(resolveErr))
		return &gen.VerifyCredentialResult{
			Verified:  false,
			Principal: conv.PtrEmpty(target),
			Detail:    conv.PtrEmpty(conv.TruncateDetail(resolveErr.Error(), verifyDetailMaxLen)),
		}, nil
	}

	return &gen.VerifyCredentialResult{
		Verified:  true,
		Principal: conv.PtrEmpty(principal.Email),
		Detail:    nil,
	}, nil
}

// GetGcpSetupInfo reports the service account a customer must grant impersonation
// rights to. It is deliberately readable before any credential exists, because
// making that grant is a precondition of creating one at all.
func (s *Service) GetGcpSetupInfo(ctx context.Context, payload *gen.GetGcpSetupInfoPayload) (*gen.GcpSetupInfo, error) {
	_, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	// An unresolvable identity is reported as an absent email rather than an
	// error: the role to grant is still useful, and the page can explain the gap
	// instead of failing to render.
	gramSA, err := s.gcpIdentity.GramPrincipal(ctx)
	if err != nil {
		logger.WarnContext(ctx, "could not resolve gram's own gcp identity for setup info", attr.SlogError(err))
	}

	return &gen.GcpSetupInfo{
		ServiceAccountEmail: conv.PtrEmpty(gramSA),
		RequiredRole:        impersonationRole,
	}, nil
}

func (s *Service) DeleteAwsIamCredential(ctx context.Context, payload *gen.DeleteAwsIamCredentialPayload) error {
	return s.deleteExternalCredential(ctx, "aws_iam", payload.ID)
}

func (s *Service) DeleteGcpIamCredential(ctx context.Context, payload *gen.DeleteGcpIamCredentialPayload) error {
	return s.deleteExternalCredential(ctx, "gcp_iam", payload.ID)
}

// deleteExternalCredential soft-deletes a credential scoped to the given
// provider and emits the provider-specific audit event. A missing (or
// wrong-provider) id is a no-op so deletes stay idempotent.
//
// Delete is refused while a live external key still names the credential.
// Nothing in the database enforces that: `deleted` is a generated column, so the
// soft delete is an UPDATE and external_keys_external_credential_id_fkey never
// fires. Without the guard, deleting a credential leaves every key behind it
// pointing at a tombstone — the key still lists and still looks healthy, but
// nothing can reach it, and the failure only surfaces at signing time.
//
// The platform tier has its own delete path in adminhandlers.go and no such
// guard. That is sound rather than an oversight: an external key is written with
// a non-NULL organization_id and validated against a credential in that same
// organization, while platform credentials carry organization_id IS NULL, so no
// key can name one. Giving the platform tier keys of its own (AGE-3069) would
// end that, and would need this guard mirrored there.
func (s *Service) deleteExternalCredential(ctx context.Context, provider, rawID string) error {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(rawID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Lock the credential before the preflight so a concurrent key write cannot
	// commit between it and the delete.
	_, err = q.LockExternalCredentialForUpdate(ctx, repo.LockExternalCredentialForUpdateParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       provider,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting external credential").LogError(ctx, logger)
	}

	referenced, err := q.SoftDeleteExternalCredentialPreflight(ctx, id)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error checking external credential references").LogError(ctx, logger)
	}
	if referenced {
		return oops.E(oops.CodeConflict, nil, "external credential is still in use by an external key")
	}

	deleted, err := q.SoftDeleteExternalCredential(ctx, repo.SoftDeleteExternalCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       provider,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting external credential").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	var auditErr error
	switch deleted.Provider {
	case "aws_iam":
		auditErr = s.audit.LogAwsIamCredentialDelete(ctx, dbtx, audit.LogAwsIamCredentialDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			CredentialURN:    urn.NewAwsIamCredential(deleted.ID),
			CredentialName:   deleted.Name,
		})
	case "gcp_iam":
		auditErr = s.audit.LogGcpIamCredentialDelete(ctx, dbtx, audit.LogGcpIamCredentialDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			CredentialURN:    urn.NewGcpIamCredential(deleted.ID),
			CredentialName:   deleted.Name,
		})
	default:
		auditErr = fmt.Errorf("unexpected external credential provider: %s", deleted.Provider)
	}
	if auditErr != nil {
		return oops.E(oops.CodeUnexpected, auditErr, "error recording external credential deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving external credential deletion").LogError(ctx, logger)
	}

	return nil
}

type awsCredentialInput struct {
	assumeRoleArn *string
	oidcAudience  *string
	oidcSubject   *string
	stsRegion     *string
}

type awsColumns struct {
	AssumeRoleArn pgtype.Text
	ExternalID    pgtype.Text
	OidcAudience  pgtype.Text
	OidcSubject   pgtype.Text
	StsRegion     pgtype.Text
}

// resolveAwsColumns validates the AWS form and produces the subtype column
// values. The authentication approach is inferred from which fields are set:
// assume_role_arn + oidc_audience assumes the role with a web identity;
// assume_role_arn alone assumes the role with a Gram-generated ExternalId
// (preserved on update); no fields records a KMS key-policy grant.
func (s *Service) resolveAwsColumns(ctx context.Context, logger *slog.Logger, in awsCredentialInput, existingExternalID pgtype.Text) (awsColumns, error) {
	arn := conv.PtrToPGTextTrimmed(in.assumeRoleArn)
	audience := conv.PtrToPGTextTrimmed(in.oidcAudience)
	subject := conv.PtrToPGTextTrimmed(in.oidcSubject)
	region := conv.PtrToPGTextTrimmed(in.stsRegion)

	cols := awsColumns{
		AssumeRoleArn: arn,
		ExternalID:    pgtype.Text{String: "", Valid: false},
		OidcAudience:  audience,
		OidcSubject:   subject,
		StsRegion:     region,
	}

	switch {
	case audience.Valid:
		if !arn.Valid {
			return cols, oops.E(oops.CodeBadRequest, nil, "assume_role_arn is required when oidc_audience is set").LogError(ctx, logger)
		}
	case subject.Valid:
		return cols, oops.E(oops.CodeBadRequest, nil, "oidc_subject requires oidc_audience").LogError(ctx, logger)
	case arn.Valid:
		if existingExternalID.Valid {
			cols.ExternalID = existingExternalID
		} else {
			generated, err := generateExternalID()
			if err != nil {
				return cols, oops.E(oops.CodeUnexpected, err, "error generating external id").LogError(ctx, logger)
			}
			cols.ExternalID = pgtype.Text{String: generated, Valid: true}
		}
	}

	// sts_region only applies when Gram assumes a role; reject it for the
	// key-policy grant approach (no assume_role_arn).
	if region.Valid && !arn.Valid {
		return cols, oops.E(oops.CodeBadRequest, nil, "sts_region requires assume_role_arn").LogError(ctx, logger)
	}

	return cols, nil
}

type gcpCredentialInput struct {
	impersonateServiceAccount *string
	wifPoolID                 *string
	wifProviderID             *string
	wifProjectNumber          *string
}

type gcpColumns struct {
	ImpersonateServiceAccount pgtype.Text
	WifPoolID                 pgtype.Text
	WifProviderID             pgtype.Text
	WifProjectNumber          pgtype.Text
}

// resolveGcpColumns validates the GCP form and produces the subtype column
// values. The wif_* fields must be provided together; the approach (Workload
// Identity Federation, impersonation, or ambient) follows from which fields are
// set.
func (s *Service) resolveGcpColumns(ctx context.Context, logger *slog.Logger, in gcpCredentialInput) (gcpColumns, error) {
	impersonate := conv.PtrToPGTextTrimmed(in.impersonateServiceAccount)
	poolID := conv.PtrToPGTextTrimmed(in.wifPoolID)
	providerID := conv.PtrToPGTextTrimmed(in.wifProviderID)
	projectNumber := conv.PtrToPGTextTrimmed(in.wifProjectNumber)

	cols := gcpColumns{
		ImpersonateServiceAccount: impersonate,
		WifPoolID:                 poolID,
		WifProviderID:             providerID,
		WifProjectNumber:          projectNumber,
	}

	wifSet := 0
	for _, f := range []pgtype.Text{poolID, providerID, projectNumber} {
		if f.Valid {
			wifSet++
		}
	}
	if wifSet != 0 && wifSet != 3 {
		return cols, oops.E(oops.CodeBadRequest, nil, "wif_pool_id, wif_provider_id, and wif_project_number must be set together").LogError(ctx, logger)
	}

	return cols, nil
}

func awsSnapshot(aws repo.AwsIamCredential, name string) *audit.AwsIamCredentialSnapshot {
	return &audit.AwsIamCredentialSnapshot{
		Name:          name,
		AssumeRoleArn: aws.AssumeRoleArn.String,
		HasExternalID: aws.ExternalID.Valid,
		OidcAudience:  aws.OidcAudience.String,
		OidcSubject:   aws.OidcSubject.String,
		StsRegion:     aws.StsRegion.String,
	}
}

func gcpSnapshot(gcp repo.GcpIamCredential, name string) *audit.GcpIamCredentialSnapshot {
	return &audit.GcpIamCredentialSnapshot{
		Name:                      name,
		ImpersonateServiceAccount: gcp.ImpersonateServiceAccount.String,
		WifPoolID:                 gcp.WifPoolID.String,
		WifProviderID:             gcp.WifProviderID.String,
		WifProjectNumber:          gcp.WifProjectNumber.String,
	}
}

func generateExternalID() (string, error) {
	const numBytes = 32
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate external id bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
