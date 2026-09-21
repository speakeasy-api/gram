package plugins

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// Distribution reads authorize the skill, not the plugin's administrative surface.
func (s *Service) distributionAuthContext(ctx context.Context, id string) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	skillID, err := uuid.Parse(id)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid skill id")
	}
	dimensions := authz.Selector{authz.SelectorKeyProjectID: ac.ProjectID.String()}
	if err := s.authz.RequireAnyUnblocked(ctx,
		authz.Check{Scope: authz.ScopeSkillRead, ResourceKind: "", ResourceID: ac.ProjectID.String(), Dimensions: dimensions},
		authz.Check{Scope: authz.ScopeSkillRead, ResourceKind: authz.ResourceKindSkill, ResourceID: skillID.String(), Dimensions: dimensions},
	); err != nil {
		return nil, err
	}
	if _, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "validate distribution project")
	}
	if _, err := skillsrepo.New(s.db).GetSkill(ctx, skillsrepo.GetSkillParams{ProjectID: *ac.ProjectID, ID: skillID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get distribution skill")
	}
	return ac, nil
}

func (s *Service) ListDistributionPlugins(ctx context.Context, payload *gen.ListDistributionPluginsPayload) (*gen.ListDistributionPluginsResult, error) {
	ac, err := s.distributionAuthContext(ctx, payload.SkillID)
	if err != nil {
		return nil, err
	}
	// Keep lazy provisioning admin-only: a read grant must not create configuration
	// or audience assignments. Non-admins can read existing targets without it.
	if err := s.ensureDefaultPlugin(ctx, ac); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListPlugins(ctx, repo.ListPluginsParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list distribution plugins")
	}
	result := &gen.ListDistributionPluginsResult{Plugins: make([]*gen.DistributionPlugin, 0, len(rows))}
	for _, p := range rows {
		result.Plugins = append(result.Plugins, &gen.DistributionPlugin{ID: p.ID.String(), Name: p.Name, Description: conv.FromPGText[string](p.Description), IsDefault: p.IsDefault.Valid && p.IsDefault.Bool})
	}
	return result, nil
}

func (s *Service) GetDistributionPlugin(ctx context.Context, payload *gen.GetDistributionPluginPayload) (*gen.DistributionPlugin, error) {
	ac, err := s.distributionAuthContext(ctx, payload.SkillID)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id")
	}
	p, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: id, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get distribution plugin")
	}
	return &gen.DistributionPlugin{ID: p.ID.String(), Name: p.Name, Description: conv.FromPGText[string](p.Description), IsDefault: p.IsDefault.Valid && p.IsDefault.Bool}, nil
}
