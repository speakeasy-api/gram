package environments

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/environments"
	srv "github.com/speakeasy-api/gram/gen/http/environments/server"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/environments/repo"
)

type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), auth: auth.New(logger, db, redisClient)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) CreateEnvironment(ctx context.Context, payload *gen.CreateEnvironmentPayload) (*gen.Environment, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("auth not found in context")
	}

	slug := conv.ToSlug(payload.Name)

	input := repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Slug:           slug,
		Name:           payload.Name,
	}

	if payload.Description != nil {
		input.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	environment, err := s.repo.CreateEnvironment(ctx, input)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(payload.Entries))
	values := make([]string, len(payload.Entries))
	for i, entry := range payload.Entries {
		names[i] = entry.Name
		values[i] = entry.Value
	}

	_, err = s.repo.CreateEnvironmentEntries(ctx, repo.CreateEnvironmentEntriesParams{
		EnvironmentID: environment.ID,
		Names:         names,
		Values:        values,
	})
	if err != nil {
		return nil, err
	}

	entries := make([]*gen.EnvironmentEntry, len(payload.Entries))
	for i, entry := range payload.Entries {
		entries[i] = &gen.EnvironmentEntry{
			Name:  entry.Name,
			Value: entry.Value,
		}
	}

	return &gen.Environment{
		ID:             environment.ID.String(),
		OrganizationID: environment.OrganizationID,
		ProjectID:      environment.ProjectID.String(),
		Name:           environment.Name,
		Slug:           environment.Slug,
		Description:    conv.FromPGText(environment.Description),
		Entries:        entries,
		CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) ListEnvironments(ctx context.Context, payload *gen.ListEnvironmentsPayload) (*gen.ListEnvironmentsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("auth not found in context")
	}

	environments, err := s.repo.ListEnvironments(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	var result []*gen.Environment
	for _, environment := range environments {
		entries, err := s.repo.ListEnvironmentEntries(ctx, environment.ID)
		if err != nil {
			return nil, err
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
			Slug:           environment.Slug,
			Description:    conv.FromPGText(environment.Description),
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
		return nil, errors.New("auth not found in context")
	}

	environment, err := s.repo.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
		Slug:      payload.Slug,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, err
	}

	updateInput := repo.UpdateEnvironmentParams{
		Slug:        payload.Slug,
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
		return nil, err
	}

	projectID := *authCtx.ProjectID
	if environment.ProjectID.String() != projectID.String() {
		return nil, errors.New("environment not found")
	}

	for _, updatedEntry := range payload.EntriesToUpdate {
		if _, err := s.repo.UpsertEnvironmentEntry(ctx, repo.UpsertEnvironmentEntryParams{
			EnvironmentID: environment.ID,
			Name:          updatedEntry.Name,
			Value:         updatedEntry.Value,
		}); err != nil {
			return nil, err
		}
	}
	for _, removedEntry := range payload.EntriesToRemove {
		if err := s.repo.DeleteEnvironmentEntry(ctx, repo.DeleteEnvironmentEntryParams{
			EnvironmentID: environment.ID,
			Name:          removedEntry,
		}); err != nil {
			return nil, err
		}
	}

	entries, err := s.repo.ListEnvironmentEntries(ctx, environment.ID)
	if err != nil {
		return nil, err
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
		Slug:           environment.Slug,
		Description:    conv.FromPGText(environment.Description),
		Entries:        genEntries,
		CreatedAt:      environment.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      environment.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) DeleteEnvironment(ctx context.Context, payload *gen.DeleteEnvironmentPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return errors.New("auth not found in context")
	}

	return s.repo.DeleteEnvironment(ctx, repo.DeleteEnvironmentParams{
		Slug:      payload.Slug,
		ProjectID: *authCtx.ProjectID,
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
