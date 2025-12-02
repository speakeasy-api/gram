package releases

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/releases/repo"
)

// Service manages toolset releases and state capture
type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
}

// NewService creates a new releases service
func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	logger = logger.With(attr.SlogComponent("releases"))

	return &Service{
		logger: logger,
		db:     db,
		repo:   repo.New(db),
	}
}

// CreateReleaseParams contains all the state IDs needed to create a release
type CreateReleaseParams struct {
	ToolsetID                   uuid.UUID
	SourceStateID               uuid.NullUUID
	ToolsetVersionID            uuid.UUID
	GlobalVariationsVersionID   uuid.NullUUID
	ToolsetVariationsVersionID  uuid.NullUUID
	Notes                       *string
	ReleasedByUserID            string
}

// CreateRelease creates a new release for a toolset
func (s *Service) CreateRelease(ctx context.Context, params CreateReleaseParams) (*repo.ToolsetRelease, error) {
	release, err := s.repo.CreateRelease(ctx, repo.CreateReleaseParams{
		ToolsetID:                  params.ToolsetID,
		SourceStateID:              params.SourceStateID,
		ToolsetVersionID:           params.ToolsetVersionID,
		GlobalVariationsVersionID:  params.GlobalVariationsVersionID,
		ToolsetVariationsVersionID: params.ToolsetVariationsVersionID,
		Notes:                      pgxNullText(params.Notes),
		ReleasedByUserID:           params.ReleasedByUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create release").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "created release",
		attr.SlogToolsetID(params.ToolsetID.String()),
		attr.SlogReleaseNumber(release.ReleaseNumber),
		attr.SlogUserID(params.ReleasedByUserID),
	)

	return &release, nil
}

// GetRelease retrieves a release by ID
func (s *Service) GetRelease(ctx context.Context, releaseID uuid.UUID) (*repo.ToolsetRelease, error) {
	release, err := s.repo.GetRelease(ctx, releaseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "release not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get release").
			Log(ctx, s.logger)
	}

	return &release, nil
}

// GetReleaseByNumber retrieves a release by toolset ID and release number
func (s *Service) GetReleaseByNumber(ctx context.Context, toolsetID uuid.UUID, releaseNumber int64) (*repo.ToolsetRelease, error) {
	release, err := s.repo.GetReleaseByNumber(ctx, repo.GetReleaseByNumberParams{
		ToolsetID:     toolsetID,
		ReleaseNumber: releaseNumber,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "release not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get release").
			Log(ctx, s.logger)
	}

	return &release, nil
}

// ListReleasesParams contains parameters for listing releases
type ListReleasesParams struct {
	ToolsetID uuid.UUID
	Limit     int32
	Offset    int32
}

// ListReleases returns all releases for a toolset, ordered by release number desc
func (s *Service) ListReleases(ctx context.Context, params ListReleasesParams) ([]repo.ToolsetRelease, error) {
	if params.Limit == 0 {
		params.Limit = 50 // default limit
	}

	releases, err := s.repo.ListReleases(ctx, repo.ListReleasesParams{
		ToolsetID:  params.ToolsetID,
		LimitCount: params.Limit,
		OffsetCount: params.Offset,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list releases").
			Log(ctx, s.logger)
	}

	return releases, nil
}

// GetLatestRelease returns the most recent release for a toolset
func (s *Service) GetLatestRelease(ctx context.Context, toolsetID uuid.UUID) (*repo.ToolsetRelease, error) {
	release, err := s.repo.GetLatestRelease(ctx, toolsetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "no releases found for toolset")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get latest release").
			Log(ctx, s.logger)
	}

	return &release, nil
}

// CountReleases returns the total number of releases for a toolset
func (s *Service) CountReleases(ctx context.Context, toolsetID uuid.UUID) (int64, error) {
	count, err := s.repo.CountReleases(ctx, toolsetID)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to count releases").
			Log(ctx, s.logger)
	}

	return count, nil
}

// DeleteRelease deletes a release (use with caution - this is destructive)
func (s *Service) DeleteRelease(ctx context.Context, releaseID uuid.UUID) error {
	err := s.repo.DeleteRelease(ctx, releaseID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete release").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "deleted release",
		attr.SlogReleaseID(releaseID.String()),
	)

	return nil
}

// pgxNullText converts a *string to pgtype.Text
func pgxNullText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}
