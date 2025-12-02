package releases

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/releases/repo"
)

// CaptureSystemSourceState creates a system_source_states record capturing prompt templates
func (s *Service) CaptureSystemSourceState(ctx context.Context, projectID uuid.UUID, promptTemplateIDs []uuid.UUID) (*repo.SystemSourceState, error) {
	state, err := s.repo.CreateSystemSourceState(ctx, repo.CreateSystemSourceStateParams{
		ProjectID:         projectID,
		PromptTemplateIds: promptTemplateIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to capture system source state").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "captured system source state",
		attr.SlogProjectID(projectID.String()),
		attr.SlogPromptTemplateCount(len(promptTemplateIDs)),
	)

	return &state, nil
}

// CaptureSourceState creates a source_states record combining deployment and system sources
func (s *Service) CaptureSourceState(ctx context.Context, projectID, deploymentID, systemSourceStateID uuid.UUID) (*repo.SourceState, error) {
	// Check if this exact combination already exists
	existingState, err := s.repo.GetSourceStateByComponents(ctx, repo.GetSourceStateByComponentsParams{
		DeploymentID:        deploymentID,
		SystemSourceStateID: systemSourceStateID,
	})
	if err == nil {
		s.logger.InfoContext(ctx, "reusing existing source state",
			attr.SlogSourceStateID(existingState.ID.String()),
			attr.SlogDeploymentID(deploymentID.String()),
		)
		return &existingState, nil
	}

	// Create new source state
	state, err := s.repo.CreateSourceState(ctx, repo.CreateSourceStateParams{
		ProjectID:           projectID,
		DeploymentID:        deploymentID,
		SystemSourceStateID: systemSourceStateID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to capture source state").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "captured source state",
		attr.SlogSourceStateID(state.ID.String()),
		attr.SlogDeploymentID(deploymentID.String()),
		attr.SlogSystemSourceStateID(systemSourceStateID.String()),
	)

	return &state, nil
}

// GetSystemSourceState retrieves a system source state by ID
func (s *Service) GetSystemSourceState(ctx context.Context, id uuid.UUID) (*repo.SystemSourceState, error) {
	state, err := s.repo.GetSystemSourceState(ctx, id)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get system source state").
			Log(ctx, s.logger)
	}

	return &state, nil
}

// GetSourceState retrieves a source state by ID
func (s *Service) GetSourceState(ctx context.Context, id uuid.UUID) (*repo.SourceState, error) {
	state, err := s.repo.GetSourceState(ctx, id)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get source state").
			Log(ctx, s.logger)
	}

	return &state, nil
}

// CreateToolVariationsGroupVersion creates a new version snapshot of a variations group
func (s *Service) CreateToolVariationsGroupVersion(ctx context.Context, groupID uuid.UUID, variationIDs []uuid.UUID, predecessorID uuid.NullUUID) (*repo.ToolVariationsGroupVersion, error) {
	version, err := s.repo.CreateToolVariationsGroupVersion(ctx, repo.CreateToolVariationsGroupVersionParams{
		GroupID:       groupID,
		VariationIds:  variationIDs,
		PredecessorID: predecessorID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create tool variations group version").
			Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "created tool variations group version",
		attr.SlogToolVariationsGroupID(groupID.String()),
		attr.SlogToolsetVersion(version.Version),
		attr.SlogVariationCount(len(variationIDs)),
	)

	return &version, nil
}

// GetToolVariationsGroupVersion retrieves a specific version of a variations group
func (s *Service) GetToolVariationsGroupVersion(ctx context.Context, versionID uuid.UUID) (*repo.ToolVariationsGroupVersion, error) {
	version, err := s.repo.GetToolVariationsGroupVersion(ctx, versionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get tool variations group version").
			Log(ctx, s.logger)
	}

	return &version, nil
}

// GetLatestToolVariationsGroupVersion retrieves the latest version of a variations group
func (s *Service) GetLatestToolVariationsGroupVersion(ctx context.Context, groupID uuid.UUID) (*repo.ToolVariationsGroupVersion, error) {
	version, err := s.repo.GetLatestToolVariationsGroupVersion(ctx, groupID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get latest tool variations group version").
			Log(ctx, s.logger)
	}

	return &version, nil
}

// ListToolVariationsGroupVersions retrieves all versions of a variations group
func (s *Service) ListToolVariationsGroupVersions(ctx context.Context, groupID uuid.UUID) ([]repo.ToolVariationsGroupVersion, error) {
	versions, err := s.repo.ListToolVariationsGroupVersions(ctx, groupID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list tool variations group versions").
			Log(ctx, s.logger)
	}

	return versions, nil
}
