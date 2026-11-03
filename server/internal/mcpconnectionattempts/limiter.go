package mcpconnectionattempts

import (
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// onceKeyBurst lets a short retry loop record before the per-key rate applies.
	onceKeyBurst = 5

	// onceKeyInterval is about one row per minute for a repeating key.
	onceKeyInterval = time.Minute

	// onceOrgBurst and onceOrgPerSecond cap one organization's rows across keys per replica.
	// The writer takes 500 rows a flush, so one organization can't fill the shared queue.
	onceOrgBurst     = 200
	onceOrgPerSecond = 10

	// onceQueueShareNumerator and onceQueueShareDenominator give Once rows 3/4 of the queue,
	// so a burst across organizations can't crowd out Step and End.
	onceQueueShareNumerator   = 3
	onceQueueShareDenominator = 4

	// onceMaxKeys bounds memory to about 10 MiB, one template row per key.
	onceMaxKeys = 10_000

	// onceMaxOrgs bounds the per-organization ceilings the same way.
	onceMaxOrgs = 2_000

	// oncePendingPerPass bounds one periodic pass, so it holds the lock only briefly.
	oncePendingPerPass = 1_000
)

// onceKey is what one repeating single-request row looks like.
type onceKey struct {
	organizationID string
	serverID       string
	step           string
	what           string
	subject        string
	providerHost   string
	memberID       string
}

// onceEntry is one key's limit and the rows it has held back.
type onceEntry struct {
	limiter  *rate.Limiter
	pending  uint32
	template Row
}

// onceDecision is what happened to one row.
type onceDecision struct {
	// allowed says the row may go on.
	allowed bool

	// repeats is how many held-back rows it stands for.
	repeats uint32

	// lost is how many held-back rows evicted keys took with them.
	lost int
}

// onceLimiter holds back Once rows, which have no proof of a real flow behind them, and
// keeps their count so true totals survive. It does no I/O, so its lock is only held briefly.
type onceLimiter struct {
	mu          sync.Mutex
	keys        *simplelru.LRU[onceKey, *onceEntry]
	orgs        *simplelru.LRU[string, *rate.Limiter]
	withPending map[onceKey]struct{}
	lost        int
}

func newOnceLimiter() *onceLimiter {
	l := &onceLimiter{mu: sync.Mutex{}, keys: nil, orgs: nil, withPending: map[onceKey]struct{}{}, lost: 0}
	keys, keysErr := simplelru.NewLRU(onceMaxKeys, func(key onceKey, entry *onceEntry) {
		l.lost += int(entry.pending)
		delete(l.withPending, key)
	})
	orgs, orgsErr := simplelru.NewLRU[string, *rate.Limiter](onceMaxOrgs, nil)
	if keysErr != nil || orgsErr != nil {
		// Only a non-positive size fails, and these are constants.
		panic("connection attempt limiter sizes must be positive")
	}
	l.keys, l.orgs = keys, orgs
	return l
}

func onceKeyOf(row Row) onceKey {
	// An anonymous subject is new on every request, so it can't separate callers.
	subject := row.SubjectURN
	if strings.HasPrefix(subject, string(urn.SessionSubjectKindAnonymous)+":") {
		subject = ""
	}
	what := row.Reason
	if what == "" {
		what = row.StepResult
	}
	return onceKey{organizationID: row.OrganizationID, serverID: row.ServerID, step: row.Step, what: what, subject: subject, providerHost: row.ProviderHost, memberID: row.MemberID}
}

func (l *onceLimiter) entry(key onceKey) *onceEntry {
	entry, ok := l.keys.Get(key)
	if !ok {
		entry = &onceEntry{limiter: rate.NewLimiter(rate.Every(onceKeyInterval), onceKeyBurst), pending: 0, template: Row{}} //nolint:exhaustruct // the zero row until one is held back
		l.keys.Add(key, entry)
	}
	return entry
}

func (l *onceLimiter) org(organizationID string) *rate.Limiter {
	org, ok := l.orgs.Get(organizationID)
	if !ok {
		org = rate.NewLimiter(onceOrgPerSecond, onceOrgBurst)
		l.orgs.Add(organizationID, org)
	}
	return org
}

func (l *onceLimiter) hold(key onceKey, entry *onceEntry, row Row) {
	entry.pending++
	entry.template = row
	l.withPending[key] = struct{}{}
}

func (l *onceLimiter) takeLost() int {
	lost := l.lost
	l.lost = 0
	return lost
}

// take decides one row. A row held back by either limit spends from neither, and is counted on its key.
func (l *onceLimiter) take(row Row, now time.Time) onceDecision {
	key := onceKeyOf(row)

	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.entry(key)
	reservation := entry.limiter.ReserveN(now, 1)
	if reservation.DelayFrom(now) > 0 {
		reservation.CancelAt(now)
		l.hold(key, entry, row)
		return onceDecision{allowed: false, repeats: 0, lost: l.takeLost()}
	}
	if !l.org(row.OrganizationID).AllowN(now, 1) {
		reservation.CancelAt(now)
		l.hold(key, entry, row)
		return onceDecision{allowed: false, repeats: 0, lost: l.takeLost()}
	}
	repeats := entry.pending
	entry.pending = 0
	entry.template = Row{} //nolint:exhaustruct // drops the held-back row
	delete(l.withPending, key)
	return onceDecision{allowed: true, repeats: repeats, lost: l.takeLost()}
}

// holdBack counts a row another limit refused, and returns any count lost to eviction.
func (l *onceLimiter) holdBack(row Row) int {
	key := onceKeyOf(row)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.hold(key, l.entry(key), row)
	return l.takeLost()
}

// pendingRows returns one row per key with a held-back count, for bursts that stopped. The row
// is one of those held back, so it stands for the rest. A periodic pass stays within the
// organization's ceiling, and the last pass, at shutdown, takes everything.
func (l *onceLimiter) pendingRows(now time.Time, last bool) []Row {
	l.mu.Lock()
	defer l.mu.Unlock()

	var rows []Row
	for key := range l.withPending {
		if !last && len(rows) >= oncePendingPerPass {
			break
		}
		entry, ok := l.keys.Peek(key)
		if !ok {
			delete(l.withPending, key)
			continue
		}
		if !last && !l.org(key.organizationID).AllowN(now, 1) {
			continue
		}
		row := entry.template
		row.Repeats = entry.pending - 1
		entry.pending = 0
		entry.template = Row{} //nolint:exhaustruct // drops the held-back row
		delete(l.withPending, key)
		rows = append(rows, row)
	}
	return rows
}
