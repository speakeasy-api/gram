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
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// emaIssuerKeyName labels the issuer's KMS keys, external key rows and key set.
const emaIssuerKeyName = "Speakeasy issuer"

// emaIssuerKMSPrefix names the issuer's KMS keys.
const emaIssuerKMSPrefix = "emaissuer"

// sweepOrganizationLimit bounds one system sweep; the next run takes the rest.
const sweepOrganizationLimit = 500

// EMAIssuerTokenPurger drops vendor tokens obtained with an organization's
// issuer, so disabling it or a compromised rotation cuts access at once.
type EMAIssuerTokenPurger interface {
	PurgeEMAIssuerTokens(ctx context.Context, organizationID string) error
}

// SetEMAIssuerTokenPurger installs the vendor-token cache hook.
func (p *Provisioner) SetEMAIssuerTokenPurger(purger EMAIssuerTokenPurger) {
	p.emaTokenPurger = purger
}

func (p *Provisioner) purgeEMAIssuerTokens(ctx context.Context, organizationID string) {
	// TODO(AIM-523): log at error when the purger is nil once signing caches vendor tokens.
	if p.emaTokenPurger == nil {
		return
	}
	if err := p.emaTokenPurger.PurgeEMAIssuerTokens(context.WithoutCancel(ctx), organizationID); err != nil {
		p.logger.ErrorContext(ctx, "failed to purge vendor tokens for issuer", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
	}
}

// issuerError marks an unusable platform signing credential as misconfiguration.
func issuerError(err error) error {
	if errors.Is(err, ErrSigningCredentialUnusable) {
		return fmt.Errorf("%w: %w", internalsessionissuers.ErrMisconfigured, err)
	}
	return err
}

// issuerActor attributes interactive changes to their admin; background calls are system changes.
func issuerActor(ctx context.Context, organizationID string) (urn.Principal, *string) {
	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil && auth.ActiveOrganizationID == organizationID {
		if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
			return actor, auth.Email
		}
	}
	return systemActor(), systemActorDisplayName()
}

func issuerKeyEvent(ctx context.Context, organizationID string, key jwksrepo.JsonWebKey, before, after *audit.JsonWebKeySnapshot) audit.LogJsonWebKeyEvent {
	event := keyEvent(organizationID, key, before, after)
	event.Actor, event.ActorDisplayName = issuerActor(ctx, organizationID)
	return event
}

func issuerEvent(ctx context.Context, organizationID string, issuer jwksrepo.InternalSessionIssuer) audit.LogInternalSessionIssuerEvent {
	actor, displayName := issuerActor(ctx, organizationID)
	return audit.LogInternalSessionIssuerEvent{OrganizationID: organizationID, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil, IssuerURN: urn.NewInternalSessionIssuer(issuer.ID), Issuer: issuer.Issuer}
}

// EnsureEMAIssuer creates the organization's issuer with one dedicated KMS
// key, re-enables a disabled one, or returns the live one. Concurrent callers
// adopt the winner and disable their unused key.
func (p *Provisioner) EnsureEMAIssuer(ctx context.Context, organizationID string) (uuid.UUID, error) {
	existing, err := jwksrepo.New(p.db).GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err == nil {
		if existing.DisabledAt.Valid {
			if err := p.setEMAIssuerDisabled(ctx, organizationID, false); err != nil {
				return uuid.Nil, err
			}
		}
		return existing.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("read organization issuer: %w", err)
	}
	// A disabled or missing organization gets no KMS key.
	org, err := orgrepo.New(p.db).GetOrganizationMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows) || (err == nil && org.DisabledAt.Valid):
		return uuid.Nil, internalsessionissuers.ErrNotFound
	case err != nil:
		return uuid.Nil, fmt.Errorf("read organization: %w", err)
	}
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	signer, err := p.resolveSigningCredential(ctx, logger, repo.New(p.db))
	if err != nil {
		return uuid.Nil, issuerError(err)
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
			return p.EnsureEMAIssuer(ctx, organizationID)
		}
		return uuid.Nil, issuerError(err)
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
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, internalsessionissuers.ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("lock issuer organization: %w", err)
	}
	_, err = jq.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err == nil {
		return uuid.Nil, &adoptedError{client: nil}
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
	set, err := jq.CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{OrganizationID: organizationID, ExternalKeyID: backing.ID, Name: emaIssuerKeyName, IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create issuer key set: %w", err)
	}
	issuerURL, err := internalsessionissuers.URL(p.cfg.ServerURL, organizationID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %w", internalsessionissuers.ErrMisconfigured, err)
	}
	issuer, err := jq.CreateInternalSessionIssuer(ctx, jwksrepo.CreateInternalSessionIssuerParams{OrganizationID: organizationID, JsonWebKeySetID: set.ID, Issuer: issuerURL.String()})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create organization issuer: %w", err)
	}
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: organizationID, JsonWebKeySetID: set.ID, ExternalKeyID: backing.ID, State: "active", Kid: created.kid, PublicJwk: created.publicJWK})
	if err != nil {
		return uuid.Nil, fmt.Errorf("publish organization issuer key: %w", err)
	}
	actor, displayName := issuerActor(ctx, organizationID)
	if err := p.audit.LogJsonWebKeySetCreate(ctx, tx, audit.LogJsonWebKeySetCreateEvent{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil, SetURN: urn.NewJsonWebKeySet(set.ID), SetName: set.Name}); err != nil {
		return uuid.Nil, fmt.Errorf("audit issuer key set: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, tx, issuerKeyEvent(ctx, organizationID, key, nil, nil)); err != nil {
		return uuid.Nil, fmt.Errorf("audit issuer key: %w", err)
	}
	if err := p.audit.LogInternalSessionIssuerEnable(ctx, tx, issuerEvent(ctx, organizationID, issuer)); err != nil {
		return uuid.Nil, fmt.Errorf("audit issuer enable: %w", err)
	}
	if err := commitPublication(ctx, tx, organizationID); err != nil {
		return uuid.Nil, err
	}
	return issuer.ID, nil
}

// setEMAIssuerDisabled flips signing for the organization's issuer and audits
// a real change. Keys stay published either way.
func (p *Provisioner) setEMAIssuerDisabled(ctx context.Context, organizationID string, disabled bool) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer update: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	if _, err := lockEMAIssuer(ctx, jq, organizationID); err != nil {
		return err
	}
	before, err := jq.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("read issuer: %w", err)
	}
	if before.DisabledAt.Valid == disabled {
		return nil
	}
	after, err := jq.SetInternalSessionIssuerDisabled(ctx, jwksrepo.SetInternalSessionIssuerDisabledParams{Disabled: disabled, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("update issuer: %w", err)
	}
	if disabled {
		err = p.audit.LogInternalSessionIssuerDisable(ctx, tx, issuerEvent(ctx, organizationID, after))
	} else {
		err = p.audit.LogInternalSessionIssuerEnable(ctx, tx, issuerEvent(ctx, organizationID, after))
	}
	if err != nil {
		return fmt.Errorf("audit issuer update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issuer update: %w", err)
	}
	return nil
}

func lockEMAIssuer(ctx context.Context, jq *jwksrepo.Queries, organizationID string) (jwksrepo.JsonWebKeySet, error) {
	set, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return jwksrepo.JsonWebKeySet{}, internalsessionissuers.ErrNotFound
	case err != nil:
		return jwksrepo.JsonWebKeySet{}, fmt.Errorf("lock issuer: %w", err)
	}
	return set, nil
}

// DisableEMAIssuer stops signing with the organization's issuer and purges
// cached vendor tokens. Keys stay published, so enabling again resumes with
// the same issuer URL and keys. Disabling twice is a no-op.
func (p *Provisioner) DisableEMAIssuer(ctx context.Context, organizationID string) error {
	if err := p.setEMAIssuerDisabled(ctx, organizationID, true); err != nil {
		return err
	}
	p.purgeEMAIssuerTokens(ctx, organizationID)
	return nil
}

// RotateEMAIssuer rotates the issuer's signing key in two calls, like
// RotateClient: the first publishes a pending key and returns
// RotationPendingError; a call after ReadyAt activates it and retires the
// previous key, which stays published for RetiredKeyOverlap. activeKid is the
// key the caller saw active.
func (p *Provisioner) RotateEMAIssuer(ctx context.Context, organizationID, activeKid string) error {
	expired, err := p.rotateEMAIssuerRows(ctx, organizationID, activeKid)
	p.retireEMAIssuerKeys(ctx, organizationID, expired)
	if errors.Is(err, errPublishPending) {
		return p.publishEMAIssuerKey(ctx, organizationID, activeKid)
	}
	return err
}

// errPublishPending tells RotateEMAIssuer to mint the pending key with no lock held.
var errPublishPending = errors.New("publish a pending issuer key")

// rotateEMAIssuerRows runs the locked half of a rotation and returns the
// expired keys it revoked; their KMS versions are retired after commit.
func (p *Provisioner) rotateEMAIssuerRows(ctx context.Context, organizationID, activeKid string) ([]jwksrepo.JsonWebKey, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issuer rotation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := lockEMAIssuer(ctx, jq, organizationID)
	if err != nil {
		return nil, err
	}
	expired, err := p.revokeExpiredEMAIssuerKeys(ctx, tx, organizationID, set.ID)
	if err != nil {
		return nil, err
	}
	commit := func(result error) ([]jwksrepo.JsonWebKey, error) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit issuer rotation: %w", err)
		}
		return expired, result
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, fmt.Errorf("load issuer keys: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if active.ID == uuid.Nil {
		return nil, internalsessionissuers.ErrNotFound
	}
	if active.Kid != activeKid {
		known, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, Kid: activeKid})
		if err != nil {
			return nil, fmt.Errorf("check rotation source kid: %w", err)
		}
		if !known {
			return commit(internalsessionissuers.ErrUnknownKid)
		}
		// Another caller finished this rotation.
		return commit(nil)
	}
	if pending.ID == uuid.Nil {
		return commit(errPublishPending)
	}
	if pending.UpdatedAt.InfinityModifier != pgtype.Finite {
		expired, err := commit(nil)
		if err != nil {
			return nil, err
		}
		published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: pending.ID, OrganizationID: organizationID})
		if err != nil {
			return expired, fmt.Errorf("observe recovered issuer key publication: %w", err)
		}
		return expired, &internalsessionissuers.RotationPendingError{ReadyAt: published.Time.Add(internalsessionissuers.ActivationDelay)}
	}
	ready, err := repo.New(tx).RotationPublicationReady(ctx, repo.RotationPublicationReadyParams{ID: pending.ID, OrganizationID: organizationID, CacheSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)})
	if err != nil {
		return nil, fmt.Errorf("check issuer rotation cache window: %w", err)
	}
	if !ready {
		return commit(&internalsessionissuers.RotationPendingError{ReadyAt: pending.UpdatedAt.Time.Add(internalsessionissuers.ActivationDelay)})
	}
	if err := p.activateEMAIssuerKey(ctx, tx, organizationID, set, pending); err != nil {
		return nil, err
	}
	return commit(nil)
}

func (p *Provisioner) revokeExpiredEMAIssuerKeys(ctx context.Context, tx pgx.Tx, organizationID string, setID uuid.UUID) ([]jwksrepo.JsonWebKey, error) {
	expired, err := jwksrepo.New(tx).RevokeExpiredEMAIssuerKeys(ctx, jwksrepo.RevokeExpiredEMAIssuerKeysParams{JsonWebKeySetID: setID, OrganizationID: organizationID, OverlapSeconds: int32(internalsessionissuers.RetiredKeyOverlap / time.Second)})
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

// retireEMAIssuerKeys disables committed revocations' KMS versions, never
// under a lock and never for a rolled-back transaction.
func (p *Provisioner) retireEMAIssuerKeys(ctx context.Context, organizationID string, revoked []jwksrepo.JsonWebKey) {
	if len(revoked) == 0 {
		return
	}
	p.retireKeyMaterial(ctx, p.logger.With(attr.SlogOrganizationID(organizationID)), organizationID, revoked)
}

// SweepEMAIssuerKeys revokes the organization's retired issuer keys past the
// overlap and disables their KMS versions. Cheap when there is nothing to do.
func (p *Provisioner) SweepEMAIssuerKeys(ctx context.Context, organizationID string) error {
	due, err := jwksrepo.New(p.db).HasExpiredEMAIssuerKeys(ctx, jwksrepo.HasExpiredEMAIssuerKeysParams{OrganizationID: organizationID, OverlapSeconds: int32(internalsessionissuers.RetiredKeyOverlap / time.Second)})
	if err != nil {
		return fmt.Errorf("check expired issuer keys: %w", err)
	}
	if !due {
		return nil
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer key sweep: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	set, err := lockEMAIssuer(ctx, jwksrepo.New(tx), organizationID)
	if err != nil {
		return err
	}
	expired, err := p.revokeExpiredEMAIssuerKeys(ctx, tx, organizationID, set.ID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issuer key sweep: %w", err)
	}
	p.retireEMAIssuerKeys(ctx, organizationID, expired)
	return nil
}

// SweepAllEMAIssuerKeys sweeps every organization with expired issuer keys,
// up to a batch, and reports how many it swept.
func (p *Provisioner) SweepAllEMAIssuerKeys(ctx context.Context) (int, error) {
	organizations, err := jwksrepo.New(p.db).ListOrganizationsWithExpiredEMAIssuerKeys(ctx, jwksrepo.ListOrganizationsWithExpiredEMAIssuerKeysParams{OverlapSeconds: int32(internalsessionissuers.RetiredKeyOverlap / time.Second), MaxOrganizations: sweepOrganizationLimit})
	if err != nil {
		return 0, fmt.Errorf("list organizations with expired issuer keys: %w", err)
	}
	var errs []error
	for _, organizationID := range organizations {
		if err := p.SweepEMAIssuerKeys(ctx, organizationID); err != nil && !errors.Is(err, internalsessionissuers.ErrNotFound) {
			errs = append(errs, fmt.Errorf("sweep organization issuer keys: %w", err))
		}
	}
	return len(organizations), errors.Join(errs...)
}

// activateEMAIssuerKey retires any active key, activates next and re-points
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
		return fmt.Errorf("audit issuer key set update: %w", err)
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
		return issuerError(err)
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
		if _, ok := errors.AsType[*adoptedError](err); ok {
			if pendingErr != nil {
				return pendingErr
			}
			return nil
		}
		return issuerError(err)
	}
	return pendingErr
}

func (p *Provisioner) publishEMAIssuerKeyRows(ctx context.Context, organizationID, activeKid string, signer *signingCredential, created *createdKey) (*internalsessionissuers.RotationPendingError, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issuer key publication: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := lockEMAIssuer(ctx, jq, organizationID)
	if err != nil {
		return nil, err
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, fmt.Errorf("re-read issuer keys: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if active.ID == uuid.Nil {
		return nil, internalsessionissuers.ErrNotFound
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
		return &internalsessionissuers.RotationPendingError{ReadyAt: published.Time.Add(internalsessionissuers.ActivationDelay)}, &adoptedError{client: nil}
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
	return &internalsessionissuers.RotationPendingError{ReadyAt: published.Time.Add(internalsessionissuers.ActivationDelay)}, nil
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

// RotateEMAIssuerCompromised is the compromise path: it revokes every live
// issuer key (active, pending and retired) at once and activates a fresh key
// immediately. The issuer URL is unchanged; verifiers drop the revoked keys
// within internalsessionissuers.CacheTTL.
func (p *Provisioner) RotateEMAIssuerCompromised(ctx context.Context, organizationID string) error {
	if _, err := jwksrepo.New(p.db).GetOrganizationInternalSessionIssuer(ctx, organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return internalsessionissuers.ErrNotFound
		}
		return fmt.Errorf("read issuer: %w", err)
	}
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	signer, err := p.resolveSigningCredential(ctx, logger, repo.New(p.db))
	if err != nil {
		return issuerError(err)
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
	revoked, err := p.replaceEMAIssuerKeyRows(ctx, organizationID, signer, created)
	if err != nil {
		p.abandonKey(ctx, logger, kms, uuid.Nil, created.key, signer, err)
		return issuerError(err)
	}
	p.retireEMAIssuerKeys(ctx, organizationID, revoked)
	p.purgeEMAIssuerTokens(ctx, organizationID)
	return nil
}

// replaceEMAIssuerKeyRows revokes every live issuer key and activates created,
// in one transaction under the issuer lock.
func (p *Provisioner) replaceEMAIssuerKeyRows(ctx context.Context, organizationID string, signer *signingCredential, created *createdKey) ([]jwksrepo.JsonWebKey, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issuer key replacement: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := lockEMAIssuer(ctx, jq, organizationID)
	if err != nil {
		return nil, err
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, fmt.Errorf("re-read issuer keys: %w", err)
	}
	revoked := make([]jwksrepo.JsonWebKey, 0, len(keys))
	for _, key := range keys {
		row, err := jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: key.ID, OrganizationID: organizationID})
		if err != nil {
			return nil, fmt.Errorf("revoke issuer key: %w", err)
		}
		if err := p.audit.LogJsonWebKeyRevoke(ctx, tx, issuerKeyEvent(ctx, organizationID, row, keySnapshot(key), keySnapshot(row))); err != nil {
			return nil, fmt.Errorf("audit issuer key revocation: %w", err)
		}
		revoked = append(revoked, row)
	}
	next, err := p.insertEMAIssuerKey(ctx, tx, organizationID, set.ID, "pending", signer, created)
	if err != nil {
		return nil, err
	}
	if err := p.activateEMAIssuerKey(ctx, tx, organizationID, set, next); err != nil {
		return nil, err
	}
	if err := commitPublication(ctx, tx, organizationID); err != nil {
		return nil, err
	}
	return revoked, nil
}

// emaPublicationAbsent waits for any issuer write on the organization to
// resolve, then reports whether resourceName backs no key.
func (p *Provisioner) emaPublicationAbsent(ctx context.Context, organizationID, resourceName string) (bool, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin publication reconciliation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	// Creation holds the organization row; key writes hold the issuer's set.
	if _, err := jq.LockOrganizationForEMAIssuer(ctx, organizationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("lock organization for publication reconciliation: %w", err)
	}
	if _, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("lock issuer for publication reconciliation: %w", err)
	}
	referenced, err := repo.New(tx).ManagedKeyResourceExists(ctx, resourceName)
	if err != nil {
		return false, fmt.Errorf("lookup published key resource: %w", err)
	}
	return !referenced, nil
}
