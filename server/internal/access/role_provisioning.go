package access

import (
	"context"
	"errors"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) provisioningAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing organization auth context")
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return ac, nil
}

func (s *Service) provisioningSettings() *roleprovisioning.SettingsService {
	return roleprovisioning.NewSettings(s.db, s.audit)
}

func (s *Service) GetRoleProvisioning(ctx context.Context, _ *gen.GetRoleProvisioningPayload) (*gen.RoleProvisioningStatus, error) {
	ac, err := s.provisioningAdmin(ctx)
	if err != nil {
		return nil, err
	}
	return s.roleProvisioningStatus(ctx, ac.ActiveOrganizationID)
}

func (s *Service) ConfigureRoleProvisioning(ctx context.Context, payload *gen.ConfigureRoleProvisioningPayload) (*gen.RoleProvisioningStatus, error) {
	ac, err := s.provisioningAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "missing provisioning configuration")
	}
	project, err := provisioningProject(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	roles := make([]roleprovisioning.Selection, 0, len(payload.Roles))
	for _, r := range payload.Roles {
		if r == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "missing role selection")
		}
		destination, err := provisioningProject(r.ProjectID)
		if err != nil {
			return nil, err
		}
		roles = append(roles, roleprovisioning.Selection{RoleURN: r.RoleUrn, Enabled: r.Enabled, ProjectID: destination})
	}
	_, err = s.provisioningSettings().Configure(ctx, roleprovisioning.ConfigureInput{
		Actor:          roleprovisioning.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), PublicationUserID: ac.UserID},
		OrganizationID: ac.ActiveOrganizationID, ExpectedVersion: payload.ExpectedVersion, Enabled: payload.Enabled, ProjectID: project, Roles: roles,
	})
	switch {
	case errors.Is(err, roleprovisioning.ErrConflict):
		return nil, oops.E(oops.CodeConflict, err, "role provisioning settings changed; reload before saving")
	case errors.Is(err, roleprovisioning.ErrInvalid):
		return nil, oops.E(oops.CodeBadRequest, err, "invalid role provisioning configuration")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "save role provisioning settings").LogError(ctx, s.logger)
	}
	return s.roleProvisioningStatus(ctx, ac.ActiveOrganizationID)
}

func provisioningProject(value *string) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid destination project")
	}
	return &id, nil
}
func provisioningID(value uuid.NullUUID) *string {
	if !value.Valid {
		return nil
	}
	id := value.UUID.String()
	return &id
}
func (s *Service) roleProvisioningStatus(ctx context.Context, organizationID string) (*gen.RoleProvisioningStatus, error) {
	status, err := s.provisioningSettings().Status(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read role provisioning status").LogError(ctx, s.logger)
	}
	result := &gen.RoleProvisioningStatus{Enabled: status.Enabled, Version: status.Version, ProjectID: provisioningID(status.ProjectID), Roles: []*gen.RoleProvisioningRoleStatus{}, Projects: []*gen.RoleProvisioningProject{}}
	for _, r := range status.Roles {
		var pending *string
		if r.PendingReason != "" {
			pending = &r.PendingReason
		}
		result.Roles = append(result.Roles, &gen.RoleProvisioningRoleStatus{RoleUrn: r.RoleURN, Name: r.Name, Configured: r.Configured, Enabled: r.Enabled, ProjectID: provisioningID(r.ProjectID), AppliedProjectID: provisioningID(r.AppliedProjectID), PluginID: provisioningID(r.PluginID), OriginAudience: r.OriginAudience, PublicationStatus: r.PublicationStatus, PendingReason: pending})
	}
	for _, p := range status.Projects {
		result.Projects = append(result.Projects, &gen.RoleProvisioningProject{ID: p.ID.String(), Name: p.Name})
	}
	return result, nil
}
