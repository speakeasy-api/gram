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
	observabilityCredentialGrace     = 7 * 24 * time.Hour
)

func (s *Service) RotateObservabilityCredential(ctx context.Context, payload *gen.RotateObservabilityCredentialPayload) (*gen.RotateObservabilityCredentialResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

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

	previous, err := keysrepo.New(s.db).ListPluginHooksAPIKeysByProject(ctx, keysrepo.ListPluginHooksAPIKeysByProjectParams{
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: *ac.ProjectID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list observability plugin keys").LogError(ctx, s.logger)
	}

	marketplaceRepublished := false
	marketplaceUpdateDeferred := false

	connected, err := s.projectMarketplaceConnected(ctx, *ac.ProjectID)
	if err != nil {
		return nil, err
	}

	if connected && s.hooksRolloutEligible(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug) {
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
			GitHubUsernames:   nil,
			CommitMessage:     "Rotate observability plugin credential",
			SkipIfUnchanged:   true,
			RotateHooksKey:    true,
			HooksKeyCandidate: &candidate,
		})
		if err != nil {
			return nil, err
		}
		marketplaceRepublished = !outcome.Skipped
		marketplaceUpdateDeferred = outcome.HooksConfigDeferred
	} else {
		if connected {
			marketplaceUpdateDeferred = true
		}
		if err := s.persistRotatedHooksAPIKey(ctx, ac, candidate); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "persist hooks api key").LogError(ctx, s.logger)
		}
	}

	var expireAt *time.Time
	if payload.PreviousKeyFate == previousKeyFateGrace {
		t := time.Now().UTC().Add(observabilityCredentialGrace)
		expireAt = &t
	}

	rotated, err := s.applyPreviousHooksKeyFate(ctx, ac, previous, payload.PreviousKeyFate, expireAt)
	if err != nil {
		return nil, err
	}

	result := &gen.RotateObservabilityCredentialResult{
		Key:                       &candidate.fullKey,
		KeyPrefix:                 candidate.keyPrefix,
		PreviousKeyFate:           payload.PreviousKeyFate,
		PreviousKeys:              rotated,
		PreviousKeysExpireAt:      nil,
		MarketplaceRepublished:    marketplaceRepublished,
		MarketplaceUpdateDeferred: &marketplaceUpdateDeferred,
	}
	if expireAt != nil {
		formatted := expireAt.Format(time.RFC3339)
		result.PreviousKeysExpireAt = &formatted
	}
	return result, nil
}

func (s *Service) projectMarketplaceConnected(ctx context.Context, projectID uuid.UUID) (bool, error) {
	if s.github == nil {
		return false, nil
	}
	_, err := s.repo.GetGitHubConnection(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.E(oops.CodeUnexpected, err, "get github connection").LogError(ctx, s.logger)
	}
	return true, nil
}

func (s *Service) persistRotatedHooksAPIKey(ctx context.Context, ac *contextvalues.AuthContext, candidate pluginAPIKeyCandidate) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	projectID := uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}
	scopes := []string{candidate.scope.String()}
	createdKey, err := keysrepo.New(dbtx).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
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
		KeyURN:           urn.NewAPIKey(createdKey.ID),
		KeyName:          candidate.keyName,
		Scopes:           scopes,
	}); err != nil {
		return fmt.Errorf("audit log key creation: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (s *Service) applyPreviousHooksKeyFate(
	ctx context.Context,
	ac *contextvalues.AuthContext,
	previous []keysrepo.ApiKey,
	fate string,
	expireAt *time.Time,
) ([]*gen.RotatedObservabilityKey, error) {
	rotated := make([]*gen.RotatedObservabilityKey, 0, len(previous))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin previous key update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	keysQ := keysrepo.New(dbtx)
	for _, key := range previous {
		if !auth.IsPluginHooksAPIKeyName(key.Name) {
			continue
		}

		switch fate {
		case previousKeyFateRevokeImmediately:
			deleted, err := keysQ.DeleteAPIKey(ctx, keysrepo.DeleteAPIKeyParams{
				ID:             key.ID,
				OrganizationID: ac.ActiveOrganizationID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return nil, oops.E(oops.CodeUnexpected, err, "revoke previous hooks key").LogError(ctx, s.logger)
			}
			if err := s.audit.LogKeyRevoke(ctx, dbtx, audit.LogKeyRevokeEvent{
				OrganizationID:   ac.ActiveOrganizationID,
				ProjectID:        deleted.ProjectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
				ActorDisplayName: ac.Email,
				ActorSlug:        nil,
				KeyURN:           urn.NewAPIKey(deleted.ID),
				KeyName:          deleted.Name,
				Scopes:           deleted.Scopes,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "audit log key revocation").LogError(ctx, s.logger)
			}
		case previousKeyFateGrace:
			if expireAt == nil {
				return nil, oops.E(oops.CodeUnexpected, nil, "grace rotation missing expiry").LogError(ctx, s.logger)
			}
			if _, err := keysQ.SetAPIKeyExpiresAt(ctx, keysrepo.SetAPIKeyExpiresAtParams{
				ExpiresAt:      conv.ToPGTimestamptz(*expireAt),
				ID:             key.ID,
				OrganizationID: ac.ActiveOrganizationID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return nil, oops.E(oops.CodeUnexpected, err, "expire previous hooks key").LogError(ctx, s.logger)
			}
		}

		rotated = append(rotated, &gen.RotatedObservabilityKey{
			ID:        key.ID.String(),
			Name:      key.Name,
			KeyPrefix: key.KeyPrefix,
		})
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit previous key update").LogError(ctx, s.logger)
	}
	return rotated, nil
}
