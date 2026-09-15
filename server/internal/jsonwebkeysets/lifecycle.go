package jsonwebkeysets

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	gensvc "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// PublishKey mints a new key from the set's current backing external key and
// publishes it. The KMS read happens before the transaction; the transaction
// then locks the set, re-verifies the backing key is still the one the JWK was
// minted from, and inserts.
func (s *Service) PublishKey(ctx context.Context, payload *gensvc.PublishKeyPayload) (*gensvc.JSONWebKey, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	setID, err := uuid.Parse(payload.SetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key set id").LogError(ctx, logger)
	}

	if err := s.allowMint(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	set, err := repo.New(s.db).GetJsonWebKeySet(ctx, repo.GetJsonWebKeySetParams{
		ID:             setID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "key set not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading key set").LogError(ctx, logger)
	}

	minted, err := s.mintFromExternalKey(ctx, logger, authCtx.ActiveOrganizationID, set.ExternalKeyID)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error publishing key").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	locked, err := s.lockSetForKeyWrite(ctx, logger, q, authCtx.ActiveOrganizationID, setID, "key set not found")
	if err != nil {
		return nil, err
	}

	// The mint ran unlocked, so the set may have been re-pointed since. A JWK
	// published under the new backing key's row but minted from the old one
	// would sign with a key its own external_key_id disowns, so refuse and let
	// the caller retry against the settled configuration.
	if locked.ExternalKeyID != minted.externalKeyID {
		return nil, oops.E(oops.CodeConflict, nil, "the set's backing key changed while publishing; try again")
	}

	if err := s.lockBackingKey(ctx, logger, q, authCtx.ActiveOrganizationID, minted.externalKeyID); err != nil {
		return nil, err
	}

	kidExists, err := q.JsonWebKeyKidExistsInSet(ctx, repo.JsonWebKeyKidExistsInSetParams{
		JsonWebKeySetID: setID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		Kid:             minted.kid,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error checking for an existing kid").LogError(ctx, logger)
	}
	if kidExists {
		return nil, oops.E(oops.CodeConflict, nil, "a key with this kid is already published in this set or was revoked from it; rotate the backing key to a new KMS key version before publishing")
	}

	// Publish-before-sign: a new key normally enters as pending so verifier
	// caches pick it up before it signs anything. Minting straight to active is
	// reserved for a set with no active key, where there is nothing to overlap
	// with and waiting would only extend a window in which the set cannot sign.
	state := keyStateActive
	_, err = q.GetActiveJsonWebKey(ctx, repo.GetActiveJsonWebKeyParams{
		JsonWebKeySetID: setID,
		OrganizationID:  authCtx.ActiveOrganizationID,
	})
	switch {
	case err == nil:
		state = keyStatePending
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the active key").LogError(ctx, logger)
	}

	key, err := q.CreateJsonWebKey(ctx, repo.CreateJsonWebKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		JsonWebKeySetID: setID,
		ExternalKeyID:   minted.externalKeyID,
		State:           state,
		Kid:             minted.kid,
		PublicJwk:       minted.publicJWK,
	})
	if err != nil {
		if mapped := mapKeyUniqueViolation(err); mapped != nil {
			return nil, mapped
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error publishing key").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeyPublish(ctx, dbtx, keyEvent(authCtx, key, nil, nil)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording key publication").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving key").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeyView(key), nil
}

// ActivateKey makes a pending or retired key the set's active signing key,
// retiring the previously active key first. The one-active partial unique
// index is non-deferrable, so the two writes are ordered statements under the
// set lock rather than one statement whose scan order could transiently
// violate it.
func (s *Service) ActivateKey(ctx context.Context, payload *gensvc.ActivateKeyPayload) (*gensvc.JSONWebKey, error) {
	authCtx, logger, key, dbtx, err := s.beginKeyTransition(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if key.State == keyStateActive {
		return mv.BuildJsonWebKeyView(key), nil
	}

	q := repo.New(dbtx)

	previous, err := q.RetireActiveJsonWebKey(ctx, repo.RetireActiveJsonWebKeyParams{
		JsonWebKeySetID: key.JsonWebKeySetID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		ExcludeID:       key.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No active key to retire; the set was between active keys.
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error retiring the active key").LogError(ctx, logger)
	default:
		before := keySnapshot(previous)
		before.State = keyStateActive
		if err := s.audit.LogJsonWebKeyRetire(ctx, dbtx, keyEvent(authCtx, previous, before, keySnapshot(previous))); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error recording key retirement").LogError(ctx, logger)
		}
	}

	activated, err := q.ActivateJsonWebKey(ctx, repo.ActivateJsonWebKeyParams{
		ID:             key.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if mapped := mapKeyUniqueViolation(err); mapped != nil {
			return nil, mapped
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error activating key").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeyActivate(ctx, dbtx, keyEvent(authCtx, activated, keySnapshot(key), keySnapshot(activated))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording key activation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving key activation").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeyView(activated), nil
}

// RetireKey takes the active key out of signing use while leaving it published
// so outstanding tokens keep verifying. Retiring an already-retired key is a
// no-op; a pending key has never signed anything, so "retire" would be a
// misnomer for what is really a withdrawal (revoke).
func (s *Service) RetireKey(ctx context.Context, payload *gensvc.RetireKeyPayload) (*gensvc.JSONWebKey, error) {
	authCtx, logger, key, dbtx, err := s.beginKeyTransition(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	switch key.State {
	case keyStateRetired:
		return mv.BuildJsonWebKeyView(key), nil
	case keyStateActive:
	default:
		return nil, oops.E(oops.CodeConflict, nil, "only the active key can be retired; revoke a pending key to withdraw it")
	}

	retired, err := repo.New(dbtx).RetireJsonWebKey(ctx, repo.RetireJsonWebKeyParams{
		ID:             key.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retiring key").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeyRetire(ctx, dbtx, keyEvent(authCtx, retired, keySnapshot(key), keySnapshot(retired))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording key retirement").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving key retirement").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeyView(retired), nil
}

// RevokeKey withdraws a key from the published set entirely. The row is
// soft-deleted in the same statement, which is what removes the kid from every
// listing and releases the backing external key's delete guard — and why a
// revoked key reads as not found afterwards.
func (s *Service) RevokeKey(ctx context.Context, payload *gensvc.RevokeKeyPayload) (*gensvc.JSONWebKey, error) {
	authCtx, logger, key, dbtx, err := s.beginKeyTransition(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	revoked, err := repo.New(dbtx).RevokeJsonWebKey(ctx, repo.RevokeJsonWebKeyParams{
		ID:             key.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error revoking key").LogError(ctx, logger)
	}

	if err := s.audit.LogJsonWebKeyRevoke(ctx, dbtx, keyEvent(authCtx, revoked, keySnapshot(key), keySnapshot(revoked))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording key revocation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving key revocation").LogError(ctx, logger)
	}

	return mv.BuildJsonWebKeyView(revoked), nil
}

// beginKeyTransition is the entry every key lifecycle handler shares: resolve
// access, find the key, open the transaction, lock the key's set to serialize
// against every other lifecycle transition and set deletion, and re-read the
// key under that lock. The per-transition statements stay in the handlers —
// their write shapes genuinely differ — but the locking order lives once.
//
// On success the caller owns the returned transaction and must roll it back or
// commit it. On error there is nothing to release.
func (s *Service) beginKeyTransition(ctx context.Context, rawID string) (*contextvalues.AuthContext, *slog.Logger, repo.JsonWebKey, pgx.Tx, error) {
	var noKey repo.JsonWebKey

	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, logger, noKey, nil, err
	}

	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, logger, noKey, nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, logger, noKey, nil, oops.E(oops.CodeUnexpected, err, "error updating key").LogError(ctx, logger)
	}

	handedOff := false
	defer func() {
		if !handedOff {
			o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
		}
	}()

	q := repo.New(dbtx)

	// The first read only discovers which set to lock; it runs before the lock,
	// so the state it saw can be stale and the key is read again below.
	key, err := q.GetJsonWebKey(ctx, repo.GetJsonWebKeyParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, logger, noKey, nil, oops.E(oops.CodeNotFound, err, "key not found")
	case err != nil:
		return nil, logger, noKey, nil, oops.E(oops.CodeUnexpected, err, "error loading key").LogError(ctx, logger)
	}

	if _, err := s.lockSetForKeyWrite(ctx, logger, q, authCtx.ActiveOrganizationID, key.JsonWebKeySetID, "key not found"); err != nil {
		return nil, logger, noKey, nil, err
	}

	key, err = q.GetJsonWebKey(ctx, repo.GetJsonWebKeyParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, logger, noKey, nil, oops.E(oops.CodeNotFound, err, "key not found")
	case err != nil:
		return nil, logger, noKey, nil, oops.E(oops.CodeUnexpected, err, "error loading key").LogError(ctx, logger)
	}

	handedOff = true

	return authCtx, logger, key, dbtx, nil
}

// lockSetForKeyWrite locks the set row FOR UPDATE, serializing every key write
// (publish, activate, retire, revoke, cascade delete) behind one lock. The
// not-found message is caller-supplied because a vanished set means different
// things to different callers: to a set operation the set is missing, to a key
// operation the key is (the cascade soft-deleted it).
func (s *Service) lockSetForKeyWrite(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string, setID uuid.UUID, notFoundMsg string) (repo.JsonWebKeySet, error) {
	set, err := q.LockJsonWebKeySetForKeyWrite(ctx, repo.LockJsonWebKeySetForKeyWriteParams{
		ID:             setID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return set, oops.E(oops.CodeNotFound, err, "%s", notFoundMsg)
	case err != nil:
		return set, oops.E(oops.CodeUnexpected, err, "error locking key set").LogError(ctx, logger)
	}

	return set, nil
}

// mapKeyUniqueViolation translates the two unique indexes a key write can trip
// into conflicts, or returns nil for anything else. The set lock makes both
// races unreachable in the current handlers, but the index is the invariant's
// real owner, so its violations must read as conflicts rather than as server
// faults if a future path reaches them.
func mapKeyUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation {
		return nil
	}

	switch pgErr.ConstraintName {
	case "json_web_keys_one_active_idx":
		return oops.E(oops.CodeConflict, err, "another key was activated concurrently; try again")
	case "json_web_keys_set_kid_idx":
		return oops.E(oops.CodeConflict, err, "a key with this kid is already published in this set")
	default:
		return nil
	}
}

// keySnapshot captures the audited state of a key row.
func keySnapshot(key repo.JsonWebKey) *audit.JsonWebKeySnapshot {
	return &audit.JsonWebKeySnapshot{
		Kid:           key.Kid,
		State:         key.State,
		ExternalKeyID: key.ExternalKeyID.String(),
	}
}

// keyEvent assembles the audit payload every key action shares, deriving the
// actor from the auth context and the set association from the row itself.
func keyEvent(authCtx *contextvalues.AuthContext, key repo.JsonWebKey, before, after *audit.JsonWebKeySnapshot) audit.LogJsonWebKeyEvent {
	return audit.LogJsonWebKeyEvent{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		KeyURN:            urn.NewJsonWebKey(key.ID),
		Kid:               key.Kid,
		SetURN:            urn.NewJsonWebKeySet(key.JsonWebKeySetID),
		KeySnapshotBefore: before,
		KeySnapshotAfter:  after,
	}
}
