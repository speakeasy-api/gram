package toolsets

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// createStagingVersion creates a staging copy of a toolset
func (s *Service) createStagingVersion(ctx context.Context, projectID uuid.UUID, toolsetSlug string) (*repo.Toolset, error) {
	// Get the parent toolset
	parentToolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      toolsetSlug,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "toolset not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset").
			Log(ctx, s.logger)
	}

	// Check if staging version already exists
	existingStaging, err := s.repo.GetStagingToolset(ctx, uuid.NullUUID{
		UUID:  parentToolset.ID,
		Valid: true,
	})
	if err == nil {
		s.logger.InfoContext(ctx, "staging version already exists",
			attr.SlogToolsetSlug(toolsetSlug),
			attr.SlogToolsetSlug(existingStaging.Slug),
		)
		return &existingStaging, nil
	}

	// Create staging toolset
	stagingSlug := fmt.Sprintf("%s-staging", toolsetSlug)
	stagingName := fmt.Sprintf("%s (staging)", parentToolset.Name)

	stagingToolset, err := s.repo.CreateToolset(ctx, repo.CreateToolsetParams{
		OrganizationID:         parentToolset.OrganizationID,
		ProjectID:              projectID,
		Name:                   stagingName,
		Slug:                   stagingSlug,
		Description:            parentToolset.Description,
		DefaultEnvironmentSlug: parentToolset.DefaultEnvironmentSlug,
		McpSlug:                pgtype.Text{String: "", Valid: false}, // Staging toolsets are not published as MCP servers
		McpEnabled:             false,
		ParentToolsetID:        uuid.NullUUID{UUID: parentToolset.ID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create staging toolset").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "created staging version",
		attr.SlogToolsetSlug(toolsetSlug),
		attr.SlogToolsetSlug(stagingToolset.Slug),
		attr.SlogToolsetID(stagingToolset.ID.String()),
	)

	return &stagingToolset, nil
}

// getStagingVersion retrieves the staging version of a toolset if it exists
func (s *Service) getStagingVersion(ctx context.Context, projectID uuid.UUID, toolsetSlug string) (*repo.Toolset, error) {
	// Get the parent toolset first
	parentToolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      toolsetSlug,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "toolset not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset").
			Log(ctx, s.logger)
	}

	// Get staging version
	stagingToolset, err := s.repo.GetStagingToolset(ctx, uuid.NullUUID{
		UUID:  parentToolset.ID,
		Valid: true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "staging version not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get staging version").
			Log(ctx, s.logger)
	}

	return &stagingToolset, nil
}

// GetToolsetWithStagingVersion retrieves a toolset along with its staging version (if any)
func (s *Service) GetToolsetWithStagingVersion(ctx context.Context, projectID uuid.UUID, toolsetSlug string) (*repo.GetToolsetWithStagingVersionRow, error) {
	result, err := s.repo.GetToolsetWithStagingVersion(ctx, repo.GetToolsetWithStagingVersionParams{
		Slug:      toolsetSlug,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "toolset not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset with staging version").
			Log(ctx, s.logger)
	}

	return &result, nil
}

// DiscardStagingVersion deletes the staging version of a toolset
func (s *Service) discardStagingVersion(ctx context.Context, projectID uuid.UUID, toolsetSlug string) error {
	// Get staging version
	stagingToolset, err := s.getStagingVersion(ctx, projectID, toolsetSlug)
	if err != nil {
		return err
	}

	// Delete staging toolset
	err = s.repo.DeleteToolset(ctx, repo.DeleteToolsetParams{
		Slug:      stagingToolset.Slug,
		ProjectID: projectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete staging version").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "discarded staging version",
		attr.SlogToolsetSlug(toolsetSlug),
		attr.SlogToolsetSlug(stagingToolset.Slug),
	)

	return nil
}

// SwitchEditingMode toggles a toolset between iteration and staging modes
func (s *Service) switchEditingMode(ctx context.Context, projectID uuid.UUID, toolsetSlug string, newMode string) (*repo.Toolset, error) {
	// Validate mode
	if newMode != "iteration" && newMode != "staging" {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid editing mode: must be 'iteration' or 'staging'")
	}

	// Get current toolset to check existing mode
	currentToolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      toolsetSlug,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "toolset not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset").
			Log(ctx, s.logger)
	}

	// Check if already in requested mode
	if currentToolset.EditingMode == newMode {
		return &currentToolset, nil
	}

	// Update editing mode
	updatedToolset, err := s.repo.UpdateToolsetEditingMode(ctx, repo.UpdateToolsetEditingModeParams{
		EditingMode: newMode,
		Slug:        toolsetSlug,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update editing mode").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "switched editing mode",
		attr.SlogToolsetSlug(toolsetSlug),
		attr.SlogOldMode(currentToolset.EditingMode),
		attr.SlogNewMode(newMode),
	)

	return &updatedToolset, nil
}

// UpdateCurrentRelease sets the current release for a toolset
func (s *Service) UpdateCurrentRelease(ctx context.Context, toolsetID uuid.UUID, releaseID uuid.UUID) (*repo.Toolset, error) {
	updatedToolset, err := s.repo.UpdateToolsetCurrentRelease(ctx, repo.UpdateToolsetCurrentReleaseParams{
		CurrentReleaseID: uuid.NullUUID{UUID: releaseID, Valid: true},
		ID:               toolsetID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update current release").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "updated current release",
		attr.SlogToolsetID(toolsetID.String()),
		attr.SlogReleaseID(releaseID.String()),
	)

	return &updatedToolset, nil
}

// GetToolsetByID retrieves a toolset by ID
func (s *Service) GetToolsetByID(ctx context.Context, toolsetID uuid.UUID) (*repo.Toolset, error) {
	toolset, err := s.repo.GetToolsetByID(ctx, toolsetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "toolset not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset").
			Log(ctx, s.logger)
	}

	return &toolset, nil
}
