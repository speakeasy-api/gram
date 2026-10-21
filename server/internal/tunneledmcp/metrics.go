package tunneledmcp

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/tunnelmetrics"
	"github.com/speakeasy-api/gram/tunnel/metrics"
)

func (s *Service) GetServerMetrics(ctx context.Context, payload *gen.GetServerMetricsPayload) (*types.TunnelMetrics, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id")
	}
	if _, err = repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{ID: id, ProjectID: *authCtx.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server").LogError(ctx, s.logger)
	}
	duration, step := 24*time.Hour, 15*time.Minute
	switch payload.Window {
	case "hour":
		duration, step = time.Hour, time.Minute
	case "week":
		duration, step = 7*24*time.Hour, time.Hour
	}
	now := time.Now().UTC()
	view := &types.TunnelMetrics{State: "unavailable", LastSampleAt: nil, ObservedAt: now.Format(time.RFC3339), BucketSeconds: int(step.Seconds()), Points: []*types.TunnelMetricPoint{}, Clients: []*types.TunnelClientCount{}, ActiveServers: 0}
	if s.Metrics == nil {
		return view, nil
	}
	since := now.Add(-duration).Truncate(step)
	rows, err := s.Metrics.Read(ctx, *authCtx.ProjectID, id, since, step)
	if errors.Is(err, tunnelmetrics.ErrRangeTooLarge) {
		view.State = "too_large"
		return view, nil
	}
	if err != nil {
		s.logger.WarnContext(ctx, "load tunnel metrics", attr.SlogError(err))
		return view, nil
	}
	view.State = "available"
	fillMetrics(view, rows, since, now, step)
	return view, nil
}

type metricBucket struct {
	point *types.TunnelMetricPoint
	bins  [12]uint64
}

// fillMetrics shapes ClickHouse's per-interval aggregates into the response.
// An interval with any observation starts its request counters at zero;
// intervals with none stay null, so the dashboard can show gaps.
func fillMetrics(view *types.TunnelMetrics, rows tunnelmetrics.Aggregates, since, now time.Time, step time.Duration) {
	buckets := map[int64]*metricBucket{}
	for t := since; t.Before(now); t = t.Add(step) {
		point := &types.TunnelMetricPoint{Time: t.Format(time.RFC3339), CoverageSamples: 0, RequestCoverageSamples: 0, CollectionPartial: false, ToolCalls: nil, ToolsList: nil, OtherRequests: nil, Successes: nil, Errors: nil, Canceled: nil, Incomplete: nil, Rejected: nil, P50Ms: nil, P95Ms: nil, Connections: nil, ConsumerSessions: nil, ActiveRequests: nil, ConnectionsOpened: nil}
		view.Points = append(view.Points, point)
		buckets[t.Unix()] = &metricBucket{bins: [12]uint64{}, point: point}
	}
	clients := map[string]int64{}
	servers := map[string]struct{}{}
	var latest time.Time
	add := func(dst **int64, n uint64) {
		if *dst == nil {
			*dst = new(int64(0))
		}
		**dst += int64(min(n, math.MaxInt64))
	}
	observed := func(p *types.TunnelMetricPoint) {
		add(&p.ToolCalls, 0)
		add(&p.ToolsList, 0)
		add(&p.OtherRequests, 0)
		add(&p.Successes, 0)
		add(&p.Errors, 0)
		add(&p.Canceled, 0)
		add(&p.Incomplete, 0)
		add(&p.Rejected, 0)
	}
	for _, r := range rows.Requests {
		b := buckets[r.Bucket.Truncate(step).Unix()]
		if b == nil {
			continue
		}
		if r.Latest.After(latest) {
			latest = r.Latest
		}
		observed(b.point)
		switch r.Kind {
		case "requests":
			switch r.Method {
			case "tools/call":
				add(&b.point.ToolCalls, r.Attempts)
			case "tools/list":
				add(&b.point.ToolsList, r.Attempts)
			default:
				add(&b.point.OtherRequests, r.Attempts)
			}
			add(&b.point.Successes, r.Successes)
			add(&b.point.Errors, r.Errors)
			add(&b.point.Canceled, r.Canceled)
			add(&b.point.Incomplete, r.Incomplete)
			add(&b.point.Rejected, r.Rejected)
			for i, n := range r.Bins {
				if i < len(b.bins) {
					b.bins[i] += n
				}
			}
			clients[r.Client] += int64(min(r.Attempts, math.MaxInt64))
			for _, server := range r.Servers {
				servers[server] = struct{}{}
			}
		case "coverage":
			b.point.RequestCoverageSamples += int(min(r.Snapshots, math.MaxInt32))
			b.point.CollectionPartial = b.point.CollectionPartial || r.Lost
		}
	}
	for _, r := range rows.Connections {
		b := buckets[r.Bucket.Truncate(step).Unix()]
		if b == nil || r.Slots == 0 {
			continue
		}
		if r.Latest.After(latest) {
			latest = r.Latest
		}
		b.point.CoverageSamples = int(min(r.Slots, math.MaxInt32))
		b.point.CollectionPartial = b.point.CollectionPartial || r.Lost
		b.point.Connections = new(r.Connections)
		b.point.ConsumerSessions = new(r.Consumers)
		b.point.ActiveRequests = new(r.Substreams)
		add(&b.point.ConnectionsOpened, r.Opened)
	}
	for _, b := range buckets {
		b.point.P50Ms = percentile(b.bins, 50)
		b.point.P95Ms = percentile(b.bins, 95)
	}
	for family, count := range clients {
		if count > 0 {
			view.Clients = append(view.Clients, &types.TunnelClientCount{Family: family, Requests: count})
		}
	}
	sort.Slice(view.Clients, func(i, j int) bool {
		if view.Clients[i].Requests == view.Clients[j].Requests {
			return view.Clients[i].Family < view.Clients[j].Family
		}
		return view.Clients[i].Requests > view.Clients[j].Requests
	})
	view.ActiveServers = len(servers)
	if !latest.IsZero() {
		view.LastSampleAt = new(latest.Format(time.RFC3339))
	}
}
func percentile(bins [12]uint64, percent uint64) *int64 {
	var total uint64
	for _, n := range bins {
		total += n
	}
	if total == 0 {
		return nil
	}
	rank := (total*percent + 99) / 100
	var seen uint64
	for i, n := range bins {
		seen += n
		if seen >= rank {
			if i == len(metrics.LatencyBounds) {
				return new(int64(-1))
			}
			return new(metrics.LatencyBounds[i])
		}
	}
	return nil
}
