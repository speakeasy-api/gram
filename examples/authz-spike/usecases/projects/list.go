// Package projects illustrates project visibility using one shared authorization
// client. These methods show the access boundary after the existing domain's
// session and organization-membership admission, not a replacement for those gates.
package projects

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
)

// Project is a minimal domain adapter for an existing trusted repository row.
type Project struct {
	// OrganizationID is the project's actual tenant from stored data.
	OrganizationID string

	// ID identifies the project within that organization.
	ID string

	// Name stands in for project metadata returned after authorization.
	Name string
}

// AuthzTarget supplies actual ancestry without implying any permissions.
func (p Project) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: p.OrganizationID, Kind: authz.Project, ID: p.ID},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: p.OrganizationID, Kind: authz.Organization, ID: p.OrganizationID},
		},
	}
}

// Repository loads trusted project rows without interpreting grant policy.
type Repository interface {
	// List returns tenant-scoped candidates in repository order. This example
	// supports only a bounded complete collection, not an arbitrary database page.
	List(ctx context.Context, organizationID string) ([]Project, error)

	// Get returns a row from this organization or a safe not-found error.
	Get(ctx context.Context, organizationID, projectID string) (Project, error)
}

// Service reuses the application-wide client rather than constructing one per call.
type Service struct {
	authz    *authz.Client
	projects Repository
}

// NewService injects the shared authorization client and ordinary repository.
func NewService(authorization *authz.Client, repository Repository) *Service {
	return &Service{authz: authorization, projects: repository}
}

// List sketches ListProjects after trusted identity and domain admission. Subject
// is supplied explicitly because the local client has no context convenience API.
func (s *Service) List(ctx context.Context, subject authz.Subject) ([]Project, error) {
	rows, err := s.projects.List(ctx, subject.Principal.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list project candidates: %w", err)
	}

	targets := make([]authz.Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, row.AuthzTarget())
	}
	allowed, err := s.authz.Filter(ctx, subject, authz.ProjectRead, targets)
	if err != nil {
		return nil, err
	}

	visible := make(map[authz.ResourceRef]bool, len(allowed))
	for _, reference := range allowed {
		visible[reference] = true
	}
	result := make([]Project, 0, len(allowed))
	for _, row := range rows {
		if visible[row.AuthzTarget().Resource] {
			result = append(result, row)
		}
	}
	return result, nil
}

// Get checks the same named permission and target as List before returning a row.
// The existing metadata lookup is reused; no separate reference query is needed.
func (s *Service) Get(ctx context.Context, subject authz.Subject, projectID string) (Project, error) {
	project, err := s.projects.Get(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.ProjectRead, Target: project.AuthzTarget(), Tool: nil, Creation: false,
	}); err != nil {
		return Project{}, err
	}
	return project, nil
}
