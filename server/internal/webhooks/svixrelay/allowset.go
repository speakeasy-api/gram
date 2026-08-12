package svixrelay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/webhooks/svixrelay/repo"
)

const (
	// allowSetTTL bounds how stale a cached answer may be. Short enough that
	// enabling or disabling webhooks takes effect promptly, long enough that the
	// steady state costs one query per organization per interval rather than one
	// per message.
	allowSetTTL = 30 * time.Second

	// allowSetMaxSize caps the cache so the long tail of organizations that emit
	// an event once cannot grow it without bound. Well above the number of
	// organizations producing events in any TTL window, so eviction is not part
	// of the steady state.
	allowSetMaxSize = 4096
)

// allowSet answers "may this organization receive webhooks, and to which Svix
// app" while keeping the database off the hot path.
//
// The subscriber sees every event for every organization, including the large
// majority belonging to organizations that have never enabled webhooks, so a
// query per message would add I/O to events that are about to be dropped. A
// per-organization cache reduces that to one query per organization per TTL,
// and it caches "not eligible" as readily as an app id — that negative entry is
// what makes the common case free.
//
// Resolving per organization rather than loading the whole eligible set costs a
// query the first time each organization is seen, and in exchange the work
// tracks the organizations actually producing traffic instead of the size of
// the customer base. It also means an organization that enables webhooks is
// picked up within a TTL rather than immediately; the same bound already
// applied to disabling them.
type allowSet struct {
	db *pgxpool.Pool

	// An empty value is a cached "not eligible".
	apps   *expirable.LRU[string, string]
	flight singleflight.Group
}

func newAllowSet(db *pgxpool.Pool) *allowSet {
	return &allowSet{
		db:     db,
		apps:   expirable.NewLRU[string, string](allowSetMaxSize, nil, allowSetTTL),
		flight: singleflight.Group{},
	}
}

// svixAppFor returns the Svix application id for an organization, or an empty
// string when the organization should not receive webhooks.
//
// An error means the answer is unknown, never "not eligible". Callers must
// treat it as a reason to retry: delivering to an organization that disabled
// webhooks is a customer-visible incident, whereas redelivering later is not.
func (a *allowSet) svixAppFor(ctx context.Context, orgID string) (string, error) {
	if appID, ok := a.apps.Get(orgID); ok {
		return appID, nil
	}

	// Messages for a single organization are handled concurrently, so an expiry
	// would otherwise let every in-flight message for that organization issue
	// its own query; singleflight collapses them into one.
	value, err, _ := a.flight.Do(orgID, func() (any, error) {
		row, err := repo.New(a.db).GetWebhookEnabledOrg(ctx, orgID)
		switch {
		case err == nil:
			a.apps.Add(orgID, row.SvixAppID.String)
			return row.SvixAppID.String, nil
		case errors.Is(err, pgx.ErrNoRows):
			// Either the organization does not exist or it fails one of the
			// eligibility conditions. Both are a drop, and caching that is the
			// point of the set.
			a.apps.Add(orgID, "")
			return "", nil
		default:
			// Deliberately not cached: an outage must not be remembered as a "no".
			return "", fmt.Errorf("look up webhook enabled org: %w", err)
		}
	})
	if err != nil {
		return "", err
	}

	appID, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("webhook allow set singleflight returned unexpected type %T", value)
	}

	return appID, nil
}
