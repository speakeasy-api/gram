package orgencryption

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// DefaultSwitchGracePeriod is how long a KEK switch keeps the previous
	// KEK's wraps before the health probe completes it. A day leaves time for
	// every process to unwrap under the new KEK, and for a customer to notice a
	// misconfigured grant and roll back, before the old wraps are dropped.
	DefaultSwitchGracePeriod = 24 * time.Hour

	// managedKeyName is the external_keys.name of a managed KEK.
	managedKeyName = "Speakeasy-managed organization encryption key"

	// keyCleanupTimeout bounds best-effort cleanup of an abandoned KMS key,
	// independently of the request that abandoned it.
	keyCleanupTimeout = 30 * time.Second

	// reasonMaxLen bounds provider error text stored on the configuration.
	reasonMaxLen = 300

	// systemActorComponent names the provisioner in audit entries it writes on
	// its own behalf.
	systemActorComponent = "organization-encryption"
)

// Actor is who an audited change is attributed to.
type Actor struct {
	// Principal is the actor's identity.
	Principal urn.Principal

	// DisplayName renders the actor in the audit log, when known.
	DisplayName *string
}

// SystemActor attributes a change to the provisioner itself, for lazy
// provisioning and the health probe.
func SystemActor() Actor {
	return Actor{Principal: urn.NewSystemPrincipal(systemActorComponent), DisplayName: new("Speakeasy")}
}

// ProvisionerConfig configures key management.
type ProvisionerConfig struct {
	// Keys locates Speakeasy's key ring and platform credential.
	Keys KeyConfig

	// SwitchGracePeriod is how long a switch keeps the previous KEK's wraps.
	// Zero means DefaultSwitchGracePeriod.
	SwitchGracePeriod time.Duration
}

// Provisioner creates and manages organization KEKs and DEKs. Its operations
// call KMS management APIs, so it runs only in the server and worker.
type Provisioner struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	identity     *gcpauth.Identity
	keys         *keyResolver
	provisioning gcpkms.EncryptionProvisioningClientFactory
	audit        *audit.Logger
	service      *Service
	cfg          ProvisionerConfig
	now          func() time.Time
}

// NewProvisioner wires key management. service, when set, has its caches
// dropped after every change this provisioner makes.
func NewProvisioner(logger *slog.Logger, db *pgxpool.Pool, identity *gcpauth.Identity, clients gcpkms.EncryptionClientFactory, provisioning gcpkms.EncryptionProvisioningClientFactory, auditLogger *audit.Logger, service *Service, cfg ProvisionerConfig) (*Provisioner, error) {
	if err := cfg.Keys.validate(); err != nil {
		return nil, err
	}
	if cfg.SwitchGracePeriod <= 0 {
		cfg.SwitchGracePeriod = DefaultSwitchGracePeriod
	}

	return &Provisioner{
		logger:       logger.With(attr.SlogComponent("orgencryption")),
		db:           db,
		identity:     identity,
		keys:         &keyResolver{cfg: cfg.Keys, identity: identity, clients: clients},
		provisioning: provisioning,
		audit:        auditLogger,
		service:      service,
		cfg:          cfg,
		now:          time.Now,
	}, nil
}

func (p *Provisioner) forget(organizationID string) {
	if p.service != nil {
		p.service.Forget(organizationID)
	}
}

// EnsureUseCase makes sure the organization can encrypt for useCase, minting a
// Speakeasy-managed KEK on first use and a DEK for the use case. It is
// idempotent and safe to call concurrently.
func (p *Provisioner) EnsureUseCase(ctx context.Context, organizationID string, useCase UseCase, actor Actor) error {
	if err := useCase.validate(); err != nil {
		return err
	}
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))

	_, err := repo.New(p.db).GetActiveDataEncryptionKey(ctx, repo.GetActiveDataEncryptionKeyParams{OrganizationID: organizationID, UseCase: string(useCase)})
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load active data encryption key: %w", err)
	}

	config, err := p.ensureConfig(ctx, logger, organizationID, actor)
	if err != nil {
		return err
	}

	return p.createDataKey(ctx, logger, config, useCase, actor, false)
}

// ensureConfig returns the organization's configuration, creating it with a
// freshly minted managed KEK when there is none.
func (p *Provisioner) ensureConfig(ctx context.Context, logger *slog.Logger, organizationID string, actor Actor) (repo.OrganizationEncryptionConfig, error) {
	config, err := repo.New(p.db).GetOrganizationEncryptionConfig(ctx, organizationID)
	switch {
	case err == nil:
		return config, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("load organization encryption config: %w", err)
	}

	platformCredentialID, err := p.resolvePlatformCredential(ctx, logger)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	client, err := p.openProvisioningClient(ctx)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	defer o11y.LogDefer(ctx, logger, "close kms provisioning client", client.Close)

	keyName, err := p.mintManagedKey(ctx, client)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	config, created, err := p.recordManagedConfig(ctx, organizationID, platformCredentialID, keyName, actor)
	var commitErr *commitError
	switch {
	case errors.As(err, &commitErr):
		// COMMIT may have landed even though its response was lost; disabling
		// the key could strand a live configuration. Leave it for the sweep,
		// which only disables keys nothing references.
		logger.ErrorContext(ctx, "commit of managed encryption key is uncertain; key left enabled", attr.SlogError(err))
		return repo.OrganizationEncryptionConfig{}, err
	case err != nil:
		p.abandonManagedKey(ctx, logger, client, keyName, err)
		return repo.OrganizationEncryptionConfig{}, err
	case !created:
		p.abandonManagedKey(ctx, logger, client, keyName, errors.New("another request created the configuration first"))
	}

	return config, nil
}

// mintManagedKey creates a KEK in Speakeasy's key ring and grants the platform
// service account on it.
func (p *Provisioner) mintManagedKey(ctx context.Context, client gcpkms.EncryptionProvisioningClient) (string, error) {
	keyID, err := gcpkms.NewKeyID(managedKeyIDPrefix)
	if err != nil {
		return "", fmt.Errorf("name managed encryption key: %w", err)
	}

	created, err := client.CreateEncryptionKey(ctx, gcpkms.CreateEncryptionKeyParams{KeyRing: p.cfg.Keys.KeyRing, KeyID: keyID})
	if err != nil {
		return "", fmt.Errorf("create managed encryption key: %w", err)
	}

	if err := client.GrantEncrypterDecrypter(ctx, created.KeyName, p.cfg.Keys.PlatformServiceAccount); err != nil {
		p.abandonManagedKey(ctx, p.logger, client, created.KeyName, err)
		return "", fmt.Errorf("grant platform service account on managed encryption key: %w", err)
	}

	return created.KeyName, nil
}

// commitError marks a failed COMMIT, whose outcome is unknown.
type commitError struct{ err error }

func (e *commitError) Error() string {
	return "commit organization encryption change: " + e.err.Error()
}
func (e *commitError) Unwrap() error { return e.err }

// recordManagedConfig writes the managed KEK and the configuration naming it.
// It reports created=false when a concurrent request already wrote one.
func (p *Provisioner) recordManagedConfig(ctx context.Context, organizationID string, credentialID uuid.UUID, keyName string, actor Actor) (repo.OrganizationEncryptionConfig, bool, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("begin organization encryption transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, organizationID); err != nil {
		return repo.OrganizationEncryptionConfig{}, false, err
	}

	existing, err := q.GetOrganizationEncryptionConfig(ctx, organizationID)
	switch {
	case err == nil:
		return existing, false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("re-read organization encryption config: %w", err)
	}

	key, err := q.CreateEncryptionExternalKey(ctx, repo.CreateEncryptionExternalKeyParams{
		OrganizationID:       conv.ToPGText(organizationID),
		ExternalCredentialID: credentialID,
		Algorithm:            gcpkms.SymmetricAlgorithm,
		Name:                 managedKeyName,
	})
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("record managed encryption key: %w", err)
	}
	if _, err := q.CreateEncryptionGcpKmsKey(ctx, repo.CreateEncryptionGcpKmsKeyParams{ResourceName: keyName, ExternalKeyID: key.ID, OrganizationID: organizationID}); err != nil {
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("record managed encryption key resource: %w", err)
	}
	if err := q.CreateManagedEncryptionKey(ctx, repo.CreateManagedEncryptionKeyParams{ExternalKeyID: key.ID, OrganizationID: organizationID}); err != nil {
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("mark managed encryption key: %w", err)
	}

	config, err := q.CreateOrganizationEncryptionConfig(ctx, repo.CreateOrganizationEncryptionConfigParams{
		OrganizationID:   organizationID,
		Mode:             string(ModeSpeakeasyManaged),
		State:            string(ConfigStateActive),
		KekExternalKeyID: key.ID,
	})
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("create organization encryption config: %w", err)
	}

	if err := p.audit.LogOrganizationEncryptionConfigCreate(ctx, tx, configEvent(actor, nil, &config)); err != nil {
		return repo.OrganizationEncryptionConfig{}, false, fmt.Errorf("audit organization encryption config create: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return repo.OrganizationEncryptionConfig{}, false, &commitError{err: err}
	}

	return config, true, nil
}

// createDataKey mints a DEK for useCase and wraps it under every KEK the
// configuration reads with. With rotate set it retires the current active DEK
// in the same transaction; otherwise it is a no-op when one already exists.
func (p *Provisioner) createDataKey(ctx context.Context, logger *slog.Logger, config repo.OrganizationEncryptionConfig, useCase UseCase, actor Actor, rotate bool) error {
	if ConfigState(config.State) == ConfigStateSuspended {
		return ErrSuspended
	}

	dataKeyID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate data encryption key id: %w", err)
	}
	dataKey := make([]byte, encryption.DataKeyBytes)
	if _, err := rand.Read(dataKey); err != nil {
		return fmt.Errorf("generate data encryption key: %w", err)
	}

	wraps, err := p.wrapUnderConfig(ctx, logger, config, dataKeyID, string(useCase), dataKey)
	if err != nil {
		return err
	}

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin organization encryption transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, config.OrganizationID); err != nil {
		return err
	}
	if err := requireConfigUnchanged(ctx, q, config); err != nil {
		return err
	}

	var retired *repo.DataEncryptionKey
	if rotate {
		row, err := q.RetireActiveDataEncryptionKey(ctx, repo.RetireActiveDataEncryptionKeyParams{OrganizationID: config.OrganizationID, UseCase: string(useCase)})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotProvisioned
		case err != nil:
			return fmt.Errorf("retire active data encryption key: %w", err)
		}
		retired = &row
	} else {
		_, err := q.GetActiveDataEncryptionKey(ctx, repo.GetActiveDataEncryptionKeyParams{OrganizationID: config.OrganizationID, UseCase: string(useCase)})
		switch {
		case err == nil:
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("re-read active data encryption key: %w", err)
		}
	}

	dek, err := q.CreateDataEncryptionKey(ctx, repo.CreateDataEncryptionKeyParams{
		ID:                 dataKeyID,
		OrganizationID:     config.OrganizationID,
		EncryptionConfigID: config.ID,
		UseCase:            string(useCase),
	})
	if err != nil {
		return fmt.Errorf("create data encryption key: %w", err)
	}
	for _, w := range wraps {
		if _, err := q.CreateDataEncryptionKeyWrap(ctx, repo.CreateDataEncryptionKeyWrapParams{
			OrganizationID:      config.OrganizationID,
			DataEncryptionKeyID: dek.ID,
			ExternalKeyID:       w.keyID,
			ExternalKeyVersion:  conv.ToPGText(w.version),
			WrappedKey:          w.wrapped,
		}); err != nil {
			return fmt.Errorf("record data encryption key wrap: %w", err)
		}
	}

	if retired != nil {
		if err := p.audit.LogDataEncryptionKeyRetire(ctx, tx, dataKeyEvent(actor, *retired)); err != nil {
			return fmt.Errorf("audit data encryption key retire: %w", err)
		}
	}
	if err := p.audit.LogDataEncryptionKeyCreate(ctx, tx, dataKeyEvent(actor, dek)); err != nil {
		return fmt.Errorf("audit data encryption key create: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return &commitError{err: err}
	}
	p.forget(config.OrganizationID)

	return nil
}

// RotateDataKey replaces the use case's active DEK with a new one. The old DEK
// keeps decrypting values sealed with it.
func (p *Provisioner) RotateDataKey(ctx context.Context, organizationID string, useCase UseCase, actor Actor) error {
	if err := useCase.validate(); err != nil {
		return err
	}
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))

	config, err := getConfig(ctx, repo.New(p.db), organizationID)
	if err != nil {
		return err
	}

	return p.createDataKey(ctx, logger, config, useCase, actor, true)
}

// keyWrap is a DEK wrapped by one KEK, ready to insert.
type keyWrap struct {
	keyID   uuid.UUID
	version string
	wrapped []byte
}

// wrapUnderConfig wraps a DEK under the current KEK and, during a switch, the
// previous one too, so a rollback leaves the new DEK readable.
func (p *Provisioner) wrapUnderConfig(ctx context.Context, logger *slog.Logger, config repo.OrganizationEncryptionConfig, dataKeyID uuid.UUID, useCase string, dataKey []byte) ([]keyWrap, error) {
	keyIDs := []uuid.UUID{config.KekExternalKeyID}
	if config.PreviousKekExternalKeyID.Valid {
		keyIDs = append(keyIDs, config.PreviousKekExternalKeyID.UUID)
	}

	q := repo.New(p.db)
	wraps := make([]keyWrap, 0, len(keyIDs))
	for _, keyID := range keyIDs {
		key, err := p.keys.open(ctx, logger, q, config.OrganizationID, keyID)
		if err != nil {
			return nil, err
		}

		result, err := key.wrap(ctx, config.OrganizationID, dataKeyID, useCase, dataKey)
		closeErr := key.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			logger.WarnContext(ctx, "close kms encryption client", attr.SlogError(closeErr))
		}

		wraps = append(wraps, keyWrap{keyID: keyID, version: result.KeyVersionName, wrapped: result.Ciphertext})
	}

	return wraps, nil
}

// SwitchKey starts moving the organization to another KEK: a customer key it
// registered, or a managed one. It proves the new key can encrypt and decrypt,
// then wraps every readable DEK under it. Both KEKs read until the switch
// completes or rolls back.
func (p *Provisioner) SwitchKey(ctx context.Context, organizationID string, keyID uuid.UUID, actor Actor) (repo.OrganizationEncryptionConfig, error) {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))
	q := repo.New(p.db)

	config, err := getConfig(ctx, q, organizationID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	if ConfigState(config.State) != ConfigStateActive {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: configuration is %s", ErrInvalidState, config.State)
	}
	if keyID == config.KekExternalKeyID {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: the organization already uses this key", ErrInvalidState)
	}

	next, err := p.keys.open(ctx, logger, q, organizationID, keyID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	defer o11y.LogDefer(ctx, logger, "close kms encryption client", next.Close)

	if result := gcpkms.VerifyEncryptionKey(ctx, next.client, next.resourceName); !result.Verified {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: key failed verification (%s): %s", ErrInvalidKey, result.Reason, conv.TruncateDetail(result.Detail, reasonMaxLen))
	}

	current, err := p.keys.open(ctx, logger, q, organizationID, config.KekExternalKeyID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	defer o11y.LogDefer(ctx, logger, "close kms encryption client", current.Close)

	rows, err := q.ListDataEncryptionKeyWrapsForKey(ctx, repo.ListDataEncryptionKeyWrapsForKeyParams{ExternalKeyID: config.KekExternalKeyID, OrganizationID: organizationID})
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("list data encryption key wraps: %w", err)
	}

	type rewrap struct {
		dataKeyID uuid.UUID
		wrap      keyWrap
	}
	rewraps := make([]rewrap, 0, len(rows))
	for _, row := range rows {
		if !row.WrapID.Valid {
			return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: data encryption key %s has no wrap under the current key", ErrKeyUnavailable, row.DataEncryptionKey.ID)
		}

		dataKey, err := current.unwrap(ctx, organizationID, row.DataEncryptionKey.ID, row.DataEncryptionKey.UseCase, row.WrappedKey)
		if err != nil {
			return repo.OrganizationEncryptionConfig{}, err
		}
		result, err := next.wrap(ctx, organizationID, row.DataEncryptionKey.ID, row.DataEncryptionKey.UseCase, dataKey)
		if err != nil {
			return repo.OrganizationEncryptionConfig{}, err
		}

		rewraps = append(rewraps, rewrap{dataKeyID: row.DataEncryptionKey.ID, wrap: keyWrap{keyID: keyID, version: result.KeyVersionName, wrapped: result.Ciphertext}})
	}

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("begin organization encryption transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	tq := repo.New(tx)
	if err := lockOrganization(ctx, tq, organizationID); err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	if err := requireConfigUnchanged(ctx, tq, config); err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	// The key was verified before the transaction; re-check it under a lock
	// that a concurrent delete or managed-key sweep must wait on.
	target, err := tq.LockEncryptionKeyForSwitch(ctx, repo.LockEncryptionKeyForSwitchParams{ID: keyID, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: key was deleted", ErrInvalidKey)
	case err != nil:
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("lock key encryption key for switch: %w", err)
	}
	if target.Managed && target.KmsDisabledAt.Valid {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: managed key was disabled", ErrInvalidKey)
	}

	readable, err := tq.ListReadableDataEncryptionKeys(ctx, organizationID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("re-list data encryption keys: %w", err)
	}
	if !slices.EqualFunc(readable, rewraps, func(dek repo.DataEncryptionKey, r rewrap) bool { return dek.ID == r.dataKeyID }) {
		return repo.OrganizationEncryptionConfig{}, ErrConcurrentChange
	}

	switched, err := tq.StartEncryptionConfigSwitch(ctx, repo.StartEncryptionConfigSwitchParams{
		KekExternalKeyID: keyID,
		Mode:             string(modeFor(next.managed)),
		OrganizationID:   organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.OrganizationEncryptionConfig{}, ErrConcurrentChange
	case err != nil:
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("start encryption key switch: %w", err)
	}

	for _, r := range rewraps {
		if _, err := tq.CreateDataEncryptionKeyWrap(ctx, repo.CreateDataEncryptionKeyWrapParams{
			OrganizationID:      organizationID,
			DataEncryptionKeyID: r.dataKeyID,
			ExternalKeyID:       r.wrap.keyID,
			ExternalKeyVersion:  conv.ToPGText(r.wrap.version),
			WrappedKey:          r.wrap.wrapped,
		}); err != nil {
			return repo.OrganizationEncryptionConfig{}, fmt.Errorf("record data encryption key wrap: %w", err)
		}
	}

	if err := p.audit.LogOrganizationEncryptionConfigSwitchKey(ctx, tx, configEvent(actor, &config, &switched)); err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("audit encryption key switch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return repo.OrganizationEncryptionConfig{}, &commitError{err: err}
	}
	p.forget(organizationID)

	return switched, nil
}

// CompleteSwitch drops the previous KEK's wraps once every readable DEK is
// wrapped by the current KEK. A managed previous KEK is then disabled by the
// sweep.
func (p *Provisioner) CompleteSwitch(ctx context.Context, organizationID string, actor Actor) (repo.OrganizationEncryptionConfig, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("begin organization encryption transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, organizationID); err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	config, err := getConfig(ctx, q, organizationID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	if ConfigState(config.State) != ConfigStateSwitching || !config.PreviousKekExternalKeyID.Valid {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: no switch is in flight", ErrInvalidState)
	}

	if err := requireAllWrapped(ctx, q, organizationID, config.KekExternalKeyID); err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	completed, err := q.CompleteEncryptionConfigSwitch(ctx, organizationID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("complete encryption key switch: %w", err)
	}
	if _, err := q.SoftDeleteDataEncryptionKeyWrapsForKey(ctx, repo.SoftDeleteDataEncryptionKeyWrapsForKeyParams{
		OrganizationID: organizationID,
		ExternalKeyID:  config.PreviousKekExternalKeyID.UUID,
	}); err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("drop previous key wraps: %w", err)
	}

	if err := p.audit.LogOrganizationEncryptionConfigCompleteSwitch(ctx, tx, configEvent(actor, &config, &completed)); err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("audit encryption key switch completion: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return repo.OrganizationEncryptionConfig{}, &commitError{err: err}
	}
	p.forget(organizationID)

	return completed, nil
}

// RollbackSwitch returns the organization to the previous KEK and drops the
// wraps made under the KEK it was switching to.
func (p *Provisioner) RollbackSwitch(ctx context.Context, organizationID string, actor Actor) (repo.OrganizationEncryptionConfig, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("begin organization encryption transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, organizationID); err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	config, err := getConfig(ctx, q, organizationID)
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}
	if ConfigState(config.State) != ConfigStateSwitching || !config.PreviousKekExternalKeyID.Valid {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("%w: no switch is in flight", ErrInvalidState)
	}

	previous, err := q.GetEncryptionKeyForUse(ctx, repo.GetEncryptionKeyForUseParams{ID: config.PreviousKekExternalKeyID.UUID, OrganizationID: organizationID})
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("load previous key encryption key: %w", err)
	}
	if err := requireAllWrapped(ctx, q, organizationID, previous.ID); err != nil {
		return repo.OrganizationEncryptionConfig{}, err
	}

	rolledBack, err := q.RollbackEncryptionConfigSwitch(ctx, repo.RollbackEncryptionConfigSwitchParams{
		Mode:           string(modeFor(previous.Managed)),
		OrganizationID: organizationID,
	})
	if err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("roll back encryption key switch: %w", err)
	}
	if _, err := q.SoftDeleteDataEncryptionKeyWrapsForKey(ctx, repo.SoftDeleteDataEncryptionKeyWrapsForKeyParams{
		OrganizationID: organizationID,
		ExternalKeyID:  config.KekExternalKeyID,
	}); err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("drop abandoned key wraps: %w", err)
	}

	if err := p.audit.LogOrganizationEncryptionConfigRollbackSwitch(ctx, tx, configEvent(actor, &config, &rolledBack)); err != nil {
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("audit encryption key switch rollback: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return repo.OrganizationEncryptionConfig{}, &commitError{err: err}
	}
	p.forget(organizationID)

	return rolledBack, nil
}

// Suspend stops every encrypt and decrypt for the organization. reason is
// provider error text and must never carry key material.
func (p *Provisioner) Suspend(ctx context.Context, organizationID string, reason string, actor Actor) error {
	return p.transition(ctx, organizationID, actor, func(q *repo.Queries) (repo.OrganizationEncryptionConfig, error) {
		return q.SuspendEncryptionConfig(ctx, repo.SuspendEncryptionConfigParams{
			SuspendedReason: conv.ToPGText(conv.TruncateDetail(reason, reasonMaxLen)),
			OrganizationID:  organizationID,
		})
	}, p.audit.LogOrganizationEncryptionConfigSuspend)
}

// Resume lifts a suspension after proving the current KEK unwraps every DEK.
func (p *Provisioner) Resume(ctx context.Context, organizationID string, actor Actor) error {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID))

	config, err := getConfig(ctx, repo.New(p.db), organizationID)
	if err != nil {
		return err
	}
	if _, err := p.probeKey(ctx, logger, organizationID, config.KekExternalKeyID); err != nil {
		return err
	}

	return p.resume(ctx, organizationID, actor)
}

func (p *Provisioner) resume(ctx context.Context, organizationID string, actor Actor) error {
	return p.transition(ctx, organizationID, actor, func(q *repo.Queries) (repo.OrganizationEncryptionConfig, error) {
		return q.ResumeEncryptionConfig(ctx, organizationID)
	}, p.audit.LogOrganizationEncryptionConfigResume)
}

// transition applies a state change under the organization lock and audits
// it. A change that matches no row (already in the target state) is a no-op.
func (p *Provisioner) transition(
	ctx context.Context,
	organizationID string,
	actor Actor,
	apply func(q *repo.Queries) (repo.OrganizationEncryptionConfig, error),
	log func(ctx context.Context, dbtx auditrepo.DBTX, event audit.LogOrganizationEncryptionConfigEvent) error,
) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin organization encryption transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, organizationID); err != nil {
		return err
	}
	before, err := getConfig(ctx, q, organizationID)
	if err != nil {
		return err
	}

	after, err := apply(q)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("update organization encryption state: %w", err)
	}

	if err := log(ctx, tx, configEvent(actor, &before, &after)); err != nil {
		return fmt.Errorf("audit organization encryption state change: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return &commitError{err: err}
	}
	p.forget(organizationID)

	return nil
}

// resolvePlatformCredential loads and screens the platform credential managed
// KEKs are recorded against, and proves Speakeasy can assume it.
func (p *Provisioner) resolvePlatformCredential(ctx context.Context, logger *slog.Logger) (uuid.UUID, error) {
	row, err := repo.New(p.db).GetPlatformGcpIamCredential(ctx, p.cfg.Keys.PlatformCredentialID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, fmt.Errorf("%w: platform credential not found", ErrPlatformCredentialUnusable)
	case err != nil:
		return uuid.Nil, fmt.Errorf("load platform encryption credential: %w", err)
	}

	if got := strings.TrimSpace(row.GcpIamCredential.ImpersonateServiceAccount.String); !strings.EqualFold(got, p.cfg.Keys.PlatformServiceAccount) {
		return uuid.Nil, fmt.Errorf("%w: credential impersonates %q, configured service account is %q", ErrPlatformCredentialUnusable, got, p.cfg.Keys.PlatformServiceAccount)
	}

	credential, problem, detail, err := p.identity.ScreenStoredCredential(ctx, logger, gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: row.GcpIamCredential.ImpersonateServiceAccount.String,
		HasWifConfig:              row.GcpIamCredential.WifPoolID.Valid || row.GcpIamCredential.WifProviderID.Valid || row.GcpIamCredential.WifProjectNumber.Valid,
		SkipProjectVerification:   row.GcpIamCredential.SkipProjectVerification,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("screen platform encryption credential: %w", err)
	}
	if problem != "" {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrPlatformCredentialUnusable, detail)
	}
	if _, err := p.identity.ResolvePrincipal(ctx, credential); err != nil {
		return uuid.Nil, fmt.Errorf("%w: cannot assume the platform service account: %w", ErrPlatformCredentialUnusable, err)
	}

	return row.ExternalCredential.ID, nil
}

// openProvisioningClient builds a KMS client under Speakeasy's own identity,
// which creates keys; the platform service account only ever holds rights on
// keys it was granted.
func (p *Provisioner) openProvisioningClient(ctx context.Context) (gcpkms.EncryptionProvisioningClient, error) {
	ambient, err := p.identity.TokenSource(ctx, gcpauth.Credential{
		ImpersonateServiceAccount: "",
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("resolve Speakeasy's own identity for key creation: %w", err)
	}

	client, err := p.provisioning(ctx, ambient)
	if err != nil {
		return nil, fmt.Errorf("build kms client for key creation: %w", err)
	}

	return client, nil
}

// abandonManagedKey disables a managed KEK no row will reference and removes
// its grant. Failures are logged for manual cleanup: crypto keys cannot be
// deleted, only left disabled.
func (p *Provisioner) abandonManagedKey(ctx context.Context, logger *slog.Logger, client gcpkms.EncryptionProvisioningClient, keyName string, cause error) {
	logger = logger.With(attr.SlogError(cause))

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
	defer cancel()

	if err := client.DisableEncryptionKey(cleanupCtx, keyName); err != nil {
		logger.ErrorContext(ctx, "failed to disable abandoned managed encryption key; disable it by hand", attr.SlogError(err))
	}
	if err := client.RevokeEncrypterDecrypter(cleanupCtx, keyName, p.cfg.Keys.PlatformServiceAccount); err != nil {
		logger.ErrorContext(ctx, "failed to revoke grant on abandoned managed encryption key; remove it by hand", attr.SlogError(err))
	}
}

func lockOrganization(ctx context.Context, q *repo.Queries, organizationID string) error {
	_, err := q.LockOrganizationForEncryption(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: organization not found or disabled", ErrInvalidState)
	case err != nil:
		return fmt.Errorf("lock organization for encryption: %w", err)
	}

	return nil
}

func getConfig(ctx context.Context, q *repo.Queries, organizationID string) (repo.OrganizationEncryptionConfig, error) {
	config, err := q.GetOrganizationEncryptionConfig(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.OrganizationEncryptionConfig{}, ErrNotProvisioned
	case err != nil:
		return repo.OrganizationEncryptionConfig{}, fmt.Errorf("load organization encryption config: %w", err)
	}

	return config, nil
}

// requireConfigUnchanged re-reads the configuration under the organization
// lock and refuses to continue if its KEKs or state moved while KMS calls ran.
func requireConfigUnchanged(ctx context.Context, q *repo.Queries, want repo.OrganizationEncryptionConfig) error {
	got, err := getConfig(ctx, q, want.OrganizationID)
	if err != nil {
		return err
	}
	if got.ID != want.ID || got.State != want.State || got.KekExternalKeyID != want.KekExternalKeyID || got.PreviousKekExternalKeyID != want.PreviousKekExternalKeyID {
		return ErrConcurrentChange
	}

	return nil
}

// requireAllWrapped refuses to continue unless every readable DEK has a live
// wrap under keyID.
func requireAllWrapped(ctx context.Context, q *repo.Queries, organizationID string, keyID uuid.UUID) error {
	rows, err := q.ListDataEncryptionKeyWrapsForKey(ctx, repo.ListDataEncryptionKeyWrapsForKeyParams{ExternalKeyID: keyID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("list data encryption key wraps: %w", err)
	}
	for _, row := range rows {
		if !row.WrapID.Valid {
			return fmt.Errorf("%w: data encryption key %s is not wrapped by key %s", ErrInvalidState, row.DataEncryptionKey.ID, keyID)
		}
	}

	return nil
}

func modeFor(managed bool) Mode {
	if managed {
		return ModeSpeakeasyManaged
	}

	return ModeCustomerManaged
}

func configEvent(actor Actor, before, after *repo.OrganizationEncryptionConfig) audit.LogOrganizationEncryptionConfigEvent {
	id := uuid.Nil
	organizationID := ""
	if after != nil {
		id, organizationID = after.ID, after.OrganizationID
	}

	return audit.LogOrganizationEncryptionConfigEvent{
		OrganizationID:       organizationID,
		Actor:                actor.Principal,
		ActorDisplayName:     actor.DisplayName,
		ActorSlug:            nil,
		ConfigURN:            urn.NewOrganizationEncryptionConfig(id),
		ConfigSnapshotBefore: configSnapshot(before),
		ConfigSnapshotAfter:  configSnapshot(after),
	}
}

func configSnapshot(config *repo.OrganizationEncryptionConfig) *audit.OrganizationEncryptionConfigSnapshot {
	if config == nil {
		return nil
	}

	previous := ""
	if config.PreviousKekExternalKeyID.Valid {
		previous = config.PreviousKekExternalKeyID.UUID.String()
	}

	return &audit.OrganizationEncryptionConfigSnapshot{
		Mode:                     config.Mode,
		State:                    config.State,
		KekExternalKeyID:         config.KekExternalKeyID.String(),
		PreviousKekExternalKeyID: previous,
	}
}

func dataKeyEvent(actor Actor, dek repo.DataEncryptionKey) audit.LogDataEncryptionKeyEvent {
	return audit.LogDataEncryptionKeyEvent{
		OrganizationID:   dek.OrganizationID,
		Actor:            actor.Principal,
		ActorDisplayName: actor.DisplayName,
		ActorSlug:        nil,
		KeyURN:           urn.NewDataEncryptionKey(dek.ID),
		UseCase:          dek.UseCase,
	}
}

// pgTimestamp converts a nullable timestamp to a time, reporting validity.
func pgTimestamp(ts pgtype.Timestamptz) (time.Time, bool) {
	return ts.Time, ts.Valid
}
