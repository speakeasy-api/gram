package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// transactionScope keeps uncommitted reads private to one Encryptor. Plain TTL
// slots avoid background expiry workers and bound the retained key material.
type transactionScope struct {
	db             repo.DBTX
	mu             sync.Mutex
	config         cachedConfig
	configExpires  time.Time
	dataKey        cachedDataKey
	dataKeyID      uuid.UUID
	dataKeyExpires time.Time
}

// ForIn returns an Encryptor using db for every database read. It skips the
// optional unwrap timestamp update to avoid retaining shared key-row locks.
// It does no I/O. db must be a writable READ COMMITTED transaction (or a pool).
// Repeatable-read snapshots cannot uphold bounded suspension visibility. The
// caller owns db and must not use the Encryptor after its transaction ends.
// Uncommitted state never enters the Service's shared caches.
//
// Each returned Encryptor reads its configuration through db and keeps it in
// a private cache. For key material it first reads the Service's committed DEK
// cache, which only holds DEKs unwrapped through the pool, and unwraps through
// db only on a miss. That private unwrap never enters the shared cache. A
// caller that warms the shared cache before it opens the transaction (For and
// RequireUsable) therefore makes no KMS call while the transaction is open.
func (s *Service) ForIn(db repo.DBTX, organizationID string, useCase UseCase) (*Encryptor, error) {
	if db == nil {
		return nil, errors.New("orgencryption: database is required")
	}
	// Interfaces containing a nil pointer must also fail without panicking.
	v := reflect.ValueOf(db)
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return nil, errors.New("orgencryption: database is required")
	}
	e, err := s.For(organizationID, useCase)
	if err != nil {
		return nil, err
	}
	var scope transactionScope
	scope.db = db
	e.transaction = &scope
	return e, nil
}

func (e *Encryptor) config(ctx context.Context) (cachedConfig, error) {
	tx := e.transaction
	if tx == nil {
		return e.service.config(ctx, e.organizationID, e.useCase)
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return cachedConfig{}, fmt.Errorf("load transaction encryption config: %w", err)
	}
	if !time.Now().Before(tx.configExpires) {
		cfg, err := e.service.loadConfigIn(ctx, tx.db, e.organizationID, e.useCase)
		if err != nil {
			return cachedConfig{}, err
		}
		tx.config = cfg
		tx.configExpires = time.Now().Add(configCacheTTL)
	}
	if err := requireUsableState(string(tx.config.state)); err != nil {
		return cachedConfig{}, err
	}
	return tx.config, nil
}

func (e *Encryptor) dataKey(ctx context.Context, id uuid.UUID) (cachedDataKey, error) {
	tx := e.transaction
	if tx == nil {
		return e.service.dataKey(ctx, e.organizationID, id)
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return cachedDataKey{}, fmt.Errorf("load transaction data encryption key: %w", err)
	}
	if tx.dataKeyID == id && time.Now().Before(tx.dataKeyExpires) {
		return tx.dataKey, nil
	}
	// A DEK id always names the same key material, and the shared cache only
	// holds DEKs unwrapped from committed rows, so reusing one keeps the same
	// revocation bound as For: DataKeyCacheTTL. The configuration, and with it
	// suspension and the active DEK, is still read through db.
	if shared, ok := e.service.dataKeys.Get(id); ok {
		e.service.noteUse(id)
		return shared, nil
	}
	key, err := e.service.unwrapDataKeyIn(ctx, tx.db, e.organizationID, id, false)
	if err != nil {
		return cachedDataKey{}, err
	}
	tx.dataKey = key
	tx.dataKeyID = id
	tx.dataKeyExpires = time.Now().Add(DataKeyCacheTTL)
	return key, nil
}
