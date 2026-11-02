package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// ClaudeObservabilityPlugins renders a member's Claude observability plugin
// for upload to claude.ai, with a hooks key owned by that member.
type ClaudeObservabilityPlugins interface {
	ClaudeWebObservabilityPlugin(ctx context.Context, orgID, orgSlug string, projectID uuid.UUID, projectSlug, userID string) ([]byte, error)
}

// WithClaudeObservabilityPlugins sets the renderer behind
// getClaudeObservabilityPlugin. Nil reports the plugin as unavailable.
func (s *Service) WithClaudeObservabilityPlugins(renderer ClaudeObservabilityPlugins) *Service {
	s.claudePlugins = renderer
	return s
}

// GetClaudeObservabilityPlugin returns the observability plugin the device
// installs locally, packaged for claude.ai: Claude's cloud environment runs no
// device agent to report the user, so the plugin's own key identifies them.
func (s *Service) GetClaudeObservabilityPlugin(ctx context.Context, payload *gen.GetClaudeObservabilityPluginPayload) (*gen.GetClaudeObservabilityPluginResult, io.ReadCloser, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}
	if _, ok := deviceagenttoken.EffectiveAgent(ctx); ok {
		return nil, nil, oops.E(oops.CodeForbidden, nil, "the Claude observability plugin belongs to a person; an agent identity has none")
	}
	email, err := pollingEmail(authCtx, conv.PtrValOr(payload.Email, ""))
	if err != nil {
		return nil, nil, err
	}

	organizationID := authCtx.ActiveOrganizationID
	principals, err := plugins.ResolveDeliveryPrincipals(ctx, s.db, organizationID, email, "")
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin delivery principals").LogError(ctx, s.logger)
	}
	userID := memberUserID(principals)
	if userID == "" {
		return nil, nil, oops.E(oops.CodeForbidden, nil, "the enrolled user is not a member of this organization")
	}

	rows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "error listing agent plugins").LogError(ctx, s.logger)
	}
	row := observabilityProject(rows)
	if row == nil || s.claudePlugins == nil {
		return nil, nil, oops.E(oops.CodeNotFound, nil, "this organization has no observability plugin")
	}

	archive, err := s.claudePlugins.ClaudeWebObservabilityPlugin(ctx, organizationID, row.OrganizationSlug, row.ProjectID, row.ProjectSlug, userID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "error generating claude observability plugin").LogError(ctx, s.logger)
	}
	return &gen.GetClaudeObservabilityPluginResult{
		ContentType:        "application/zip",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s-observability-claude.zip"`, row.OrganizationSlug),
	}, io.NopCloser(bytes.NewReader(archive)), nil
}

// observabilityProject picks the project whose observability plugin the
// device is delivered: the first published one that has not disabled it. Rows
// arrive default project first.
func observabilityProject(rows []repo.GetAgentPluginSetRow) *repo.GetAgentPluginSetRow {
	for i := range rows {
		if rows[i].MarketplaceToken.Valid && rows[i].ObservabilityEnabled {
			return &rows[i]
		}
	}
	return nil
}
