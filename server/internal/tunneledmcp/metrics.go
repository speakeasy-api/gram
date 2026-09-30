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
	rows, err := s.Metrics.Read(ctx, *authCtx.ProjectID, id, since)
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
	point   *types.TunnelMetricPoint
	bins    [12]uint64
	samples map[int64][3]uint64
}

func fillMetrics(view *types.TunnelMetrics, rows []tunnelmetrics.Row, since, now time.Time, step time.Duration) {
	buckets := map[int64]*metricBucket{}
	for t := since; t.Before(now); t = t.Add(step) {
		point := &types.TunnelMetricPoint{Time: t.Format(time.RFC3339), CoverageSamples: 0, RequestCoverageSamples: 0, CollectionPartial: false, ToolCalls: nil, ToolsList: nil, OtherRequests: nil, Successes: nil, Errors: nil, Canceled: nil, Incomplete: nil, P50Ms: nil, P95Ms: nil, Connections: nil, ConsumerSessions: nil, ActiveRequests: nil, ConnectionsOpened: nil}
		view.Points = append(view.Points, point)
		buckets[t.Unix()] = &metricBucket{bins: [12]uint64{}, point: point, samples: map[int64][3]uint64{}}
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
	for _, r := range rows {
		b := buckets[r.Bucket.Truncate(step).Unix()]
		if b == nil {
			continue
		}
		if r.Bucket.After(latest) {
			latest = r.Bucket
		}
		switch r.Kind {
		case "requests":
			// An observed bucket starts at zero; missing buckets remain null.
			add(&b.point.ToolCalls, 0)
			add(&b.point.ToolsList, 0)
			add(&b.point.OtherRequests, 0)
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
			for i, n := range r.Bins {
				if i < len(b.bins) {
					b.bins[i] += n
				}
			}
			clients[r.Client] += int64(min(r.Attempts, math.MaxInt64))
			if r.Attempts > 0 && r.Server != uuid.Nil.String() {
				servers[r.Server] = struct{}{}
			}
		case "coverage":
			b.point.RequestCoverageSamples++
			b.point.CollectionPartial = b.point.CollectionPartial || r.Incomplete > 0
			add(&b.point.ToolCalls, 0)
			add(&b.point.ToolsList, 0)
			add(&b.point.OtherRequests, 0)
			add(&b.point.Successes, 0)
			add(&b.point.Errors, 0)
			add(&b.point.Canceled, 0)
			add(&b.point.Incomplete, 0)
		case "connections":
			b.point.CollectionPartial = b.point.CollectionPartial || r.Incomplete > 0
			sample := b.samples[r.Bucket.Unix()]
			sample[0] += uint64(r.Connections)
			sample[1] += uint64(r.Consumers)
			sample[2] += uint64(r.Substreams)
			b.samples[r.Bucket.Unix()] = sample
			add(&b.point.ConnectionsOpened, r.Opened)
		}
	}
	for _, b := range buckets {
		b.point.CoverageSamples = len(b.samples)
		if len(b.samples) > 0 {
			var connections, consumers, substreams float64
			for _, v := range b.samples {
				connections += float64(v[0])
				consumers += float64(v[1])
				substreams += float64(v[2])
			}
			n := float64(len(b.samples))
			b.point.Connections = new(connections / n)
			b.point.ConsumerSessions = new(consumers / n)
			b.point.ActiveRequests = new(substreams / n)
		}
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
