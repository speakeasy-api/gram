package identityproviderconnections

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// emaIssuerKeyName labels the issuer's KMS keys, external key rows and key set.
const emaIssuerKeyName = "Speakeasy issuer"

// emaIssuerKMSPrefix names the issuer's KMS keys.
const emaIssuerKMSPrefix = internalsessionissuers.KMSKeyPrefix

// sweepOrganizationPage is how many organizations one sweep query returns.
const sweepOrganizationPage = 500

// sweepOrganizationTimeout bounds one organization's share of a sweep, so a
// KMS hang on its keys can't hold the cursor in place.
const sweepOrganizationTimeout = 2 * time.Minute

// EMAIssuerActivationCheck approves activating a ready pending issuer key
// with no admin present. A *internalsessionissuers.SigningRefusedError means
// the issuer can't sign now, so the key waits.
type EMAIssuerActivationCheck func(ctx context.Context, organizationID string, pending jwksrepo.JsonWebKey) error

// SetEMAIssuerActivationCheck lets the sweep activate ready pending keys.
func (p *Provisioner) SetEMAIssuerActivationCheck(check EMAIssuerActivationCheck) {
	p.emaActivationCheck = check
}

// issuerError marks an unusable platform signing credential as misconfiguration.
func issuerError(err error) error {
	if errors.Is(err, ErrSigningCredentialUnusable) {
		return fmt.Errorf("%w: %w", internalsessionissuers.ErrMisconfigured, err)
	}
	return err
}

func (p *Provisioner) issuerLifecycle() *internalsessionissuers.Lifecycle {
	return internalsessionissuers.NewLifecycle(p.db, p.audit)
}

// issuerDeployment keeps preview copies from taking authority over source keys.
func (p *Provisioner) issuerDeployment() internalsessionissuers.Deployment {
	return internalsessionissuers.Deployment{ServerURL: p.cfg.ServerURL, AllowLoopbackIssuer: p.cfg.AllowLoopbackIssuer, KeyRing: p.cfg.KeyRing}
}

func (p *Provisioner) checkIssuerDeployment(ctx context.Context, organizationID string) error {
	issuer, err := jwksrepo.New(p.db).GetOrganizationInternalSessionIssuerURL(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return internalsessionissuers.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load issuer ownership: %w", err)
	}
	if err := p.issuerDeployment().CheckIssuer(organizationID, issuer); err != nil {
		return fmt.Errorf("check issuer deployment: %w", err)
	}
	return nil
}

// EnsureEMAIssuer creates the organization's issuer with one dedicated KMS
// key, re-enables a disabled one, or returns the live one. Concurrent callers
// adopt the winner and disable their unused key.
func (p *Provisioner) EnsureEMAIssuer(ctx context.Context, organizationID string) (uuid.UUID, error) {
	existing, err := jwksrepo.New(p.db).GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err == nil {
		if err := p.issuerDeployment().CheckIssuer(organizationID, existing.Issuer); err != nil {
			return uuid.Nil, fmt.Errorf("check existing issuer deployment: %w", err)
		}
		if existing.DisabledAt.Valid {
			if err := p.issuerLifecycle().SetDisabled(ctx, organizationID, false); err != nil {
				return uuid.Nil, fmt.Errorf("re-enable issuer: %w", err)
			}
		}
		return existing.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("read organization issuer: %w", err)
	}
	if _, err := internalsessionissuers.URL(p.cfg.ServerURL, organizationID, p.cfg.AllowLoopbackIssuer); err != nil {
		return uuid.Nil, fmt.Errorf("%w: issuer origin is unavailable", internalsessionissuers.ErrMisconfigured)
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
	issuerURL, err := internalsessionissuers.URL(p.cfg.ServerURL, organizationID, p.cfg.AllowLoopbackIssuer)
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
	actor, displayName := internalsessionissuers.Actor(ctx, organizationID)
	if err := p.audit.LogJsonWebKeySetCreate(ctx, tx, audit.LogJsonWebKeySetCreateEvent{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil, SetURN: urn.NewJsonWebKeySet(set.ID), SetName: set.Name, InternalSessionIssuer: true}); err != nil {
		return uuid.Nil, fmt.Errorf("audit issuer key set: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, tx, internalsessionissuers.KeyEvent(ctx, organizationID, key, nil, nil)); err != nil {
		return uuid.Nil, fmt.Errorf("audit issuer key: %w", err)
	}
	if err := p.audit.LogInternalSessionIssuerEnable(ctx, tx, internalsessionissuers.IssuerEvent(ctx, organizationID, issuer)); err != nil {
		return uuid.Nil, fmt.Errorf("audit issuer enable: %w", err)
	}
	if err := commitPublication(ctx, tx, organizationID); err != nil {
		return uuid.Nil, err
	}
	return issuer.ID, nil
}

// RotateEMAIssuer rotates the issuer's signing key in two calls, like
// RotateClient: the first publishes a pending key and returns
// RotationPendingError; a call after ReadyAt activates it and retires the
// previous key, which stays published for RetiredKeyOverlap. activeKid is the
// key the caller saw active.
func (p *Provisioner) RotateEMAIssuer(ctx context.Context, organizationID, activeKid string) error {
	if err := p.checkIssuerDeployment(ctx, organizationID); err != nil {
		return err
	}
	_, err := p.rotateEMAIssuerRows(ctx, organizationID, activeKid, uuid.Nil)
	if _, _, cleanupErr := p.CleanupEMAIssuerKMS(ctx, organizationID); cleanupErr != nil {
		p.logger.WarnContext(ctx, "issuer kms cleanup is pending; the sweep retries it", attr.SlogOrganizationID(organizationID), attr.SlogError(cleanupErr))
	}
	if errors.Is(err, errPublishPending) {
		return p.publishEMAIssuerKey(ctx, organizationID, activeKid)
	}
	return err
}

// errPublishPending tells RotateEMAIssuer to mint the pending key with no lock held.
var errPublishPending = errors.New("publish a pending issuer key")

// rotateEMAIssuerRows runs the locked half of a rotation. Keys it revokes
// stay pending KMS cleanup until CleanupEMAIssuerKMS confirms them. A sweep
// passes the ready pending key it approved as only: then it only activates
// that key, never publishes one, and skips a disabled issuer.
func (p *Provisioner) rotateEMAIssuerRows(ctx context.Context, organizationID, activeKid string, only uuid.UUID) (activated bool, err error) {
	sweep := only != uuid.Nil
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin issuer rotation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := internalsessionissuers.LockIssuerKeys(ctx, jq, organizationID)
	if err != nil {
		return false, fmt.Errorf("lock issuer keys: %w", err)
	}
	// Expiring retired keys is backstop work, not the caller's choice, so its
	// audit rows name the system on every path.
	sysCtx, cancelSys := systemContext(ctx)
	defer cancelSys()
	if err := p.revokeExpiredEMAIssuerKeys(sysCtx, tx, organizationID, set.ID); err != nil {
		return false, err
	}
	commit := func(result error) error {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit issuer rotation: %w", err)
		}
		return result
	}
	// A disable that committed after the caller's checks still wins.
	issuer, err := jq.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("recheck issuer for rotation: %w", err)
	}
	if issuer.DisabledAt.Valid {
		if sweep {
			return false, commit(nil)
		}
		return false, commit(&internalsessionissuers.SigningRefusedError{State: internalsessionissuers.StateDisabled, Reason: ""})
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return false, fmt.Errorf("load issuer keys: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if active.ID == uuid.Nil {
		return false, commit(internalsessionissuers.ErrNotFound)
	}
	if active.Kid != activeKid {
		known, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, Kid: activeKid})
		if err != nil {
			return false, fmt.Errorf("check rotation source kid: %w", err)
		}
		if !known {
			return false, commit(internalsessionissuers.ErrUnknownKid)
		}
		// Another caller finished this rotation.
		return false, commit(nil)
	}
	if sweep && pending.ID != only {
		// Revoked or replaced since it was approved.
		return false, commit(nil)
	}
	if pending.ID == uuid.Nil {
		return false, commit(errPublishPending)
	}
	if pending.UpdatedAt.InfinityModifier != pgtype.Finite {
		if err := commit(nil); err != nil {
			return false, err
		}
		published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: pending.ID, OrganizationID: organizationID})
		if err != nil {
			return false, fmt.Errorf("observe recovered issuer key publication: %w", err)
		}
		return false, &internalsessionissuers.RotationPendingError{ReadyAt: published.Time.Add(internalsessionissuers.ActivationDelay)}
	}
	ready, err := repo.New(tx).RotationPublicationReady(ctx, repo.RotationPublicationReadyParams{ID: pending.ID, OrganizationID: organizationID, CacheSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)})
	if err != nil {
		return false, fmt.Errorf("check issuer rotation cache window: %w", err)
	}
	if !ready {
		return false, commit(&internalsessionissuers.RotationPendingError{ReadyAt: pending.UpdatedAt.Time.Add(internalsessionissuers.ActivationDelay)})
	}
	if sweep {
		// Re-run the gates under the lock: a pause or rollout change after the
		// approval must stop the activation.
		if err := p.emaActivationCheck(ctx, organizationID, pending); err != nil {
			if _, refused := errors.AsType[*internalsessionissuers.SigningRefusedError](err); refused {
				return false, commit(nil)
			}
			return false, fmt.Errorf("recheck issuer key activation: %w", err)
		}
	}
	if err := p.activateEMAIssuerKey(ctx, tx, organizationID, set, pending); err != nil {
		return false, err
	}
	if err := commit(nil); err != nil {
		return false, err
	}
	return true, nil
}

// activateReadyEMAIssuerKey finishes a routine rotation whose pending key is
// past the activation delay, through the same locked path as a second rotate
// call, once the activation check approves it. Audit rows name the system.
func (p *Provisioner) activateReadyEMAIssuerKey(ctx context.Context, organizationID string) (bool, error) {
	if p.emaActivationCheck == nil {
		return false, nil
	}
	q := jwksrepo.New(p.db)
	issuer, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read issuer for activation: %w", err)
	case issuer.DisabledAt.Valid:
		return false, nil
	}
	keys, err := q.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: issuer.JsonWebKeySetID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return false, fmt.Errorf("load issuer keys for activation: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if pending.ID == uuid.Nil || active.ID == uuid.Nil {
		return false, nil
	}
	if pending.UpdatedAt.InfinityModifier != pgtype.Finite {
		// A publication that committed without being observed starts its
		// activation delay now; a later sweep activates it.
		if _, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: pending.ID, OrganizationID: organizationID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("observe unobserved issuer key publication: %w", err)
		}
		return false, nil
	}
	ready, err := repo.New(p.db).RotationPublicationReady(ctx, repo.RotationPublicationReadyParams{ID: pending.ID, OrganizationID: organizationID, CacheSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("check issuer key activation delay: %w", err)
	case !ready:
		return false, nil
	}
	if err := p.checkIssuerDeployment(ctx, organizationID); err != nil {
		if errors.Is(err, internalsessionissuers.ErrMisconfigured) {
			// Another deployment's issuer, e.g. after an outbound origin change:
			// it can never activate here, so it is skipped, not failed.
			p.logger.WarnContext(ctx, "skipping issuer key activation for an issuer this deployment doesn't own", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
			return false, nil
		}
		return false, err
	}
	sysCtx, cancel := systemContext(ctx)
	defer cancel()
	if err := p.emaActivationCheck(sysCtx, organizationID, pending); err != nil {
		if _, refused := errors.AsType[*internalsessionissuers.SigningRefusedError](err); refused {
			return false, nil
		}
		return false, fmt.Errorf("check issuer key activation: %w", err)
	}
	activated, err := p.rotateEMAIssuerRows(sysCtx, organizationID, active.Kid, pending.ID)
	if _, notReady := errors.AsType[*internalsessionissuers.RotationPendingError](err); notReady {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("activate ready issuer key: %w", err)
	}
	return activated, nil
}

// systemContext keeps the caller's trace and deadline but none of its values,
// so audit rows written under it name the system, never a request actor.
func systemContext(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := trace.ContextWithSpan(context.Background(), trace.SpanFromContext(ctx))
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithCancel(detached)
}

func (p *Provisioner) revokeExpiredEMAIssuerKeys(ctx context.Context, tx pgx.Tx, organizationID string, setID uuid.UUID) error {
	expired, err := jwksrepo.New(tx).RevokeExpiredEMAIssuerKeys(ctx, jwksrepo.RevokeExpiredEMAIssuerKeysParams{JsonWebKeySetID: setID, OrganizationID: organizationID, OverlapSeconds: int32(internalsessionissuers.RetiredKeyOverlap / time.Second)})
	if err != nil {
		return fmt.Errorf("revoke expired issuer keys: %w", err)
	}
	for _, key := range expired {
		before := internalsessionissuers.KeySnapshot(key)
		before.State = "retired"
		if err := p.audit.LogJsonWebKeyRevoke(ctx, tx, internalsessionissuers.KeyEvent(ctx, organizationID, key, before, internalsessionissuers.KeySnapshot(key))); err != nil {
			return fmt.Errorf("audit expired issuer key: %w", err)
		}
	}
	return nil
}

// CleanupEMAIssuerKMS disables the KMS version and removes the signer grant
// of every revoked issuer key still pending cleanup, marking each one done.
// It never runs under a lock; a key that fails stays pending for the sweep.
func (p *Provisioner) CleanupEMAIssuerKMS(ctx context.Context, organizationID string) (cleaned, pending int, err error) {
	keys, err := jwksrepo.New(p.db).ListPendingEMAIssuerKMSCleanup(ctx, organizationID)
	if err != nil {
		return 0, 0, fmt.Errorf("list pending issuer kms cleanup: %w", err)
	}
	if len(keys) == 0 {
		return 0, 0, nil
	}
	for _, key := range keys {
		issuer, err := jwksrepo.New(p.db).GetInternalSessionIssuerForOwnership(ctx, jwksrepo.GetInternalSessionIssuerForOwnershipParams{OrganizationID: organizationID, JsonWebKeySetID: key.JsonWebKeySetID})
		if err != nil {
			return 0, len(keys), fmt.Errorf("load cleanup issuer ownership: %w", err)
		}
		if err := p.issuerDeployment().CheckIssuer(organizationID, issuer); err != nil {
			return 0, len(keys), err
		}
	}
	openCtx, cancelOpen := p.issuerKeyCleanupContext(ctx)
	defer cancelOpen()
	kms, err := p.openKMSClient(openCtx)
	if err != nil {
		return 0, len(keys), fmt.Errorf("open kms client for issuer cleanup: %w", err)
	}
	defer o11y.LogDefer(ctx, p.logger, "failed to close gcp kms client", func() error { return kms.Close() })
	var errs []error
	for _, key := range keys {
		// A caller deadline caps the whole run; keys left stay pending.
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			errs = append(errs, fmt.Errorf("issuer kms cleanup stopped at its deadline: %w", context.DeadlineExceeded))
			break
		}
		// Each key gets its own deadline, so one slow key can't starve the rest.
		if err := p.cleanupEMAIssuerKey(ctx, kms, organizationID, key); err != nil {
			errs = append(errs, err)
			touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if touchErr := jwksrepo.New(p.db).TouchJsonWebKeyKMSCleanupAttempt(touchCtx, jwksrepo.TouchJsonWebKeyKMSCleanupAttemptParams{ID: key.ID, OrganizationID: organizationID}); touchErr != nil {
				errs = append(errs, fmt.Errorf("record issuer kms cleanup attempt: %w", touchErr))
			}
			cancel()
			continue
		}
		cleaned++
	}
	return cleaned, len(keys) - cleaned, errors.Join(errs...)
}

// issuerKeyCleanupContext is detached from cancellation so a cancelled request
// still records a finished step, but never runs past the cleanup timeout or
// the caller's deadline.
func (p *Provisioner) issuerKeyCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(p.issuerKeyCleanupTimeout)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

func (p *Provisioner) cleanupEMAIssuerKey(ctx context.Context, kms gcpkms.ProvisioningClient, organizationID string, key jwksrepo.JsonWebKey) error {
	keyCtx, cancel := p.issuerKeyCleanupContext(ctx)
	defer cancel()
	if err := p.disableEMAIssuerKey(keyCtx, kms, organizationID, key); err != nil {
		return err
	}
	if err := jwksrepo.New(p.db).MarkJsonWebKeyKMSDisabled(keyCtx, jwksrepo.MarkJsonWebKeyKMSDisabledParams{ID: key.ID, OrganizationID: organizationID}); err != nil {
		return fmt.Errorf("record issuer kms cleanup: %w", err)
	}
	return nil
}

func (p *Provisioner) disableEMAIssuerKey(ctx context.Context, kms gcpkms.ProvisioningClient, organizationID string, key jwksrepo.JsonWebKey) error {
	q := repo.New(p.db)
	row, err := q.GetManagedKeyResource(ctx, repo.GetManagedKeyResourceParams{ID: key.ExternalKeyID, OrganizationID: conv.ToPGText(organizationID)})
	if err != nil {
		return fmt.Errorf("load revoked issuer key resource: %w", err)
	}
	if err := p.issuerDeployment().CheckKey(row.ResourceName); err != nil {
		return fmt.Errorf("check revoked issuer key deployment: %w", err)
	}
	// Disabling the version and removing the grant are independent: either
	// one alone stops the old signer, so they run concurrently and a stalled
	// disable can't spend the grant removal's deadline.
	var disableErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := kms.DisableKeyVersion(ctx, row.ResourceName); err != nil {
			disableErr = fmt.Errorf("disable revoked issuer kms key version: %w", err)
		}
	})
	revokeErr := p.revokeEMAIssuerKeyGrant(ctx, kms, q, row)
	wg.Wait()
	return errors.Join(disableErr, revokeErr)
}

func (p *Provisioner) revokeEMAIssuerKeyGrant(ctx context.Context, kms gcpkms.ProvisioningClient, q *repo.Queries, row repo.GetManagedKeyResourceRow) error {
	keyName, err := gcpkms.KeyNameForVersion(row.ResourceName)
	if err != nil {
		return fmt.Errorf("derive revoked issuer kms key name: %w", err)
	}
	// The grant belongs to the account the key's credential impersonated when
	// the key was created, read even after the credential is deleted. The
	// configured signer may have changed since, so it never stands in; with no
	// known account, cleanup stays pending rather than claim success.
	granted, err := q.GetPlatformGcpIamCredentialServiceAccount(ctx, row.ExternalCredentialID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load revoked issuer key's signing credential: %w", err)
	}
	serviceAccount := strings.TrimSpace(granted.String)
	if serviceAccount == "" {
		return errors.New("revoked issuer key's signer grant has no known service account; remove it by hand")
	}
	if err := kms.RevokeSignerVerifier(ctx, keyName, serviceAccount); err != nil {
		return fmt.Errorf("revoke signer grant on revoked issuer key: %w", err)
	}
	return nil
}

// sweepEMAIssuerOrganization activates a ready pending key, revokes retired
// issuer keys past the overlap, then retries every pending KMS cleanup. It
// reports how many stay pending.
func (p *Provisioner) sweepEMAIssuerOrganization(ctx context.Context, organizationID string) (activated bool, pending int, err error) {
	activated, activateErr := p.activateReadyEMAIssuerKey(ctx, organizationID)
	if activated {
		p.logger.InfoContext(ctx, "activated a ready internal session issuer key", attr.SlogOrganizationID(organizationID))
	}
	// Cleanup of earlier revocations never waits on this revocation step.
	var revokeErr error
	due, err := jwksrepo.New(p.db).HasExpiredEMAIssuerKeys(ctx, jwksrepo.HasExpiredEMAIssuerKeysParams{OrganizationID: organizationID, OverlapSeconds: int32(internalsessionissuers.RetiredKeyOverlap / time.Second)})
	switch {
	case err != nil:
		revokeErr = fmt.Errorf("check expired issuer keys: %w", err)
	case due:
		revokeErr = p.revokeExpiredIssuerKeysNow(ctx, organizationID)
	}
	_, pending, cleanupErr := p.CleanupEMAIssuerKMS(ctx, organizationID)
	return activated, pending, errors.Join(activateErr, revokeErr, cleanupErr)
}

func (p *Provisioner) revokeExpiredIssuerKeysNow(ctx context.Context, organizationID string) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer key sweep: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	set, err := internalsessionissuers.LockIssuerKeys(ctx, jwksrepo.New(tx), organizationID)
	if err != nil {
		return fmt.Errorf("lock issuer keys: %w", err)
	}
	if err := p.revokeExpiredEMAIssuerKeys(ctx, tx, organizationID, set.ID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issuer key sweep: %w", err)
	}
	return nil
}

// SweepEMAIssuerKeysFrom sweeps organizations with expired keys or pending KMS
// cleanup, paging by organization id from after. With a budget it stops
// between organizations once the budget is spent and reports where to resume,
// so one call stays bounded however large the fleet. Pending is the
// fleet-wide count of revoked keys still awaiting KMS cleanup afterwards, or
// -1 when it could not be read.
func (p *Provisioner) SweepEMAIssuerKeysFrom(ctx context.Context, after string, budget time.Duration) (internalsessionissuers.SweepProgress, error) {
	progress := internalsessionissuers.SweepProgress{Swept: 0, Activated: 0, Pending: -1, Next: "", Done: false}
	q := jwksrepo.New(p.db)
	started := time.Now()
	var errs []error
pages:
	for {
		organizations, err := q.ListOrganizationsWithPendingEMAIssuerWork(ctx, jwksrepo.ListOrganizationsWithPendingEMAIssuerWorkParams{AfterOrganizationID: after, OverlapSeconds: int32(internalsessionissuers.RetiredKeyOverlap / time.Second), ActivationSeconds: int32(internalsessionissuers.ActivationDelay / time.Second), MaxOrganizations: p.sweepPage})
		if err != nil {
			errs = append(errs, fmt.Errorf("list organizations with issuer key work: %w", err))
			progress.Next = after
			break
		}
		for _, organizationID := range organizations {
			// Every call sweeps at least one organization, so it always advances.
			if progress.Swept > 0 && ((budget > 0 && time.Since(started) >= budget) || ctx.Err() != nil) {
				progress.Next = after
				break pages
			}
			orgCtx, cancel := context.WithTimeout(ctx, p.sweepOrganizationTimeout)
			activated, _, err := p.sweepEMAIssuerOrganization(orgCtx, organizationID)
			cancel()
			if activated {
				progress.Activated++
			}
			if err != nil && !errors.Is(err, internalsessionissuers.ErrNotFound) {
				p.logger.WarnContext(ctx, "internal session issuer key sweep failed for an organization", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
				errs = append(errs, fmt.Errorf("sweep organization %s issuer keys: %w", organizationID, err))
			}
			progress.Swept++
			after = organizationID
		}
		if len(organizations) < int(p.sweepPage) {
			progress.Done = true
			break
		}
	}
	count, err := q.CountPendingEMAIssuerKMSCleanup(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("count pending issuer kms cleanup: %w", err))
	} else {
		progress.Pending = int(count)
	}
	return progress, errors.Join(errs...)
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
		before := internalsessionissuers.KeySnapshot(previous)
		before.State = "active"
		if err := p.audit.LogJsonWebKeyRetire(ctx, tx, internalsessionissuers.KeyEvent(ctx, organizationID, previous, before, internalsessionissuers.KeySnapshot(previous))); err != nil {
			return fmt.Errorf("audit issuer key retirement: %w", err)
		}
	}
	activated, err := jq.ActivateJsonWebKey(ctx, jwksrepo.ActivateJsonWebKeyParams{ID: next.ID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("activate issuer key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyActivate(ctx, tx, internalsessionissuers.KeyEvent(ctx, organizationID, activated, internalsessionissuers.KeySnapshot(next), internalsessionissuers.KeySnapshot(activated))); err != nil {
		return fmt.Errorf("audit issuer key activation: %w", err)
	}
	updated, err := jq.SetOrganizationEMAIssuerBackingKey(ctx, jwksrepo.SetOrganizationEMAIssuerBackingKeyParams{ExternalKeyID: activated.ExternalKeyID, ID: set.ID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("re-point issuer backing key: %w", err)
	}
	actor, displayName := internalsessionissuers.Actor(ctx, organizationID)
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

		InternalSessionIssuer: true,
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
	set, err := internalsessionissuers.LockIssuerKeys(ctx, jq, organizationID)
	if err != nil {
		return nil, fmt.Errorf("lock issuer keys: %w", err)
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
	if err := p.audit.LogJsonWebKeyPublish(ctx, tx, internalsessionissuers.KeyEvent(ctx, organizationID, key, nil, nil)); err != nil {
		return jwksrepo.JsonWebKey{}, fmt.Errorf("audit issuer key publication: %w", err)
	}
	return key, nil
}

// ProvisionEMAIssuerKey activates a fresh key on a keyless issuer, the second
// step of a compromise response. It mints nothing when a key is already
// active, and a concurrent caller's key wins, so repeats never pile up keys.
func (p *Provisioner) ProvisionEMAIssuerKey(ctx context.Context, organizationID string) error {
	if err := p.checkIssuerDeployment(ctx, organizationID); err != nil {
		return err
	}
	keyless, err := p.emaIssuerKeyless(ctx, p.db, organizationID)
	if err != nil || !keyless {
		return err
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
	if err := p.activateReplacementRows(ctx, organizationID, signer, created); err != nil {
		p.abandonKey(ctx, logger, kms, uuid.Nil, created.key, signer, err)
		if _, ok := errors.AsType[*adoptedError](err); ok {
			return nil
		}
		return issuerError(err)
	}
	return nil
}

func (p *Provisioner) emaIssuerKeyless(ctx context.Context, db jwksrepo.DBTX, organizationID string) (bool, error) {
	q := jwksrepo.New(db)
	issuer, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, internalsessionissuers.ErrNotFound
	case err != nil:
		return false, fmt.Errorf("read issuer: %w", err)
	}
	_, err = q.GetActiveJsonWebKey(ctx, jwksrepo.GetActiveJsonWebKeyParams{JsonWebKeySetID: issuer.JsonWebKeySetID, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("read active issuer key: %w", err)
	}
	return false, nil
}

func (p *Provisioner) activateReplacementRows(ctx context.Context, organizationID string, signer *signingCredential, created *createdKey) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer key replacement: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	jq := jwksrepo.New(tx)
	set, err := internalsessionissuers.LockIssuerKeys(ctx, jq, organizationID)
	if err != nil {
		return fmt.Errorf("lock issuer keys: %w", err)
	}
	keyless, err := p.emaIssuerKeyless(ctx, tx, organizationID)
	if err != nil {
		return err
	}
	if !keyless {
		return &adoptedError{client: nil}
	}
	next, err := p.insertEMAIssuerKey(ctx, tx, organizationID, set.ID, "pending", signer, created)
	if err != nil {
		return err
	}
	if err := p.activateEMAIssuerKey(ctx, tx, organizationID, set, next); err != nil {
		return err
	}
	return commitPublication(ctx, tx, organizationID)
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
	referenced, err := repo.New(tx).ManagedKeyResourceExists(ctx, repo.ManagedKeyResourceExistsParams{ResourceName: resourceName, OrganizationID: organizationID})
	if err != nil {
		return false, fmt.Errorf("lookup published key resource: %w", err)
	}
	return !referenced, nil
}
