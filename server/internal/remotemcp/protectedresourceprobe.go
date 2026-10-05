package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

const (
	// protectedResourceProbeRecheck is how long a replica trusts its own last
	// check of a server's row, so proxied requests cost no database round trips.
	protectedResourceProbeRecheck = time.Hour

	// protectedResourceStaleAfter is how long a read of the metadata, successful
	// or not, keeps the server from being probed again on use.
	protectedResourceStaleAfter = 24 * time.Hour

	// protectedResourceProbeSlots caps the detached probes in flight per replica;
	// a use arriving with every slot taken is dropped, a later one retries.
	protectedResourceProbeSlots = 2

	// protectedResourceProbeBudget caps one detached probe: discovery's own
	// ten-second budget plus the row read and write.
	protectedResourceProbeBudget = 20 * time.Second
)

// protectedResourceMismatchMessage is recorded when the document's resource
// member or metadata location does not match the server's URL.
const protectedResourceMismatchMessage = "The metadata document resource or location does not match the requested resource."

type protectedResourceCheck struct {
	at time.Time
}

// protectedResourceProbeState is the per-replica debounce for
// probeProtectedResourceOnUse. checked holds *protectedResourceCheck so an
// entry is only ever removed by identity.
type protectedResourceProbeState struct {
	checked sync.Map
	slots   chan struct{}
	now     func() time.Time
}

func newProtectedResourceProbeState() *protectedResourceProbeState {
	return &protectedResourceProbeState{checked: sync.Map{}, slots: make(chan struct{}, protectedResourceProbeSlots), now: time.Now}
}

// sweep drops checks past their recheck window so the map stays bounded by
// the servers used recently, not ever.
func (s *protectedResourceProbeState) sweep(now time.Time) {
	s.checked.Range(func(key, v any) bool {
		if check, ok := v.(*protectedResourceCheck); ok && now.Sub(check.at) >= protectedResourceProbeRecheck {
			s.checked.CompareAndDelete(key, v)
		}
		return true
	})
}

// probeProtectedResourceOnUse keeps the protected resource row of a server in
// use fresh, off the request path. It never fails the proxied response.
func (f *ProxyManager) probeProtectedResourceOnUse(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, organizationID string, resourceURL string) {
	// Metadata persists as-is, so it is only read over TLS.
	if !urls.IsAbsoluteHTTPSOrLoopback(resourceURL) {
		return
	}
	state := f.protectedResourceProbes
	key := projectID.String() + " " + resourceURL
	now := state.now()
	check := &protectedResourceCheck{at: now}
	if v, loaded := state.checked.LoadOrStore(key, check); loaded {
		if prev, ok := v.(*protectedResourceCheck); ok && now.Sub(prev.at) < protectedResourceProbeRecheck {
			return
		}
		if !state.checked.CompareAndSwap(key, v, check) {
			return
		}
	}
	select {
	case state.slots <- struct{}{}:
	default:
		state.checked.CompareAndDelete(key, check)
		return
	}

	// Only the trace carries over: the probe outlives the request and must not inherit its values.
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		defer func() { <-state.slots }()
		if f.afterProtectedResourceProbe != nil {
			defer f.afterProtectedResourceProbe()
		}
		ctx, cancel := context.WithTimeout(detached, protectedResourceProbeBudget)
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorContext(ctx, "protected resource probe panicked", attr.SlogError(fmt.Errorf("%v", rec)))
			}
		}()
		if err := refreshProtectedResource(ctx, f.db, f.guardianPolicy, projectID, organizationID, resourceURL, now); err != nil {
			state.checked.CompareAndDelete(key, check)
			logger.ErrorContext(ctx, "refresh protected resource on use", attr.SlogError(err))
		}
		state.sweep(state.now())
	}()
}

// refreshProtectedResource probes resourceURL unless its row was read, or
// failed to be read, within protectedResourceStaleAfter. Only database
// failures are returned; a failed probe is recorded on the row.
func refreshProtectedResource(ctx context.Context, db *pgxpool.Pool, policy *guardian.Policy, projectID uuid.UUID, organizationID string, resourceURL string, now time.Time) error {
	existing, err := repo.New(db).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: resourceURL})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("get remote protected resource: %w", err)
	default:
		visited := existing.MetadataFetchedAt.Time
		if existing.MetadataLastErrorAt.Time.After(visited) {
			visited = existing.MetadataLastErrorAt.Time
		}
		if now.Sub(visited) < protectedResourceStaleAfter {
			return nil
		}
	}

	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, resourceURL)
	if err != nil {
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok {
			return recordProtectedResourceFetchError(ctx, db, projectID, organizationID, resourceURL, typed)
		}
		return nil
	}
	return recordProtectedResource(ctx, db, projectID, organizationID, resourceURL, doc)
}
