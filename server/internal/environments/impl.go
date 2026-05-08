package environments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	srv "github.com/speakeasy-api/gram/server/gen/http/environments/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	repo    *repo.Queries
	auth    *auth.Auth
	authz   *authz.Engine
	entries *EnvironmentEntries
	audit   *audit.Logger
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	enc *encryption.Client,
	authz *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("environments"))
	envRepo := repo.New(db)
	mcpMetadataRepo := mcpmetadata_repo.New(db)

	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/environments"),
		logger:  logger,
		db:      db,
		repo:    envRepo,
		auth:    auth.New(logger, db, sessions, authz),
		authz:   authz,
		entries: NewEnvironmentEntries(logger, db, enc, mcpMetadataRepo),
		audit:   auditLogger,
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

// computeValueHash computes a SHA-256 hash of the given value.
// This hash is used to identify matching values across environments without exposing the actual value.
func computeValueHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (s *Service) CreateEnvironment(ctx context.Context, payload *gen.CreateEnvironmentPayload) (*types.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	slug := conv.ToSlug(payload.Name)

	input := repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Slug:           slug,
		Name:           payload.Name,
		Description:    conv.PtrToPGText(payload.Description),
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access environments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	er := s.repo.WithTx(dbtx)
	entriesRepo := NewEnvironmentEntries(logger, dbtx, s.entries.enc, s.entries.mcpMetadataRepo)

	environment, err := er.CreateEnvironment(ctx, input)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create environment").Log(ctx, logger)
	}

	names := make([]string, len(payload.Entries))
	values := make([]string, len(payload.Entries))
	for i, entry := range payload.Entries {
		names[i] = entry.Name
		values[i] = entry.Value
	}

	rows, err := entriesRepo.CreateEnvironmentEntries(ctx, repo.CreateEnvironmentEntriesParams{
		EnvironmentID: environment.ID,
		Names:         names,
		Values:        values,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create environment entries").Log(ctx, logger)
	}

	environmentView := buildEnvironmentView(environment, rows)

	if err := s.audit.LogEnvironmentCreate(ctx, dbtx, audit.LogEnvironmentCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		EnvironmentURN:   urn.NewEnvironment(environment.ID),
		EnvironmentName:  environment.Name,
		EnvironmentSlug:  environment.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to log environment creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create environment").Log(ctx, logger)
	}

	return environmentView, nil
}

func (s *Service) ListEnvironments(ctx context.Context, payload *gen.ListEnvironmentsPayload) (*gen.ListEnvironmentsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	environments, err := s.repo.ListEnvironments(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environments").Log(ctx, s.logger)
	}

	var result []*types.Environment
	for _, environment := range environments {
		entries, err := s.entries.ListEnvironmentEntries(ctx, *authCtx.ProjectID, environment.ID, true)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, s.logger)
		}

		var genEntries []*types.EnvironmentEntry
		for _, entry := range entries {
			genEntries = append(genEntries, &types.EnvironmentEntry{
				Name:      entry.Name,
				Value:     entry.Value,
				ValueHash: computeValueHash(entry.Value),
				CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
			})
		}

		result = append(result, &types.Environment{
			ID:             environment.ID.String(),
			OrganizationID: environment.OrganizationID,
			ProjectID:      environment.ProjectID.String(),
			Name:           environment.Name,
			Slug:           types.Slug(environment.Slug),
			Description:    conv.FromPGText[string](environment.Description),
			Entries:        genEntries,
			CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListEnvironmentsResult{Environments: result}, nil

}

func (s *Service) UpdateEnvironment(ctx context.Context, payload *gen.UpdateEnvironmentPayload) (*types.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogEnvironmentSlug(string(payload.Slug)))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access environments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	er := s.repo.WithTx(dbtx)
	entriesRepo := NewEnvironmentEntries(logger, dbtx, s.entries.enc, s.entries.mcpMetadataRepo)

	environment, err := er.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "environment not found").Log(ctx, logger)
	}

	beforeEntries, err := entriesRepo.ListEnvironmentEntries(ctx, *authCtx.ProjectID, environment.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, logger)
	}
	beforeView := buildEnvironmentView(environment, beforeEntries)

	updateInput := repo.UpdateEnvironmentParams{
		Slug:        conv.ToLower(payload.Slug),
		ProjectID:   *authCtx.ProjectID,
		Name:        environment.Name,
		Description: environment.Description,
	}
	if payload.Name != nil {
		updateInput.Name = *payload.Name
	}

	if payload.Description != nil {
		updateInput.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	updatedEnvironment, err := er.UpdateEnvironment(ctx, updateInput)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update environment").Log(ctx, logger)
	}

	projectID := *authCtx.ProjectID
	if environment.ProjectID.String() != projectID.String() {
		return nil, oops.E(oops.CodeNotFound, nil, "environment not found")
	}

	for _, updatedEntry := range payload.EntriesToUpdate {
		if err := entriesRepo.UpdateEnvironmentEntry(ctx, repo.UpsertEnvironmentEntryParams{
			EnvironmentID: environment.ID,
			Name:          updatedEntry.Name,
			Value:         updatedEntry.Value, // This is the actual environment value to update too, do not redact it
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to update environment entry").Log(ctx, logger)
		}
	}
	for _, removedEntry := range payload.EntriesToRemove {
		if err := entriesRepo.DeleteEnvironmentEntry(ctx, repo.DeleteEnvironmentEntryParams{
			EnvironmentID: environment.ID,
			Name:          removedEntry,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to delete environment entry").Log(ctx, logger)
		}
	}

	// Re-fetch environment to get the latest state after all updates
	environment, err = er.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to re-fetch environment").Log(ctx, logger)
	}

	entries, err := entriesRepo.ListEnvironmentEntries(ctx, projectID, environment.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, logger)
	}

	afterView := buildEnvironmentView(environment, entries)

	if err := s.audit.LogEnvironmentUpdate(ctx, dbtx, audit.LogEnvironmentUpdateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		EnvironmentURN:            urn.NewEnvironment(updatedEnvironment.ID),
		EnvironmentName:           updatedEnvironment.Name,
		EnvironmentSlug:           updatedEnvironment.Slug,
		EnvironmentSnapshotBefore: beforeView,
		EnvironmentSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to log environment update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update environment").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) CloneEnvironment(ctx context.Context, payload *gen.CloneEnvironmentPayload) (*types.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogEnvironmentSlug(string(payload.Slug)))

	// Source lookup is project-bounded at the SQL layer (ProjectID parameter), so
	// cross-project leakage isn't possible even before authz runs. We need the
	// source env id to express the authz check at the right granularity.
	sourceEnv, err := s.repo.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "environment not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch source environment").Log(ctx, logger)
	}

	// Authz: env:write on the source env with project_id as a constraining
	// dimension. env:write implies env:read via scope expansion, so this single
	// check covers both reading the source and producing the destination. Future
	// per-env granularity is additive at the role layer (per-env grants instead
	// of wildcard).
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeEnvironmentWrite,
		ResourceKind: "environment",
		ResourceID:   sourceEnv.ID.String(),
		Dimensions:   map[string]string{"project_id": authCtx.ProjectID.String()},
	}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access environments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	er := s.repo.WithTx(dbtx)
	entriesRepo := NewEnvironmentEntries(logger, dbtx, s.entries.enc, s.entries.mcpMetadataRepo)

	newName := payload.NewName
	newSlug := conv.ToSlug(newName)

	newEnv, err := er.CreateEnvironment(ctx, repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           newName,
		Slug:           newSlug,
		Description:    sourceEnv.Description,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "an environment with this name already exists in this project")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create cloned environment").Log(ctx, logger)
	}

	copyValues := payload.CopyValues != nil && *payload.CopyValues
	if copyValues {
		if err := er.CloneEnvironmentEntriesWithValues(ctx, repo.CloneEnvironmentEntriesWithValuesParams{
			NewEnvironmentID:    newEnv.ID,
			SourceEnvironmentID: sourceEnv.ID,
			ProjectID:           *authCtx.ProjectID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to clone environment entries").Log(ctx, logger)
		}
	} else {
		placeholder, err := s.entries.enc.Encrypt([]byte(""))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to prepare placeholder value").Log(ctx, logger)
		}
		if err := er.CloneEnvironmentEntryNames(ctx, repo.CloneEnvironmentEntryNamesParams{
			NewEnvironmentID:    newEnv.ID,
			SourceEnvironmentID: sourceEnv.ID,
			PlaceholderValue:    placeholder,
			ProjectID:           *authCtx.ProjectID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to clone environment entry names").Log(ctx, logger)
		}
	}

	entries, err := entriesRepo.ListEnvironmentEntries(ctx, *authCtx.ProjectID, newEnv.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list cloned environment entries").Log(ctx, logger)
	}

	if err := s.audit.LogEnvironmentCreate(ctx, dbtx, audit.LogEnvironmentCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		EnvironmentURN:   urn.NewEnvironment(newEnv.ID),
		EnvironmentName:  newEnv.Name,
		EnvironmentSlug:  newEnv.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to log environment clone").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to clone environment").Log(ctx, logger)
	}

	return buildEnvironmentView(newEnv, entries), nil
}

func (s *Service) DeleteEnvironment(ctx context.Context, payload *gen.DeleteEnvironmentPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogEnvironmentSlug(string(payload.Slug)))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access environments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	er := s.repo.WithTx(dbtx)

	deleted, err := er.DeleteEnvironment(ctx, repo.DeleteEnvironmentParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "failed to delete environment").Log(ctx, logger)
	}

	if err := s.audit.LogEnvironmentDelete(ctx, dbtx, audit.LogEnvironmentDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		EnvironmentURN:   urn.NewEnvironment(deleted.ID),
		EnvironmentName:  deleted.Name,
		EnvironmentSlug:  deleted.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to save environment delete audit log event").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete environment").Log(ctx, logger)
	}

	return nil
}

func buildEnvironmentEntries(entries []repo.EnvironmentEntry) []*types.EnvironmentEntry {
	genEntries := make([]*types.EnvironmentEntry, len(entries))
	for i, entry := range entries {
		genEntries[i] = &types.EnvironmentEntry{
			Name:      entry.Name,
			Value:     entry.Value,
			ValueHash: computeValueHash(entry.Value),
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return genEntries
}

func buildEnvironmentView(environment repo.Environment, entries []repo.EnvironmentEntry) *types.Environment {
	return &types.Environment{
		ID:             environment.ID.String(),
		OrganizationID: environment.OrganizationID,
		ProjectID:      environment.ProjectID.String(),
		Name:           environment.Name,
		Slug:           types.Slug(environment.Slug),
		Description:    conv.FromPGText[string](environment.Description),
		Entries:        buildEnvironmentEntries(entries),
		CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func (s *Service) SetSourceEnvironmentLink(ctx context.Context, payload *gen.SetSourceEnvironmentLinkPayload) (*gen.SourceEnvironmentLink, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	environmentID, err := uuid.Parse(payload.EnvironmentID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid environment_id").Log(ctx, s.logger)
	}

	// Verify the environment exists and belongs to the project
	_, err = s.repo.GetEnvironmentByID(ctx, repo.GetEnvironmentByIDParams{
		ID:        environmentID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "environment not found").Log(ctx, s.logger)
	}

	link, err := s.repo.SetSourceEnvironment(ctx, repo.SetSourceEnvironmentParams{
		SourceKind:    string(payload.SourceKind),
		SourceSlug:    payload.SourceSlug,
		ProjectID:     *authCtx.ProjectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to set source environment link").Log(ctx, s.logger)
	}

	return &gen.SourceEnvironmentLink{
		ID:            link.ID.String(),
		SourceKind:    gen.SourceKind(link.SourceKind),
		SourceSlug:    link.SourceSlug,
		EnvironmentID: link.EnvironmentID.String(),
	}, nil
}

func (s *Service) DeleteSourceEnvironmentLink(ctx context.Context, payload *gen.DeleteSourceEnvironmentLinkPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	err := s.repo.DeleteSourceEnvironment(ctx, repo.DeleteSourceEnvironmentParams{
		SourceKind: string(payload.SourceKind),
		SourceSlug: payload.SourceSlug,
		ProjectID:  *authCtx.ProjectID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "failed to delete source environment link").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) GetSourceEnvironment(ctx context.Context, payload *gen.GetSourceEnvironmentPayload) (*types.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	environment, err := s.repo.GetEnvironmentForSource(ctx, repo.GetEnvironmentForSourceParams{
		SourceKind: string(payload.SourceKind),
		SourceSlug: payload.SourceSlug,
		ProjectID:  *authCtx.ProjectID,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "environment not found for source").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment for source").Log(ctx, s.logger)
	}

	entries, err := s.entries.ListEnvironmentEntries(ctx, *authCtx.ProjectID, environment.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, s.logger)
	}

	genEntries := make([]*types.EnvironmentEntry, len(entries))
	for i, entry := range entries {
		genEntries[i] = &types.EnvironmentEntry{
			Name:      entry.Name,
			Value:     entry.Value,
			ValueHash: computeValueHash(entry.Value),
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &types.Environment{
		ID:             environment.ID.String(),
		OrganizationID: environment.OrganizationID,
		ProjectID:      environment.ProjectID.String(),
		Name:           environment.Name,
		Slug:           types.Slug(environment.Slug),
		Description:    conv.FromPGText[string](environment.Description),
		Entries:        genEntries,
		CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) SetToolsetEnvironmentLink(ctx context.Context, payload *gen.SetToolsetEnvironmentLinkPayload) (*gen.ToolsetEnvironmentLink, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	environmentID, err := uuid.Parse(payload.EnvironmentID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid environment_id").Log(ctx, s.logger)
	}

	// Verify the environment exists and belongs to the project
	_, err = s.repo.GetEnvironmentByID(ctx, repo.GetEnvironmentByIDParams{
		ID:        environmentID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "environment not found").Log(ctx, s.logger)
	}

	link, err := s.repo.SetToolsetEnvironment(ctx, repo.SetToolsetEnvironmentParams{
		ToolsetID:     toolsetID,
		ProjectID:     *authCtx.ProjectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to set toolset environment link").Log(ctx, s.logger)
	}

	return &gen.ToolsetEnvironmentLink{
		ID:            link.ID.String(),
		ToolsetID:     link.ToolsetID.String(),
		EnvironmentID: link.EnvironmentID.String(),
	}, nil
}

func (s *Service) DeleteToolsetEnvironmentLink(ctx context.Context, payload *gen.DeleteToolsetEnvironmentLinkPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	err = s.repo.DeleteToolsetEnvironment(ctx, repo.DeleteToolsetEnvironmentParams{
		ToolsetID: toolsetID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "failed to delete toolset environment link").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) GetToolsetEnvironment(ctx context.Context, payload *gen.GetToolsetEnvironmentPayload) (*types.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	environment, err := s.repo.GetEnvironmentForToolset(ctx, repo.GetEnvironmentForToolsetParams{
		ToolsetID: toolsetID,
		ProjectID: *authCtx.ProjectID,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "environment not found for toolset").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment for toolset").Log(ctx, s.logger)
	}

	entries, err := s.entries.ListEnvironmentEntries(ctx, *authCtx.ProjectID, environment.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, s.logger)
	}

	genEntries := make([]*types.EnvironmentEntry, len(entries))
	for i, entry := range entries {
		genEntries[i] = &types.EnvironmentEntry{
			Name:      entry.Name,
			Value:     entry.Value,
			ValueHash: computeValueHash(entry.Value),
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &types.Environment{
		ID:             environment.ID.String(),
		OrganizationID: environment.OrganizationID,
		ProjectID:      environment.ProjectID.String(),
		Name:           environment.Name,
		Slug:           types.Slug(environment.Slug),
		Description:    conv.FromPGText[string](environment.Description),
		Entries:        genEntries,
		CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
