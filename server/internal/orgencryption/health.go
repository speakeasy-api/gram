package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

const (
	// DefaultCheckInterval is how often each organization's KEKs are probed.
	// Hourly keeps a revoked customer key from going unnoticed for long while
	// costing two KMS calls per DEK per hour.
	DefaultCheckInterval = time.Hour

	// checkOrganizationTimeout bounds one organization's probe, so one slow
	// customer KMS cannot stall a sweep.
	checkOrganizationTimeout = time.Minute

	// recordCheckTimeout bounds recording a probe's outcome. It runs on a
	// context detached from the probe's, so a probe that timed out still
	// advances the check time and moves to the back of the queue.
	recordCheckTimeout = 10 * time.Second
)

// Check probes the organization's KEKs: it unwraps every DEK under each KEK
// the configuration reads with, and re-wraps a DEK whose KEK has a new primary
// version. A permanent failure of the current KEK suspends the organization; a
// later clean probe resumes it. A switch past its grace period with every DEK
// wrapped by the new KEK is completed.
func (p *Provisioner) Check(ctx context.Context, organizationID string) error {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	q := repo.New(p.db)

	config, err := getConfig(ctx, q, organizationID)
	if err != nil {
		return err
	}

	probeErr := p.checkConfig(ctx, logger, config)

	var checkError *string
	if probeErr != nil {
		checkError = new(conv.TruncateDetail(probeErr.Error(), reasonMaxLen))
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordCheckTimeout)
	defer cancel()
	if err := q.RecordEncryptionKeyCheck(recordCtx, repo.RecordEncryptionKeyCheckParams{
		LastKekCheckError: conv.PtrToPGText(checkError),
		OrganizationID:    organizationID,
	}); err != nil {
		return errors.Join(probeErr, fmt.Errorf("record encryption key check: %w", err))
	}

	return probeErr
}

func (p *Provisioner) checkConfig(ctx context.Context, logger *slog.Logger, config repo.OrganizationEncryptionConfig) error {
	organizationID := config.OrganizationID

	// Suspended is a known state the probe may resume from; anything else that
	// is not usable is reported rather than treated as healthy.
	if err := requireUsableState(config.State); err != nil && !errors.Is(err, ErrSuspended) {
		return err
	}

	_, currentErr := p.probeKey(ctx, logger, organizationID, config.KekExternalKeyID)
	if currentErr != nil {
		if rolledBack, err := p.rollBackBrokenSwitch(ctx, logger, config, currentErr); rolledBack || err != nil {
			return err
		}
		if permanentKeyFailure(currentErr) && ConfigState(config.State) != ConfigStateSuspended {
			logger.WarnContext(ctx, "suspending organization encryption: key encryption key is unusable", attr.SlogError(currentErr))
			if err := p.Suspend(ctx, organizationID, currentErr.Error(), SystemActor()); err != nil {
				return errors.Join(currentErr, err)
			}
		}
		return currentErr
	}

	var previousErr error
	if config.PreviousKekExternalKeyID.Valid {
		_, previousErr = p.probeKey(ctx, logger, organizationID, config.PreviousKekExternalKeyID.UUID)
	}

	if ConfigState(config.State) == ConfigStateSuspended {
		if err := p.resume(ctx, organizationID, SystemActor()); err != nil {
			return err
		}
		return previousErr
	}

	if ConfigState(config.State) == ConfigStateSwitching {
		if started, ok := pgTimestamp(config.SwitchStartedAt); ok && p.now().Sub(started) >= p.cfg.SwitchGracePeriod {
			if _, err := p.CompleteSwitch(ctx, organizationID, SystemActor()); err != nil {
				return errors.Join(previousErr, fmt.Errorf("complete switch after grace period: %w", err))
			}
			return nil
		}
	}

	return previousErr
}

// rollBackBrokenSwitch handles a switch whose new KEK failed permanently. While
// the previous KEK still unwraps every DEK, the organization goes back to it
// instead of being suspended: reads already fall back to the previous KEK, and
// suspending would block them for a fault the rollback avoids. The customer can
// switch again once the new key's access is fixed. It reports whether it
// rolled back; with false and no error, the caller handles currentErr.
func (p *Provisioner) rollBackBrokenSwitch(ctx context.Context, logger *slog.Logger, config repo.OrganizationEncryptionConfig, currentErr error) (bool, error) {
	if ConfigState(config.State) != ConfigStateSwitching || !config.PreviousKekExternalKeyID.Valid || !permanentKeyFailure(currentErr) {
		return false, nil
	}
	if _, err := p.probeKey(ctx, logger, config.OrganizationID, config.PreviousKekExternalKeyID.UUID); err != nil {
		return false, nil
	}

	logger.WarnContext(ctx, "rolling back organization encryption key switch: new key encryption key is unusable", attr.SlogError(currentErr))
	if _, err := p.RollbackSwitch(ctx, config.OrganizationID, SystemActor()); err != nil {
		return false, errors.Join(currentErr, fmt.Errorf("roll back switch to unusable key: %w", err))
	}

	return true, fmt.Errorf("rolled back the key switch because the new key is unusable: %w", currentErr)
}

// probeKey unwraps every DEK wrapped by keyID and re-wraps any whose wrap was
// made under an older primary version. It returns how many it re-wrapped.
func (p *Provisioner) probeKey(ctx context.Context, logger *slog.Logger, organizationID string, keyID uuid.UUID) (int, error) {
	q := repo.New(p.db)

	key, err := p.keys.open(ctx, logger, q, organizationID, keyID)
	if err != nil {
		return 0, err
	}
	defer o11y.LogDefer(ctx, logger, "close kms encryption client", key.Close)

	rows, err := q.ListDataEncryptionKeyWrapsForKey(ctx, repo.ListDataEncryptionKeyWrapsForKeyParams{ExternalKeyID: keyID, OrganizationID: organizationID})
	if err != nil {
		return 0, fmt.Errorf("list data encryption key wraps: %w", err)
	}

	rewrapped := 0
	for _, row := range rows {
		dek := row.DataEncryptionKey
		// Every readable DEK is wrapped by every KEK the configuration reads
		// with, so a missing wrap leaves that DEK unreadable under this key.
		if !row.WrapID.Valid {
			return rewrapped, fmt.Errorf("%w: data encryption key %s has no wrap under key %s", ErrKeyUnavailable, dek.ID, keyID)
		}

		dataKey, err := unwrapDataKey(ctx, key, organizationID, dek, row.WrappedKey)
		if err != nil {
			return rewrapped, err
		}
		if err := q.TouchDataEncryptionKeyWrapUnwrapped(ctx, repo.TouchDataEncryptionKeyWrapUnwrappedParams{ID: row.WrapID.UUID, OrganizationID: organizationID}); err != nil {
			return rewrapped, fmt.Errorf("record data encryption key unwrap: %w", err)
		}

		// Encrypting again is the only way to learn the current primary
		// version; a changed one means the old version may later be disabled.
		result, err := key.wrap(ctx, organizationID, dek.ID, dek.UseCase, dataKey)
		if err != nil {
			return rewrapped, err
		}
		if result.KeyVersionName == row.ExternalKeyVersion.String {
			continue
		}
		if _, err := q.ReplaceDataEncryptionKeyWrap(ctx, repo.ReplaceDataEncryptionKeyWrapParams{
			WrappedKey:         result.Ciphertext,
			ExternalKeyVersion: conv.ToPGText(result.KeyVersionName),
			ID:                 row.WrapID.UUID,
			OrganizationID:     organizationID,
		}); err != nil {
			return rewrapped, fmt.Errorf("re-wrap data encryption key: %w", err)
		}
		rewrapped++
	}

	return rewrapped, nil
}

// permanentKeyFailure reports whether a KEK failure needs someone to fix
// access or configuration, as opposed to an outage worth riding out.
func permanentKeyFailure(err error) bool {
	if errors.Is(err, ErrInvalidKey) {
		return true
	}

	switch gcpkms.ReasonForError(err) {
	case gcpkms.ReasonPermissionDenied, gcpkms.ReasonKeyNotFound, gcpkms.ReasonKeyUnusable, gcpkms.ReasonInvalidResourceName:
		return true
	default:
		return false
	}
}

// CheckDue probes up to pageSize organizations whose last check is older than
// interval, oldest first, and stops starting new ones once budget has passed.
// It returns how many it checked. A failure for one organization is logged and
// recorded on its configuration, so it moves to the back of the queue rather
// than blocking the rest.
func (p *Provisioner) CheckDue(ctx context.Context, interval time.Duration, pageSize int32, budget time.Duration) (int, error) {
	organizationIDs, err := repo.New(p.db).ListEncryptionConfigsDueForCheck(ctx, repo.ListEncryptionConfigsDueForCheckParams{
		CheckIntervalSeconds: conv.SafeInt32(int(interval / time.Second)),
		PageSize:             pageSize,
	})
	if err != nil {
		return 0, fmt.Errorf("list organizations due for encryption check: %w", err)
	}

	deadline := p.now().Add(budget)
	checked := 0
	for _, organizationID := range organizationIDs {
		if !p.now().Before(deadline) {
			break
		}

		checkCtx, cancel := context.WithTimeout(ctx, checkOrganizationTimeout)
		if err := p.Check(checkCtx, organizationID); err != nil {
			p.logger.WarnContext(ctx, "organization encryption check failed", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		}
		cancel()
		checked++
	}

	return checked, nil
}

// SweepManagedKeys disables managed KEKs nothing has depended on for the
// managed key retention and removes the platform grant on them. It returns
// how many it disabled.
func (p *Provisioner) SweepManagedKeys(ctx context.Context, pageSize int32) (int, error) {
	q := repo.New(p.db)
	retiredBefore := conv.ToPGTimestamptz(p.now().Add(-p.cfg.ManagedKeyRetention))

	rows, err := q.ListManagedEncryptionKeysPendingDisable(ctx, repo.ListManagedEncryptionKeysPendingDisableParams{RetiredBefore: retiredBefore, PageSize: pageSize})
	if err != nil {
		return 0, fmt.Errorf("list managed encryption keys pending disable: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	client, err := p.openProvisioningClient(ctx)
	if err != nil {
		return 0, err
	}
	defer o11y.LogDefer(ctx, p.logger, "close kms provisioning client", client.Close)

	disabled := 0
	var errs []error
	for _, row := range rows {
		done, err := p.disableManagedKey(ctx, client, row, retiredBefore)
		if err != nil {
			p.logger.WarnContext(ctx, "disable unused managed encryption key", attr.SlogOrganizationID(row.OrganizationID), attr.SlogError(err))
			errs = append(errs, err)
			continue
		}
		if done {
			disabled++
		}
	}

	return disabled, errors.Join(errs...)
}

// disableManagedKey disables one managed KEK while holding the organization
// lock, after re-checking that nothing uses it. The pending list is a snapshot;
// a switch that started using the key since then commits under the same lock,
// and takes a share lock on the key that the disabled marker then refuses.
// The KMS calls run inside the transaction: holding one organization's lock
// for two management calls is cheap next to disabling a key in use.
// It reports whether it disabled the key.
func (p *Provisioner) disableManagedKey(ctx context.Context, client gcpkms.EncryptionProvisioningClient, row repo.ListManagedEncryptionKeysPendingDisableRow, retiredBefore pgtype.Timestamptz) (bool, error) {
	if err := p.keys.checkManagedResource(row.ResourceName); err != nil {
		return false, err
	}

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin managed key disable transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, row.OrganizationID); err != nil {
		return false, err
	}
	unused, err := q.ManagedEncryptionKeyIsUnused(ctx, repo.ManagedEncryptionKeyIsUnusedParams{ExternalKeyID: row.ExternalKeyID, OrganizationID: row.OrganizationID, RetiredBefore: retiredBefore})
	if err != nil {
		return false, fmt.Errorf("re-check managed encryption key use: %w", err)
	}
	if !unused {
		return false, nil
	}

	if err := client.DisableEncryptionKey(ctx, row.ResourceName); err != nil {
		return false, fmt.Errorf("disable managed encryption key: %w", err)
	}
	if err := client.RevokeEncrypterDecrypter(ctx, row.ResourceName, p.cfg.Keys.PlatformServiceAccount); err != nil {
		return false, fmt.Errorf("revoke platform grant on managed encryption key: %w", err)
	}
	if err := q.MarkManagedEncryptionKeyDisabled(ctx, repo.MarkManagedEncryptionKeyDisabledParams{ExternalKeyID: row.ExternalKeyID, OrganizationID: row.OrganizationID}); err != nil {
		return false, fmt.Errorf("mark managed encryption key disabled: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, &commitError{err: err}
	}

	return true, nil
}

// ProbeOutcomeCredentialUnusable reports a key Speakeasy cannot reach because
// its record or credential is unusable: deleted, wrong purpose, or screened
// out. No KMS call was made.
const ProbeOutcomeCredentialUnusable gcpkms.VerifyReason = "credential_unusable" //nolint:gosec // a probe outcome name, not a credential

// verifyFailedDetail is the detail returned when verification failed for an
// internal reason. The underlying error is logged, never returned.
const verifyFailedDetail = "key verification could not be completed; try again shortly"

// VerifyKey proves Speakeasy can encrypt and decrypt with one of the
// organization's keys. It first proves Speakeasy can assume the key's
// credential, so a missing impersonation grant reports as an unusable
// credential rather than as a KMS permission error. Nothing is persisted.
func (p *Provisioner) VerifyKey(ctx context.Context, organizationID string, keyID uuid.UUID) gcpkms.VerifyResult {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))

	key, err := p.keys.open(ctx, logger, repo.New(p.db), organizationID, keyID)
	if err != nil {
		switch {
		case errors.Is(err, gcpkms.ErrInvalidResourceName):
			return unverified(gcpkms.ReasonInvalidResourceName, err.Error())
		case errors.Is(err, ErrInvalidKey), errors.Is(err, ErrPlatformCredentialUnusable):
			return unverified(ProbeOutcomeCredentialUnusable, err.Error())
		default:
			logger.ErrorContext(ctx, "open encryption key for verification", attr.SlogError(err))
			return unverified(gcpkms.ReasonUnexpected, verifyFailedDetail)
		}
	}
	defer o11y.LogDefer(ctx, logger, "close kms encryption client", key.Close)

	// Resolving the principal is what separates "Speakeasy cannot assume this
	// identity" from a key-level permission problem; the token source is lazy.
	if _, err := p.identity.ResolvePrincipal(ctx, key.credential); err != nil {
		logger.InfoContext(ctx, "encryption key verification could not assume the credential", attr.SlogError(err))
		return unverified(ProbeOutcomeCredentialUnusable, "cannot authenticate as the key's credential: "+err.Error())
	}

	return gcpkms.VerifyEncryptionKey(ctx, key.client, key.resourceName)
}

func unverified(reason gcpkms.VerifyReason, detail string) gcpkms.VerifyResult {
	return gcpkms.VerifyResult{Verified: false, Reason: reason, Algorithm: "", Detail: detail}
}
