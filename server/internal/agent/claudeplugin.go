package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	// Only a per-user key: the org install key is shared by every device in
	// the organization, so a vouched email from it proves nothing, and this
	// call mints a key attributed to the member and revokes their previous
	// one. resolveMember refuses agent identities and charges the member's
	// getMyConnections budget, so repeated downloads cannot churn keys
	// without limit.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}
	if slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String()) {
		return nil, nil, oops.E(oops.CodeForbidden, nil, "the Claude observability plugin requires a per-user key; the org install key cannot download it")
	}
	member, err := s.resolveMember(ctx, nil, "the Claude observability plugin")
	if err != nil {
		return nil, nil, err
	}
	if member.userID == "" {
		return nil, nil, oops.E(oops.CodeForbidden, nil, "the enrolled user is not a member of this organization")
	}
	organizationID, userID, principals := member.organizationID, member.userID, member.principals

	rows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "error listing agent plugins").LogError(ctx, s.logger)
	}
	row := mv.ObservabilityPluginRow(rows)
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
