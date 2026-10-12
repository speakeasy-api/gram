package identityproviderconnections

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/emaissuer"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// emaIssuerKeyName labels the issuer's KMS keys and external key rows.
const emaIssuerKeyName = "Enterprise authorization issuer"

// emaIssuerKMSPrefix names the issuer's KMS keys.
const emaIssuerKMSPrefix = "emaissuer"

// EMAIssuerTokenPurger drops vendor tokens obtained with an organization's
// issuer, so disabling or replacing a key cuts access immediately.
type EMAIssuerTokenPurger interface {
	PurgeEMAIssuerTokens(ctx context.Context, organizationID string) error
}

// SetEMAIssuerTokenPurger installs the vendor-token cache hook.
func (p *Provisioner) SetEMAIssuerTokenPurger(purger EMAIssuerTokenPurger) {
	p.emaTokenPurger = purger
}

func (p *Provisioner) purgeEMAIssuerTokens(ctx context.Context, organizationID string) {
	if p.emaTokenPurger == nil {
		return
	}
	if err := p.emaTokenPurger.PurgeEMAIssuerTokens(context.WithoutCancel(ctx), organizationID); err != nil {
		p.logger.ErrorContext(ctx, "failed to purge vendor tokens for issuer", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
	}
}

// issuerActor attributes interactive changes to their admin; background calls are system changes.
func issuerActor(ctx context.Context, organizationID string) (urn.Principal, *string) {
	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil && auth.ActiveOrganizationID == organizationID {
		if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
			email := auth.Email
			return actor, email
		}
	}
	return systemActor(), systemActorDisplayName()
}

func issuerKeyEvent(ctx context.Context, organizationID string, key jwksrepo.JsonWebKey, before, after *audit.JsonWebKeySnapshot) audit.LogJsonWebKeyEvent {
	event := keyEvent(organizationID, key, before, after)
	event.Actor, event.ActorDisplayName = issuerActor(ctx, organizationID)
	return event
}

// EnsureEMAIssuer creates the organization's issuer with one dedicated KMS key,
// or returns the existing one. Concurrent callers adopt the winner and disable
// their unused key.
func (p *Provisioner) EnsureEMAIssuer(ctx context.Context, organizationID string) (uuid.UUID, error) {
	q := jwksrepo.New(p.db)
	existing, err := q.GetOrganizationEMAIssuer(ctx, organizationID)
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("read organization issuer: %w", err)
	}
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	signer, err := p.resolveSigningCredential(ctx, logger, repo.New(p.db))
	if err != nil {
		return uuid.Nil, err
	}
	kms, err := p.openKMSClient(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })
	created, err := p.createSigningKey(ctx, logger, kms, emaIssuerKMSPrefix, uuid.Nil, signer)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := p.createEMAIssuerRows(ctx, organizationID, signer, created)
	if err != nil {
		p.abandonKey(ctx, logger, kms, uuid.Nil, created.key, signer, err)
		if _, ok := errors.AsType[*adoptedError](err); ok {
			return id, nil
		}
		return uuid.Nil, err
	}
	return id, nil
}

func (p *Provisioner) createEMAIssuerRows(ctx context.Context, organizationID string, signer *signingCredential, created *createdKey) (uuid.UUID, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin issuer provisioning: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	if _, err := jq.LockOrganizationForEMAIssuer(ctx, organizationID); err != nil {
		return uuid.Nil, fmt.Errorf("lock issuer organization: %w", err)
	}
	existing, err := jq.GetOrganizationEMAIssuer(ctx, organizationID)
	if err == nil {
		return existing.ID, &adoptedError{client: nil}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("recheck organization issuer: %w", err)
	}
	backing, err := p.createManagedExternalKey(ctx, tx, organizationID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, signer, emaIssuerKeyName, created)
	if err != nil {
		return uuid.Nil, err
	}
	if err := requireSigningCredentialUnchanged(ctx, repo.New(tx), signer); err != nil {
		return uuid.Nil, err
	}
	set, err := jq.CreateOrganizationEMAIssuer(ctx, jwksrepo.CreateOrganizationEMAIssuerParams{OrganizationID: organizationID, ExternalKeyID: backing.ID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create organization issuer: %w", err)
	}
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: organizationID, JsonWebKeySetID: set.ID, ExternalKeyID: backing.ID, State: "active", Kid: created.kid, PublicJwk: created.publicJWK})
	if err != nil {
		return uuid.Nil, fmt.Errorf("publish organization issuer key: %w", err)
	}
	actor, displayName := issuerActor(ctx, organizationID)
	if err := p.audit.LogJsonWebKeySetCreate(ctx, tx, audit.LogJsonWebKeySetCreateEvent{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil, SetURN: urn.NewJsonWebKeySet(set.ID), SetName: set.Name}); err != nil {
		return uuid.Nil, fmt.Errorf("audit organization issuer: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, tx, issuerKeyEvent(ctx, organizationID, key, nil, nil)); err != nil {
		return uuid.Nil, fmt.Errorf("audit organization issuer key: %w", err)
	}
	if err := commitPublication(ctx, tx, organizationID); err != nil {
		return uuid.Nil, err
	}
	return set.ID, nil
}

// RotateEMAIssuer rotates the issuer's signing key in two calls, like
// RotateClient: the first publishes a pending key and returns
// RotationPendingError; a call after ReadyAt activates it and retires the
// previous key, which stays published for emaissuer.RetiredKeyOverlap. The
// issuer URL never changes. activeKid is the key the caller saw active.
func (p *Provisioner) RotateEMAIssuer(ctx context.Context, organizationID, activeKid string) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer rotation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emaissuer.ErrNotFound
	case err != nil:
		return fmt.Errorf("lock issuer: %w", err)
	}
	expired, err := p.revokeExpiredEMAIssuerKeys(ctx, tx, organizationID, set.ID)
	if err != nil {
		return err
	}
	defer func() {
		if len(expired) > 0 {
			p.retireKeyMaterial(ctx, p.logger.With(attr.SlogOrganizationID(organizationID)), organizationID, expired)
		}
	}()
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return fmt.Errorf("load issuer keys: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if active.ID == uuid.Nil {
		return emaissuer.ErrNotFound
	}
	if active.Kid != activeKid {
		known, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, Kid: activeKid})
		if err != nil {
			return fmt.Errorf("check rotation source kid: %w", err)
		}
		if !known {
			return emaissuer.ErrUnknownKid
		}
		// Rotation already finished; keep any expired-key revocations.
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer key expiry: %w", err)
		}
		return nil
	}
	if pending.ID == uuid.Nil {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer key expiry: %w", err)
		}
		return p.publishEMAIssuerKey(ctx, organizationID, activeKid)
	}
	if pending.UpdatedAt.InfinityModifier != pgtype.Finite {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer key expiry: %w", err)
		}
		published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: pending.ID, OrganizationID: organizationID})
		if err != nil {
			return fmt.Errorf("observe recovered issuer key publication: %w", err)
		}
		return &emaissuer.RotationPendingError{ReadyAt: published.Time.Add(emaissuer.CacheTTL)}
	}
	ready, err := repo.New(tx).RotationPublicationReady(ctx, repo.RotationPublicationReadyParams{ID: pending.ID, OrganizationID: organizationID, CacheSeconds: int32(emaissuer.CacheTTL / time.Second)})
	if err != nil {
		return fmt.Errorf("check issuer rotation cache window: %w", err)
	}
	if !ready {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer key expiry: %w", err)
		}
		return &emaissuer.RotationPendingError{ReadyAt: pending.UpdatedAt.Time.Add(emaissuer.CacheTTL)}
	}
	if err := p.activateEMAIssuerKey(ctx, tx, organizationID, set, pending); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issuer rotation: %w", err)
	}
	return nil
}

func (p *Provisioner) revokeExpiredEMAIssuerKeys(ctx context.Context, tx pgx.Tx, organizationID string, setID uuid.UUID) ([]jwksrepo.JsonWebKey, error) {
	expired, err := jwksrepo.New(tx).RevokeExpiredEMAIssuerKeys(ctx, jwksrepo.RevokeExpiredEMAIssuerKeysParams{JsonWebKeySetID: setID, OrganizationID: organizationID, OverlapSeconds: int32(emaissuer.RetiredKeyOverlap / time.Second)})
	if err != nil {
		return nil, fmt.Errorf("revoke expired issuer keys: %w", err)
	}
	for _, key := range expired {
		before := keySnapshot(key)
		before.State = "retired"
		if err := p.audit.LogJsonWebKeyRevoke(ctx, tx, issuerKeyEvent(ctx, organizationID, key, before, keySnapshot(key))); err != nil {
			return nil, fmt.Errorf("audit expired issuer key: %w", err)
		}
	}
	return expired, nil
}

// activateEMAIssuerKey retires the active key, activates next and re-points
// the issuer's backing key, under the issuer lock.
func (p *Provisioner) activateEMAIssuerKey(ctx context.Context, tx pgx.Tx, organizationID string, set jwksrepo.JsonWebKeySet, next jwksrepo.JsonWebKey) error {
	jq := jwksrepo.New(tx)
	previous, err := jq.RetireActiveJsonWebKey(ctx, jwksrepo.RetireActiveJsonWebKeyParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, ExcludeID: next.ID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("retire active issuer key: %w", err)
	default:
		before := keySnapshot(previous)
		before.State = "active"
		if err := p.audit.LogJsonWebKeyRetire(ctx, tx, issuerKeyEvent(ctx, organizationID, previous, before, keySnapshot(previous))); err != nil {
			return fmt.Errorf("audit issuer key retirement: %w", err)
		}
	}
	activated, err := jq.ActivateJsonWebKey(ctx, jwksrepo.ActivateJsonWebKeyParams{ID: next.ID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("activate issuer key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyActivate(ctx, tx, issuerKeyEvent(ctx, organizationID, activated, keySnapshot(next), keySnapshot(activated))); err != nil {
		return fmt.Errorf("audit issuer key activation: %w", err)
	}
	updated, err := jq.SetOrganizationEMAIssuerBackingKey(ctx, jwksrepo.SetOrganizationEMAIssuerBackingKeyParams{ExternalKeyID: activated.ExternalKeyID, ID: set.ID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("re-point issuer backing key: %w", err)
	}
	actor, displayName := issuerActor(ctx, organizationID)
	if err := p.audit.LogJsonWebKeySetUpdate(ctx, tx, audit.LogJsonWebKeySetUpdateEvent{
		OrganizationID:    organizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:             actor,
		ActorDisplayName:  displayName,
		ActorSlug:         nil,
		SetURN:            urn.NewJsonWebKeySet(set.ID),
		SetName:           updated.Name,
		SetSnapshotBefore: &audit.JsonWebKeySetSnapshot{Name: set.Name, ExternalKeyID: set.ExternalKeyID.String()},
		SetSnapshotAfter:  &audit.JsonWebKeySetSnapshot{Name: updated.Name, ExternalKeyID: updated.ExternalKeyID.String()},
	}); err != nil {
		return fmt.Errorf("audit issuer update: %w", err)
	}
	return nil
}

// publishEMAIssuerKey creates the KMS key with no locks held, then publishes it
// as pending. A pending key published meanwhile wins and the new one is
// abandoned.
func (p *Provisioner) publishEMAIssuerKey(ctx context.Context, organizationID, activeKid string) error {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	signer, err := p.resolveSigningCredential(ctx, logger, repo.New(p.db))
	if err != nil {
		return err
	}
	kms, err := p.openKMSClient(ctx)
	if err != nil {
		return err
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })
	created, err := p.createSigningKey(ctx, logger, kms, emaIssuerKMSPrefix, uuid.Nil, signer)
	if err != nil {
		return err
	}
	pendingErr, err := p.publishEMAIssuerKeyRows(ctx, organizationID, activeKid, signer, created)
	if err != nil {
		p.abandonKey(ctx, logger, kms, uuid.Nil, created.key, signer, err)
		if _, ok := errors.AsType[*adoptedError](err); ok && pendingErr != nil {
			return pendingErr
		}
		if _, ok := errors.AsType[*adoptedError](err); ok {
			return nil
		}
		return err
	}
	return pendingErr
}

func (p *Provisioner) publishEMAIssuerKeyRows(ctx context.Context, organizationID, activeKid string, signer *signingCredential, created *createdKey) (*emaissuer.RotationPendingError, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issuer key publication: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, emaissuer.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("lock issuer: %w", err)
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, fmt.Errorf("re-read issuer keys: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if active.ID == uuid.Nil {
		return nil, emaissuer.ErrNotFound
	}
	if active.Kid != activeKid {
		// Another caller finished the rotation meanwhile.
		return nil, &adoptedError{client: nil}
	}
	if pending.ID != uuid.Nil {
		if err := tx.Rollback(ctx); err != nil {
			return nil, fmt.Errorf("release issuer locks: %w", err)
		}
		published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: pending.ID, OrganizationID: organizationID})
		if err != nil {
			return nil, fmt.Errorf("observe adopted issuer key publication: %w", err)
		}
		return &emaissuer.RotationPendingError{ReadyAt: published.Time.Add(emaissuer.CacheTTL)}, &adoptedError{client: nil}
	}
	key, err := p.insertEMAIssuerKey(ctx, tx, organizationID, set.ID, "pending", signer, created)
	if err != nil {
		return nil, err
	}
	// Infinity marks a publication whose commit was not yet observed, so a
	// retry after a crash starts the cache window only once it sees the row.
	if err := repo.New(tx).MarkRotationPublicationUnobserved(ctx, repo.MarkRotationPublicationUnobservedParams{ID: key.ID, OrganizationID: organizationID}); err != nil {
		return nil, fmt.Errorf("mark unobserved issuer key publication: %w", err)
	}
	if err := commitPublication(ctx, tx, organizationID); err != nil {
		return nil, err
	}
	published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: key.ID, OrganizationID: organizationID})
	if err != nil {
		return nil, unobservedPublicationError(organizationID, fmt.Errorf("observe committed issuer key publication: %w", err))
	}
	return &emaissuer.RotationPendingError{ReadyAt: published.Time.Add(emaissuer.CacheTTL)}, nil
}

func (p *Provisioner) insertEMAIssuerKey(ctx context.Context, tx pgx.Tx, organizationID string, setID uuid.UUID, state string, signer *signingCredential, created *createdKey) (jwksrepo.JsonWebKey, error) {
	jq := jwksrepo.New(tx)
	exists, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{JsonWebKeySetID: setID, OrganizationID: organizationID, Kid: created.kid})
	if err != nil {
		return jwksrepo.JsonWebKey{}, fmt.Errorf("check issuer kid: %w", err)
	}
	if exists {
		return jwksrepo.JsonWebKey{}, ErrKeyAlreadyPublished
	}
	backing, err := p.createManagedExternalKey(ctx, tx, organizationID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, signer, emaIssuerKeyName, created)
	if err != nil {
		return jwksrepo.JsonWebKey{}, err
	}
	if err := requireSigningCredentialUnchanged(ctx, repo.New(tx), signer); err != nil {
		return jwksrepo.JsonWebKey{}, err
	}
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: organizationID, JsonWebKeySetID: setID, ExternalKeyID: backing.ID, State: state, Kid: created.kid, PublicJwk: created.publicJWK})
	if err != nil {
		return jwksrepo.JsonWebKey{}, fmt.Errorf("publish issuer key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, tx, issuerKeyEvent(ctx, organizationID, key, nil, nil)); err != nil {
		return jwksrepo.JsonWebKey{}, fmt.Errorf("audit issuer key publication: %w", err)
	}
	return key, nil
}

// ReplaceEMAIssuerKey is the compromise path: it revokes kid at once, disables
// its KMS version and, when kid was the active key, activates a fresh key
// immediately. The issuer URL is unchanged; verifiers drop the revoked key
// within emaissuer.CacheTTL. Cached vendor tokens are purged.
func (p *Provisioner) ReplaceEMAIssuerKey(ctx context.Context, organizationID, kid string) error {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	target, err := p.liveEMAIssuerKey(ctx, organizationID, kid)
	if err != nil {
		return err
	}
	var created *createdKey
	var signer *signingCredential
	if target.State == "active" {
		signer, err = p.resolveSigningCredential(ctx, logger, repo.New(p.db))
		if err != nil {
			return err
		}
		kms, err := p.openKMSClient(ctx)
		if err != nil {
			return err
		}
		defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })
		created, err = p.createSigningKey(ctx, logger, kms, emaIssuerKMSPrefix, uuid.Nil, signer)
		if err != nil {
			return err
		}
		revoked, err := p.replaceEMAIssuerKeyRows(ctx, organizationID, kid, signer, created)
		if err != nil {
			p.abandonKey(ctx, logger, kms, uuid.Nil, created.key, signer, err)
			return err
		}
		p.retireKeyMaterial(ctx, logger, organizationID, revoked)
		p.purgeEMAIssuerTokens(ctx, organizationID)
		return nil
	}
	revoked, err := p.replaceEMAIssuerKeyRows(ctx, organizationID, kid, nil, nil)
	if err != nil {
		return err
	}
	p.retireKeyMaterial(ctx, logger, organizationID, revoked)
	p.purgeEMAIssuerTokens(ctx, organizationID)
	return nil
}

func (p *Provisioner) liveEMAIssuerKey(ctx context.Context, organizationID, kid string) (jwksrepo.JsonWebKey, error) {
	q := jwksrepo.New(p.db)
	set, err := q.GetOrganizationEMAIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return jwksrepo.JsonWebKey{}, emaissuer.ErrNotFound
	case err != nil:
		return jwksrepo.JsonWebKey{}, fmt.Errorf("read issuer: %w", err)
	}
	keys, err := q.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return jwksrepo.JsonWebKey{}, fmt.Errorf("load issuer keys: %w", err)
	}
	for _, key := range keys {
		if key.Kid == kid {
			return key, nil
		}
	}
	return jwksrepo.JsonWebKey{}, emaissuer.ErrKeyNotFound
}

// replaceEMAIssuerKeyRows revokes kid and, given a created key, activates it
// in the same transaction. A target that stopped being active meanwhile is
// only revoked and the created key is abandoned by the caller.
func (p *Provisioner) replaceEMAIssuerKeyRows(ctx context.Context, organizationID, kid string, signer *signingCredential, created *createdKey) ([]jwksrepo.JsonWebKey, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issuer key replacement: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, emaissuer.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("lock issuer: %w", err)
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, fmt.Errorf("re-read issuer keys: %w", err)
	}
	var target jwksrepo.JsonWebKey
	for _, key := range keys {
		if key.Kid == kid {
			target = key
		}
	}
	if target.ID == uuid.Nil {
		return nil, emaissuer.ErrKeyNotFound
	}
	if (target.State == "active") != (created != nil) {
		// The key changed state since it was read; let the caller retry.
		return nil, fmt.Errorf("%w: issuer key changed state; retry", emaissuer.ErrKeyNotFound)
	}
	revoked, err := jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: target.ID, OrganizationID: organizationID})
	if err != nil {
		return nil, fmt.Errorf("revoke issuer key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyRevoke(ctx, tx, issuerKeyEvent(ctx, organizationID, revoked, keySnapshot(target), keySnapshot(revoked))); err != nil {
		return nil, fmt.Errorf("audit issuer key revocation: %w", err)
	}
	if created != nil {
		next, err := p.insertEMAIssuerKey(ctx, tx, organizationID, set.ID, "pending", signer, created)
		if err != nil {
			return nil, err
		}
		if err := p.activateEMAIssuerKey(ctx, tx, organizationID, set, next); err != nil {
			return nil, err
		}
	}
	if err := commitPublication(ctx, tx, organizationID); err != nil {
		return nil, err
	}
	return []jwksrepo.JsonWebKey{revoked}, nil
}

// DisableEMAIssuer withdraws the organization's issuer: every key is revoked
// and disabled in KMS, discovery stops serving, signing fails, and cached
// vendor tokens are purged. Enabling again provisions a new key behind the
// same issuer URL.
func (p *Provisioner) DisableEMAIssuer(ctx context.Context, organizationID string) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer disable: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emaissuer.ErrNotFound
	case err != nil:
		return fmt.Errorf("lock issuer: %w", err)
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return fmt.Errorf("load issuer keys: %w", err)
	}
	revoked := make([]jwksrepo.JsonWebKey, 0, len(keys))
	for _, key := range keys {
		row, err := jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: key.ID, OrganizationID: organizationID})
		if err != nil {
			return fmt.Errorf("revoke issuer key: %w", err)
		}
		if err := p.audit.LogJsonWebKeyRevoke(ctx, tx, issuerKeyEvent(ctx, organizationID, row, keySnapshot(key), keySnapshot(row))); err != nil {
			return fmt.Errorf("audit issuer key revocation: %w", err)
		}
		revoked = append(revoked, row)
	}
	deleted, err := jq.SoftDeleteOrganizationEMAIssuer(ctx, jwksrepo.SoftDeleteOrganizationEMAIssuerParams{ID: set.ID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("withdraw issuer: %w", err)
	}
	actor, displayName := issuerActor(ctx, organizationID)
	if err := p.audit.LogJsonWebKeySetDelete(ctx, tx, audit.LogJsonWebKeySetDeleteEvent{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil, SetURN: urn.NewJsonWebKeySet(deleted.ID), SetName: deleted.Name}); err != nil {
		return fmt.Errorf("audit issuer withdrawal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issuer disable: %w", err)
	}
	p.retireKeyMaterial(ctx, p.logger.With(attr.SlogOrganizationID(organizationID)), organizationID, revoked)
	p.purgeEMAIssuerTokens(ctx, organizationID)
	return nil
}
