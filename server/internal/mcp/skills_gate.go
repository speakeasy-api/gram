package mcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// skillsOverMCPEnabled reports whether the hosted request's endpoint serves
// the Skills extension (SEP-2640). The product feature is evaluated at most once per
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

// skillsOverMCPEnabledForOrganization reports whether the organization that
// owns a served toolset has the staff-managed skills_over_mcp product feature,
// which is off by default.
func (s *Service) skillsOverMCPEnabledForOrganization(ctx context.Context, organizationID string) bool {
	return mcpskills.Enabled(ctx, mcpskills.FeatureChecker(s.platformFeatureChecker), organizationID)
}

// hostedExtensions lists the extension capability labels the hosted surface
// declares to this request, which is also the set whose methods dispatch
// admits. Only the Skills extension exists today, and only behind its product feature.
func (s *Service) hostedExtensions(ctx context.Context, payload *mcpInputs) []string {
	if !s.skillsOverMCPEnabled(ctx, payload) {
		return nil
	}
	return []string{mcpversions.ExtensionSkills}
}
