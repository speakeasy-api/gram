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
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
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
	logger = logger.With(attr.SlogComponent("externalcredentials"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/externalcredentials"),
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

func (s *Service) CreateAwsIamCredential(ctx context.Context, payload *gen.CreateAwsIamCredentialPayload) (*gen.AwsIamCredential, error) {
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

	cols, err := s.resolveGcpColumns(ctx, logger, gcpCredentialInput{
		impersonateServiceAccount: payload.ImpersonateServiceAccount,
		wifPoolID:                 payload.WifPoolID,
		wifProviderID:             payload.WifProviderID,
		wifProjectNumber:          payload.WifProjectNumber,
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
		ImpersonateServiceAccount: cols.ImpersonateServiceAccount,
		WifPoolID:                 cols.WifPoolID,
		WifProviderID:             cols.WifProviderID,
		WifProjectNumber:          cols.WifProjectNumber,
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

	return mv.BuildGcpIamCredentialView(ec, gcp), nil
}

func (s *Service) UpdateGcpIamCredential(ctx context.Context, payload *gen.UpdateGcpIamCredentialPayload) (*gen.GcpIamCredential, error) {
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

	cols, err := s.resolveGcpColumns(ctx, logger, gcpCredentialInput{
		impersonateServiceAccount: payload.ImpersonateServiceAccount,
		wifPoolID:                 payload.WifPoolID,
		wifProviderID:             payload.WifProviderID,
		wifProjectNumber:          payload.WifProjectNumber,
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

	gcp, err := q.UpdateGcpIamCredential(ctx, repo.UpdateGcpIamCredentialParams{
		ImpersonateServiceAccount: cols.ImpersonateServiceAccount,
		WifPoolID:                 cols.WifPoolID,
		WifProviderID:             cols.WifProviderID,
		WifProjectNumber:          cols.WifProjectNumber,
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

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

func (s *Service) DeleteAwsIamCredential(ctx context.Context, payload *gen.DeleteAwsIamCredentialPayload) error {
	return s.deleteExternalCredential(ctx, "aws_iam", payload.ID)
}

func (s *Service) DeleteGcpIamCredential(ctx context.Context, payload *gen.DeleteGcpIamCredentialPayload) error {
	return s.deleteExternalCredential(ctx, "gcp_iam", payload.ID)
}

// deleteExternalCredential soft-deletes a credential scoped to the given
// provider and emits the provider-specific audit event. A missing (or
// wrong-provider) id is a no-op so deletes stay idempotent.
func (s *Service) deleteExternalCredential(ctx context.Context, provider, rawID string) error {
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
		return oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

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
