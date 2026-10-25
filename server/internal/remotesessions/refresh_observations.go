package remotesessions

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
)

// RefreshObservation is one logical refresh a request made: the outcome
// RefreshNow returned, not a count of calls to the provider. A refresh that
// adopted another caller's result may or may not have contacted the provider.
type RefreshObservation struct {
	// Started is when the refresh began.
	Started time.Time
	// ProviderIssuerID is the provider's remote session issuer, unset when
	// the session's client was not loaded.
	ProviderIssuerID uuid.NullUUID
	// Finished reports that the refresh returned before Close.
	Finished bool
	// Outcome is RefreshNow's classification; unset when not Finished.
	Outcome remotesessionmetrics.RefreshOutcome
}

// RefreshObservations collects the request-driven refreshes started while one
// gateway request is served. It is safe for concurrent use. A refresh holds
// its slot from the moment it starts, so one still running at Close is
// reported unfinished rather than lost; what it does afterwards is ignored.
type RefreshObservations struct {
	mu      sync.Mutex
	limit   int
	closed  bool
	items   []RefreshObservation
	dropped int
}

// NewRefreshObservations returns a collector that keeps at most limit
// refreshes and counts the rest.
func NewRefreshObservations(limit int) *RefreshObservations {
	return &RefreshObservations{mu: sync.Mutex{}, limit: limit, closed: false, items: nil, dropped: 0}
}

type refreshObservationsKey struct{}

// WithRefreshObservations makes request-driven refreshes under ctx report to
// sink. Only the request that owns sink installs it.
func WithRefreshObservations(ctx context.Context, sink *RefreshObservations) context.Context {
	return context.WithValue(ctx, refreshObservationsKey{}, sink)
}

func refreshObservationsFrom(ctx context.Context) *RefreshObservations {
	sink, _ := ctx.Value(refreshObservationsKey{}).(*RefreshObservations)
	return sink
}

// start reserves a slot for a refresh, or returns -1 when the collector is
// closed or full; a full collector counts the refresh as dropped.
func (o *RefreshObservations) start(started time.Time) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return -1
	}
	if len(o.items) >= o.limit {
		o.dropped++
		return -1
	}
	o.items = append(o.items, RefreshObservation{Started: started, ProviderIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Finished: false, Outcome: ""})
	return len(o.items) - 1
}

func (o *RefreshObservations) setProvider(slot int, provider uuid.UUID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || slot < 0 {
		return
	}
	o.items[slot].ProviderIssuerID = uuid.NullUUID{UUID: provider, Valid: true}
}

func (o *RefreshObservations) finish(slot int, outcome remotesessionmetrics.RefreshOutcome) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || slot < 0 {
		return
	}
	o.items[slot].Finished = true
	o.items[slot].Outcome = outcome
}

// Close returns the refreshes as they stand and how many were not kept
// because the collector was full. Later changes are ignored.
func (o *RefreshObservations) Close() (observations []RefreshObservation, dropped int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	return append([]RefreshObservation(nil), o.items...), o.dropped
}

// refreshOutcomeOf is RefreshNow's classification of one call.
func refreshOutcomeOf(result RefreshResult, err error) remotesessionmetrics.RefreshOutcome {
	if err == nil {
		return result.Outcome
	}
	if refreshErr, ok := errors.AsType[*RefreshError](err); ok {
		return refreshErr.Outcome
	}
	return remotesessionmetrics.RefreshOutcomeInternalError
}
