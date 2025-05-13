package environments

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/environments"
	srv "github.com/speakeasy-api/gram/gen/http/environments/server"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
)

type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	repo    *repo.Queries
	auth    *auth.Auth
	entries *EnvironmentEntries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption) *Service {
	envRepo := repo.New(db)
	return &Service{
		tracer:  otel.Tracer("github.com/speakeasy-api/gram/internal/environments"),
		logger:  logger,
		db:      db,
		repo:    envRepo,
		auth:    auth.New(logger, db, sessions),
		entries: NewEnvironmentEntries(logger, envRepo, enc),
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

func (s *Service) CreateEnvironment(ctx context.Context, payload *gen.CreateEnvironmentPayload) (*gen.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	slug := conv.ToSlug(payload.Name)

	input := repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Slug:           slug,
		Name:           payload.Name,
		Description:    conv.PtrToPGText(payload.Description),
	}

	environment, err := s.repo.CreateEnvironment(ctx, input)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create environment").Log(ctx, s.logger)
	}

	names := make([]string, len(payload.Entries))
	values := make([]string, len(payload.Entries))
	for i, entry := range payload.Entries {
		names[i] = entry.Name
		values[i] = entry.Value
	}

	rows, err := s.entries.CreateEnvironmentEntries(ctx, repo.CreateEnvironmentEntriesParams{
		EnvironmentID: environment.ID,
		Names:         names,
		Values:        values,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create environment entries").Log(ctx, s.logger)
	}

	entries := make([]*gen.EnvironmentEntry, len(payload.Entries))
	for i, entry := range rows {
		entries[i] = &gen.EnvironmentEntry{
			Name:      entry.Name,
			Value:     entry.Value,
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.Environment{
		ID:             environment.ID.String(),
		OrganizationID: environment.OrganizationID,
		ProjectID:      environment.ProjectID.String(),
		Name:           environment.Name,
		Slug:           types.Slug(environment.Slug),
		Description:    conv.FromPGText[string](environment.Description),
		Entries:        entries,
		CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) ListEnvironments(ctx context.Context, payload *gen.ListEnvironmentsPayload) (*gen.ListEnvironmentsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	environments, err := s.repo.ListEnvironments(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environments").Log(ctx, s.logger)
	}

	var result []*gen.Environment
	for _, environment := range environments {
		entries, err := s.entries.ListEnvironmentEntries(ctx, environment.ID, true)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, s.logger)
		}

		var genEntries []*gen.EnvironmentEntry
		for _, entry := range entries {
			genEntries = append(genEntries, &gen.EnvironmentEntry{
				Name:      entry.Name,
				Value:     entry.Value,
				CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
			})
		}

		result = append(result, &gen.Environment{
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

func (s *Service) UpdateEnvironment(ctx context.Context, payload *gen.UpdateEnvironmentPayload) (*gen.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	environment, err := s.repo.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "environment not found").Log(ctx, s.logger)
	}

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

	_, err = s.repo.UpdateEnvironment(ctx, updateInput)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update environment").Log(ctx, s.logger)
	}

	projectID := *authCtx.ProjectID
	if environment.ProjectID.String() != projectID.String() {
		return nil, oops.E(oops.CodeNotFound, nil, "environment not found")
	}

	for _, updatedEntry := range payload.EntriesToUpdate {
		if err := s.entries.UpdateEnvironmentEntry(ctx, repo.UpsertEnvironmentEntryParams{
			EnvironmentID: environment.ID,
			Name:          updatedEntry.Name,
			Value:         updatedEntry.Value, // This is the actual environment value to update too, do not redact it
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to update environment entry").Log(ctx, s.logger)
		}
	}
	for _, removedEntry := range payload.EntriesToRemove {
		if err := s.entries.DeleteEnvironmentEntry(ctx, repo.DeleteEnvironmentEntryParams{
			EnvironmentID: environment.ID,
			Name:          removedEntry,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to delete environment entry").Log(ctx, s.logger)
		}
	}

	entries, err := s.entries.ListEnvironmentEntries(ctx, environment.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list environment entries").Log(ctx, s.logger)
	}

	genEntries := make([]*gen.EnvironmentEntry, len(entries))
	for i, entry := range entries {
		genEntries[i] = &gen.EnvironmentEntry{
			Name:      entry.Name,
			Value:     entry.Value,
			CreatedAt: entry.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: entry.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.Environment{
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

func (s *Service) DeleteEnvironment(ctx context.Context, payload *gen.DeleteEnvironmentPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	return s.repo.DeleteEnvironment(ctx, repo.DeleteEnvironmentParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
