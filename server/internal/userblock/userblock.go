// Package userblock reads whether staff blocked a user from the platform
// (users.blacklisted_at). Every authentication path that resolves a user, or a
// token a user authorized, checks it, so a blocked user cannot use the
// dashboard, API keys, MCP servers, the Platform MCP, assistants, or any other
// surface, and cannot create anything.
//
// Almost no users are blocked, so each process holds the whole blocked set in
// memory and reloads it with one query every refreshInterval. A request check
// is a map lookup: no database or network call on the request path, and the
// cost does not grow with traffic or with the number of users.
package userblock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/emailnorm"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/users/repo"
)

// refreshInterval bounds how long a request can still pass after staff block
// a user, on processes that did not apply the block themselves.
const refreshInterval = 10 * time.Second

// maxStaleness bounds how long a process keeps serving its last loaded set
// while reloads fail. Past it, checks fail closed.
const maxStaleness = 5 * time.Minute

// loadTimeout bounds one reload of the blocked set.
const loadTimeout = 5 * time.Second

// LogEvent names the log line every refusal of a blocked user writes, so
// staff can follow a blocked user's attempts in Datadog.
const LogEvent = "user_blocked_request_refused"

// ErrBlocked marks a refusal because a user is blocked from the platform. It
// never reaches the caller's response: refusals are opaque, so a blocked
// user cannot tell that they are blocked.
var ErrBlocked = errors.New("user is blocked from the platform")

// LogRefusal records that surface refused userID because staff blocked them.
// Every refusal path calls it, whatever opaque response it writes.
func LogRefusal(ctx context.Context, logger *slog.Logger, surface, userID string) {
	logger.WarnContext(ctx, "refused request from blocked user",
		attr.SlogEvent(LogEvent),
		attr.SlogReason(surface),
		attr.SlogUserID(userID),
	)
}

// RefusalForEmail is Refusal for a surface that knows the blocked person only
// by email.
func RefusalForEmail(ctx context.Context, logger *slog.Logger, surface, email string) error {
	logger.WarnContext(ctx, "refused request from blocked user",
		attr.SlogEvent(LogEvent),
		attr.SlogReason(surface),
		attr.SlogAuthUserEmail(email),
	)
	return oops.E(oops.CodeNotFound, ErrBlocked, "%s", oops.CodeNotFound.UserMessage())
}

// Refusal logs a refusal and returns the opaque not-found error a surface
// with no response contract of its own returns.
func Refusal(ctx context.Context, logger *slog.Logger, surface, userID string) error {
	LogRefusal(ctx, logger, surface, userID)
	return oops.E(oops.CodeNotFound, ErrBlocked, "%s", oops.CodeNotFound.UserMessage())
}

// snapshot is one database's blocked set. Snapshots are shared per pool, so
// every gate in a process agrees, and gates over different databases (test
// clones) never share state.
type snapshot struct {
	queries *repo.Queries

	mu       sync.RWMutex
	blocked  blockedSet
	loadedAt time.Time
	// checkedAt is when a reload was last attempted, successful or not, so a
	// failing database is retried once per interval rather than per request.
	checkedAt time.Time

	reload sync.Mutex
}

var (
	snapshotsMu sync.Mutex
	snapshots   = map[*pgxpool.Pool]*snapshot{}
)

func snapshotFor(db *pgxpool.Pool) *snapshot {
	snapshotsMu.Lock()
	defer snapshotsMu.Unlock()
	snap, ok := snapshots[db]
	if !ok {
		snap = &snapshot{
			queries:   repo.New(db),
			mu:        sync.RWMutex{},
			blocked:   blockedSet{userIDs: nil, emails: nil},
			loadedAt:  time.Time{},
			checkedAt: time.Time{},
			reload:    sync.Mutex{},
		}
		snapshots[db] = snap
	}
	return snap
}

// Forget drops db's loaded set so the next check reloads it. A process that
// changes a user's block state calls it to apply the change at once; tests
// call it after blocking a user.
func Forget(db *pgxpool.Pool) {
	if db == nil {
		return
	}
	snap := snapshotFor(db)
	snap.mu.Lock()
	snap.loadedAt, snap.checkedAt = time.Time{}, time.Time{}
	snap.mu.Unlock()
}

// blockedSet holds blocked user IDs and their canonicalized emails.
type blockedSet struct {
	userIDs map[string]struct{}
	emails  map[string]struct{}
}

// Gate checks users against the blocked set of one database.
type Gate struct {
	snap *snapshot
}

// NewGate returns a gate reading db, or nil when db is nil. A nil gate blocks
// no one, for wiring and tests that run without a database. Gates are cheap:
// all gates over one pool share its snapshot.
func NewGate(db *pgxpool.Pool) *Gate {
	if db == nil {
		return nil
	}
	return &Gate{snap: snapshotFor(db)}
}

// Blocked reports whether userID is blocked. An empty user ID is not blocked.
func (g *Gate) Blocked(ctx context.Context, userID string) (bool, error) {
	if g == nil || userID == "" {
		return false, nil
	}
	set, err := g.snap.current(ctx, time.Now())
	if err != nil {
		return false, err
	}
	_, blocked := set.userIDs[userID]
	return blocked, nil
}

// BlockedEmail reports whether a blocked user holds email, matched
// case-insensitively and across Gmail/Googlemail alias variations. It covers
// paths that know a person only by email.
func (g *Gate) BlockedEmail(ctx context.Context, email string) (bool, error) {
	email = emailnorm.Canonicalize(email)
	if g == nil || email == "" {
		return false, nil
	}
	set, err := g.snap.current(ctx, time.Now())
	if err != nil {
		return false, err
	}
	_, blocked := set.emails[email]
	return blocked, nil
}

// current returns the blocked set, reloading it when it is older than
// refreshInterval. Only one caller reloads at a time; the others keep using
// the previous set unless it is past maxStaleness or was never loaded.
func (s *snapshot) current(ctx context.Context, now time.Time) (blockedSet, error) {
	s.mu.RLock()
	set, loadedAt, checkedAt := s.blocked, s.loadedAt, s.checkedAt
	s.mu.RUnlock()
	if !loadedAt.IsZero() && now.Sub(checkedAt) < refreshInterval {
		return set, nil
	}

	if !loadedAt.IsZero() && now.Sub(loadedAt) < maxStaleness {
		// Serve the loaded set while one caller reloads.
		if !s.reload.TryLock() {
			return set, nil
		}
	} else {
		s.reload.Lock()
	}
	defer s.reload.Unlock()

	// Another caller may have reloaded while this one waited.
	s.mu.RLock()
	set, loadedAt, checkedAt = s.blocked, s.loadedAt, s.checkedAt
	s.mu.RUnlock()
	if !loadedAt.IsZero() && now.Sub(checkedAt) < refreshInterval {
		return set, nil
	}

	// One request's cancellation must not fail the reload every other request
	// is waiting on.
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
	defer cancel()
	rows, err := s.queries.ListBlacklistedUsers(loadCtx)
	if err != nil {
		s.mu.Lock()
		s.checkedAt = now
		s.mu.Unlock()
		if !loadedAt.IsZero() && now.Sub(loadedAt) < maxStaleness {
			return set, nil
		}
		return blockedSet{}, fmt.Errorf("load blocked users: %w", err)
	}
	fresh := blockedSet{userIDs: make(map[string]struct{}, len(rows)), emails: make(map[string]struct{}, len(rows))}
	for _, row := range rows {
		fresh.userIDs[row.ID] = struct{}{}
		if canonical := emailnorm.Canonicalize(row.Email); canonical != "" {
			fresh.emails[canonical] = struct{}{}
		}
	}
	s.mu.Lock()
	s.blocked, s.loadedAt, s.checkedAt = fresh, now, now
	s.mu.Unlock()
	return fresh, nil
}

// BlockedEmailIn reports whether a blocked user holds email, through the
// in-memory snapshot when db is a pool and with a direct read otherwise.
func BlockedEmailIn(ctx context.Context, db repo.DBTX, email string) (bool, error) {
	if pool, ok := db.(*pgxpool.Pool); ok {
		return NewGate(pool).BlockedEmail(ctx, email)
	}
	canonical := emailnorm.Canonicalize(email)
	if canonical == "" {
		return false, nil
	}
	blocked, err := repo.New(db).IsEmailBlacklisted(ctx, canonical)
	if err != nil {
		return false, fmt.Errorf("read email block state: %w", err)
	}
	return blocked, nil
}

// BlockedIn reports whether userID is blocked, through the in-memory snapshot
// when db is a pool and with a direct read inside a transaction.
func BlockedIn(ctx context.Context, db repo.DBTX, userID string) (bool, error) {
	if pool, ok := db.(*pgxpool.Pool); ok {
		return NewGate(pool).Blocked(ctx, userID)
	}
	if userID == "" {
		return false, nil
	}
	blocked, err := repo.New(db).IsUserBlacklisted(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("read user block state: %w", err)
	}
	return blocked, nil
}

// Check returns an opaque unauthorized error, the same one an invalid or
// expired credential gets, when any of userIDs is blocked, and fails
// closed with an unexpected error when the blocked set cannot be read. Pass
// every user behind a credential: the caller and, for delegated tokens, the
// user who authorized or owns it.
func (g *Gate) Check(ctx context.Context, logger *slog.Logger, surface string, userIDs ...string) error {
	for _, userID := range userIDs {
		blocked, err := g.Blocked(ctx, userID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error checking user access").LogError(ctx, logger)
		}
		if blocked {
			LogRefusal(ctx, logger, surface, userID)
			return oops.E(oops.CodeUnauthorized, ErrBlocked, "%s", oops.CodeUnauthorized.UserMessage())
		}
	}
	return nil
}
