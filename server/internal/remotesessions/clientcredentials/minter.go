package clientcredentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	// grantTimeout bounds one grant, including its retry without the
	// resource, so a slow upstream cannot outlive the lease.
	grantTimeout = 20 * time.Second

	// leaseTTL outlives grantTimeout so a slow holder keeps single flight
	// until it has stored its result or given up.
	leaseTTL = 30 * time.Second

	// waitBudget bounds how long a request waits on a concurrent holder. A
	// waiter takes the lease over as soon as the holder releases it or its TTL
	// lapses, so this only ends a wait the cache keeps alive; it outlives
	// leaseTTL so an abandoned lease lapses first.
	waitBudget = leaseTTL + 5*time.Second

	// waitInterval is the poll cadence while waiting. A token request takes
	// hundreds of milliseconds, so polling faster only adds cache load.
	waitInterval = 200 * time.Millisecond

	// detachedWriteTimeout bounds cache writes that outlive the caller's
	// context: storing a result and releasing the lease. Each is one Redis
	// round trip.
	detachedWriteTimeout = 2 * time.Second

	// failureTTL suppresses repeated grants after a rejection a retry cannot
	// fix, such as invalid_client, until the client's credentials change.
	failureTTL = 30 * time.Second

	// forgetMinAge keeps a just-minted credential that the upstream rejected.
	// An upstream that rejects a fresh token rejects the next one too, so
	// minting again would only turn every request into a token request.
	forgetMinAge = 30 * time.Second
)

// errWaitExpired reports that a concurrent holder kept the lease without
// storing a credential or a failure for waitBudget.
var errWaitExpired = errors.New("clientcredentials: concurrent grant did not finish")

// Minter obtains upstream credentials with the client credentials grant,
// cached across replicas. It is safe for concurrent use.
type Minter struct {
	// logger is the component logger.
	logger *slog.Logger

	// db reads client registrations.
	db *pgxpool.Pool

	// enc encrypts cached access tokens.
	enc *encryption.Client

	// endpoints binds registrations to their token endpoints and egress.
	endpoints *remotesessions.ChallengeManager

	// leases single-flights grants across replicas. Nil when the cache cannot
	// hold leases, in which case every miss mints.
	leases cache.LeaseCache

	// credentials holds minted credentials.
	credentials cache.TypedCacheObject[credentialEntry]

	// failures holds recent failures a retry cannot fix.
	failures cache.TypedCacheObject[failureEntry]

	// now is the clock, replaced in tests.
	now func() time.Time
}

var _ remotesessions.ClientCredentialSource = (*Minter)(nil)

// New builds a minter over the challenge manager's egress, tunnel and client
// assertion configuration. store holds credentials, recent failures and the
// single-flight leases; without lease support, every miss mints.
func New(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, endpoints *remotesessions.ChallengeManager, store cache.Cache) *Minter {
	logger = logger.With(attr.SlogComponent("client_credentials"))
	leases, _ := store.(cache.LeaseCache)

	return &Minter{
		logger:      logger,
		db:          db,
		enc:         enc,
		endpoints:   endpoints,
		leases:      leases,
		credentials: cache.NewTypedObjectCache[credentialEntry](logger, store, cache.SuffixNone),
		failures:    cache.NewTypedObjectCache[failureEntry](logger, store, cache.SuffixNone),
		now:         time.Now,
	}
}

// Credential returns a cached credential for the client, or mints one. Errors
// a caller can classify:
//   - remotesessions.ErrClientCredentialClientNotFound;
//   - remotesessions.ErrTokenEndpointConfiguration: the registration cannot
//     authenticate, or the upstream issued a token Speakeasy cannot present;
//   - *remotesessions.TokenEndpointError: the token endpoint rejected the
//     grant or could not be reached. Code is oautherr.CodeInvalidClient when
//     the upstream rejected the client's credentials.
//
// A rejection a retry cannot fix is replayed from the cache for failureTTL,
// or until the client's credentials change.
func (m *Minter) Credential(ctx context.Context, req remotesessions.ClientCredentialRequest) (remotesessions.ClientCredential, error) {
	var none remotesessions.ClientCredential

	if req.OrganizationID == "" || req.ClientID == uuid.Nil {
		return none, errors.New("client credentials request requires an organization and a client")
	}

	logger := m.logger.With(
		attr.SlogOrganizationID(req.OrganizationID),
		attr.SlogRemoteSessionClientID(req.ClientID.String()),
	)

	client, err := repo.New(m.db).GetClientCredentialsGrantClient(ctx, repo.GetClientCredentialsGrantClientParams{
		ID:             req.ClientID,
		OrganizationID: conv.ToPGText(req.OrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, remotesessions.ErrClientCredentialClientNotFound
	case err != nil:
		return none, fmt.Errorf("read client credentials client: %w", err)
	}

	resource := sentResource(client, req.Resource)
	keys := newCacheKeys(client, resource)

	if cred, ok := m.cached(ctx, logger, keys); ok {
		return cred, nil
	}

	if err := m.cachedFailure(ctx, keys); err != nil {
		return none, err
	}

	owner := uuid.NewString()
	held, err := m.acquire(ctx, keys, owner)
	switch {
	case err != nil:
		// Minting unserialized beats refusing to mint.
		logger.WarnContext(ctx, "client credentials lease unavailable; minting without single flight",
			attr.SlogCacheKey(keys.lease),
			attr.SlogError(err),
		)
	case !held:
		cred, acquired, err := m.await(ctx, logger, keys, owner)
		switch {
		case acquired:
			held = true
		case errors.Is(err, errWaitExpired):
			logger.WarnContext(ctx, "timed out waiting on a concurrent client credentials grant; minting directly",
				attr.SlogCacheKey(keys.lease),
			)
		default:
			return cred, err
		}
	}

	if held {
		defer o11y.LogDefer(ctx, logger, "release client credentials lease", func() error {
			// Detached: callers disconnect mid-grant, and a skipped release
			// strands the lease for its full TTL.
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
			defer cancel()

			if _, err := m.leases.ReleaseLeaseIfOwner(releaseCtx, keys.lease, owner); err != nil {
				return fmt.Errorf("release lease: %w", err)
			}

			return nil
		})

		// A holder that finished while this request acquired the lease has
		// already stored its result.
		if cred, ok := m.cached(ctx, logger, keys); ok {
			return cred, nil
		}

		if err := m.cachedFailure(ctx, keys); err != nil {
			return none, err
		}
	}

	cred, err := m.grant(ctx, logger, client, resource, keys)
	if err != nil {
		m.rememberFailure(ctx, logger, keys, err)

		return none, err
	}

	return cred, nil
}

// credential wraps a minted or cached token. Its Forget drops exactly entry
// from the cache after the upstream rejected the token, so the next Credential
// call mints a new one; nil entry, a token that was never cached, has nothing
// to drop. Forget reports false, and keeps the token, when it was minted less
// than forgetMinAge ago: an upstream that rejects a fresh token rejects the
// next one too, so minting again would only turn every request into a token
// request. An entry another replica already replaced is left in place.
func (m *Minter) credential(value string, scheme remotesessions.ClientCredentialScheme, expiresAt, mintedAt time.Time, entry *credentialEntry) remotesessions.ClientCredential {
	return remotesessions.NewClientCredential(value, scheme, expiresAt, func(ctx context.Context) (bool, error) {
		if m.now().Sub(mintedAt) < forgetMinAge {
			return false, nil
		}

		if entry == nil {
			return true, nil
		}

		if _, err := m.credentials.CompareAndDelete(ctx, *entry); err != nil {
			return false, fmt.Errorf("forget client credential: %w", err)
		}

		return true, nil
	})
}

func (m *Minter) acquire(ctx context.Context, keys cacheKeys, owner string) (bool, error) {
	if m.leases == nil {
		return false, errors.New("cache does not support leases")
	}

	held, err := m.leases.AcquireLease(ctx, keys.lease, owner, leaseTTL)
	if err != nil {
		return false, fmt.Errorf("acquire lease: %w", err)
	}

	return held, nil
}

// cached returns the stored credential while it is still served. Any cache or
// decryption failure is a miss.
func (m *Minter) cached(ctx context.Context, logger *slog.Logger, keys cacheKeys) (remotesessions.ClientCredential, bool) {
	var none remotesessions.ClientCredential

	entry, err := m.credentials.Get(ctx, keys.credential)
	if err != nil || entry.AccessTokenEncrypted == "" || !m.now().Before(entry.ExpiresAt) {
		return none, false
	}

	value, err := m.enc.Decrypt(entry.AccessTokenEncrypted)
	if err != nil {
		logger.ErrorContext(ctx, "decrypt cached client credential", attr.SlogCacheKey(keys.credential), attr.SlogError(err))

		return none, false
	}

	if value == "" {
		return none, false
	}

	return m.credential(value, entry.Scheme, entry.ExpiresAt, entry.MintedAt, &entry), true
}

// cachedFailure replays a recent failure as the error class it was recorded
// from, or returns nil.
func (m *Minter) cachedFailure(ctx context.Context, keys cacheKeys) error {
	entry, err := m.failures.Get(ctx, keys.failure)
	switch {
	case err != nil:
		return nil
	case entry.Configuration:
		return fmt.Errorf("recent client credentials grant failure: %w", remotesessions.ErrTokenEndpointConfiguration)
	case entry.StatusCode != 0:
		return fmt.Errorf("recent client credentials grant failure: %w", &remotesessions.TokenEndpointError{
			StatusCode: entry.StatusCode,
			Code:       entry.Code,
			Transport:  false,
			Signing:    false,
		})
	default:
		return nil
	}
}

// rememberFailure records a failure a retry cannot fix, for this request's
// waiters and for later requests. Transport, signing, server and request
// timeout failures may pass on their own and are not recorded. A 429 is
// recorded, so a rate limited upstream gets failureTTL of back-off.
func (m *Minter) rememberFailure(ctx context.Context, logger *slog.Logger, keys cacheKeys, err error) {
	entry := failureEntry{Key: keys.failure, Configuration: false, StatusCode: 0, Code: ""}
	rejected, isRejection := errors.AsType[*remotesessions.TokenEndpointError](err)

	switch {
	case errors.Is(err, remotesessions.ErrTokenEndpointConfiguration):
		entry.Configuration = true
	case isRejection && !rejected.Transport && !rejected.Signing && rejected.StatusCode/100 == 4 && rejected.StatusCode != http.StatusRequestTimeout:
		entry.StatusCode = rejected.StatusCode
		entry.Code = rejected.Code
	default:
		logger.WarnContext(ctx, "client credentials grant failed", attr.SlogError(err))

		return
	}

	logger.WarnContext(ctx, "client credentials grant rejected", attr.SlogError(err))

	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()

	if err := m.failures.Store(storeCtx, entry); err != nil {
		logger.WarnContext(ctx, "record client credentials failure", attr.SlogCacheKey(keys.failure), attr.SlogError(err))
	}
}

// await waits on a concurrent holder. It adopts the holder's credential or
// failure, or takes the lease over once the holder releases it without
// storing either, as after a transport or server failure. The bool reports
// that this request then holds the lease under owner. It returns
// errWaitExpired when the lease outlives waitBudget.
func (m *Minter) await(ctx context.Context, logger *slog.Logger, keys cacheKeys, owner string) (remotesessions.ClientCredential, bool, error) {
	var none remotesessions.ClientCredential

	deadline := time.NewTimer(waitBudget)
	defer deadline.Stop()

	tick := time.NewTicker(waitInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return none, false, fmt.Errorf("wait for concurrent client credentials grant: %w", ctx.Err())
		case <-deadline.C:
			return none, false, errWaitExpired
		case <-tick.C:
			if cred, ok := m.cached(ctx, logger, keys); ok {
				return cred, false, nil
			}

			if err := m.cachedFailure(ctx, keys); err != nil {
				return none, false, err
			}

			held, err := m.acquire(ctx, keys, owner)
			if err != nil {
				logger.WarnContext(ctx, "retry client credentials lease", attr.SlogCacheKey(keys.lease), attr.SlogError(err))

				continue
			}

			if held {
				return none, true, nil
			}
		}
	}
}
