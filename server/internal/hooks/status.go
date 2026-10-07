package hooks

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// GetStatus reports whether the organization has any hook telemetry source
// configured. It reads configuration rows only, never hook traffic, so the
// dashboard can call it on every policy edit.
func (s *Service) GetStatus(ctx context.Context, payload *gen.GetStatusPayload) (*gen.HooksStatus, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	row, err := s.repo.GetHooksConfiguration(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read hooks configuration").
			LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	return &gen.HooksStatus{
		Configured:              row.AgentHooksKey || row.AnthropicInferenceHooks,
		AgentHooksKey:           row.AgentHooksKey,
		AnthropicInferenceHooks: row.AnthropicInferenceHooks,
	}, nil
}
