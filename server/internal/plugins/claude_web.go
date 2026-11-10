package plugins

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
)

// claudeWebHooksKeyPurpose names the keys baked into plugins members upload to
// claude.ai. It must stay in step with the name pattern in
// RevokeMemberClaudeWebHooksAPIKeys.
const claudeWebHooksKeyPurpose = "claude-web-hooks"

// ClaudeWebObservabilityPlugin renders the flat Claude observability plugin
// a member uploads to claude.ai. Its hooks key is owned by the member, so
// ingest attributes their cloud sessions to them; minting it revokes the
// member's earlier keys of this kind.
func (s *Service) ClaudeWebObservabilityPlugin(ctx context.Context, orgID, orgSlug string, projectID uuid.UUID, projectSlug, userID string) ([]byte, error) {
	publishedHooksConfig, err := s.publishedHooksConfig(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read published hooks config: %w", err)
	}

	candidate, err := s.buildPluginAPIKeyCandidate(auth.APIKeyScopeHooks, claudeWebHooksKeyPurpose)
	if err != nil {
		return nil, fmt.Errorf("build hooks api key: %w", err)
	}

	cfg := s.generateConfig(ctx, orgID, orgSlug, projectSlug, projectID, s.projectServerURL(ctx, projectID), publishedHooksConfig)
	cfg.HooksAPIKey = candidate.fullKey
	archive, err := ObservabilityPluginZip(cfg, "claude")
	if err != nil {
		return nil, fmt.Errorf("generate claude observability plugin: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	keys := keysrepo.New(tx)
	// Serialize this member's downloads: without the lock, two racing
	// transactions could each miss the other's uncommitted key and both
	// stay live.
	if err := keys.LockMemberClaudeWebHooksAPIKeys(ctx, keysrepo.LockMemberClaudeWebHooksAPIKeysParams{
		OrganizationID:  orgID,
		CreatedByUserID: userID,
	}); err != nil {
		return nil, fmt.Errorf("lock member hooks api keys: %w", err)
	}
	created, err := keys.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  orgID,
		Name:            candidate.keyName,
		KeyHash:         candidate.keyHash,
		KeyPrefix:       candidate.keyPrefix,
		Scopes:          []string{candidate.scope.String()},
		CreatedByUserID: userID,
		ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("create hooks api key: %w", err)
	}
	_, err = keys.RevokeMemberClaudeWebHooksAPIKeys(ctx, keysrepo.RevokeMemberClaudeWebHooksAPIKeysParams{
		OrganizationID:       orgID,
		CreatedByUserID:      userID,
		ReplacementKeyHash:   candidate.keyHash,
		ReplacementCreatedAt: created.CreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("revoke previous hooks api keys: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit hooks api key: %w", err)
	}
	return archive, nil
}
