package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

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
	if err := q.RecordEncryptionKeyCheck(ctx, repo.RecordEncryptionKeyCheckParams{
		LastKekCheckError: conv.PtrToPGText(checkError),
		OrganizationID:    organizationID,
	}); err != nil {
		return errors.Join(probeErr, fmt.Errorf("record encryption key check: %w", err))
	}

	return probeErr
}

func (p *Provisioner) checkConfig(ctx context.Context, logger *slog.Logger, config repo.OrganizationEncryptionConfig) error {
	organizationID := config.OrganizationID

	_, currentErr := p.probeKey(ctx, logger, organizationID, config.KekExternalKeyID)
	if currentErr != nil {
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
		if !row.WrapID.Valid {
			continue
		}
		dek := row.DataEncryptionKey

		dataKey, err := key.unwrap(ctx, organizationID, dek.ID, dek.UseCase, row.WrappedKey)
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

// SweepManagedKeys disables managed KEKs nothing depends on any more and
// removes the platform grant on them. It returns how many it disabled.
func (p *Provisioner) SweepManagedKeys(ctx context.Context, pageSize int32) (int, error) {
	q := repo.New(p.db)

	rows, err := q.ListManagedEncryptionKeysPendingDisable(ctx, pageSize)
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
		if err := p.disableManagedKey(ctx, q, client, row); err != nil {
			p.logger.WarnContext(ctx, "disable unused managed encryption key", attr.SlogOrganizationID(row.OrganizationID), attr.SlogError(err))
			errs = append(errs, err)
			continue
		}
		disabled++
	}

	return disabled, errors.Join(errs...)
}

func (p *Provisioner) disableManagedKey(ctx context.Context, q *repo.Queries, client gcpkms.EncryptionProvisioningClient, row repo.ListManagedEncryptionKeysPendingDisableRow) error {
	if err := p.keys.checkManagedResource(row.ResourceName); err != nil {
		return err
	}
	if err := client.DisableEncryptionKey(ctx, row.ResourceName); err != nil {
		return fmt.Errorf("disable managed encryption key: %w", err)
	}
	if err := client.RevokeEncrypterDecrypter(ctx, row.ResourceName, p.cfg.Keys.PlatformServiceAccount); err != nil {
		return fmt.Errorf("revoke platform grant on managed encryption key: %w", err)
	}
	if err := q.MarkManagedEncryptionKeyDisabled(ctx, repo.MarkManagedEncryptionKeyDisabledParams{ExternalKeyID: row.ExternalKeyID, OrganizationID: row.OrganizationID}); err != nil {
		return fmt.Errorf("mark managed encryption key disabled: %w", err)
	}

	return nil
}
