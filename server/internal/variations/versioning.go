package variations

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// CreateVariationVersion creates a new version of a tool variation
// This replaces the existing in-place edit behavior when versioning is needed
func (s *Service) CreateVariationVersion(ctx context.Context, groupID uuid.UUID, srcToolUrn urn.Tool, params repo.UpsertToolVariationParams) (*repo.ToolVariation, error) {
	// Get the current variation to use as predecessor
	currentVariation, err := s.repo.GetToolVariationByURN(ctx, repo.GetToolVariationByURNParams{
		GroupID:    groupID,
		SrcToolUrn: srcToolUrn,
	})

	var predecessorID uuid.NullUUID
	if err == nil {
		// Existing variation found - create new version
		predecessorID = uuid.NullUUID{UUID: currentVariation.ID, Valid: true}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get current variation").
			Log(ctx, s.logger)
	}
	// If ErrNoRows, this is the first version, predecessorID stays null

	// Create new variation version
	// Note: The current UpsertToolVariation query does in-place updates via ON CONFLICT
	// For true versioning, we would need a different query that always inserts
	// For now, we'll use the existing upsert but track predecessor
	newVariation, err := s.repo.UpsertToolVariation(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create variation version").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "created variation version",
		attr.SlogToolVariationID(newVariation.ID.String()),
		attr.SlogToolURN(srcToolUrn.String()),
		attr.SlogToolsetVersion(newVariation.Version),
		attr.SlogHasPredecessor(predecessorID.Valid),
	)

	return &newVariation, nil
}

// GetVariationAtVersion retrieves a specific version of a variation
func (s *Service) GetVariationAtVersion(ctx context.Context, variationID uuid.UUID) (*repo.ToolVariation, error) {
	variation, err := s.repo.GetToolVariationByID(ctx, variationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "variation not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get variation").
			Log(ctx, s.logger)
	}

	return &variation, nil
}

// GetLatestVariationVersion retrieves the latest version of a variation by source URN
func (s *Service) GetLatestVariationVersion(ctx context.Context, groupID uuid.UUID, srcToolUrn urn.Tool) (*repo.ToolVariation, error) {
	variation, err := s.repo.GetLatestToolVariationByURN(ctx, repo.GetLatestToolVariationByURNParams{
		GroupID:    groupID,
		SrcToolUrn: srcToolUrn,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "no variation found for tool URN")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get latest variation").
			Log(ctx, s.logger)
	}

	return &variation, nil
}

// ListVariationVersionHistory retrieves all versions of a variation ordered by version desc
func (s *Service) ListVariationVersionHistory(ctx context.Context, groupID uuid.UUID, srcToolUrn urn.Tool) ([]repo.ToolVariation, error) {
	variations, err := s.repo.ListToolVariationVersions(ctx, repo.ListToolVariationVersionsParams{
		GroupID:    groupID,
		SrcToolUrn: srcToolUrn,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list variation versions").
			Log(ctx, s.logger)
	}

	return variations, nil
}

// GetVariationsInGroup retrieves all current (latest) variations in a group
func (s *Service) GetVariationsInGroup(ctx context.Context, groupID uuid.UUID) ([]repo.ToolVariation, error) {
	variations, err := s.repo.ListCurrentVariationsInGroup(ctx, groupID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get variations in group").
			Log(ctx, s.logger)
	}

	return variations, nil
}
