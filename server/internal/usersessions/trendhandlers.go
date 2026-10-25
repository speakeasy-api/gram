package usersessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/gatewayscope"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// maxTrendWindow bounds a trend read: at hourly buckets it is at most 745
// buckets, when the window's ends fall off the hour.
const maxTrendWindow = 31 * 24 * time.Hour

var trendStrides = map[string]time.Duration{"hour": time.Hour, "day": 24 * time.Hour}

// GetTrend counts one gateway's credentials over a window. It reads the same
// project as the session list, and the gateway itself as connection health
// does: a gateway the caller may not read is not found, alike with a missing
// one.
func (s *Service) GetTrend(ctx context.Context, payload *gen.GetTrendPayload) (*gen.UserSessionTrend, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	from, err := time.Parse(time.RFC3339Nano, payload.From)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid from")
	}
	to, err := time.Parse(time.RFC3339Nano, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid to")
	}
	stride, ok := trendStrides[payload.Bucket]
	if !ok {
		return nil, oops.E(oops.CodeBadRequest, nil, "bucket must be hour or day")
	}
	if !to.After(from) || to.Sub(from) > maxTrendWindow {
		return nil, oops.E(oops.CodeBadRequest, nil, "to must be after from and at most 31 days later")
	}

	gateway, err := gatewayscope.Resolve(ctx, s.db, *authCtx.ProjectID, payload.MetaMcpServerID)
	if err != nil {
		return nil, fmt.Errorf("scope trend to gateway: %w", err)
	}
	// Grants name the gateway canonically, however the caller spelled it.
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, gateway.ID.String(), authCtx.ProjectID.String())); err != nil {
		if denied, ok := errors.AsType[*oops.ShareableError](err); ok && denied.Code == oops.CodeForbidden {
			return nil, oops.E(oops.CodeNotFound, err, "gateway not found")
		}
		return nil, err
	}

	fromTime := pgtype.Timestamptz{Time: from, InfinityModifier: 0, Valid: true}
	toTime := pgtype.Timestamptz{Time: to, InfinityModifier: 0, Valid: true}
	q := repo.New(s.db)
	events, err := q.GetGatewaySessionTrendEvents(ctx, repo.GetGatewaySessionTrendEventsParams{
		ResourceIdentity: gateway.ResourceIdentity.String,
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		ToTime:           toTime,
		FromTime:         fromTime,
		Stride:           pgtype.Interval{Microseconds: stride.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read gateway session trend").LogError(ctx, s.logger)
	}
	excluded, err := q.CountGatewaySessionTrendExcluded(ctx, repo.CountGatewaySessionTrendExcludedParams{
		ResourceIdentity: gateway.ResourceIdentity.String,
		IssuerID:         gateway.IssuerID,
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		ToTime:           toTime,
		FromTime:         fromTime,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count gateway sessions left out of the trend").LogError(ctx, s.logger)
	}

	trend := buildTrend(from, to, stride, events)
	trend.Bucket = payload.Bucket
	trend.ExcludedLegacyRows = excluded.LegacyRows
	if gateway.IssuerID.Valid && gateway.IssuerID.UUID != uuid.Nil {
		trend.UnattributedRows = &excluded.UnattributedRows
	}
	return trend, nil
}

// buildTrend lays the events over every bucket in [from, to): bucket edges are
// stride multiples from the UTC epoch, the first and last clipped to the
// window. Created and revoked are events during a bucket; active is the stock
// immediately before its end, carried through buckets with no events.
func buildTrend(from, to time.Time, stride time.Duration, events []repo.GetGatewaySessionTrendEventsRow) *gen.UserSessionTrend {
	type delta struct{ starts, created, ends, revoked int64 }
	deltas := map[int64]*delta{}
	var active int64
	at := func(bucket pgtype.Timestamptz) *delta {
		key := bucket.Time.Unix()
		if deltas[key] == nil {
			deltas[key] = &delta{starts: 0, created: 0, ends: 0, revoked: 0}
		}
		return deltas[key]
	}
	for _, event := range events {
		switch event.Kind {
		case "baseline":
			active = event.Total
		case "start":
			d := at(event.Bucket)
			d.starts, d.created = event.Total, event.Flagged
		case "end":
			d := at(event.Bucket)
			d.ends, d.revoked = event.Total, event.Flagged
		}
	}

	trend := &gen.UserSessionTrend{
		From: from.UTC().Format(time.RFC3339Nano), To: to.UTC().Format(time.RFC3339Nano), Bucket: "",
		Buckets: []*gen.UserSessionTrendBucket{}, ActiveAtStart: active, Created: 0, Revoked: 0, ActiveAtEnd: 0, ExcludedLegacyRows: 0, UnattributedRows: nil,
	}
	for edge := from.UTC().Truncate(stride); edge.Before(to); edge = edge.Add(stride) {
		start, end := edge, edge.Add(stride)
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		var d delta
		if found := deltas[edge.Unix()]; found != nil {
			d = *found
		}
		active += d.starts - d.ends
		trend.Created += d.created
		trend.Revoked += d.revoked
		trend.Buckets = append(trend.Buckets, &gen.UserSessionTrendBucket{
			Start: start.UTC().Format(time.RFC3339Nano), End: end.UTC().Format(time.RFC3339Nano),
			Created: d.created, Revoked: d.revoked, Active: active,
		})
	}
	trend.ActiveAtEnd = active
	return trend
}
