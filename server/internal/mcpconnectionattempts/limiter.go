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

	// onceMaxKeys bounds memory to a few MiB. An evicted key only gets a fresh burst.
	onceMaxKeys = 10_000

	// onceMaxOrgs bounds the per-organization ceilings the same way.
	onceMaxOrgs = 2_000
)

// onceKey is what one repeating single-request row looks like.
type onceKey struct {
	organizationID string
	serverID       string
	what           string
	subject        string
}

// onceLimiter holds back Once rows, which have no proof of a real flow behind them.
// It does no I/O, so its lock is only held for a map update.
type onceLimiter struct {
	mu   sync.Mutex
	keys *simplelru.LRU[onceKey, *rate.Limiter]
	orgs *simplelru.LRU[string, *rate.Limiter]
}

func newOnceLimiter() *onceLimiter {
	keys, keysErr := simplelru.NewLRU[onceKey, *rate.Limiter](onceMaxKeys, nil)
	orgs, orgsErr := simplelru.NewLRU[string, *rate.Limiter](onceMaxOrgs, nil)
	if keysErr != nil || orgsErr != nil {
		// Only a non-positive size fails, and these are constants.
		panic("connection attempt limiter sizes must be positive")
	}
	return &onceLimiter{mu: sync.Mutex{}, keys: keys, orgs: orgs}
}

// allow reports whether a row may go on. A key that is held back uses none of its organization's ceiling.
func (l *onceLimiter) allow(row Row, now time.Time) bool {
	// An anonymous subject is new on every request, so it can't separate callers.
	subject := row.SubjectURN
	if strings.HasPrefix(subject, string(urn.SessionSubjectKindAnonymous)+":") {
		subject = ""
	}
	what := row.Reason
	if what == "" {
		what = row.StepResult
	}
	key := onceKey{organizationID: row.OrganizationID, serverID: row.ServerID, what: what, subject: subject}

	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.keys.Get(key)
	if !ok {
		limiter = rate.NewLimiter(rate.Every(onceKeyInterval), onceKeyBurst)
		l.keys.Add(key, limiter)
	}
	if !limiter.AllowN(now, 1) {
		return false
	}
	org, ok := l.orgs.Get(row.OrganizationID)
	if !ok {
		org = rate.NewLimiter(onceOrgPerSecond, onceOrgBurst)
		l.orgs.Add(row.OrganizationID, org)
	}
	return org.AllowN(now, 1)
}
