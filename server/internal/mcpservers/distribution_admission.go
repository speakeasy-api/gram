package mcpservers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/oops"
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
		return admission.RolloutConfig{}, fmt.Errorf("resolve MCP server distribution rollout: %w", err)
	}
	return rollout, nil
}

func (s *Service) checkDistributionAdmission(ctx context.Context, tx pgx.Tx, rollout admission.RolloutConfig, rolloutErr error, organizationID string, projectID, serverID uuid.UUID, proposedURL string, targetChange bool) error {
	if s == nil || s.distributionAdmission == nil {
		err := oops.E(oops.CodeUnexpected, admission.ErrUnavailable, "distribution admission unavailable")
		if s != nil && s.logger != nil {
			return err.LogError(ctx, s.logger)
		}
		return err
	}
	if err := s.distributionAdmission.CheckMCPServerTarget(ctx, tx, rollout, rolloutErr, organizationID, projectID, serverID, proposedURL, targetChange); err != nil {
		if errors.Is(err, admission.ErrApprovalRequired) || errors.Is(err, admission.ErrDistributionDisabled) {
			return oops.E(oops.CodeConflict, err, "direct-remote distribution is not admitted")
		}
		return oops.E(oops.CodeUnexpected, err, "check direct-remote distribution admission").LogError(ctx, s.logger)
	}
	return nil
}
