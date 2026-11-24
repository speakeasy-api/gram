package mcpconnectionhealth

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// flagOn checks the rollout flag as the person from cached definitions, then
// as the organization exactly as before, which also covers remote-only setups.
func (s *Service) flagOn(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if s.features == nil {
		return false
	}
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	distinctID, person := flagTarget(ctx, authCtx)
	on, err := s.features.IsFlagEnabledLocal(ctx, feature.FlagMCPConnectionHealth, distinctID, groups, person)
	if err != nil {
		// The organization check below reports the same failure.
		s.logger.DebugContext(ctx, "evaluate mcp connection health flag locally", attr.SlogError(err))
	}
	if on {
		return true
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagMCPConnectionHealth, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate mcp connection health flag", attr.SlogError(err))
	}
	return evaluation == feature.EvaluationEnabled
}

// flagTarget is a signed-in session's stored email with a lowercased "email";
// anyone else, API keys included, is the organization with an empty "email".
func flagTarget(ctx context.Context, authCtx *contextvalues.AuthContext) (distinctID string, personProperties map[string]string) {
	if contextvalues.HasValidatedGramSession(ctx) && authCtx.Email != nil {
		if email := strings.ToLower(strings.TrimSpace(*authCtx.Email)); email != "" {
			return *authCtx.Email, map[string]string{"email": email}
		}
	}
	return authCtx.ActiveOrganizationID, map[string]string{"email": ""}
}
