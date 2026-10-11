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

func (s *Service) checkRemoteDistributionAdmission(ctx context.Context, tx pgx.Tx, organizationID string, projectID, remoteID uuid.UUID, proposedURL string) error {
	if s == nil || s.distributionAdmission == nil {
		err := oops.E(oops.CodeUnexpected, admission.ErrUnavailable, "distribution admission unavailable")
		if s != nil && s.logger != nil {
			return err.LogError(ctx, s.logger)
		}
		return err
	}
	// Changing the URL is a write: an organization administrator's own change
	// is recorded as the Shadow MCP approval it stands for.
	ctx, err := admission.AdministratorWriteContext(ctx, s.authz, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "distribution approval could not be verified safely").LogError(ctx, s.logger)
	}
	if err := s.distributionAdmission.CheckRemoteTarget(ctx, tx, organizationID, projectID, remoteID, proposedURL); err != nil {
		if errors.Is(err, admission.ErrApprovalRequired) {
			return oops.E(oops.CodeConflict, err, "direct-remote distribution is not admitted")
		}
		return oops.E(oops.CodeUnexpected, err, "check direct-remote distribution admission").LogError(ctx, s.logger)
	}
	return nil
}
