// refreshservice.go is the single concurrency-safe entry point for refreshing
// a remote session's upstream tokens. Three callers share it: the lazy MCP
// resolution path (tokenservice.go), the org-admin manual refresh handler
// (organizationsessionhandlers.go), and the scheduled pre-emptive refresh
// activity (background worker). Sharing matters because refresh tokens rotate:
// two callers presenting the same refresh token to a provider with reuse
// detection (OAuth 2.0 Security BCP 4.13.2) can revoke the whole token family.

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RefreshOutcome classifies how RefreshNow obtained (or failed to obtain) a
// usable access token. Values are stable, low-cardinality strings safe for
// metrics labels and Temporal activity results.
type RefreshOutcome string

const (
	// RefreshOutcomeRefreshed: this caller executed the upstream refresh_token
	// grant and persisted the rotated pair.
	RefreshOutcomeRefreshed RefreshOutcome = "refreshed"
	// RefreshOutcomeAdoptedConcurrentWinner: another caller refreshed the row
	// while this one was acquiring or POSTing; its persisted token was adopted
	// and no (or a losing) upstream call was made by this caller.
	RefreshOutcomeAdoptedConcurrentWinner RefreshOutcome = "adopted_concurrent_winner"
	// RefreshOutcomeSessionInactive: the row was revoked or deleted before the
	// refresh could run. Not an error — there is simply nothing to refresh.
	RefreshOutcomeSessionInactive RefreshOutcome = "session_inactive"
)

// RefreshResult is RefreshNow's success shape. Session and AccessToken are
// zero/empty when Outcome is RefreshOutcomeSessionInactive.
type RefreshResult struct {
	Session     remotesessions_repo.RemoteSession
	AccessToken string
	Outcome     RefreshOutcome
}

// RefreshService owns the single-flighted refresh of a remote session. It is
// deliberately constructible without any HTTP-serving context so the
// background worker can hold one alongside the request-path callers.
type RefreshService struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	enc    *encryption.Client
	policy *guardian.Policy
	locks  cache.Cache
}

func NewRefreshService(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy, locks cache.Cache) *RefreshService {
	return &RefreshService{
		logger: logger.With(attr.SlogComponent("remotesessions_refresh")),
		db:     db,
		enc:    enc,
		policy: policy,
		locks:  locks,
	}
}

// FallbackResourceForClient derives the RFC 8707 resource for a client from
// its attached MCP servers. RefreshNow applies it to rows that carry no
// persisted resource; the consent connect arm applies it to the grant it is
// about to mint.
func (s *RefreshService) FallbackResourceForClient(ctx context.Context, clientID uuid.UUID) (string, error) {
	rows, err := remotesessions_repo.New(s.db).ListOrganizationMcpServersForClient(ctx, clientID)
	if err != nil {
		return "", fmt.Errorf("list mcp servers for client: %w", err)
	}
	return clientUpstreamResource(rows), nil
}

var errRefreshNotApplied = errors.New("remotesessions: refreshed tokens matched no active session")

// Ordering is the invariant, guarded by TestRefreshTimingInvariant. Invert it
// and a waiter gives up while the holder is still mid-POST, then presents the
// same refresh token upstream.
//
//	refreshUpstreamTimeout < refreshLockTTL <= refreshWaitBudget
const (
	// Nothing else bounds the POST: the pooled client sets no Timeout (its 30s
	// is dial-only) and the MCP mux sets no WriteTimeout.
	refreshUpstreamTimeout = 10 * time.Second

	refreshLockTTL        = 12 * time.Second
	refreshWaitBudget     = 12 * time.Second
	refreshWaitPoll       = 200 * time.Millisecond
	refreshReleaseTimeout = 5 * time.Second
)

func refreshLockKey(subject urn.SessionSubject, clientID uuid.UUID) string {
	return "remotesession:refresh:" + subject.String() + ":" + clientID.String()
}

// RefreshNow refreshes the upstream access token for sess, single-flighted per
// (subject, client) binding so concurrent callers do not all present the same
// refresh token to a provider that rotates them. Losers wait for the winner's
// write instead. The re-reads either side of the refresh cover what the lock
// cannot: Redis down, the lease expiring, a holder that never writes, or one
// that finished while this caller was still acquiring.
//
// The refresh_token grant replays the session's persisted RFC 8707 resource
// binding when one was captured at code exchange. Rows minted before the
// resource column existed fall back to callerResource, and then to the
// client's own derivation — one derivation rule for every refresh path.
//
// A non-nil error is either an operator-actionable *TokenRefreshError or an
// internal infrastructure failure. A definitive invalid_grant clears only the
// refresh grant: the existing access token remains usable until its own known
// expiry. This keeps proactive refresh failure from forcing a reconnect.
func (s *RefreshService) RefreshNow(ctx context.Context, sess remotesessions_repo.RemoteSession, callerResource string) (RefreshResult, error) {
	var zero RefreshResult
	q := remotesessions_repo.New(s.db)

	lockKey := refreshLockKey(sess.SubjectUrn, sess.RemoteSessionClientID)
	lockAcquiredAt := time.Now()
	held, lockErr := s.locks.Add(ctx, lockKey, refreshLockTTL)

	switch {
	case lockErr != nil:
		// Refreshing unserialized beats refusing to refresh; the CAS still
		// stops a losing writer from clobbering.
		s.logger.WarnContext(ctx, "remote session refresh lock unavailable; refreshing without single-flight",
			attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
			attr.SlogCacheKey(lockKey),
			attr.SlogError(lockErr),
		)
	case !held:
		if winner, ok := s.awaitRefreshedSession(ctx, q, sess); ok {
			return winner, nil
		}
		// A duplicate POST beats a guaranteed reconnect prompt.
		s.logger.WarnContext(ctx, "timed out waiting on a concurrent remote session refresh; refreshing directly",
			attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
			attr.SlogCacheKey(lockKey),
		)
	default:
		defer o11y.LogDefer(ctx, s.logger, func() error {
			// Past TTL the key may be a new holder's lock. Skip delete; TTL reaps.
			if time.Since(lockAcquiredAt) >= refreshLockTTL {
				s.logger.WarnContext(ctx, "remote session refresh outlived its lock lease; leaving release to TTL",
					attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
					attr.SlogCacheKey(lockKey),
				)
				return nil
			}

			// Detached: MCP clients disconnect mid-refresh routinely, and a
			// skipped release strands the lease for its full TTL.
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshReleaseTimeout)
			defer cancel()

			return s.locks.Delete(releaseCtx, lockKey)
		})
	}

	// sess predates the lock, so a holder that finished while we were acquiring
	// has already consumed its refresh token. Replaying one makes providers with
	// reuse detection (OAuth 2.0 Security BCP 4.13.2) revoke the whole family,
	// the winner's fresh pair included, which nothing below can detect.
	snapshotAt := sess.UpdatedAt.Time

	current, currentErr := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            sess.SubjectUrn,
		RemoteSessionClientID: sess.RemoteSessionClientID,
	})
	switch {
	case errors.Is(currentErr, pgx.ErrNoRows):
		// Revoked while we were acquiring. Refreshing anyway would rotate
		// tokens upstream that nothing will ever hold.
		var inactive remotesessions_repo.RemoteSession
		return RefreshResult{Session: inactive, AccessToken: "", Outcome: RefreshOutcomeSessionInactive}, nil
	case currentErr != nil:
		// Whatever broke this read breaks the write below too, and a refresh we
		// cannot persist leaves the stored token dead upstream.
		return zero, fmt.Errorf("re-read active remote_session: %w", currentErr)
	}

	sess = current

	if current.UpdatedAt.Time.After(snapshotAt) {
		if accessTokenUsable(current, time.Now()) {
			plain, err := s.enc.Decrypt(current.AccessTokenEncrypted)
			if err != nil {
				return zero, fmt.Errorf("decrypt concurrently refreshed access token: %w", err)
			}
			return RefreshResult{Session: current, AccessToken: plain, Outcome: RefreshOutcomeAdoptedConcurrentWinner}, nil
		}
	}
	if !current.RefreshTokenEncrypted.Valid || current.RefreshTokenEncrypted.String == "" {
		return zero, ErrNoValidToken
	}
	if !refreshTokenUsable(current, time.Now()) {
		return zero, ErrNoValidToken
	}

	// The persisted binding wins over the caller-derived fallback: the token
	// family was granted for the resource recorded at code exchange, and a
	// request-derived value can drift from it (e.g. after MCP server moves).
	resource := conv.FromPGTextOrEmpty[string](sess.Resource)
	if resource == "" {
		resource = callerResource
	}
	if resource == "" {
		// Legacy row minted before the resource column, with no caller value:
		// derive the client's own so it is never replayed against another
		// upstream's audience.
		var err error
		resource, err = s.FallbackResourceForClient(ctx, sess.RemoteSessionClientID)
		if err != nil {
			return zero, fmt.Errorf("derive fallback resource: %w", err)
		}
	}

	updated, accessToken, refreshErr := refreshSessionTokens(ctx, q, s.enc, s.policy, sess, resource)
	if refreshErr == nil {
		// The stamp is permanent, so record which rows the backfill wrote and
		// what it wrote — the only way to find them again if a value is wrong.
		if !sess.Resource.Valid && updated.Resource.Valid {
			s.logger.InfoContext(ctx, "backfilled the remote session resource binding during refresh",
				attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
				attr.SlogUserSessionIssuerID(sess.UserSessionIssuerID.String()),
				attr.SlogOAuthResource(updated.Resource.String),
			)
		}
		return RefreshResult{Session: updated, AccessToken: accessToken, Outcome: RefreshOutcomeRefreshed}, nil
	}

	var tokenRefreshErr *TokenRefreshError
	if errors.As(refreshErr, &tokenRefreshErr) && tokenRefreshErr.invalidGrant() {
		_, err := q.ClearRemoteSessionRefreshTokenAfterInvalidGrant(ctx, remotesessions_repo.ClearRemoteSessionRefreshTokenAfterInvalidGrantParams{
			ID:                    sess.ID,
			SubjectUrn:            sess.SubjectUrn,
			RemoteSessionClientID: sess.RemoteSessionClientID,
			ExpectedUpdatedAt:     sess.UpdatedAt,
		})
		switch {
		case err == nil:
			return zero, refreshErr
		case !errors.Is(err, pgx.ErrNoRows):
			return zero, fmt.Errorf("clear remote session refresh token after invalid_grant: %w", err)
		}
		// A row that moved after our refresh attempt belongs to a concurrent
		// winner. Re-read it below and adopt that token instead of clearing it.
	}

	latest, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            sess.SubjectUrn,
		RemoteSessionClientID: sess.RemoteSessionClientID,
	})
	if err != nil {
		return zero, refreshErr
	}

	// Nothing else moved the row, so our failure is the real state of the
	// session rather than the losing half of a race.
	if !latest.UpdatedAt.Time.After(sess.UpdatedAt.Time) {
		return zero, refreshErr
	}

	if !accessTokenUsable(latest, time.Now()) {
		return zero, refreshErr
	}
	plain, err := s.enc.Decrypt(latest.AccessTokenEncrypted)
	if err != nil {
		return zero, refreshErr
	}

	return RefreshResult{Session: latest, AccessToken: plain, Outcome: RefreshOutcomeAdoptedConcurrentWinner}, nil
}

func accessTokenUsable(sess remotesessions_repo.RemoteSession, now time.Time) bool {
	return authorizationUsable(sess, now) &&
		(!sess.AccessExpiresAt.Valid || sess.AccessExpiresAt.Time.After(now))
}

func refreshTokenUsable(sess remotesessions_repo.RemoteSession, now time.Time) bool {
	return authorizationUsable(sess, now) &&
		(!sess.RefreshExpiresAt.Valid || sess.RefreshExpiresAt.Time.After(now))
}

func authorizationUsable(sess remotesessions_repo.RemoteSession, now time.Time) bool {
	return !sess.AuthorizationExpiresAt.Valid || sess.AuthorizationExpiresAt.Time.After(now)
}

// awaitRefreshedSession returns the session row and access token the lock
// holder wrote. Reports false if the row has not moved within
// refreshWaitBudget, rather than stranding the caller on a dead holder.
func (s *RefreshService) awaitRefreshedSession(
	ctx context.Context,
	q *remotesessions_repo.Queries,
	sess remotesessions_repo.RemoteSession,
) (RefreshResult, bool) {
	var zero RefreshResult

	ticker := time.NewTicker(refreshWaitPoll)
	defer ticker.Stop()

	deadline := time.NewTimer(refreshWaitBudget)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return zero, false
		case <-deadline.C:
			return zero, false
		case <-ticker.C:
		}

		latest, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
			SubjectUrn:            sess.SubjectUrn,
			RemoteSessionClientID: sess.RemoteSessionClientID,
		})
		if err != nil {
			return zero, false
		}
		if !latest.UpdatedAt.Time.After(sess.UpdatedAt.Time) {
			continue
		}
		if !accessTokenUsable(latest, time.Now()) {
			return zero, false
		}

		plain, err := s.enc.Decrypt(latest.AccessTokenEncrypted)
		if err != nil {
			return zero, false
		}

		return RefreshResult{Session: latest, AccessToken: plain, Outcome: RefreshOutcomeAdoptedConcurrentWinner}, true
	}
}
