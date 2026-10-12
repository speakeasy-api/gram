package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// challengeScopesWriteBudget bounds the detached write of a challenge's scope
// param. Two statements against a small table; anything slower is a stuck
// database, not a slow write.
const challengeScopesWriteBudget = 5 * time.Second

// challengeScopesRecheck is how long an identical challenge from the same
// resource is trusted to still be recorded before the row is read again, so a
// client retrying against a dead grant costs no database round trips.
const challengeScopesRecheck = 10 * time.Minute

// challengeScopesWriteSlots caps the detached writes in flight per replica;
// a challenge arriving with every slot taken is dropped, the next one records it.
const challengeScopesWriteSlots = 4

// challengeScopesLockStripes is the number of locks writes are serialised
// under, so the row ends up holding the latest observation of its resource.
const challengeScopesLockStripes = 16

type challengeObservation struct {
	scopes []string
	at     time.Time
}

// challengeScopesState is the per-replica debounce for observeChallengeScopes.
// seen holds *challengeObservation so a stale entry is only ever removed by
// identity and a newer observation of the same resource survives.
type challengeScopesState struct {
	seen  sync.Map
	locks [challengeScopesLockStripes]sync.Mutex
	slots chan struct{}
}

func newChallengeScopesState() *challengeScopesState {
	return &challengeScopesState{seen: sync.Map{}, locks: [challengeScopesLockStripes]sync.Mutex{}, slots: make(chan struct{}, challengeScopesWriteSlots)}
}

func (s *challengeScopesState) lock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.locks[h.Sum32()%challengeScopesLockStripes]
}

// sweep drops observations past their recheck window so the map stays bounded
// by the resources challenged recently, not ever.
func (s *challengeScopesState) sweep(now time.Time) {
	s.seen.Range(func(key, v any) bool {
		if obs, ok := v.(*challengeObservation); ok && now.Sub(obs.at) >= challengeScopesRecheck {
			s.seen.CompareAndDelete(key, v)
		}
		return true
	})
}

// observeChallengeScopes records the scope auth-param of an upstream 401 or
// 403 challenge on the server's protected resource row, off the request path.
// It never fails the proxied response.
func (f *ProxyManager) observeChallengeScopes(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, resourceURL string, status int, wwwAuthenticate []string) {
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return
	}
	scopes := parseChallengeScopes(wwwAuthenticate)
	if scopes == nil {
		return
	}

	key := projectID.String() + " " + resourceURL
	now := time.Now()
	if v, ok := f.challengeScopes.seen.Load(key); ok {
		if prev, ok := v.(*challengeObservation); ok && slices.Equal(prev.scopes, scopes) && now.Sub(prev.at) < challengeScopesRecheck {
			if f.afterChallengeScopes != nil {
				f.afterChallengeScopes()
			}
			return
		}
	}
	select {
	case f.challengeScopes.slots <- struct{}{}:
	default:
		return
	}
	obs := &challengeObservation{scopes: scopes, at: now}
	f.challengeScopes.seen.Store(key, obs)

	// Only the trace carries over: the write outlives the request and must not inherit its values.
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		defer func() { <-f.challengeScopes.slots }()
		if f.afterChallengeScopes != nil {
			defer f.afterChallengeScopes()
		}
		defer func() {
			if rec := recover(); rec != nil {
				f.challengeScopes.seen.CompareAndDelete(key, obs)
				logger.ErrorContext(detached, "record protected resource challenge scopes panicked", attr.SlogError(fmt.Errorf("%v", rec)))
			}
		}()
		mu := f.challengeScopes.lock(key)
		mu.Lock()
		defer mu.Unlock()
		// A newer observation of this resource has taken over the entry; its write lands last.
		if v, ok := f.challengeScopes.seen.Load(key); ok && v != obs {
			return
		}
		ctx, cancel := context.WithTimeout(detached, challengeScopesWriteBudget)
		defer cancel()
		recorded, err := recordChallengeScopes(ctx, f.db, projectID, resourceURL, scopes)
		if !recorded {
			f.challengeScopes.seen.CompareAndDelete(key, obs)
		}
		if err != nil {
			logger.ErrorContext(ctx, "record protected resource challenge scopes", attr.SlogError(err))
		}
		f.challengeScopes.sweep(time.Now())
	}()
}

// recordChallengeScopes reports whether the scopes are persisted, including when
// they already match. A missing row is a no-op, but must not debounce a retry.
func recordChallengeScopes(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, resourceURL string, scopes []string) (bool, error) {
	q := repo.New(db)
	existing, err := q.GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: resourceURL})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("get remote protected resource: %w", err)
	case slices.Equal(existing.ChallengeScopes, scopes):
		return true, nil
	}
	rows, err := q.RecordRemoteProtectedResourceChallengeScopes(ctx, repo.RecordRemoteProtectedResourceChallengeScopesParams{
		ChallengeScopes:    scopes,
		ProjectID:          projectID,
		ResourceIdentifier: resourceURL,
	})
	if err != nil {
		return false, fmt.Errorf("record remote protected resource challenge scopes: %w", err)
	}
	return rows > 0, nil
}
