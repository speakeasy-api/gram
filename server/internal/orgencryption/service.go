package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

const (
	// DataKeyCacheTTL is how long an unwrapped DEK stays in memory. It is the
	// longest a process keeps decrypting after a customer revokes access to
	// their KEK, traded against one KMS call per DEK per process per period.
	DataKeyCacheTTL = 5 * time.Minute

	// dataKeyCacheSize bounds the unwrapped DEKs one process holds: one per
	// (organization, use case) it serves, plus decrypt-only DEKs.
	dataKeyCacheSize = 4096

	// ConfigCacheTTL is how long a process trusts a configuration read. It
	// bounds how long a suspension takes to stop every process from using DEKs
	// it already holds, at the cost of one indexed read per organization per
	// period. It equally bounds how long another process keeps reporting
	// ErrNotProvisioned for a use case whose DEK was just created.
	ConfigCacheTTL = 10 * time.Second

	// configCacheSize bounds the configurations and active DEK ids one process
	// remembers.
	configCacheSize = 4096

	// kmsCallTimeout bounds one unwrap, so a hung KMS call does not hold a
	// caller past its own deadline budget.
	kmsCallTimeout = 10 * time.Second
)

// Service encrypts and decrypts organization data. It never provisions keys;
// a missing configuration or DEK fails with ErrNotProvisioned.
type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	keys   *keyResolver

	dataKeys *expirable.LRU[uuid.UUID, cachedDataKey]
	configs  *expirable.LRU[configCacheKey, cachedConfig]
	flights  singleflight.Group
}

type cachedDataKey struct {
	key []byte

	// organizationID and useCase are the DEK's owners. A ciphertext naming a
	// DEK that belongs to someone else is refused before any decryption.
	organizationID string
	useCase        string
}

type configCacheKey struct {
	organizationID string
	useCase        UseCase
}

type cachedConfig struct {
	state ConfigState

	// activeDataKeyID is the DEK that encrypts for the use case, or uuid.Nil
	// when none exists yet.
	activeDataKeyID uuid.UUID
}

// NewService builds the encrypt and decrypt path.
func NewService(logger *slog.Logger, db *pgxpool.Pool, identity *gcpauth.Identity, clients gcpkms.EncryptionClientFactory, cfg KeyConfig) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &Service{
		logger:   logger.With(attr.SlogComponent("orgencryption")),
		db:       db,
		keys:     &keyResolver{cfg: cfg, identity: identity, clients: clients},
		dataKeys: expirable.NewLRU[uuid.UUID, cachedDataKey](dataKeyCacheSize, nil, DataKeyCacheTTL),
		configs:  expirable.NewLRU[configCacheKey, cachedConfig](configCacheSize, nil, ConfigCacheTTL),
		flights:  singleflight.Group{},
	}, nil
}

// Encryptor seals and opens values for one organization and use case.
type Encryptor struct {
	service        *Service
	organizationID string
	useCase        UseCase
}

// For returns the Encryptor for an organization's use case. It does no I/O.
func (s *Service) For(organizationID string, useCase UseCase) (*Encryptor, error) {
	if err := useCase.validate(); err != nil {
		return nil, err
	}
	if organizationID == "" {
		return nil, errors.New("orgencryption: organization id is required")
	}

	return &Encryptor{service: s, organizationID: organizationID, useCase: useCase}, nil
}

// Encrypt seals plaintext for storage in column, a "table.column" name. The
// result only decrypts for the same organization, use case and column.
func (e *Encryptor) Encrypt(ctx context.Context, column string, plaintext []byte) (string, error) {
	cfg, err := e.service.config(ctx, e.organizationID, e.useCase)
	if err != nil {
		return "", err
	}
	if cfg.activeDataKeyID == uuid.Nil {
		return "", ErrNotProvisioned
	}

	dataKey, err := e.service.dataKey(ctx, e.organizationID, cfg.activeDataKeyID)
	if err != nil {
		return "", err
	}

	envelope, err := encryption.SealEnvelope(dataKey.key, cfg.activeDataKeyID, plaintext, valueAssociatedData(e.organizationID, e.useCase, column))
	if err != nil {
		return "", fmt.Errorf("seal organization value: %w", err)
	}

	return envelope.String(), nil
}

// Decrypt opens a value Encrypt sealed for the same organization, use case and
// column.
func (e *Encryptor) Decrypt(ctx context.Context, column string, ciphertext string) ([]byte, error) {
	envelope, err := encryption.ParseEnvelope(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCiphertext, err)
	}

	if _, err := e.service.config(ctx, e.organizationID, e.useCase); err != nil {
		return nil, err
	}

	dataKey, err := e.service.dataKey(ctx, e.organizationID, envelope.DataKeyID)
	if err != nil {
		return nil, err
	}
	if dataKey.organizationID != e.organizationID || dataKey.useCase != string(e.useCase) {
		return nil, ErrInvalidCiphertext
	}

	plaintext, err := encryption.OpenEnvelope(dataKey.key, envelope, valueAssociatedData(e.organizationID, e.useCase, column))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCiphertext, err)
	}

	return plaintext, nil
}

// config returns the organization's configuration state and active DEK for the
// use case, failing closed when it is missing or suspended.
func (s *Service) config(ctx context.Context, organizationID string, useCase UseCase) (cachedConfig, error) {
	key := configCacheKey{organizationID: organizationID, useCase: useCase}

	cfg, ok := s.configs.Get(key)
	if !ok {
		loaded, err := s.loadConfig(ctx, organizationID, useCase)
		if err != nil {
			return cachedConfig{}, err
		}
		s.configs.Add(key, loaded)
		cfg = loaded
	}

	switch cfg.state {
	case ConfigStateActive, ConfigStateSwitching:
		return cfg, nil
	case ConfigStateSuspended:
		return cachedConfig{}, ErrSuspended
	default:
		return cachedConfig{}, fmt.Errorf("%w: unknown configuration state %q", ErrInvalidState, cfg.state)
	}
}

func (s *Service) loadConfig(ctx context.Context, organizationID string, useCase UseCase) (cachedConfig, error) {
	q := repo.New(s.db)

	config, err := q.GetOrganizationEncryptionConfig(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cachedConfig{}, ErrNotProvisioned
	case err != nil:
		return cachedConfig{}, fmt.Errorf("load organization encryption config: %w", err)
	}

	loaded := cachedConfig{state: ConfigState(config.State), activeDataKeyID: uuid.Nil}

	active, err := q.GetActiveDataEncryptionKey(ctx, repo.GetActiveDataEncryptionKeyParams{OrganizationID: organizationID, UseCase: string(useCase)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return cachedConfig{}, fmt.Errorf("load active data encryption key: %w", err)
	default:
		loaded.activeDataKeyID = active.ID
	}

	return loaded, nil
}

// dataKey returns an unwrapped DEK, unwrapping it through KMS at most once per
// process and DataKeyCacheTTL.
func (s *Service) dataKey(ctx context.Context, organizationID string, dataKeyID uuid.UUID) (cachedDataKey, error) {
	if cached, ok := s.dataKeys.Get(dataKeyID); ok {
		return cached, nil
	}

	flight := s.flights.DoChan(organizationID+"/"+dataKeyID.String(), func() (any, error) {
		// The flight outlives any one caller's cancellation, so other callers
		// waiting on it still get the key; kmsCallTimeout bounds it.
		unwrapped, err := s.unwrapDataKey(context.WithoutCancel(ctx), organizationID, dataKeyID)
		if err != nil {
			return cachedDataKey{}, err
		}
		s.dataKeys.Add(dataKeyID, unwrapped)

		return unwrapped, nil
	})

	var result singleflight.Result
	select {
	case <-ctx.Done():
		return cachedDataKey{}, fmt.Errorf("wait for data encryption key: %w", ctx.Err())
	case result = <-flight:
	}
	if result.Err != nil {
		return cachedDataKey{}, result.Err
	}

	dataKey, ok := result.Val.(cachedDataKey)
	if !ok {
		return cachedDataKey{}, fmt.Errorf("unexpected data key flight result %T", result.Val)
	}

	return dataKey, nil
}

// unwrapDataKey unwraps a readable DEK with the configuration's current KEK,
// falling back to the previous KEK while a switch is in flight.
func (s *Service) unwrapDataKey(ctx context.Context, organizationID string, dataKeyID uuid.UUID) (cachedDataKey, error) {
	ctx, cancel := context.WithTimeout(ctx, kmsCallTimeout)
	defer cancel()

	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	q := repo.New(s.db)

	dek, err := q.GetReadableDataEncryptionKey(ctx, repo.GetReadableDataEncryptionKeyParams{ID: dataKeyID, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cachedDataKey{}, ErrInvalidCiphertext
	case err != nil:
		return cachedDataKey{}, fmt.Errorf("load data encryption key: %w", err)
	}

	config, err := q.GetOrganizationEncryptionConfig(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cachedDataKey{}, ErrNotProvisioned
	case err != nil:
		return cachedDataKey{}, fmt.Errorf("load organization encryption config: %w", err)
	}
	if err := requireUsableState(config.State); err != nil {
		return cachedDataKey{}, err
	}

	candidates := []uuid.UUID{config.KekExternalKeyID}
	if config.PreviousKekExternalKeyID.Valid {
		candidates = append(candidates, config.PreviousKekExternalKeyID.UUID)
	}

	var errs []error
	for _, keyID := range candidates {
		dataKey, err := s.unwrapWith(ctx, logger, q, dek, keyID)
		if err == nil {
			return cachedDataKey{key: dataKey, organizationID: dek.OrganizationID, useCase: dek.UseCase}, nil
		}
		errs = append(errs, err)
	}

	return cachedDataKey{}, errors.Join(errs...)
}

func (s *Service) unwrapWith(ctx context.Context, logger *slog.Logger, q *repo.Queries, dek repo.DataEncryptionKey, keyID uuid.UUID) ([]byte, error) {
	wrap, err := q.GetDataEncryptionKeyWrap(ctx, repo.GetDataEncryptionKeyWrapParams{
		OrganizationID:      dek.OrganizationID,
		DataEncryptionKeyID: dek.ID,
		ExternalKeyID:       keyID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("%w: no wrap under key %s", ErrKeyUnavailable, keyID)
	case err != nil:
		return nil, fmt.Errorf("load data encryption key wrap: %w", err)
	}

	key, err := s.keys.open(ctx, logger, q, dek.OrganizationID, keyID)
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, logger, "close kms encryption client", key.Close)

	dataKey, err := key.unwrap(ctx, dek.OrganizationID, dek.ID, dek.UseCase, wrap.WrappedKey)
	if err != nil {
		logger.WarnContext(ctx, "unwrap data encryption key", attr.SlogError(err))
		return nil, err
	}
	if len(dataKey) != encryption.DataKeyBytes {
		return nil, fmt.Errorf("%w: unwrapped data encryption key is %d bytes", ErrKeyUnavailable, len(dataKey))
	}

	if err := q.TouchDataEncryptionKeyWrapUnwrapped(ctx, repo.TouchDataEncryptionKeyWrapUnwrappedParams{ID: wrap.ID, OrganizationID: dek.OrganizationID}); err != nil {
		logger.WarnContext(ctx, "record data encryption key unwrap", attr.SlogError(err))
	}

	return dataKey, nil
}

// Forget drops everything cached for an organization, so the next call reads
// its configuration and unwraps its DEKs again. Provisioner calls it after a
// change in the same process; other processes catch up within ConfigCacheTTL
// for suspension and DataKeyCacheTTL for key material.
func (s *Service) Forget(organizationID string) {
	for _, key := range s.configs.Keys() {
		if key.organizationID == organizationID {
			s.configs.Remove(key)
		}
	}
	for _, id := range s.dataKeys.Keys() {
		if cached, ok := s.dataKeys.Peek(id); ok && cached.organizationID == organizationID {
			s.dataKeys.Remove(id)
		}
	}
}
