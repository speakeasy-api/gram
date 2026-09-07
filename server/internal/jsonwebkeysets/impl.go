package jsonwebkeysets

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

	gen "github.com/speakeasy-api/gram/server/gen/http/json_web_key_sets/server"
	gensvc "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// keySetClientListingCap bounds the client_ids GetSetDeletePreflight lists. The
// count it reports alongside is unbounded and authoritative, so a set with more
// referencing clients than this reports a truncated list against a full count.
const keySetClientListingCap = 50

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	auth            *auth.Auth
	authz           *authz.Engine
	audit           *audit.Logger
	gcpIdentity     *gcpauth.Identity
	kmsClients      gcpkms.SigningClientFactory
	productFeatures *productfeatures.Client
	mintLimiter     *ratelimit.Limiter
}

var (
	_ gensvc.Service = (*Service)(nil)
	_ gensvc.Auther  = (*Service)(nil)
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
	kmsClients gcpkms.SigningClientFactory,
	productFeatures *productfeatures.Client,
	mintStore ratelimit.Store,
) *Service {
	logger = logger.With(attr.SlogComponent("jsonwebkeysets"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/jsonwebkeysets"),
		logger:          logger,
		db:              db,
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		audit:           auditLogger,
		gcpIdentity:     gcpIdentity,
		kmsClients:      kmsClients,
		productFeatures: productFeatures,
		mintLimiter: ratelimit.New(mintStore, "jwks-mint",
			ratelimit.PerMinute(mintRatePerMin).WithBurst(mintRateBurst),
			ratelimit.WithMetrics(meterProvider)),
	}
}

// requireOrgAccess resolves the auth context and enforces both gates every
// handler needs: the RBAC scope and the customer-managed-keys entitlement. JWK
// sets exist only to publish keys backed by customer KMS keys, so they sit
// behind the same entitlement as the external keys and credentials beneath
// them — gating only the lower layers would leave the sets reachable by an
// organization that was never sold the feature.
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

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gensvc.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	gen.Mount(
		mux,
		gen.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CreateSet(ctx context.Context, payload *gensvc.CreateSetPayload) (*gensvc.JSONWebKeySet, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	externalKeyID, err := uuid.Parse(payload.ExternalKeyID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external key id").LogError(ctx, logger)
	}

	if err := s.allowMint(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	minted, err := s.mintFromExternalKey(ctx, logger, authCtx.ActiveOrganizationID, externalKeyID)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating key set").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	if err := s.lockBackingKey(ctx, logger, q, authCtx.ActiveOrganizationID, externalKeyID); err != nil {
		return nil, err
	}

	set, err := q.CreateJsonWebKeySet(ctx, repo.CreateJsonWebKeySetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ExternalKeyID:  externalKeyID,
		Name:           name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating key set").LogError(ctx, logger)
	}

	// The first key mints straight to active: a fresh set has no verifier caches
	// to warm, and an active key from the start avoids a window where the set is
	// attached to an issuer but cannot sign.
	key, err := q.CreateJsonWebKey(ctx, repo.CreateJsonWebKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		JsonWebKeySetID: set.ID,
		ExternalKeyID:   externalKeyID,
		State:           keyStateActive,
		Kid:             minted.kid,
		PublicJwk:       minted.publicJWK,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error publishing first key").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeySetCreate(ctx, dbtx, audit.LogJsonWebKeySetCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SetURN:           urn.NewJsonWebKeySet(set.ID),
		SetName:          set.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording key set creation").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeyPublish(ctx, dbtx, keyEvent(authCtx, key, nil, nil)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording first key publication").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving key set").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeySetView(set), nil
}

func (s *Service) UpdateSet(ctx context.Context, payload *gensvc.UpdateSetPayload) (*gensvc.JSONWebKeySet, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key set id").LogError(ctx, logger)
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	externalKeyID, err := uuid.Parse(payload.ExternalKeyID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external key id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating key set").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// The set lock serializes the re-point against an in-flight publish, whose
	// re-verification of the backing key depends on the set row holding still.
	current, err := q.LockJsonWebKeySetForKeyWrite(ctx, repo.LockJsonWebKeySetForKeyWriteParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "key set not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading key set").LogError(ctx, logger)
	}

	if err := s.lockBackingKey(ctx, logger, q, authCtx.ActiveOrganizationID, externalKeyID); err != nil {
		return nil, err
	}

	set, err := q.UpdateJsonWebKeySet(ctx, repo.UpdateJsonWebKeySetParams{
		Name:           name,
		ExternalKeyID:  externalKeyID,
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "key set not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating key set").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeySetUpdate(ctx, dbtx, audit.LogJsonWebKeySetUpdateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SetURN:           urn.NewJsonWebKeySet(set.ID),
		SetName:          set.Name,
		SetSnapshotBefore: &audit.JsonWebKeySetSnapshot{
			Name:          current.Name,
			ExternalKeyID: current.ExternalKeyID.String(),
		},
		SetSnapshotAfter: &audit.JsonWebKeySetSnapshot{
			Name:          set.Name,
			ExternalKeyID: set.ExternalKeyID.String(),
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording key set update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving key set").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeySetView(set), nil
}

func (s *Service) ListSets(ctx context.Context, payload *gensvc.ListSetsPayload) (*gensvc.ListJSONWebKeySetsResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListJsonWebKeySets(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing key sets").LogError(ctx, logger)
	}

	return &gensvc.ListJSONWebKeySetsResult{
		Sets: mv.BuildJsonWebKeySetListView(rows),
	}, nil
}

func (s *Service) GetSet(ctx context.Context, payload *gensvc.GetSetPayload) (*gensvc.JSONWebKeySet, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key set id").LogError(ctx, logger)
	}

	set, err := repo.New(s.db).GetJsonWebKeySet(ctx, repo.GetJsonWebKeySetParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "key set not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading key set").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeySetView(set), nil
}

// GetSetDeletePreflight reports the remote_session_clients still referencing a
// set, so the dashboard can tell an administrator what a delete would break
// before they confirm it.
//
// Unlike the remote_session_client delete preflight, which is informational
// because that delete cascades, this one predicts a real refusal: it counts
// live references over the same predicate DeleteSet refuses on, so a non-zero
// client_count here is exactly the condition that returns a conflict there. A missing set reports zero rather
// than 404, matching DeleteSet's own idempotent treatment of an absent id.
func (s *Service) GetSetDeletePreflight(ctx context.Context, payload *gensvc.GetSetDeletePreflightPayload) (*gensvc.JSONWebKeySetDeletePreflight, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key set id").LogError(ctx, logger)
	}

	summary, err := repo.New(s.db).SummarizeRemoteSessionClientsForJsonWebKeySet(ctx, repo.SummarizeRemoteSessionClientsForJsonWebKeySetParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		JsonWebKeySetID: id,
		LimitValue:      keySetClientListingCap,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading key set references").LogError(ctx, logger)
	}

	return &gensvc.JSONWebKeySetDeletePreflight{
		ClientCount: int(summary.ClientCount),
		ClientIds:   summary.ClientIds,
	}, nil
}

// DeleteSet soft-deletes a set and every key still published in it in one
// transaction, emitting one audit event per withdrawn key before the set's own
// deletion event. A missing id is a no-op so deletes stay idempotent.
func (s *Service) DeleteSet(ctx context.Context, payload *gensvc.DeleteSetPayload) error {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key set id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting key set").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// The set lock is what makes the cascade sound: a concurrent publish also
	// takes it, so a live key can never be inserted into the set between the
	// cascade below and the set's own soft delete.
	_, err = q.LockJsonWebKeySetForKeyWrite(ctx, repo.LockJsonWebKeySetForKeyWriteParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting key set").LogError(ctx, logger)
	}

	// Refuse while a live remote_session_client still signs with this set.
	// Deleting it would leave that client declaring an authentication method it
	// can no longer perform, failing at the counterparty's token endpoint rather
	// than here. The database will not catch it: the composite foreign key is
	// NO ACTION and `deleted` is a generated column, so a soft delete never
	// fires it. Runs under the set lock taken above, which the attach path
	// counterpart-locks FOR SHARE, so the count cannot go stale between here and
	// the commit.
	referencing, err := q.CountRemoteSessionClientsForJsonWebKeySet(ctx, repo.CountRemoteSessionClientsForJsonWebKeySetParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		JsonWebKeySetID: id,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error checking key set references").LogError(ctx, logger)
	}
	if referencing > 0 {
		return oops.E(oops.CodeConflict, nil, "key set is still in use by a remote session client; detach it there first")
	}

	withdrawn, err := q.CascadeSoftDeleteJsonWebKeys(ctx, repo.CascadeSoftDeleteJsonWebKeysParams{
		JsonWebKeySetID: id,
		OrganizationID:  authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error withdrawing key set keys").LogError(ctx, logger)
	}

	deleted, err := q.SoftDeleteJsonWebKeySet(ctx, repo.SoftDeleteJsonWebKeySetParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting key set").LogError(ctx, logger)
	}

	for _, key := range withdrawn {
		if err := s.audit.LogJsonWebKeyDelete(ctx, dbtx, keyEvent(authCtx, key, nil, nil)); err != nil {
			return oops.E(oops.CodeUnexpected, err, "error recording key withdrawal").LogError(ctx, logger)
		}
	}

	if err := s.audit.LogJsonWebKeySetDelete(ctx, dbtx, audit.LogJsonWebKeySetDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SetURN:           urn.NewJsonWebKeySet(deleted.ID),
		SetName:          deleted.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error recording key set deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving key set deletion").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) ListKeys(ctx context.Context, payload *gensvc.ListKeysPayload) (*gensvc.ListJSONWebKeysResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	setID, err := uuid.Parse(payload.SetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key set id").LogError(ctx, logger)
	}

	q := repo.New(s.db)

	_, err = q.GetJsonWebKeySet(ctx, repo.GetJsonWebKeySetParams{
		ID:             setID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "key set not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading key set").LogError(ctx, logger)
	}

	rows, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{
		JsonWebKeySetID: setID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		IncludeRevoked:  payload.IncludeRevoked,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing keys").LogError(ctx, logger)
	}

	return &gensvc.ListJSONWebKeysResult{
		Keys: mv.BuildJsonWebKeyListView(rows),
	}, nil
}

// lockBackingKey locks the external key row FOR SHARE for the rest of the
// transaction and confirms it is usable as a set's backing key. The lock is the
// counterpart to the FOR UPDATE the externalkeys delete guard takes, so a
// backing reference can never be committed against a key whose delete is in
// flight. AWS keys are refused here — the sole provider gate on the update
// path — because no awskms client exists yet, so a set backed by one could
// never publish.
func (s *Service) lockBackingKey(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string, externalKeyID uuid.UUID) error {
	key, err := q.LockExternalKeyForJwksWrite(ctx, repo.LockExternalKeyForJwksWriteParams{
		ID:             externalKeyID,
		OrganizationID: conv.ToPGText(organizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeBadRequest, nil, "external key not found").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error loading external key").LogError(ctx, logger)
	}

	if key.Provider != externalKeyProviderGcpKms {
		return oops.E(oops.CodeBadRequest, nil, "AWS KMS keys cannot back a JSON Web Key Set yet; choose a GCP KMS key").LogError(ctx, logger)
	}

	return nil
}
