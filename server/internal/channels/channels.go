// Package channels implements Speakeasy Channels: organization-scoped
// coordination between leased instances of existing Control Plane identities
// and dashboard users.
//
// Storage is normalized (see queries.sql). Channels authorization is
// organization-scoped; project ownership is resolved server-side to the
// organization's canonical default project and never comes from the caller.
package channels

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// Principal is the authenticated caller. Instances are not principals: every
// permission belongs to the parent identity named here.
type Principal struct {
	OrganizationID, SubjectID string
	// SubjectName is the authenticated account's username. It names the browser
	// session an operator gets automatically and is display only.
	SubjectName                      string
	CanConnect, CanAdmin, CanReadAll bool
}

// Authenticator revalidates identity, organization membership and Channels
// permissions. It is called for every operation and on every subscription
// reconciliation, never only at admission.
type Authenticator func(*http.Request) (Principal, error)

// Surfaces are decided by the transport that received the request, never by
// caller-supplied arguments: only the browser API can hold a dashboard session.
const (
	surfaceDashboard = "dashboard"
	dashboardSuffix  = " on dashboard"
)

// Operation outcomes. Callers distinguish them so a client can tell a rejected
// payload from lost access and only blank cached content for the latter.
var (
	// ErrDenied is a permission, visibility or enablement refusal (403).
	ErrDenied = errors.New("channels access denied")
	// ErrInvalid is a malformed or out-of-bounds request (400).
	ErrInvalid = errors.New("invalid channels request")
	// ErrLimited is a write burst refusal the caller should retry (429).
	ErrLimited = errors.New("channels write rate exceeded")
	// ErrUnavailable is missing storage or a failed dependency (503).
	ErrUnavailable = errors.New("channels storage unavailable")
)

// Limits bound every unbounded dimension of the product: payload sizes, page
// sizes, presence, concurrent streams and write bursts. The client-visible
// subset is published in each snapshot so no client hardcodes them.
type Limits struct {
	MessageBytes        int
	TitleBytes          int
	NoteBytes           int
	LabelBytes          int
	IdempotencyKeyBytes int
	// HistoryLimit is the largest page any read or history call returns.
	HistoryLimit int
	// SnapshotMessages is the bootstrap tail size.
	SnapshotMessages int
	ConversationPage int
	PresencePage     int
	PreferencePage   int
	ExceptionPage    int
	// ActiveInstances caps live leases per organization.
	ActiveInstances int
	// Streams caps concurrent subscriptions per handler, StreamsPerSubject per
	// identity within it.
	Streams           int
	StreamsPerSubject int
	WritesPerMinute   int
	WriteBurst        int
	StreamLifetime    time.Duration
	// ReconcileInterval is the safety net that recovers a lost delivery hint
	// and rechecks live authorization. Hints, not this interval, carry normal
	// latency.
	ReconcileInterval time.Duration
	// HintDebounce coalesces bursts of hints into one reconciliation.
	HintDebounce time.Duration
	// LeaseDefault and LeaseMax bound a presence lease.
	LeaseDefault, LeaseMax time.Duration
}

// DefaultLimits returns the shipped defaults. They are deliberately generous
// for a single organization and small enough that one tenant cannot exhaust a
// replica.
func DefaultLimits() Limits {
	return Limits{
		MessageBytes:        32768,
		TitleBytes:          256,
		NoteBytes:           1024,
		LabelBytes:          128,
		IdempotencyKeyBytes: 256,
		HistoryLimit:        200,
		SnapshotMessages:    50,
		ConversationPage:    200,
		PresencePage:        200,
		PreferencePage:      500,
		ExceptionPage:       500,
		ActiveInstances:     200,
		Streams:             256,
		StreamsPerSubject:   8,
		WritesPerMinute:     600,
		WriteBurst:          120,
		StreamLifetime:      30 * time.Minute,
		ReconcileInterval:   15 * time.Second,
		HintDebounce:        250 * time.Millisecond,
		LeaseDefault:        300 * time.Second,
		LeaseMax:            3600 * time.Second,
	}
}

// Retention bounds the cleanup sweep. Per-organization ages live in
// channels_settings so an organization can be given its own policy.
type Retention struct {
	// Batch is the number of rows one organization's sweep may touch per pass.
	Batch int
	// Organizations is the number of organizations one sweep may visit.
	Organizations int
}

// DefaultRetention returns sweep bounds small enough to run anywhere.
func DefaultRetention() Retention {
	return Retention{Batch: 500, Organizations: 100}
}

// Handler serves the Channels browser API and MCP endpoint. Close must be
// called before HTTP server shutdown.
type Handler struct {
	db           *pgxpool.Pool
	repo         *repo.Queries
	authenticate Authenticator
	logger       *slog.Logger
	limits       Limits
	retention    Retention
	events       *broker
	writes       *burstLimiter
	adminAudit   AdminAudit
	stopEvents   context.CancelFunc

	mu         sync.Mutex
	streams    map[string]*stream
	perSubject map[string]int
	closed     bool
	wg         sync.WaitGroup
}

// Option customizes a Handler. New stays compatible with callers that pass
// only a pool and an authenticator.
type Option func(*Handler)

// WithLogger attaches a logger for background delivery and retention work.
func WithLogger(logger *slog.Logger) Option {
	return func(h *Handler) {
		if logger != nil {
			h.logger = logger
		}
	}
}

// WithLimits replaces the operational limits.
func WithLimits(limits Limits) Option {
	return func(h *Handler) { h.limits = limits }
}

// WithRetention replaces the retention sweep bounds.
func WithRetention(retention Retention) Option {
	return func(h *Handler) { h.retention = retention }
}

// New builds a Handler and starts its shared delivery listener. One listener
// per process serves every subscriber, so subscriptions never consume a
// database connection each.
func New(db *pgxpool.Pool, authenticate Authenticator, opts ...Option) *Handler {
	limits := DefaultLimits()
	h := &Handler{
		db:           db,
		repo:         repo.New(db),
		authenticate: authenticate,
		logger:       slog.New(slog.DiscardHandler),
		limits:       limits,
		retention:    DefaultRetention(),
		events:       newBroker(),
		writes:       newBurstLimiter(limits.WritesPerMinute, limits.WriteBurst),
		adminAudit:   nil,
		stopEvents:   func() {},
		mu:           sync.Mutex{},
		streams:      map[string]*stream{},
		perSubject:   map[string]int{},
		closed:       false,
		wg:           sync.WaitGroup{},
	}
	for _, opt := range opts {
		opt(h)
	}
	h.writes = newBurstLimiter(h.limits.WritesPerMinute, h.limits.WriteBurst)
	if db != nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.stopEvents = cancel
		h.wg.Go(func() {
			h.events.run(ctx, db, h.logger)
		})
	}
	return h
}

// Close rejects future streams, gracefully completes active subscriptions and
// stops the shared delivery listener.
func (h *Handler) Close() {
	h.mu.Lock()
	h.closed = true
	cancels := make([]context.CancelFunc, 0, len(h.streams))
	for _, held := range h.streams {
		cancels = append(cancels, held.cancel)
	}
	h.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	h.stopEvents()
}

// Shutdown closes subscriptions and waits for their completion, bounded by ctx.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.Close()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("channels shutdown did not complete: %w", context.Cause(ctx))
	}
}

// streamCount reports how many subscriptions this handler currently holds.
func (h *Handler) streamCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.streams)
}

// identifier returns a 48-character random hex string, used for resume secrets.
func identifier() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
