package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	member, row, err := s.claudePluginMember(ctx)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, nil, oops.E(oops.CodeNotFound, nil, "this organization has no observability plugin")
	}

	archive, err := s.claudePlugins.ClaudeWebObservabilityPlugin(ctx, member.organizationID, row.OrganizationSlug, row.ProjectID, row.ProjectSlug, member.userID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "error generating claude observability plugin").LogError(ctx, s.logger)
	}
	return &gen.GetClaudeObservabilityPluginResult{
		ContentType:        "application/zip",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s-observability-claude.zip"`, row.OrganizationSlug),
	}, io.NopCloser(bytes.NewReader(archive)), nil
}

// GetClaudeObservabilityPluginStatus tells the device whether to offer the
// Claude observability plugin, and whether the copy the member uploaded is
// sending events: hooks authenticate with the plugin's key, and every use
// stamps the key's last_accessed_at. Nothing is minted or revoked.
func (s *Service) GetClaudeObservabilityPluginStatus(ctx context.Context, payload *gen.GetClaudeObservabilityPluginStatusPayload) (*gen.GetClaudeObservabilityPluginStatusResult, error) {
	member, row, err := s.claudePluginMember(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &gen.GetClaudeObservabilityPluginStatusResult{Available: false, KeyCreatedAt: nil, KeyLastUsedAt: nil}, nil
	}

	result := &gen.GetClaudeObservabilityPluginStatusResult{Available: true, KeyCreatedAt: nil, KeyLastUsedAt: nil}
	key, err := s.repo.GetMemberClaudeWebHooksAPIKey(ctx, repo.GetMemberClaudeWebHooksAPIKeyParams{
		OrganizationID:  member.organizationID,
		CreatedByUserID: member.userID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return result, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error reading the Claude observability plugin key").LogError(ctx, s.logger)
	}
	if key.CreatedAt.Valid {
		result.KeyCreatedAt = conv.PtrEmpty(key.CreatedAt.Time.UTC().Format(time.RFC3339))
	}
	if key.LastAccessedAt.Valid {
		result.KeyLastUsedAt = conv.PtrEmpty(key.LastAccessedAt.Time.UTC().Format(time.RFC3339))
	}
	return result, nil
}

// claudePluginMember resolves the member a Claude plugin call acts for and the
// plugin row to package, nil when the organization has none or has turned
// observability off.
//
// Only a per-user key: the org install key is shared by every device in the
// organization, so a vouched email from it proves nothing, and a download
// mints a key attributed to the member and revokes their previous one.
// resolveMember refuses agent identities and charges the member's
// getMyConnections budget, so repeated calls cannot churn keys or reads
// without limit.
func (s *Service) claudePluginMember(ctx context.Context) (agentMember, *repo.GetAgentPluginSetRow, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return agentMember{}, nil, oops.C(oops.CodeUnauthorized)
	}
	if slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String()) {
		return agentMember{}, nil, oops.E(oops.CodeForbidden, nil, "the Claude observability plugin requires a per-user key; the org install key cannot download it")
	}
	member, err := s.resolveMember(ctx, nil, "the Claude observability plugin")
	if err != nil {
		return agentMember{}, nil, err
	}
	if member.userID == "" {
		return agentMember{}, nil, oops.E(oops.CodeForbidden, nil, "the enrolled user is not a member of this organization")
	}

	rows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: member.organizationID,
		PrincipalUrns:  member.principals,
	})
	if err != nil {
		return agentMember{}, nil, oops.E(oops.CodeUnexpected, err, "error listing agent plugins").LogError(ctx, s.logger)
	}
	row := mv.ObservabilityPluginRow(rows)
	if row == nil || s.claudePlugins == nil {
		return member, nil, nil
	}
	return member, row, nil
}
