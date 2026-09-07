package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Only this digest is persisted. The refresh bearer secret is never an access ID.
func refreshHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (s *Manager) accessKey(id string) string { return SessionCacheKey(id) + ":" + s.suffix }
func (s *Manager) refreshKey(hash string) string {
	return "session-refresh:v1:" + hash + ":" + s.suffix
}

func readSession(ctx context.Context, client redis.Cmdable, key string) (Session, error) {
	raw, err := client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, redisCache.ErrCacheMiss
	}
	if err != nil {
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	return session, nil
}

// projectFamily retains the presented access credential's immutable lifetime while
// applying the refresh family's authoritative scope, including its support cap.
func projectFamily(access, family Session) Session {
	access.ActiveOrganizationID = family.ActiveOrganizationID
	access.SupportOrganizationID = family.SupportOrganizationID
	access.SupportExpiresAt = family.SupportExpiresAt
	return access
}

func (s *Manager) GetSession(ctx context.Context, id string) (Session, error) {
	session, err := readSession(ctx, s.redis, s.accessKey(id))
	if err != nil {
		return Session{}, err
	}
	if session.TTL() <= 0 {
		return Session{}, redisCache.ErrCacheMiss
	}
	if session.RefreshHash != "" {
		family, err := readSession(ctx, s.redis, s.refreshKey(session.RefreshHash))
		if err != nil {
			return Session{}, err
		}
		session = projectFamily(session, family)
	}
	if session.TTL() <= 0 {
		return Session{}, redisCache.ErrCacheMiss
	}
	return session, nil
}

func accessExpiry(session Session) time.Time {
	expiry := time.Now().Add(AccessLifetime)
	if !session.SupportExpiresAt.IsZero() && session.SupportExpiresAt.Before(expiry) {
		expiry = session.SupportExpiresAt
	}
	return expiry
}

func refreshExpiry(session Session) time.Time {
	expiry := time.Now().Add(RefreshIdleLifetime)
	if !session.SupportExpiresAt.IsZero() && session.SupportExpiresAt.Before(expiry) {
		expiry = session.SupportExpiresAt
	}
	return expiry
}

func refreshTTL(session Session) time.Duration { return time.Until(refreshExpiry(session)) }

// StoreSession is for newly authenticated sessions only. Mutations must use
// UpdateSession so they cannot recreate a logged-out or expired session.
func (s *Manager) StoreSession(ctx context.Context, session Session) error {
	if expiry := accessExpiry(session); session.ExpiresAt.IsZero() || expiry.Before(session.ExpiresAt) {
		session.ExpiresAt = expiry
	}
	if session.TTL() <= 0 {
		return redisCache.ErrCacheMiss
	}
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
	if err := s.redis.SetArgs(ctx, s.accessKey(session.SessionID), data, redis.SetArgs{Mode: "NX", ExpireAt: session.ExpiresAt}).Err(); err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	return nil
}

// watch serializes refresh, scope changes, and revocation across server replicas.
// Contention retries always reread authoritative state; there is no lock lease.
func (s *Manager) watch(ctx context.Context, keys []string, fn func(*redis.Tx) error) error {
	for range 8 {
		err := s.redis.Watch(ctx, fn, keys...)
		if err == nil {
			return nil
		}
		if !errors.Is(err, redis.TxFailedErr) {
			return fmt.Errorf("watch session state: %w", err)
		}
	}
	return fmt.Errorf("session changed concurrently: %w", redis.TxFailedErr)
}

// CreateRefreshSession attaches a refresh family after the callback has finished
// choosing its initial organization. Only the returned value contains the secret.
func (s *Manager) CreateRefreshSession(ctx context.Context, id string) (string, Session, error) {
	secret, err := NewSessionID()
	if err != nil {
		return "", Session{}, err
	}
	hash := refreshHash(secret)
	var session Session
	err = s.watch(ctx, []string{s.accessKey(id)}, func(tx *redis.Tx) error {
		current, err := readSession(ctx, tx, s.accessKey(id))
		if err != nil {
			return err
		}
		if current.RefreshHash != "" || current.TTL() <= 0 {
			return redisCache.ErrCacheMiss
		}
		current.RefreshHash = hash
		data, err := json.Marshal(current)
		if err != nil {
			return fmt.Errorf("encode session state: %w", err)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
			pipe.SetArgs(ctx, s.accessKey(id), data, redis.SetArgs{KeepTTL: true})
			//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
			pipe.SetArgs(ctx, s.refreshKey(hash), data, redis.SetArgs{ExpireAt: refreshExpiry(current)})
			return nil
		})
		session = current
		if err != nil {
			return fmt.Errorf("commit refresh initialization: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", Session{}, fmt.Errorf("create refresh session: %w", err)
	}
	return secret, session, nil
}

// Refresh always issues a new fixed-lifetime access credential and extends only
// the refresh idle window. Previous credentials keep their original expiries;
// their scope and revocation remain governed by the shared refresh family.
func (s *Manager) Refresh(ctx context.Context, secret string) (Session, error) {
	if secret == "" {
		return Session{}, redisCache.ErrCacheMiss
	}
	key := s.refreshKey(refreshHash(secret))
	var result Session
	err := s.watch(ctx, []string{key}, func(tx *redis.Tx) error {
		session, err := readSession(ctx, tx, key)
		if err != nil {
			return err
		}
		if refreshTTL(session) <= 0 {
			return redisCache.ErrCacheMiss
		}
		session.SessionID, err = NewSessionID()
		if err != nil {
			return err
		}
		session.ExpiresAt = accessExpiry(session)
		data, err := json.Marshal(session)
		if err != nil {
			return fmt.Errorf("encode session state: %w", err)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
			pipe.SetArgs(ctx, s.accessKey(session.SessionID), data, redis.SetArgs{ExpireAt: session.ExpiresAt})
			//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
			pipe.SetArgs(ctx, key, data, redis.SetArgs{ExpireAt: refreshExpiry(session)})
			return nil
		})
		result = session
		if err != nil {
			return fmt.Errorf("commit refresh session: %w", err)
		}
		return nil
	})
	if err != nil {
		return Session{}, fmt.Errorf("refresh session: %w", err)
	}
	return result, nil
}

func (s *Manager) UpdateSession(ctx context.Context, expected, replacement Session) error {
	// Scope changes cannot extend an access window or escape a support deadline.
	replacement.ExpiresAt = expected.ExpiresAt
	replacement.SupportExpiresAt = expected.SupportExpiresAt
	replacement.RefreshHash = expected.RefreshHash
	replacement.SessionID = expected.SessionID
	key := s.accessKey(expected.SessionID)
	keys := []string{key}
	if expected.RefreshHash != "" {
		keys = append(keys, s.refreshKey(expected.RefreshHash))
	}
	err := s.watch(ctx, keys, func(tx *redis.Tx) error {
		current, err := readSession(ctx, tx, key)
		if err != nil {
			return err
		}
		if current.RefreshHash != expected.RefreshHash || current.TTL() <= 0 {
			return redisCache.ErrCacheMiss
		}
		var family Session
		if expected.RefreshHash != "" {
			family, err = readSession(ctx, tx, s.refreshKey(expected.RefreshHash))
			if err != nil {
				return err
			}
			current = projectFamily(current, family)
		}
		before, err := json.Marshal(expected)
		if err != nil {
			return fmt.Errorf("encode session state: %w", err)
		}
		actual, err := json.Marshal(current)
		if err != nil {
			return fmt.Errorf("encode session state: %w", err)
		}
		if string(before) != string(actual) || current.TTL() <= 0 {
			return redisCache.ErrCacheMiss
		}
		data, err := json.Marshal(replacement)
		if err != nil {
			return fmt.Errorf("encode session state: %w", err)
		}
		var familyData []byte
		if expected.RefreshHash != "" {
			// An overlapping token may change scope, but must not move the
			// family's latest-access pointer back to itself.
			updatedFamily := replacement
			updatedFamily.SessionID = family.SessionID
			updatedFamily.ExpiresAt = family.ExpiresAt
			familyData, err = json.Marshal(updatedFamily)
			if err != nil {
				return fmt.Errorf("encode session state: %w", err)
			}
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
			pipe.SetArgs(ctx, key, data, redis.SetArgs{KeepTTL: true})
			if expected.RefreshHash != "" {
				//nolint:exhaustruct // Only the explicit creation/expiry policy applies; all other Redis SET options stay disabled.
				pipe.SetArgs(ctx, s.refreshKey(expected.RefreshHash), familyData, redis.SetArgs{KeepTTL: true})
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("commit session scope: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func (s *Manager) revokeHash(ctx context.Context, hash string) error {
	key := s.refreshKey(hash)
	var revoked Session
	err := s.watch(ctx, []string{key}, func(tx *redis.Tx) error {
		current, err := readSession(ctx, tx, key)
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, key, s.accessKey(current.SessionID))
			return nil
		})
		revoked = current
		if err != nil {
			return fmt.Errorf("commit session revocation: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}
	s.revokeIDP(ctx, revoked)
	return nil
}

func (s *Manager) revokeIDP(ctx context.Context, session Session) {
	if session.WorkOSSessionID != "" && s.idpClient != nil {
		if err := s.idpClient.RevokeSession(ctx, session.WorkOSSessionID); err != nil {
			s.logger.ErrorContext(ctx, "failed to revoke WorkOS session", attr.SlogError(err))
		}
	}
}

func (s *Manager) ClearSession(ctx context.Context, session Session) error {
	// Read the raw access record: its family may already have been revoked by
	// Logout, but the presented access ID must still be explicitly removed.
	current, err := readSession(ctx, s.redis, s.accessKey(session.SessionID))
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		return err
	}
	if err == nil {
		session = current
	}
	if session.RefreshHash != "" {
		if err := s.revokeHash(ctx, session.RefreshHash); err != nil {
			return err
		}
	}
	if err := s.redis.Del(ctx, s.accessKey(session.SessionID)).Err(); err != nil {
		return fmt.Errorf("clear session: %w", err)
	}
	if session.RefreshHash == "" {
		s.revokeIDP(ctx, session)
	}
	return nil
}

// Logout accepts a refresh credential only at the dedicated HTTP logout route.
// It also invalidates an independently supplied current access credential.
func (s *Manager) Logout(ctx context.Context, secret, accessID string) error {
	if secret != "" {
		if err := s.revokeHash(ctx, refreshHash(secret)); err != nil {
			return err
		}
	}
	if accessID != "" {
		var session Session
		session.SessionID = accessID
		return s.ClearSession(ctx, session)
	}
	return nil
}
