package cimdcandidates

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

const (
	// recordTimeout bounds the write so a slow database cannot hold up sign-in.
	// The write is one indexed upsert; anything slower is dropped.
	recordTimeout = 500 * time.Millisecond

	// recentTTL is how long a process skips repeat writes of one sighting.
	// Documents already refresh at most every five minutes per issuer; an hour
	// keeps last_seen_at useful while cutting repeats across short-lived caches.
	recentTTL = time.Hour

	// recentSize bounds the per-process memory of recent sightings.
	recentSize = 4096
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

// Recorder writes candidates and sightings, at most once per sighting per
// process window.
type Recorder struct {
	db  *pgxpool.Pool
	now func() time.Time

	mu     sync.Mutex
	recent *expirable.LRU[recentKey, struct{}]
}

func NewRecorder(db *pgxpool.Pool) *Recorder {
	return &Recorder{
		db:     db,
		now:    time.Now,
		mu:     sync.Mutex{},
		recent: expirable.NewLRU[recentKey, struct{}](recentSize, nil, recentTTL),
	}
}

// Record writes the sighting. It never returns an error: a failed write is
// logged and the next sighting tries again. A nil Recorder records nothing.
func (r *Recorder) Record(ctx context.Context, logger *slog.Logger, sighting Sighting) {
	if r == nil {
		return
	}
	key := recentKey{issuerID: sighting.UserSessionIssuerID, url: sighting.URL}
	if !r.claim(key) {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	now := conv.ToPGTimestamptz(r.now())
	written, err := repo.New(r.db).RecordSighting(ctx, repo.RecordSightingParams{
		Url:                 sighting.URL,
		ClientName:          conv.ToPGTextEmpty(sighting.ClientName),
		FirstSeenAt:         now,
		LastSeenAt:          now,
		UserSessionIssuerID: sighting.UserSessionIssuerID,
		MaxPendingPerIssuer: MaxPendingPerIssuer,
	})
	if err != nil {
		r.release(key)
		if !errors.Is(err, context.Canceled) {
			logger.ErrorContext(ctx, "record cimd client candidate", attr.SlogOAuthClientID(sighting.URL), attr.SlogError(err))
		}
		return
	}
	if written == 0 {
		logger.InfoContext(ctx, "cimd client candidate not recorded: issuer reached its pending candidate cap", attr.SlogOAuthClientID(sighting.URL))
	}
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
