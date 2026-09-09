package remotemcp

import (
	"context"
	"errors"

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

func (s *Service) checkRemoteDistributionAdmission(ctx context.Context, tx pgx.Tx, rollout admission.RolloutConfig, rolloutErr error, organizationID string, projectID, remoteID uuid.UUID, proposedURL string) error {
	if s == nil || s.distributionAdmission == nil {
		return oops.E(oops.CodeUnexpected, admission.ErrUnavailable, "distribution admission unavailable").LogError(ctx, s.logger)
	}
	if err := s.distributionAdmission.CheckRemoteTarget(ctx, tx, rollout, rolloutErr, organizationID, projectID, remoteID, proposedURL); err != nil {
		if errors.Is(err, admission.ErrApprovalRequired) || errors.Is(err, admission.ErrDistributionDisabled) {
			return oops.E(oops.CodeConflict, err, "direct-remote distribution is not admitted")
		}
		return oops.E(oops.CodeUnexpected, err, "check direct-remote distribution admission").LogError(ctx, s.logger)
	}
	return nil
}
