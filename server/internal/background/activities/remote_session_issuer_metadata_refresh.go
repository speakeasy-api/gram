package activities

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/tunnel/route"
)

const (
	// remoteSessionIssuerMetadataRefreshSpacing is the gap between two fetches against one host.
	remoteSessionIssuerMetadataRefreshSpacing = 2 * time.Second
	// remoteSessionIssuerMetadataRefreshJitter is the half-width of the random spread around that gap.
	remoteSessionIssuerMetadataRefreshJitter = time.Second
	// remoteSessionIssuerMetadataRefreshMaxPages bounds how many due pages one listing walks past unfetchable rows.
	remoteSessionIssuerMetadataRefreshMaxPages = 5
)

// issuerMetadataRefresher is the slice of remotesessions.IssuerMetadataRefresher the activities drive.
type issuerMetadataRefresher interface {
	ListDue(ctx context.Context, now time.Time, after *remotesessions.IssuerMetadataRefreshCursor, limit int32) ([]remotesessions.IssuerMetadataRefreshCandidate, *remotesessions.IssuerMetadataRefreshCursor, error)
	ListReprojectable(ctx context.Context, now time.Time, limit int32) ([]remotesessions.IssuerMetadataRefreshCandidate, error)
	RecordSkipped(ctx context.Context, candidate remotesessions.IssuerMetadataRefreshCandidate, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome)
	Reproject(ctx context.Context, candidate remotesessions.IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error)
	Refresh(ctx context.Context, candidate remotesessions.IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error)
}

// RemoteSessionIssuerMetadataRefreshCandidate is one issuer keyed by the identity every write re-asserts; a zero ProjectID or empty OrganizationID means none.
type RemoteSessionIssuerMetadataRefreshCandidate struct {
	ID             uuid.UUID
	IssuerURL      string
	Host           string
	ProjectID      uuid.UUID
	OrganizationID string
}

type ListRemoteSessionIssuerMetadataRefreshCandidatesInput struct {
	Limit int32
}

type ListRemoteSessionIssuerMetadataReprojectCandidatesInput struct {
	Limit int32
}

type ReprojectRemoteSessionIssuerMetadataInput struct {
	Issuers []RemoteSessionIssuerMetadataRefreshCandidate
}

type RefreshRemoteSessionIssuerMetadataHostInput struct {
	Host    string
	Issuers []RemoteSessionIssuerMetadataRefreshCandidate
}

// RemoteSessionIssuerMetadataRefreshResult counts the issuers an activity visited by outcome.
type RemoteSessionIssuerMetadataRefreshResult struct {
	Outcomes map[string]int
}

type RemoteSessionIssuerMetadataRefresh struct {
	logger    *slog.Logger
	refresher issuerMetadataRefresher
	routes    route.Store
}

// NewRemoteSessionIssuerMetadataRefresh wires the scheduled refresh. routes may be nil, in which case tunnel liveness is unknown and no tunneled issuer is skipped.
func NewRemoteSessionIssuerMetadataRefresh(logger *slog.Logger, refresher issuerMetadataRefresher, routes route.Store) *RemoteSessionIssuerMetadataRefresh {
	return &RemoteSessionIssuerMetadataRefresh{
		logger:    logger.With(attr.SlogComponent("remote-session-issuer-metadata-refresh")),
		refresher: refresher,
		routes:    routes,
	}
}

func (r *RemoteSessionIssuerMetadataRefresh) ListReprojectCandidates(ctx context.Context, input ListRemoteSessionIssuerMetadataReprojectCandidatesInput) ([]RemoteSessionIssuerMetadataRefreshCandidate, error) {
	candidates, err := r.refresher.ListReprojectable(ctx, time.Now(), input.Limit)
	if err != nil {
		return nil, fmt.Errorf("list issuer metadata reproject candidates: %w", err)
	}
	return toRefreshCandidates(candidates), nil
}

// ListRefreshCandidates pages the due list until Limit fetchable issuers are in hand, the table is exhausted, or the page cap is hit, so unfetchable rows at the front never hide the rest.
func (r *RemoteSessionIssuerMetadataRefresh) ListRefreshCandidates(ctx context.Context, input ListRemoteSessionIssuerMetadataRefreshCandidatesInput) ([]RemoteSessionIssuerMetadataRefreshCandidate, error) {
	now := time.Now()
	limit := int(input.Limit)
	kept := make([]remotesessions.IssuerMetadataRefreshCandidate, 0, limit)
	var after *remotesessions.IssuerMetadataRefreshCursor
	for range remoteSessionIssuerMetadataRefreshMaxPages {
		candidates, next, err := r.refresher.ListDue(ctx, now, after, input.Limit)
		if err != nil {
			return nil, fmt.Errorf("list issuer metadata refresh candidates: %w", err)
		}
		kept = append(kept, r.filterFetchable(ctx, candidates)...)
		if next == nil || len(kept) >= limit {
			break
		}
		after = next
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return toRefreshCandidates(kept), nil
}

// filterFetchable drops issuers with no routable host and issuers riding a tunnel with no live route; a route store error leaves liveness unknown and keeps the issuer.
func (r *RemoteSessionIssuerMetadataRefresh) filterFetchable(ctx context.Context, candidates []remotesessions.IssuerMetadataRefreshCandidate) []remotesessions.IssuerMetadataRefreshCandidate {
	kept := make([]remotesessions.IssuerMetadataRefreshCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Host == "" {
			r.logger.ErrorContext(ctx, "issuer url has no host for metadata refresh",
				attr.SlogRemoteSessionIssuerID(candidate.ID.String()),
				attr.SlogOAuthIssuer(candidate.IssuerURL),
			)
			r.refresher.RecordSkipped(ctx, candidate, remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError)
			continue
		}
		if r.routes == nil || !candidate.TunneledMcpServerID.Valid {
			kept = append(kept, candidate)
			continue
		}
		live, err := r.routes.Candidates(ctx, candidate.TunneledMcpServerID.UUID.String())
		if err != nil {
			r.logger.WarnContext(ctx, "load tunnel routes for issuer metadata refresh",
				attr.SlogRemoteSessionIssuerID(candidate.ID.String()),
				attr.SlogTunneledMCPServerID(candidate.TunneledMcpServerID.UUID.String()),
				attr.SlogError(err),
			)
			kept = append(kept, candidate)
			continue
		}
		if len(live) == 0 {
			r.refresher.RecordSkipped(ctx, candidate, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedTunnelDown)
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

func toRefreshCandidates(candidates []remotesessions.IssuerMetadataRefreshCandidate) []RemoteSessionIssuerMetadataRefreshCandidate {
	out := make([]RemoteSessionIssuerMetadataRefreshCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, RemoteSessionIssuerMetadataRefreshCandidate{
			ID:             candidate.ID,
			IssuerURL:      candidate.IssuerURL,
			Host:           candidate.Host,
			ProjectID:      candidate.ProjectID.UUID,
			OrganizationID: candidate.OrganizationID.String,
		})
	}
	return out
}

func toRefresherCandidate(candidate RemoteSessionIssuerMetadataRefreshCandidate) remotesessions.IssuerMetadataRefreshCandidate {
	return remotesessions.IssuerMetadataRefreshCandidate{
		ID:                  candidate.ID,
		IssuerURL:           candidate.IssuerURL,
		Host:                candidate.Host,
		ProjectID:           conv.NilableToNullUUID(candidate.ProjectID),
		OrganizationID:      conv.ToPGTextEmpty(candidate.OrganizationID),
		TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
}

// Reproject rewrites each issuer's capability columns from its stored document. Per-issuer failures become outcomes; only a canceled context aborts, checked after every issuer so a batch cut short is never reported as complete.
func (r *RemoteSessionIssuerMetadataRefresh) Reproject(ctx context.Context, input ReprojectRemoteSessionIssuerMetadataInput) (RemoteSessionIssuerMetadataRefreshResult, error) {
	result := RemoteSessionIssuerMetadataRefreshResult{Outcomes: map[string]int{}}
	for i, candidate := range input.Issuers {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("reproject issuer metadata: %w", err)
		}
		outcome, err := r.refresher.Reproject(ctx, toRefresherCandidate(candidate))
		if err != nil {
			r.logger.ErrorContext(ctx, "reproject issuer metadata", attr.SlogRemoteSessionIssuerID(candidate.ID.String()), attr.SlogError(err))
		}
		result.Outcomes[string(outcome)]++
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("reproject issuer metadata: %w", err)
		}
		heartbeat(ctx, i+1)
	}
	return result, nil
}

// RefreshHost fetches each issuer on one host in turn, spacing the fetches so the sweep never bursts against a single identity provider.
func (r *RemoteSessionIssuerMetadataRefresh) RefreshHost(ctx context.Context, input RefreshRemoteSessionIssuerMetadataHostInput) (RemoteSessionIssuerMetadataRefreshResult, error) {
	result := RemoteSessionIssuerMetadataRefreshResult{Outcomes: map[string]int{}}
	for i, candidate := range input.Issuers {
		if i > 0 {
			if err := waitFor(ctx, jitteredHostSpacing()); err != nil {
				return result, fmt.Errorf("refresh issuer metadata: %w", err)
			}
		}
		outcome, err := r.refresher.Refresh(ctx, toRefresherCandidate(candidate))
		if err != nil {
			r.logger.ErrorContext(ctx, "refresh issuer metadata",
				attr.SlogRemoteSessionIssuerID(candidate.ID.String()),
				attr.SlogServerAddress(input.Host),
				attr.SlogError(err),
			)
		}
		result.Outcomes[string(outcome)]++
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("refresh issuer metadata: %w", err)
		}
		heartbeat(ctx, i+1)
	}
	return result, nil
}

// heartbeat reports per-issuer progress when running under Temporal.
func heartbeat(ctx context.Context, visited int) {
	if activity.IsActivity(ctx) {
		activity.RecordHeartbeat(ctx, visited)
	}
}

func jitteredHostSpacing() time.Duration {
	spread := 2 * remoteSessionIssuerMetadataRefreshJitter
	return remoteSessionIssuerMetadataRefreshSpacing - remoteSessionIssuerMetadataRefreshJitter + rand.N(spread) //nolint:gosec // spacing jitter, not a secret
}

func waitFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for host spacing: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
