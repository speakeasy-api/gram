package admin

import (
	"context"
	"errors"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) GetDefaultAuthPermissionStatus(ctx context.Context, p *gen.GetDefaultAuthPermissionStatusPayload) (*gen.AdminDefaultAuthPermissionStatus, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	return s.defaultAuthPermissionStatus(ctx, organizationID), nil
}

func (s *Service) RunDefaultAuthPermissions(ctx context.Context, p *gen.RunDefaultAuthPermissionsPayload) (*gen.AdminDefaultAuthPermissionRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))

	// The run outlives the request, so a closed tab does not cut it short.
	detached := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithTimeout(detached, authClientRunTimeout)
	defer cancel()
	report, err := authclientbackfill.RunGrants(runCtx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
	switch {
	case errors.Is(err, authclientbackfill.ErrRunInProgress):
		return nil, oops.E(oops.CodeConflict, err, "a run is already in progress for this organization; try again when it finishes").LogWarn(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "grant default auth permissions").LogError(ctx, logger)
	}
	if phaseErr, failed := report.PhaseErrors[authclientbackfill.PhaseGrants]; failed {
		logger.ErrorContext(detached, "grant default auth permissions", attr.SlogError(errors.New(phaseErr)),
			attr.SlogAuditAction("default_auth_permissions:run"), attr.SlogAdminOIDCSubject(auth.OIDCSubject))
		return nil, oops.E(oops.CodeUnexpected, errors.New(phaseErr), "granting default auth permissions failed; nothing was granted. Run again")
	}
	logger.InfoContext(detached, "default auth permissions granted",
		attr.SlogAuditAction("default_auth_permissions:run"),
		attr.SlogAuditSubject("organization"),
		attr.SlogAuditSubjectID(organizationID),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
		attr.SlogAuthClientBackfillWritten(report.GrantsWritten),
		attr.SlogAuthClientBackfillAPIKeys(report.APIKeysGranted),
	)
	statusCtx, cancelStatus := context.WithTimeout(detached, authClientStatusTimeout)
	defer cancelStatus()
	return &gen.AdminDefaultAuthPermissionRun{
		GrantsWritten: int64(report.GrantsWritten),
		Status:        s.defaultAuthPermissionStatus(statusCtx, organizationID),
	}, nil
}

// defaultAuthPermissionStatus dry-runs the grants phase for one organization.
// A failed check is reported in the status rather than failing the request.
func (s *Service) defaultAuthPermissionStatus(ctx context.Context, organizationID string) *gen.AdminDefaultAuthPermissionStatus {
	status := &gen.AdminDefaultAuthPermissionStatus{
		Complete:      false,
		InProgress:    false,
		PendingGrants: 0,
		Kinds:         []*gen.AdminDefaultAuthPermissionKind{},
		Error:         nil,
	}
	report, err := authclientbackfill.RunGrants(ctx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	switch {
	case errors.Is(err, authclientbackfill.ErrRunInProgress):
		status.InProgress = true
		return status
	case err != nil:
		s.logger.ErrorContext(ctx, "check default auth permissions", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		message := "Checking default auth permissions failed."
		status.Error = &message
		return status
	}
	if phaseErr, failed := report.PhaseErrors[authclientbackfill.PhaseGrants]; failed {
		s.logger.ErrorContext(ctx, "check default auth permissions", attr.SlogOrganizationID(organizationID), attr.SlogError(errors.New(phaseErr)))
		message := "Checking default auth permissions failed."
		status.Error = &message
		return status
	}
	for _, kind := range authclientbackfill.GrantKinds {
		status.Kinds = append(status.Kinds, &gen.AdminDefaultAuthPermissionKind{Kind: string(kind), Pending: int64(report.PendingGrantsByKind[kind])})
	}
	status.PendingGrants = int64(report.PendingGrants)
	status.Complete = report.PendingGrants == 0
	return status
}
