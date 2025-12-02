package releases

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/releases/server"
	gen "github.com/speakeasy-api/gram/server/gen/releases"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/releases/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type APIService struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	auth            *auth.Auth
	releasesService *Service
	toolsetsRepo    *toolsetsRepo.Queries
}

var _ gen.Service = (*APIService)(nil)

func NewAPIService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *APIService {
	logger = logger.With(attr.SlogComponent("releases"))

	return &APIService{
		tracer:          otel.Tracer("github.com/speakeasy-api/gram/server/internal/releases"),
		logger:          logger,
		db:              db,
		auth:            auth.New(logger, db, sessions),
		releasesService: NewService(logger, db),
		toolsetsRepo:    toolsetsRepo.New(db),
	}
}

func Attach(mux goahttp.Muxer, service *APIService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *APIService) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *APIService) CreateRelease(ctx context.Context, payload *gen.CreateReleasePayload) (*types.ToolsetRelease, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Get toolset by slug
	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsetsRepo.GetToolsetParams{
		Slug:      string(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// TODO: Capture current state - this requires implementing state capture logic
	// For now, we capture tool/resource URNs from the latest version
	// In a full implementation, this would also:
	// 1. Get current deployment
	// 2. Capture system source state (prompt templates)
	// 3. Capture source state (deployment + system)
	// 4. Get variations versions (global + toolset-scoped)

	// Create toolset version with retry logic to handle concurrent requests
	// This prevents race conditions where multiple requests try to create the same version number
	var toolsetVersion toolsetsRepo.ToolsetVersion
	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Get the next version number and current state from the latest toolset version
		var nextVersion int64 = 1
		var toolUrns []urn.Tool
		var resourceUrns []urn.Resource
		var predecessorID uuid.NullUUID

		// Try to get the latest version to determine next version number and copy current state
		// If this fails (no versions exist), we'll start at version 1 with empty URNs
		latestVersion, err := s.toolsetsRepo.GetLatestToolsetVersion(ctx, toolset.ID)
		if err == nil {
			nextVersion = latestVersion.Version + 1
			toolUrns = latestVersion.ToolUrns
			resourceUrns = latestVersion.ResourceUrns
			predecessorID = uuid.NullUUID{UUID: latestVersion.ID, Valid: true}
		}

		// Try to create a new toolset version capturing the current state
		toolsetVersion, err = s.toolsetsRepo.CreateToolsetVersion(ctx, toolsetsRepo.CreateToolsetVersionParams{
			ToolsetID:     toolset.ID,
			Version:       nextVersion,
			ToolUrns:      toolUrns,
			ResourceUrns:  resourceUrns,
			PredecessorID: predecessorID,
		})

		// Success - exit retry loop
		if err == nil {
			break
		}

		// Check if this is a unique constraint violation (concurrent request created same version)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			// If this is our last retry, return the error
			if attempt == maxRetries-1 {
				return nil, oops.E(oops.CodeConflict, err, "failed to create toolset version after retries").Log(ctx, s.logger)
			}

			// Log and retry after a short delay
			s.logger.InfoContext(ctx, "version conflict, retrying",
				attr.SlogToolsetID(toolset.ID.String()),
				attr.SlogToolsetVersion(nextVersion),
				attr.SlogToolsetVersionAttempt(attempt+1),
			)

			// Small exponential backoff: 10ms, 20ms, 40ms, 80ms
			time.Sleep(time.Duration(10*(1<<uint(attempt))) * time.Millisecond)
			continue
		}

		// For any other error, fail immediately
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create toolset version").Log(ctx, s.logger)
	}

	// Create release
	release, err := s.releasesService.CreateRelease(ctx, CreateReleaseParams{
		ToolsetID:                  toolset.ID,
		SourceStateID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false}, // TODO: Capture actual state
		ToolsetVersionID:           toolsetVersion.ID,
		GlobalVariationsVersionID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetVariationsVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Notes:                      payload.Notes,
		ReleasedByUserID:           authCtx.UserID,
	})
	if err != nil {
		return nil, err // Already logged in service
	}

	return convertReleaseToAPI(release), nil
}

func (s *APIService) ListReleases(ctx context.Context, payload *gen.ListReleasesPayload) (*gen.ListReleasesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Get toolset by slug
	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsetsRepo.GetToolsetParams{
		Slug:      string(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// Set default pagination
	limit := int32(50)
	offset := int32(0)
	if payload.Limit != nil {
		limit = *payload.Limit
	}
	if payload.Offset != nil {
		offset = *payload.Offset
	}

	// List releases
	releases, err := s.releasesService.ListReleases(ctx, ListReleasesParams{
		ToolsetID: toolset.ID,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, err // Already logged in service
	}

	// Count total releases
	total, err := s.releasesService.CountReleases(ctx, toolset.ID)
	if err != nil {
		return nil, err // Already logged in service
	}

	// Convert to API types
	apiReleases := make([]*types.ToolsetRelease, len(releases))
	for i, r := range releases {
		apiReleases[i] = convertReleaseToAPI(&r)
	}

	return &gen.ListReleasesResult{
		Releases: apiReleases,
		Total:    total,
	}, nil
}

func (s *APIService) GetRelease(ctx context.Context, payload *gen.GetReleasePayload) (*types.ToolsetRelease, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	releaseID, err := uuid.Parse(payload.ReleaseID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid release ID").Log(ctx, s.logger)
	}

	release, err := s.releasesService.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err // Already logged in service
	}

	return convertReleaseToAPI(release), nil
}

func (s *APIService) GetReleaseByNumber(ctx context.Context, payload *gen.GetReleaseByNumberPayload) (*types.ToolsetRelease, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Get toolset by slug
	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsetsRepo.GetToolsetParams{
		Slug:      string(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	release, err := s.releasesService.GetReleaseByNumber(ctx, toolset.ID, payload.ReleaseNumber)
	if err != nil {
		return nil, err // Already logged in service
	}

	return convertReleaseToAPI(release), nil
}

func (s *APIService) GetLatestRelease(ctx context.Context, payload *gen.GetLatestReleasePayload) (*types.ToolsetRelease, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Get toolset by slug
	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsetsRepo.GetToolsetParams{
		Slug:      string(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	release, err := s.releasesService.GetLatestRelease(ctx, toolset.ID)
	if err != nil {
		return nil, err // Already logged in service
	}

	return convertReleaseToAPI(release), nil
}

func (s *APIService) RollbackToRelease(ctx context.Context, payload *gen.RollbackToReleasePayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Get toolset by slug
	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsetsRepo.GetToolsetParams{
		Slug:      string(payload.ToolsetSlug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// Get the release to rollback to
	release, err := s.releasesService.GetReleaseByNumber(ctx, toolset.ID, payload.ReleaseNumber)
	if err != nil {
		return nil, err // Already logged in service
	}

	// Get the current latest version for version numbering
	currentVersion, err := s.toolsetsRepo.GetLatestToolsetVersion(ctx, toolset.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get current toolset version").Log(ctx, s.logger)
	}

	// Get the version from the release we're rolling back to
	releaseVersion, err := s.toolsetsRepo.GetToolsetVersionByID(ctx, release.ToolsetVersionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get release toolset version").Log(ctx, s.logger)
	}

	// Create a new version with the state from the release version
	// This maintains version history - the new version will be latest version + 1
	// but contain the tool/resource URNs from the rollback target
	var nextVersion int64 = 1
	var predecessorID uuid.NullUUID
	if currentVersion.ID != uuid.Nil {
		nextVersion = currentVersion.Version + 1
		predecessorID = uuid.NullUUID{UUID: currentVersion.ID, Valid: true}
	}

	_, err = s.toolsetsRepo.CreateToolsetVersion(ctx, toolsetsRepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       nextVersion,
		ToolUrns:      releaseVersion.ToolUrns,
		ResourceUrns:  releaseVersion.ResourceUrns,
		PredecessorID: predecessorID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create rollback version").Log(ctx, s.logger)
	}

	// Update current_release_id to point to the release we're rolling back to
	_, err = s.toolsetsRepo.UpdateToolsetCurrentRelease(ctx, toolsetsRepo.UpdateToolsetCurrentReleaseParams{
		CurrentReleaseID: uuid.NullUUID{UUID: release.ID, Valid: true},
		ID:               toolset.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update current release").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "rolled back toolset to release",
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogReleaseNumber(payload.ReleaseNumber),
		attr.SlogToolsetVersion(nextVersion),
	)

	// TODO: Implement deployment restoration from source_state_id
	// TODO: Implement variations restoration

	// Return placeholder toolset - in full implementation, would fetch updated toolset
	//nolint:exhaustruct // Intentionally incomplete placeholder
	return &types.Toolset{
		ID:           toolset.ID.String(),
		ProjectID:    toolset.ProjectID.String(),
		Name:         toolset.Name,
		Slug:         types.Slug(toolset.Slug),
		Description:  conv.FromPGText[string](toolset.Description),
		CreatedAt:    toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    toolset.UpdatedAt.Time.Format(time.RFC3339),
		Tools:        []*types.Tool{},
		ToolUrns:     []string{},
		Resources:    []*types.Resource{},
		ResourceUrns: []string{},
	}, nil
}

// convertReleaseToAPI converts a repo.ToolsetRelease to types.ToolsetRelease
func convertReleaseToAPI(r *repo.ToolsetRelease) *types.ToolsetRelease {
	//nolint:exhaustruct // Optional fields set below based on validity
	apiRelease := &types.ToolsetRelease{
		ID:               r.ID.String(),
		ToolsetID:        r.ToolsetID.String(),
		ReleaseNumber:    r.ReleaseNumber,
		ToolsetVersionID: r.ToolsetVersionID.String(),
		ReleasedByUserID: r.ReleasedByUserID,
		CreatedAt:        r.CreatedAt.Time.Format(time.RFC3339),
	}

	if r.SourceStateID.Valid {
		sourceStateID := r.SourceStateID.UUID.String()
		apiRelease.SourceStateID = &sourceStateID
	}

	if r.GlobalVariationsVersionID.Valid {
		globalVarID := r.GlobalVariationsVersionID.UUID.String()
		apiRelease.GlobalVariationsVersionID = &globalVarID
	}

	if r.ToolsetVariationsVersionID.Valid {
		toolsetVarID := r.ToolsetVariationsVersionID.UUID.String()
		apiRelease.ToolsetVariationsVersionID = &toolsetVarID
	}

	if r.Notes.Valid {
		apiRelease.Notes = &r.Notes.String
	}

	return apiRelease
}
