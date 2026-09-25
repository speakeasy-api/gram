package plugins

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	previousKeyFateRevokeImmediately = "revoke_immediately"
	previousKeyFateGrace             = "grace"

	// observabilityCredentialGrace is how long a previous hooks key keeps
	// authenticating after a grace rotation, giving installed copies a window to
	// pick up the replacement.
	observabilityCredentialGrace = 7 * 24 * time.Hour
)

// RotateObservabilityCredential mints a replacement hooks-scoped key for the
// project's observability plugin and retires the previous ones. The replacement
// reaches installs through a hooks-only republish of the marketplace when that
// is possible; otherwise it is persisted locally and the caller is told the
// marketplace still carries the previous credential.
func (s *Service) RotateObservabilityCredential(ctx context.Context, payload *gen.RotateObservabilityCredentialPayload) (*gen.RotateObservabilityCredentialResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Matches the observability plugin download, which also mints a hooks key.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if ac.ProjectSlug == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "observability credential rotation requires a session-authenticated context")
	}

	switch payload.PreviousKeyFate {
	case previousKeyFateRevokeImmediately, previousKeyFateGrace:
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid previous key fate")
	}

	candidate, err := s.buildPluginAPIKeyCandidate(auth.APIKeyScopeHooks, "hooks")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build hooks api key").LogError(ctx, s.logger)
	}

	// A marketplace the project published earlier keeps serving the old
	// credential until it is republished, so its presence is read from the
	// persisted connection row rather than inferred from whether GitHub
	// publishing happens to be configured on this deployment.
	marketplacePublished, err := s.projectMarketplacePublished(ctx, *ac.ProjectID)
	if err != nil {
		return nil, err
	}

	marketplaceRepublished := false
	marketplaceUpdateDeferred := false

	// The hooks rollout gate is unconditional: an org it has not cleared must not
	// receive a regenerated hooks subtree, so its marketplace keeps the previous
	// credential and the rotation reports the update as deferred.
	canRepublish := s.github != nil && marketplacePublished && s.hooksRolloutEligible(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug)

	switch {
	case canRepublish:
		outcome, err := s.publishProject(ctx, publishProjectInput{
			ProjectID:        *ac.ProjectID,
			ProjectName:      "",
			ProjectSlug:      *ac.ProjectSlug,
			OrganizationID:   ac.ActiveOrganizationID,
			OrganizationSlug: ac.OrganizationSlug,
			Actor: publishActor{
				Principal:       urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
				DisplayName:     ac.Email,
				Slug:            nil,
				CreatedByUserID: ac.UserID,
			},
			GitHubUsernames: nil,
			CommitMessage:   "Rotate observability plugin credential",
			// RotateHooksKey forces the hooks component to regenerate, so the
			// publish can never be skipped as unchanged.
			SkipIfUnchanged:   true,
			RotateHooksKey:    true,
			HooksKeyCandidate: &candidate,
		})
		if err != nil {
			return nil, err
		}
		marketplaceRepublished = !outcome.Skipped
		marketplaceUpdateDeferred = outcome.HooksConfigDeferred || outcome.Skipped
	default:
		marketplaceUpdateDeferred = marketplacePublished
		if err := s.persistRotatedHooksAPIKey(ctx, ac, candidate); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "persist hooks api key").LogError(ctx, s.logger)
		}
	}

	var expiresAt *time.Time
	if payload.PreviousKeyFate == previousKeyFateGrace {
		t := time.Now().UTC().Add(observabilityCredentialGrace)
		expiresAt = &t
	}

	// The replacement is already persisted, so the fate is applied to every other
	// hooks key of this project in one statement: a rotation that overlaps this
	// one can win the race to be the newest key, but neither can leave a
	// pre-rotation credential behind.
	previous, err := s.applyPreviousHooksKeyFate(ctx, ac, candidate, payload.PreviousKeyFate, expiresAt)
	if err != nil {
		return nil, err
	}

	result := &gen.RotateObservabilityCredentialResult{
		Key:                       candidate.fullKey,
		KeyPrefix:                 candidate.keyPrefix,
		PreviousKeyFate:           payload.PreviousKeyFate,
		PreviousKeys:              previous,
		PreviousKeysExpireAt:      nil,
		MarketplaceRepublished:    marketplaceRepublished,
		MarketplaceUpdateDeferred: &marketplaceUpdateDeferred,
	}
	if expiresAt != nil && len(previous) > 0 {
		formatted := expiresAt.Format(time.RFC3339)
		result.PreviousKeysExpireAt = &formatted
	}

	return result, nil
}

// projectMarketplacePublished reports whether this project has a published
// plugin marketplace, i.e. a GitHub connection row. Unlike GetPublishStatus it
// does not short-circuit on s.github: installs keep pulling from a repo that was
// published earlier even when this deployment can no longer publish to it.
func (s *Service) projectMarketplacePublished(ctx context.Context, projectID uuid.UUID) (bool, error) {
	_, err := s.repo.GetGitHubConnection(ctx, projectID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	default:
		return false, oops.E(oops.CodeUnexpected, err, "get github connection").LogError(ctx, s.logger)
	}
}

// persistRotatedHooksAPIKey writes the replacement key when no republish
// happens. Unlike persistDownloadAPIKey this audits the creation: rotation is an
// explicit admin action, not an automated asset download.
func (s *Service) persistRotatedHooksAPIKey(ctx context.Context, ac *contextvalues.AuthContext, candidate pluginAPIKeyCandidate) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	projectID := uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}
	scopes := []string{candidate.scope.String()}
	created, err := keysrepo.New(dbtx).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  ac.ActiveOrganizationID,
		Name:            candidate.keyName,
		KeyHash:         candidate.keyHash,
		KeyPrefix:       candidate.keyPrefix,
		Scopes:          scopes,
		CreatedByUserID: ac.UserID,
		ProjectID:       projectID,
	})
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}

	if err := s.audit.LogKeyCreate(ctx, dbtx, audit.LogKeyCreateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(created.ID),
		KeyName:          candidate.keyName,
		Scopes:           scopes,
		AgentCredential:  nil,
	}); err != nil {
		return fmt.Errorf("audit log key creation: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// applyPreviousHooksKeyFate revokes or expires every hooks key of the project
// other than the replacement, and reports the ones it touched.
func (s *Service) applyPreviousHooksKeyFate(
	ctx context.Context,
	ac *contextvalues.AuthContext,
	replacement pluginAPIKeyCandidate,
	fate string,
	expiresAt *time.Time,
) ([]*gen.RotatedObservabilityKey, error) {
	projectID := uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin previous key update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	keysQ := keysrepo.New(dbtx)

	type retiredKey struct {
		id        uuid.UUID
		name      string
		keyPrefix string
		scopes    []string
	}
	var retired []retiredKey

	switch fate {
	case previousKeyFateRevokeImmediately:
		rows, err := keysQ.RevokePluginHooksAPIKeysByProject(ctx, keysrepo.RevokePluginHooksAPIKeysByProjectParams{
			OrganizationID:     ac.ActiveOrganizationID,
			ProjectID:          projectID,
			ReplacementKeyHash: replacement.keyHash,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "revoke previous hooks keys").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			retired = append(retired, retiredKey{id: row.ID, name: row.Name, keyPrefix: row.KeyPrefix, scopes: row.Scopes})
		}
	case previousKeyFateGrace:
		if expiresAt == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "grace rotation missing expiry").LogError(ctx, s.logger)
		}
		rows, err := keysQ.ExpirePluginHooksAPIKeysByProject(ctx, keysrepo.ExpirePluginHooksAPIKeysByProjectParams{
			ExpiresAt:          conv.ToPGTimestamptz(*expiresAt),
			OrganizationID:     ac.ActiveOrganizationID,
			ProjectID:          projectID,
			ReplacementKeyHash: replacement.keyHash,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "expire previous hooks keys").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			retired = append(retired, retiredKey{id: row.ID, name: row.Name, keyPrefix: row.KeyPrefix, scopes: row.Scopes})
		}
	}

	previous := make([]*gen.RotatedObservabilityKey, 0, len(retired))
	for _, key := range retired {
		// A grace window leaves the key usable, so only an immediate revoke is an
		// api_key:revoke event; the expiry is recorded by the rotation itself.
		if fate == previousKeyFateRevokeImmediately {
			if err := s.audit.LogKeyRevoke(ctx, dbtx, audit.LogKeyRevokeEvent{
				OrganizationID:   ac.ActiveOrganizationID,
				ProjectID:        projectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
				ActorDisplayName: ac.Email,
				ActorSlug:        nil,
				KeyURN:           urn.NewAPIKey(key.id),
				KeyName:          key.name,
				Scopes:           key.scopes,
				AgentCredential:  nil,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "audit log key revocation").LogError(ctx, s.logger)
			}
		}

		previous = append(previous, &gen.RotatedObservabilityKey{
			ID:        key.id.String(),
			Name:      key.name,
			KeyPrefix: key.keyPrefix,
		})
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit previous key update").LogError(ctx, s.logger)
	}

	return previous, nil
}
