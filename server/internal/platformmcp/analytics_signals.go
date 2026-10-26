package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// ErrAnalyticsDefinitionNotFound identifies an unavailable historical snapshot.
var ErrAnalyticsDefinitionNotFound = errors.New("signal definition not captured")

// GetSignalDefinitionInput names a fingerprint returned by a measured result.
type GetSignalDefinitionInput struct {
	// ProjectID is the exact project to inspect.
	ProjectID string `json:"project_id"`

	// SensorID is the contributing sensor.
	SensorID string `json:"sensor_id"`

	// DefinitionHash is the reading's semantic fingerprint.
	DefinitionHash string `json:"definition_hash"`
}

// GetSignalDefinitionOutput includes scoped immutable measurement evidence.
type GetSignalDefinitionOutput struct {
	// Project is the authorized project.
	Project AnalyticsProject `json:"project"`

	// Definition retains the question, options and context that were evaluated.
	Definition *analytics.SignalDefinition `json:"definition"`
}

func (s *AnalyticsService) Definition(ctx context.Context, principal Principal, input GetSignalDefinitionInput) (GetSignalDefinitionOutput, error) {
	var zero GetSignalDefinitionOutput
	project, err := s.authorizeDataset(ctx, principal, input.ProjectID, "signals")
	if err != nil {
		return zero, err
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	definition, err := s.engine.ReadSignalDefinition(ctx, analyticsTenant(principal, project), input.SensorID, input.DefinitionHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrAnalyticsDefinitionNotFound
	}
	if err != nil {
		return zero, fmt.Errorf("inspect signal definition: %w", err)
	}
	return GetSignalDefinitionOutput{Project: analyticsProject(project), Definition: definition}, nil
}
