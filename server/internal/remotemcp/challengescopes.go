package remotemcp

import (
	"context"
	"errors"
	"fmt"
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

type challengeObservation struct {
	scopes []string
	at     time.Time
}

// challengeScopesState is the per-replica debounce for observeChallengeScopes.
type challengeScopesState struct {
	seen  sync.Map
	slots chan struct{}
}

func newChallengeScopesState() *challengeScopesState {
	return &challengeScopesState{seen: sync.Map{}, slots: make(chan struct{}, challengeScopesWriteSlots)}
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
		if prev, ok := v.(challengeObservation); ok && slices.Equal(prev.scopes, scopes) && now.Sub(prev.at) < challengeScopesRecheck {
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
	f.challengeScopes.seen.Store(key, challengeObservation{scopes: scopes, at: now})

	// Only the trace carries over: the write outlives the request and must not inherit its values.
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		defer func() { <-f.challengeScopes.slots }()
		if f.afterChallengeScopes != nil {
			defer f.afterChallengeScopes()
		}
		ctx, cancel := context.WithTimeout(detached, challengeScopesWriteBudget)
		defer cancel()
		if err := recordChallengeScopes(ctx, f.db, projectID, resourceURL, scopes); err != nil {
			f.challengeScopes.seen.Delete(key)
			logger.ErrorContext(ctx, "record protected resource challenge scopes", attr.SlogError(err))
		}
	}()
}

func recordChallengeScopes(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, resourceURL string, scopes []string) error {
	q := repo.New(db)
	existing, err := q.GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: resourceURL})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("get remote protected resource: %w", err)
	case slices.Equal(existing.ChallengeScopes, scopes):
		return nil
	}
	if _, err := q.RecordRemoteProtectedResourceChallengeScopes(ctx, repo.RecordRemoteProtectedResourceChallengeScopesParams{
		ChallengeScopes:    scopes,
		ProjectID:          projectID,
		ResourceIdentifier: resourceURL,
	}); err != nil {
		return fmt.Errorf("record remote protected resource challenge scopes: %w", err)
	}
	return nil
}
