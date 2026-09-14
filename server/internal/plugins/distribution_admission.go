package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func (s *Service) WithDistributionAdmission(guard *admission.Guard) *Service {
	if s != nil {
		s.distributionAdmission = guard
	}
	return s
}

func (s *Service) distributionRollout(ctx context.Context, organizationID, organizationSlug string, projectID uuid.UUID) (admission.RolloutConfig, error) {
	if s == nil || s.db == nil || s.distributionAdmission == nil {
		return admission.RolloutConfig{}, admission.ErrUnavailable
	}
	rollout, err := s.distributionAdmission.ResolveProject(ctx, s.db, organizationID, organizationSlug, projectID)
	if err != nil {
		return admission.RolloutConfig{}, fmt.Errorf("resolve plugin distribution rollout: %w", err)
	}
	return rollout, nil
}

func (s *Service) assignmentAdmissionGuard(rollout admission.RolloutConfig, rolloutErr error) func(context.Context, pgx.Tx, pluginsrepo.Plugin, []string, []string) error {
	return func(ctx context.Context, tx pgx.Tx, plugin pluginsrepo.Plugin, current, desired []string) error {
		// Narrowing and removal remain available even when rollout state or
		// approval data is unavailable.
		if assignments.IsSubset(desired, current) {
			return nil
		}
		return s.distributionAdmission.CheckPluginAudience(ctx, tx, rollout, rolloutErr, plugin.OrganizationID, plugin.ProjectID, plugin.ID, desired)
	}
}

func mapDistributionAdmissionError(err error) error {
	switch {
	case errors.Is(err, admission.ErrApprovalRequired):
		return oops.E(oops.CodeConflict, err, "plugin audience requires Shadow MCP approval")
	case errors.Is(err, admission.ErrDistributionDisabled):
		return oops.E(oops.CodeConflict, err, "direct-remote distribution is temporarily disabled")
	case errors.Is(err, admission.ErrUnavailable):
		return oops.E(oops.CodeUnavailable, err, "distribution approval could not be verified safely")
	default:
		return err
	}
}

func lockDistributionAdmission(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		return fmt.Errorf("lock plugin distribution admission: %w", err)
	}
	return nil
}
