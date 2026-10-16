package channels

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// eventChannel is the PostgreSQL notification channel Channels writes delivery
// hints to. A hint is published inside the writing transaction, so PostgreSQL
// releases it only on commit: history is durable before anyone is told to
// re-read it.
const eventChannel = "channels_events"

// hint is the payload of a delivery notification. It carries routing
// identifiers and revisions only — never message bodies, labels or secrets.
type hint struct {
	Organization string `json:"org"`
	Kind         string `json:"kind"`
	Revision     int64  `json:"rev"`
}

const (
	hintMessages = "messages"
	hintPresence = "presence"
	hintThreads  = "threads"
)

// broker fans one process-wide listener out to every in-process subscriber.
// Subscribers never hold a database connection: they wait on a coalescing
// channel and reconcile with bounded queries when it fires.
type broker struct {
	mu   sync.Mutex
	next int64
	subs map[string]map[int64]chan struct{}
}

func newBroker() *broker {
	return &broker{mu: sync.Mutex{}, next: 0, subs: map[string]map[int64]chan struct{}{}}
}

// subscribe registers interest in one organization's hints. The returned
// channel has room for one pending wake-up, which is all a reconciling
// subscriber needs: a burst of hints collapses into a single pass.
func (b *broker) subscribe(organizationID string) (<-chan struct{}, func()) {
	wake := make(chan struct{}, 1)
	b.mu.Lock()
	b.next++
	id := b.next
	byOrg, ok := b.subs[organizationID]
	if !ok {
		byOrg = map[int64]chan struct{}{}
		b.subs[organizationID] = byOrg
	}
	byOrg[id] = wake
	b.mu.Unlock()
	return wake, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if byOrg, ok := b.subs[organizationID]; ok {
			delete(byOrg, id)
			if len(byOrg) == 0 {
				delete(b.subs, organizationID)
			}
		}
	}
}

// wake signals one organization's subscribers.
func (b *broker) wake(organizationID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs[organizationID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// wakeAll signals every subscriber. It runs when the listener connects or
// reconnects, because hints published while it was away are gone and only a
// reconciliation can recover them.
func (b *broker) wakeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, byOrg := range b.subs {
		for _, ch := range byOrg {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// run owns the single listening connection for this process. It reconnects
// with a bounded backoff and wakes every subscriber each time it comes back,
// so a dropped listener costs one extra reconciliation rather than lost
// messages — history is the durable source, hints are only latency.
func (b *broker) run(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger) {
	const backoff = time.Second
	for ctx.Err() == nil {
		b.listen(ctx, db, logger)
		b.wakeAll()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (b *broker) listen(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger) {
	conn, err := db.Acquire(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logger.ErrorContext(ctx, "channels delivery listener could not acquire a connection", attr.SlogError(err))
		}
		return
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+eventChannel); err != nil {
		if ctx.Err() == nil {
			logger.ErrorContext(ctx, "channels delivery listener could not subscribe", attr.SlogError(err))
		}
		return
	}
	// Anything published between the previous listener dying and this LISTEN is
	// unrecoverable, so start by making everyone reconcile.
	b.wakeAll()
	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.WarnContext(ctx, "channels delivery listener disconnected", attr.SlogError(err))
			}
			return
		}
		var decoded hint
		if err := json.Unmarshal([]byte(notification.Payload), &decoded); err != nil {
			logger.WarnContext(ctx, "channels delivery hint could not be decoded", attr.SlogError(err))
			continue
		}
		if decoded.Organization == "" {
			continue
		}
		b.wake(decoded.Organization)
	}
}
