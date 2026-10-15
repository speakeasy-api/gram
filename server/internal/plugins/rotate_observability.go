package plugins

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/attr"
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
//
// Platform MCP: intentionally omitted. The outcome is "replace this project's
// observability ingest credential" for an org admin, and no existing tool
// covers plugin marketplace credentials (the closest, get_my_install_instructions,
// documents that it returns no API key). The only useful result here is a
// plaintext secret, which the Platform MCP contract never returns, and an
// agent-driven rotation would break installs without the dashboard's one-time
// copy surface and explicit fate choice. Revisit if the product ever exposes a
// credential-free rotation outcome (for example "republish with a fresh key").
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

	// A disabled project publishes no hooks subtree, so a rotation would mint and
	// persist a credential nothing can ever use. Matches the download guard.
	observabilityEnabled, err := s.projectObservabilityEnabled(ctx, *ac.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read observability plugin setting").LogError(ctx, s.logger)
	}
	if !observabilityEnabled {
		return nil, oops.E(oops.CodeBadRequest, nil, "observability plugin is disabled for this project")
	}

	// The cutoff is read before the replacement is minted: every key that existed
	// when this rotation began is retired, and any key minted after it — including
	// an overlapping rotation's replacement — is left alone.
	retireBefore, err := keysrepo.New(s.db).CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read rotation cutoff").LogError(ctx, s.logger)
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

	var expiresAt *time.Time
	if payload.PreviousKeyFate == previousKeyFateGrace {
		t := time.Now().UTC().Add(observabilityCredentialGrace)
		expiresAt = &t
	}

	// The hooks rollout gate is unconditional: an org it has not cleared must not
	// receive a regenerated hooks subtree, so its marketplace keeps the previous
	// credential and the rotation reports the update as deferred.
	canRepublish := s.github != nil && marketplacePublished && s.hooksRolloutEligible(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug)

	var (
		previous            []*gen.RotatedObservabilityKey
		previousKeysRetired = true
	)

	switch {
	case canRepublish:
		// DNO-1227: publishProject pushes to GitHub before persisting the keys it
		// baked in, so a failed key transaction leaves the marketplace serving a
		// credential no api_keys row backs — and an ordinary republish carries it
		// forward rather than repairing it. Pre-existing, tracked separately;
		// rotation is currently the only way out of that state.
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
		marketplaceRepublished = outcome.HooksKeyPublished
		marketplaceUpdateDeferred = !outcome.HooksKeyPublished

		if outcome.HooksKeyPublished {
			// The replacement is committed and already on GitHub, so retirement
			// cannot be rolled back into it. Failing the request here would hide
			// the one-time plaintext of a credential that is already live and
			// published — strictly worse than reporting the rotation as
			// incomplete. Retirement gets its own transaction so that "not
			// retired" means exactly that; the cutoff was read before minting,
			// so a retry sweeps the same previous keys without touching this
			// replacement.
			previous, err = s.retirePreviousHooksKeysAtomically(ctx, ac, candidate, retireBefore, payload.PreviousKeyFate, expiresAt)
			if err != nil {
				s.logger.ErrorContext(ctx, "observability credential rotation published a replacement but could not retire the previous keys", attr.SlogError(err))
				previous = nil
				previousKeysRetired = false
			}
			break
		}

		// publishProject re-reads the rollout gate, so it can decline the rotation
		// after this handler's own check passed. When it does it has written
		// nothing, which leaves this identical to the no-marketplace path.
		previous, err = s.rotateCredentialAtomically(ctx, ac, candidate, retireBefore, payload.PreviousKeyFate, expiresAt)
		if err != nil {
			return nil, err
		}
	default:
		marketplaceUpdateDeferred = marketplacePublished
		// Nothing has escaped the database on this path, so minting the
		// replacement and retiring the previous keys share one transaction:
		// either the project ends the call rotated, or unchanged.
		previous, err = s.rotateCredentialAtomically(ctx, ac, candidate, retireBefore, payload.PreviousKeyFate, expiresAt)
		if err != nil {
			return nil, err
		}
	}

	result := &gen.RotateObservabilityCredentialResult{
		Key:                       candidate.fullKey,
		KeyPrefix:                 candidate.keyPrefix,
		PreviousKeyFate:           payload.PreviousKeyFate,
		PreviousKeys:              previous,
		PreviousKeysExpireAt:      latestPreviousKeyExpiry(previous),
		PreviousKeysRetired:       previousKeysRetired,
		MarketplaceRepublished:    marketplaceRepublished,
		MarketplaceUpdateDeferred: &marketplaceUpdateDeferred,
	}

	return result, nil
}

// latestPreviousKeyExpiry reports the last moment any previous key still
// authenticates. It is an upper bound, not a shared deadline: a key already
// inside a shorter window kept its earlier expiry, which is why this is read
// back from the retired keys rather than from the deadline this rotation asked
// for.
func latestPreviousKeyExpiry(previous []*gen.RotatedObservabilityKey) *string {
	var latest *string
	for _, key := range previous {
		if key.ExpiresAt == nil {
			continue
		}
		if latest == nil || *key.ExpiresAt > *latest {
			latest = key.ExpiresAt
		}
	}
	return latest
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

// rotateCredentialAtomically mints the replacement and applies the previous
// keys' fate in one transaction. Used on every path that has not already
// published the replacement: nothing has escaped the database, so a failure
// anywhere must leave the project exactly as it was rather than stranding an
// extra live key.
func (s *Service) rotateCredentialAtomically(
	ctx context.Context,
	ac *contextvalues.AuthContext,
	candidate pluginAPIKeyCandidate,
	retireBefore pgtype.Timestamptz,
	fate string,
	expiresAt *time.Time,
) ([]*gen.RotatedObservabilityKey, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin credential rotation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.createRotatedHooksAPIKey(ctx, dbtx, ac, candidate); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist hooks api key").LogError(ctx, s.logger)
	}

	previous, err := s.retirePreviousHooksKeys(ctx, dbtx, ac, candidate, retireBefore, fate, expiresAt)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit credential rotation").LogError(ctx, s.logger)
	}

	return previous, nil
}

// retirePreviousHooksKeysAtomically applies the previous keys' fate in its own
// transaction. The replacement is already committed and published on this path,
// so retirement cannot be folded back into minting — but the set-based UPDATE
// and the api_key:revoke rows it audits still have to land together. Without
// that, a failing audit insert leaves the keys retired with a partial trail
// while the response reports that nothing was retired.
func (s *Service) retirePreviousHooksKeysAtomically(
	ctx context.Context,
	ac *contextvalues.AuthContext,
	candidate pluginAPIKeyCandidate,
	retireBefore pgtype.Timestamptz,
	fate string,
	expiresAt *time.Time,
) ([]*gen.RotatedObservabilityKey, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin previous key retirement").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	previous, err := s.retirePreviousHooksKeys(ctx, dbtx, ac, candidate, retireBefore, fate, expiresAt)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit previous key retirement").LogError(ctx, s.logger)
	}

	return previous, nil
}

// createRotatedHooksAPIKey writes the replacement key. Unlike
// persistDownloadAPIKey this audits the creation: rotation is an explicit admin
// action, not an automated asset download.
func (s *Service) createRotatedHooksAPIKey(ctx context.Context, dbtx keysrepo.DBTX, ac *contextvalues.AuthContext, candidate pluginAPIKeyCandidate) error {
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

	return nil
}

// retirePreviousHooksKeys revokes or expires every hooks key of the project
// other than the replacement, and reports the ones it touched along with the
// deadline each one actually ended up with.
//
// Nb: the retirement UPDATE is set-based, but the api_key:revoke audit rows are
// written one per key. A project with a long download history therefore holds
// the transaction open across that many inserts. Batching them needs a
// multi-row insert in the audit package (each row also writes an outbox event
// keyed off the inserted row), tracked in DNO-1228.
func (s *Service) retirePreviousHooksKeys(
	ctx context.Context,
	dbtx keysrepo.DBTX,
	ac *contextvalues.AuthContext,
	replacement pluginAPIKeyCandidate,
	retireBefore pgtype.Timestamptz,
	fate string,
	expiresAt *time.Time,
) ([]*gen.RotatedObservabilityKey, error) {
	projectID := uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}

	keysQ := keysrepo.New(dbtx)

	type retiredKey struct {
		id        uuid.UUID
		name      string
		keyPrefix string
		scopes    []string
		expiresAt pgtype.Timestamptz
	}
	var retired []retiredKey

	switch fate {
	case previousKeyFateRevokeImmediately:
		rows, err := keysQ.RevokePluginHooksAPIKeysByProject(ctx, keysrepo.RevokePluginHooksAPIKeysByProjectParams{
			OrganizationID:          ac.ActiveOrganizationID,
			ProjectID:               projectID,
			ReplacementKeyHash:      replacement.keyHash,
			RetireKeysCreatedBefore: retireBefore,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "revoke previous hooks keys").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			// A revoked key is gone now, whatever expiry it carried, so its
			// deadline is deliberately not reported.
			retired = append(retired, retiredKey{id: row.ID, name: row.Name, keyPrefix: row.KeyPrefix, scopes: row.Scopes, expiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}})
		}
	case previousKeyFateGrace:
		if expiresAt == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "grace rotation missing expiry").LogError(ctx, s.logger)
		}
		rows, err := keysQ.ExpirePluginHooksAPIKeysByProject(ctx, keysrepo.ExpirePluginHooksAPIKeysByProjectParams{
			ExpiresAt:               conv.ToPGTimestamptz(*expiresAt),
			OrganizationID:          ac.ActiveOrganizationID,
			ProjectID:               projectID,
			ReplacementKeyHash:      replacement.keyHash,
			RetireKeysCreatedBefore: retireBefore,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "expire previous hooks keys").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			retired = append(retired, retiredKey{id: row.ID, name: row.Name, keyPrefix: row.KeyPrefix, scopes: row.Scopes, expiresAt: row.ExpiresAt})
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

		// The deadline reported is the one the database settled on, which for a
		// key already inside a shorter window is earlier than the one this
		// rotation asked for.
		var keyExpiry *string
		if key.expiresAt.Valid {
			formatted := key.expiresAt.Time.UTC().Format(time.RFC3339)
			keyExpiry = &formatted
		}

		previous = append(previous, &gen.RotatedObservabilityKey{
			ID:        key.id.String(),
			Name:      key.name,
			KeyPrefix: key.keyPrefix,
			ExpiresAt: keyExpiry,
		})
	}

	return previous, nil
}
