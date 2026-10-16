// Package skills illustrates production-shaped skill handlers without depending
// on generated APIs, database infrastructure, or a real authentication provider.
package skills

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
)

// AuthenticatedRequest contains trusted identity and the selected project scope.
// It carries no grants; authorization resolves live policy for the operation.
type AuthenticatedRequest struct {
	// Subject is established by middleware, including the actual delegator if any.
	Subject authz.Subject

	// ProjectID is the selected project within the subject's organization.
	ProjectID string
}

// RequestIdentity reads validated middleware state, never caller-supplied identity.
type RequestIdentity interface {
	// FromContext refuses absent or invalid authentication and project scope.
	FromContext(context.Context) (AuthenticatedRequest, error)
}

// SkillReference contains only trusted identity and ancestry, not skill content.
type SkillReference struct {
	// OrganizationID is established by the tenant-scoped repository lookup.
	OrganizationID string

	// ProjectID names the skill's actual enclosing project.
	ProjectID string

	// ID names the existing skill.
	ID string
}

// AuthzTarget describes containment; it grants no authority or ownership bypass.
func (s SkillReference) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{
			OrganizationID: s.OrganizationID, Kind: authz.Skill, ID: s.ID,
		},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: s.OrganizationID, Kind: authz.Project, ID: s.ProjectID},
			{OrganizationID: s.OrganizationID, Kind: authz.Organization, ID: s.OrganizationID},
		},
	}
}

// SkillDetail is a minimal stand-in for the generated skill response.
type SkillDetail struct {
	// ID identifies the authorized skill.
	ID string

	// Content stands in for protected content loaded only after authorization.
	Content string
}

// SkillRepository supplies trusted resource facts and separately loads content.
type SkillRepository interface {
	// GetReference scopes lookup to both organization and selected project and
	// returns a safe not-found error for missing or out-of-scope skills. It must
	// verify project/organization ownership, not echo the supplied IDs as facts.
	GetReference(ctx context.Context, organizationID, projectID, skillID string) (SkillReference, error)

	// GetDetail loads only this reference's skill within its tenant/project.
	// A move/deletion must not cause it to return a different resource or scope.
	GetDetail(context.Context, SkillReference) (SkillDetail, error)
}

// SkillFeature checks product availability independently of RBAC permission.
type SkillFeature interface {
	// RequireEnabled refuses if skills are unavailable for this organization.
	RequireEnabled(ctx context.Context, organizationID string) error
}

// Service receives authentication, authorization, and domain dependencies at startup.
type Service struct {
	identity RequestIdentity
	authz    *authz.Client
	skills   SkillRepository
	feature  SkillFeature
}

// NewService wires the same authorization client used by other domains.
func NewService(identity RequestIdentity, authorization *authz.Client, repository SkillRepository, feature SkillFeature) *Service {
	return &Service{identity: identity, authz: authorization, skills: repository, feature: feature}
}

// Get illustrates the access boundary in server/internal/skills/impl.go's Get.
// Generated payloads, response assembly, statistics, and HTTP error mapping are
// omitted; the domain's authentication and feature requirements are not bypassed.
func (s *Service) Get(ctx context.Context, skillID string) (SkillDetail, error) {
	request, err := s.identity.FromContext(ctx)
	if err != nil {
		return SkillDetail{}, fmt.Errorf("authenticate skill request: %w", err)
	}

	reference, err := s.skills.GetReference(ctx, request.Subject.Principal.OrganizationID, request.ProjectID, skillID)
	if err != nil {
		return SkillDetail{}, fmt.Errorf("resolve skill reference: %w", err)
	}

	if err := s.authz.Require(ctx, request.Subject, authz.Request{
		Permission: authz.SkillRead,
		Target:     reference.AuthzTarget(),
		Tool:       nil,
		Creation:   false,
	}); err != nil {
		return SkillDetail{}, err
	}

	if err := s.feature.RequireEnabled(ctx, request.Subject.Principal.OrganizationID); err != nil {
		return SkillDetail{}, err
	}

	detail, err := s.skills.GetDetail(ctx, reference)
	if err != nil {
		return SkillDetail{}, fmt.Errorf("load skill detail: %w", err)
	}
	return detail, nil
}
