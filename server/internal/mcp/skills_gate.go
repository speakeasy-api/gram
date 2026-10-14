package mcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// skillsOverMCPEnabled reports whether the hosted request's endpoint serves
// the Skills extension (SEP-2640). The flag is evaluated at most once per
// request, on the first method that needs it, and the answer is kept on the
// payload so capability advertisement, dispatch gating, and the handlers all
// see the same decision.
func (s *Service) skillsOverMCPEnabled(ctx context.Context, payload *mcpInputs) bool {
	if payload.skillsOverMCP != nil {
		return *payload.skillsOverMCP
	}

	enabled := s.skillsOverMCPEnabledForOrganization(ctx, payload.organizationID)
	payload.skillsOverMCP = &enabled
	return enabled
}

// skillsOverMCPEnabledForOrganization evaluates the skills-over-MCP rollout
// flag for the organization that owns a served toolset. The flag is targeted
// by organization: the distinct id is the organization id and the PostHog
// group is its slug, so the slug is loaded for the group and a lookup failure
// degrades to id-only evaluation. Without a flag provider, or when the
// provider fails, the extension stays off.
func (s *Service) skillsOverMCPEnabledForOrganization(ctx context.Context, organizationID string) bool {
	if s.features == nil || organizationID == "" {
		return false
	}

	var groups map[string]string
	org, err := s.orgsRepo.GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve organization slug for skills-over-mcp flag", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	} else {
		groups = feature.OrgProjectGroups(org.Slug, "")
	}

	on, err := s.features.IsFlagEnabled(ctx, feature.FlagSkillsOverMCP, organizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "skills-over-mcp flag check failed; treating as disabled", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return false
	}
	return on
}

// hostedExtensions lists the extension capability labels the hosted surface
// declares to this request, which is also the set whose methods dispatch
// admits. Only the Skills extension exists today, and only behind its flag.
func (s *Service) hostedExtensions(ctx context.Context, payload *mcpInputs) []string {
	if !s.skillsOverMCPEnabled(ctx, payload) {
		return nil
	}
	return []string{mcpversions.ExtensionSkills}
}
