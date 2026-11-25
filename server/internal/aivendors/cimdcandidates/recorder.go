package cimdcandidates

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

const (
	// recordTimeout bounds one write. It runs off the request, so this only
	// limits how long a slow database holds a write slot.
	recordTimeout = 2 * time.Second

	// recordConcurrency bounds writes in flight per process. Writes happen only
	// on document fetches, so a handful of slots absorbs normal traffic; past
	// that a write is dropped rather than queued.
	recordConcurrency = 8

	// recentTTL is how long a process skips repeat writes of one sighting.
	// Documents already refresh at most every five minutes per issuer; an hour
	// keeps last_seen_at useful while cutting repeats across short-lived caches.
	recentTTL = time.Hour

	// recentSize bounds the per-process memory of recent sightings.
	recentSize = 4096

	meterCandidateWrites = "cimd.candidates.writes"
)

// writeOutcome is the low-cardinality result of one candidate write.
type writeOutcome string

const (
	// writeRecorded wrote the candidate and its sighting.
	writeRecorded writeOutcome = "recorded"

	// writeCapped wrote nothing: the issuer is at its pending cap or no longer
	// exists.
	writeCapped writeOutcome = "capped"

	// writeDropped skipped the write because every slot was busy.
	writeDropped writeOutcome = "dropped"

	// writeFailed is a database error, logged with its cause.
	writeFailed writeOutcome = "failed"
)

// Sighting is one issuer admitting one uncovered client.
type Sighting struct {
	// URL is the client_id the client presented, after document validation.
	URL string

	// ClientName is the document's client_name; empty when absent.
	ClientName string

	// UserSessionIssuerID is the issuer that admitted the client.
	UserSessionIssuerID uuid.UUID
}

type recentKey struct {
	issuerID uuid.UUID
	url      string
}

// Recorder writes candidates and sightings off the request path, at most once
// per sighting per process window.
type Recorder struct {
	db     *pgxpool.Pool
	now    func() time.Time
	writes metric.Int64Counter

	slots    chan struct{}
	inflight sync.WaitGroup

	mu     sync.Mutex
	recent *expirable.LRU[recentKey, struct{}]
}

func NewRecorder(db *pgxpool.Pool, meterProvider metric.MeterProvider, logger *slog.Logger) *Recorder {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates")
	writes, err := meter.Int64Counter(
		meterCandidateWrites,
		metric.WithDescription("Count of CIMD client candidate writes by outcome"),
		metric.WithUnit("{write}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterCandidateWrites), attr.SlogError(err))
	}
	return &Recorder{
		db:       db,
		now:      time.Now,
		writes:   writes,
		slots:    make(chan struct{}, recordConcurrency),
		inflight: sync.WaitGroup{},
		mu:       sync.Mutex{},
		recent:   expirable.NewLRU[recentKey, struct{}](recentSize, nil, recentTTL),
	}
}

// Record starts the write and returns at once. The write keeps the request's
// context values but not its cancellation, so a client hanging up does not
// lose it. A failed or dropped write is forgotten, so the next sighting tries
// again. A nil Recorder records nothing.
func (r *Recorder) Record(ctx context.Context, logger *slog.Logger, sighting Sighting) {
	if r == nil {
		return
	}
	key := recentKey{issuerID: sighting.UserSessionIssuerID, url: sighting.URL}
	if !r.claim(key) {
		return
	}

	select {
	case r.slots <- struct{}{}:
	default:
		r.release(key)
		r.count(ctx, writeDropped)
		return
	}

	r.inflight.Add(1)
	go func() {
		defer r.inflight.Done()
		defer func() { <-r.slots }()

		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
		defer cancel()
		r.write(ctx, logger, key, sighting)
	}()
}

// Wait blocks until every write started so far has finished.
func (r *Recorder) Wait() {
	if r == nil {
		return
	}
	r.inflight.Wait()
}

func (r *Recorder) write(ctx context.Context, logger *slog.Logger, key recentKey, sighting Sighting) {
	now := conv.ToPGTimestamptz(r.now())
	written, err := repo.New(r.db).RecordSighting(ctx, repo.RecordSightingParams{
		Url:                 sighting.URL,
		ClientName:          conv.ToPGTextEmpty(sighting.ClientName),
		FirstSeenAt:         now,
		LastSeenAt:          now,
		UserSessionIssuerID: sighting.UserSessionIssuerID,
		MaxPendingPerIssuer: MaxPendingPerIssuer,
	})
	switch {
	case err != nil:
		r.release(key)
		r.count(ctx, writeFailed)
		logger.ErrorContext(ctx, "record cimd client candidate", attr.SlogError(err))
	case written == 0:
		r.count(ctx, writeCapped)
		logger.InfoContext(ctx, "cimd client candidate not recorded: issuer at the pending cap or no longer exists")
	default:
		r.count(ctx, writeRecorded)
	}
}

func (r *Recorder) count(ctx context.Context, outcome writeOutcome) {
	if r.writes == nil {
		return
	}
	r.writes.Add(ctx, 1, metric.WithAttributes(attr.CIMDCandidateOutcome(outcome)))
}

func (r *Recorder) claim(key recentKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.recent.Get(key); ok {
		return false
	}
	r.recent.Add(key, struct{}{})
	return true
}

func (r *Recorder) release(key recentKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recent.Remove(key)
}
