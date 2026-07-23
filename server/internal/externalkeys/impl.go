package externalkeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	srv "github.com/speakeasy-api/gram/server/gen/http/external_keys/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("externalkeys"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalkeys"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
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

func (s *Service) CreateAwsKmsKey(ctx context.Context, payload *gen.CreateAwsKmsKeyPayload) (*gen.AwsKmsKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	keyArn := strings.TrimSpace(payload.KeyArn)
	if keyArn == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "key_arn is required").LogError(ctx, logger)
	}

	credentialID, err := uuid.Parse(payload.ExternalCredentialID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external credential id").LogError(ctx, logger)
	}

	// customer_grant_reference is accepted as-is. It is only meaningful for the
	// Family-B (ambient / key-policy grant) model; enforcing that the backing
	// credential is actually ambient is deferred to AGE-2869's grant-consumption
	// path, which owns the credential auth-mode derivation.
	grantReference := conv.PtrToPGTextTrimmed(payload.CustomerGrantReference)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	if err := s.validateBackingCredential(ctx, logger, q, authCtx.ActiveOrganizationID, credentialID, "aws_kms"); err != nil {
		return nil, err
	}

	ek, err := q.CreateExternalKey(ctx, repo.CreateExternalKeyParams{
		OrganizationID:         conv.ToPGText(authCtx.ActiveOrganizationID),
		ExternalCredentialID:   credentialID,
		Provider:               "aws_kms",
		Algorithm:              payload.Algorithm,
		Name:                   name,
		CustomerGrantReference: grantReference,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external key").LogError(ctx, logger)
	}

	aws, err := q.CreateAwsKmsKey(ctx, repo.CreateAwsKmsKeyParams{
		ExternalKeyID: ek.ID,
		KeyArn:        keyArn,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating aws kms key").LogError(ctx, logger)
	}

	if err := s.audit.LogAwsKmsKeyCreate(ctx, dbtx, audit.LogAwsKmsKeyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAwsKmsKey(ek.ID),
		KeyName:          ek.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording aws kms key creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external key").LogError(ctx, logger)
	}

	return mv.BuildAwsKmsKeyView(ek, aws), nil
}

func (s *Service) UpdateAwsKmsKey(ctx context.Context, payload *gen.UpdateAwsKmsKeyPayload) (*gen.AwsKmsKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	keyArn := strings.TrimSpace(payload.KeyArn)
	if keyArn == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "key_arn is required").LogError(ctx, logger)
	}

	credentialID, err := uuid.Parse(payload.ExternalCredentialID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external credential id").LogError(ctx, logger)
	}

	grantReference := conv.PtrToPGTextTrimmed(payload.CustomerGrantReference)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	current, err := q.GetAwsKmsKey(ctx, repo.GetAwsKmsKeyParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "aws kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading aws kms key").LogError(ctx, logger)
	}

	if err := s.validateBackingCredential(ctx, logger, q, authCtx.ActiveOrganizationID, credentialID, "aws_kms"); err != nil {
		return nil, err
	}

	ek, err := q.UpdateExternalKey(ctx, repo.UpdateExternalKeyParams{
		ExternalCredentialID:   credentialID,
		Algorithm:              payload.Algorithm,
		Name:                   name,
		CustomerGrantReference: grantReference,
		ID:                     id,
		OrganizationID:         conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "aws kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external key").LogError(ctx, logger)
	}

	aws, err := q.UpdateAwsKmsKey(ctx, repo.UpdateAwsKmsKeyParams{
		KeyArn:        keyArn,
		ExternalKeyID: id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating aws kms key").LogError(ctx, logger)
	}

	if err := s.audit.LogAwsKmsKeyUpdate(ctx, dbtx, audit.LogAwsKmsKeyUpdateEvent{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		KeyURN:            urn.NewAwsKmsKey(ek.ID),
		KeyName:           ek.Name,
		KeySnapshotBefore: awsSnapshot(current.ExternalKey, current.AwsKmsKey),
		KeySnapshotAfter:  awsSnapshot(ek, aws),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording aws kms key update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external key").LogError(ctx, logger)
	}

	return mv.BuildAwsKmsKeyView(ek, aws), nil
}

func (s *Service) CreateGcpKmsKey(ctx context.Context, payload *gen.CreateGcpKmsKeyPayload) (*gen.GcpKmsKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	resourceName := strings.TrimSpace(payload.ResourceName)
	if resourceName == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "resource_name is required").LogError(ctx, logger)
	}

	credentialID, err := uuid.Parse(payload.ExternalCredentialID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external credential id").LogError(ctx, logger)
	}

	// customer_grant_reference is accepted as-is. It is only meaningful for the
	// Family-B (ambient / key-policy grant) model; enforcing that the backing
	// credential is actually ambient is deferred to AGE-2869's grant-consumption
	// path, which owns the credential auth-mode derivation.
	grantReference := conv.PtrToPGTextTrimmed(payload.CustomerGrantReference)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	if err := s.validateBackingCredential(ctx, logger, q, authCtx.ActiveOrganizationID, credentialID, "gcp_kms"); err != nil {
		return nil, err
	}

	ek, err := q.CreateExternalKey(ctx, repo.CreateExternalKeyParams{
		OrganizationID:         conv.ToPGText(authCtx.ActiveOrganizationID),
		ExternalCredentialID:   credentialID,
		Provider:               "gcp_kms",
		Algorithm:              payload.Algorithm,
		Name:                   name,
		CustomerGrantReference: grantReference,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating external key").LogError(ctx, logger)
	}

	gcp, err := q.CreateGcpKmsKey(ctx, repo.CreateGcpKmsKeyParams{
		ExternalKeyID: ek.ID,
		ResourceName:  resourceName,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating gcp kms key").LogError(ctx, logger)
	}

	if err := s.audit.LogGcpKmsKeyCreate(ctx, dbtx, audit.LogGcpKmsKeyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewGcpKmsKey(ek.ID),
		KeyName:          ek.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording gcp kms key creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external key").LogError(ctx, logger)
	}

	return mv.BuildGcpKmsKeyView(ek, gcp), nil
}

func (s *Service) UpdateGcpKmsKey(ctx context.Context, payload *gen.UpdateGcpKmsKeyPayload) (*gen.GcpKmsKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	resourceName := strings.TrimSpace(payload.ResourceName)
	if resourceName == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "resource_name is required").LogError(ctx, logger)
	}

	credentialID, err := uuid.Parse(payload.ExternalCredentialID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid external credential id").LogError(ctx, logger)
	}

	grantReference := conv.PtrToPGTextTrimmed(payload.CustomerGrantReference)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	current, err := q.GetGcpKmsKey(ctx, repo.GetGcpKmsKeyParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading gcp kms key").LogError(ctx, logger)
	}

	if err := s.validateBackingCredential(ctx, logger, q, authCtx.ActiveOrganizationID, credentialID, "gcp_kms"); err != nil {
		return nil, err
	}

	ek, err := q.UpdateExternalKey(ctx, repo.UpdateExternalKeyParams{
		ExternalCredentialID:   credentialID,
		Algorithm:              payload.Algorithm,
		Name:                   name,
		CustomerGrantReference: grantReference,
		ID:                     id,
		OrganizationID:         conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating external key").LogError(ctx, logger)
	}

	gcp, err := q.UpdateGcpKmsKey(ctx, repo.UpdateGcpKmsKeyParams{
		ResourceName:  resourceName,
		ExternalKeyID: id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating gcp kms key").LogError(ctx, logger)
	}

	if err := s.audit.LogGcpKmsKeyUpdate(ctx, dbtx, audit.LogGcpKmsKeyUpdateEvent{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		KeyURN:            urn.NewGcpKmsKey(ek.ID),
		KeyName:           ek.Name,
		KeySnapshotBefore: gcpSnapshot(current.ExternalKey, current.GcpKmsKey),
		KeySnapshotAfter:  gcpSnapshot(ek, gcp),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording gcp kms key update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving external key").LogError(ctx, logger)
	}

	return mv.BuildGcpKmsKeyView(ek, gcp), nil
}

func (s *Service) ListExternalKeys(ctx context.Context, payload *gen.ListExternalKeysPayload) (*gen.ListExternalKeysResult, error) {
	provider := pgtype.Text{String: "", Valid: false}
	if payload.Provider != nil {
		provider = conv.ToPGText(*payload.Provider)
	}

	return s.listKeys(ctx, provider)
}

func (s *Service) ListAwsKmsKeys(ctx context.Context, payload *gen.ListAwsKmsKeysPayload) (*gen.ListExternalKeysResult, error) {
	return s.listKeys(ctx, conv.ToPGText("aws_kms"))
}

func (s *Service) ListGcpKmsKeys(ctx context.Context, payload *gen.ListGcpKmsKeysPayload) (*gen.ListExternalKeysResult, error) {
	return s.listKeys(ctx, conv.ToPGText("gcp_kms"))
}

// listKeys returns the org's key summaries, optionally filtered to a single
// provider (invalid pgtype.Text = no filter).
func (s *Service) listKeys(ctx context.Context, provider pgtype.Text) (*gen.ListExternalKeysResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	rows, err := repo.New(s.db).ListExternalKeys(ctx, repo.ListExternalKeysParams{
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       provider,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing external keys").LogError(ctx, logger)
	}

	return &gen.ListExternalKeysResult{
		Keys: mv.BuildExternalKeySummaryListView(rows),
	}, nil
}

func (s *Service) GetAwsKmsKey(ctx context.Context, payload *gen.GetAwsKmsKeyPayload) (*gen.AwsKmsKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetAwsKmsKey(ctx, repo.GetAwsKmsKeyParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "aws kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading aws kms key").LogError(ctx, logger)
	}

	return mv.BuildAwsKmsKeyView(row.ExternalKey, row.AwsKmsKey), nil
}

func (s *Service) GetGcpKmsKey(ctx context.Context, payload *gen.GetGcpKmsKeyPayload) (*gen.GcpKmsKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetGcpKmsKey(ctx, repo.GetGcpKmsKeyParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading gcp kms key").LogError(ctx, logger)
	}

	return mv.BuildGcpKmsKeyView(row.ExternalKey, row.GcpKmsKey), nil
}

func (s *Service) DeleteAwsKmsKey(ctx context.Context, payload *gen.DeleteAwsKmsKeyPayload) error {
	return s.deleteExternalKey(ctx, "aws_kms", payload.ID)
}

func (s *Service) DeleteGcpKmsKey(ctx context.Context, payload *gen.DeleteGcpKmsKeyPayload) error {
	return s.deleteExternalKey(ctx, "gcp_kms", payload.ID)
}

// deleteExternalKey soft-deletes a key scoped to the given provider and emits the
// provider-specific audit event. A missing (or wrong-provider) id is a no-op so
// deletes stay idempotent.
//
// Delete is a soft delete: the external_keys row and its id are preserved. A
// caller changes a key's provider by delete + recreate, which mints a NEW
// external_keys.id. Once json_web_key_sets / json_web_keys reference
// external_keys(organization_id, id) (a foreign key with no ON DELETE, i.e.
// RESTRICT), such a swap orphans the old key material under the previous id and
// the new key must be re-pointed by the JWKS layer (AGE-2869 / AGE-2870).
// Nothing references external_keys today, so this is safe for now.
func (s *Service) deleteExternalKey(ctx context.Context, provider, rawID string) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(rawID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting external key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	deleted, err := q.SoftDeleteExternalKey(ctx, repo.SoftDeleteExternalKeyParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Provider:       provider,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting external key").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	var auditErr error
	switch deleted.Provider {
	case "aws_kms":
		auditErr = s.audit.LogAwsKmsKeyDelete(ctx, dbtx, audit.LogAwsKmsKeyDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			KeyURN:           urn.NewAwsKmsKey(deleted.ID),
			KeyName:          deleted.Name,
		})
	case "gcp_kms":
		auditErr = s.audit.LogGcpKmsKeyDelete(ctx, dbtx, audit.LogGcpKmsKeyDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			KeyURN:           urn.NewGcpKmsKey(deleted.ID),
			KeyName:          deleted.Name,
		})
	default:
		auditErr = fmt.Errorf("unexpected external key provider: %s", deleted.Provider)
	}
	if auditErr != nil {
		return oops.E(oops.CodeUnexpected, auditErr, "error recording external key deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving external key deletion").LogError(ctx, logger)
	}

	return nil
}

// validateBackingCredential confirms the referenced external credential belongs
// to the organization and is of the cloud family the key provider requires (an
// aws_kms key requires an aws_iam credential; a gcp_kms key requires a gcp_iam
// credential). It runs inside the key write transaction so a concurrent
// credential soft-delete cannot slip a live key past the check
// (external_credentials.deleted is a generated column, so the foreign key never
// fires on soft delete).
func (s *Service) validateBackingCredential(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string, credentialID uuid.UUID, keyProvider string) error {
	want, ok := credentialProviderForKey(keyProvider)
	if !ok {
		return oops.E(oops.CodeUnexpected, fmt.Errorf("unexpected key provider: %s", keyProvider), "unexpected key provider").LogError(ctx, logger)
	}

	got, err := q.GetExternalCredentialProviderForKey(ctx, repo.GetExternalCredentialProviderForKeyParams{
		ExternalCredentialID: credentialID,
		OrganizationID:       conv.ToPGText(organizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeBadRequest, nil, "external credential not found").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error loading external credential").LogError(ctx, logger)
	}

	if got != want {
		return oops.E(oops.CodeBadRequest, nil, "external credential provider %q does not match key provider %q (expected %q)", got, keyProvider, want).LogError(ctx, logger)
	}

	return nil
}

// credentialProviderForKey returns the external_credentials provider a key of the
// given key provider must be backed by.
func credentialProviderForKey(keyProvider string) (string, bool) {
	switch keyProvider {
	case "aws_kms":
		return "aws_iam", true
	case "gcp_kms":
		return "gcp_iam", true
	default:
		return "", false
	}
}

func awsSnapshot(ek repo.ExternalKey, aws repo.AwsKmsKey) *audit.AwsKmsKeySnapshot {
	return &audit.AwsKmsKeySnapshot{
		Name:                   ek.Name,
		ExternalCredentialID:   ek.ExternalCredentialID.String(),
		Algorithm:              ek.Algorithm,
		CustomerGrantReference: ek.CustomerGrantReference.String,
		KeyArn:                 aws.KeyArn,
	}
}

func gcpSnapshot(ek repo.ExternalKey, gcp repo.GcpKmsKey) *audit.GcpKmsKeySnapshot {
	return &audit.GcpKmsKeySnapshot{
		Name:                   ek.Name,
		ExternalCredentialID:   ek.ExternalCredentialID.String(),
		Algorithm:              ek.Algorithm,
		CustomerGrantReference: ek.CustomerGrantReference.String,
		ResourceName:           gcp.ResourceName,
	}
}
