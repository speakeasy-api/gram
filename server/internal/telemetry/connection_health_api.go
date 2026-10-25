package telemetry

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

// GetGatewayConnectionHealth returns one gateway's connection attempt
// outcomes per recorded stage.
func (s *Service) GetGatewayConnectionHealth(ctx context.Context, payload *telem_gen.GetGatewayConnectionHealthPayload) (*telem_gen.GetGatewayConnectionHealthResult, error) {
	target, window, err := s.connectionHealthRequest(ctx, payload.MetaMcpServerID, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	var scope ConnectionHealthScope
	if payload.Key != nil {
		scope.Key = &ConnectionHealthKey{Stage: payload.Key.Stage, Subject: payload.Key.Subject, ProviderID: payload.Key.ProviderID, ClientKey: payload.Key.Client}
	}
	scope.Caller = callerScope(payload.Caller)
	summary, err := NewConnectionHealth(s.db, s.chConn, s.logsEnabled, s.authz).Summary(ctx, target, window, scope)
	if err != nil {
		return nil, s.connectionHealthError(ctx, err)
	}
	stages := make([]*telem_gen.GatewayConnectionStage, 0, len(summary.Stages))
	for _, stage := range summary.Stages {
		buckets := make([]*telem_gen.GatewayConnectionBucket, 0, len(stage.Buckets))
		for _, bucket := range stage.Buckets {
			buckets = append(buckets, &telem_gen.GatewayConnectionBucket{BucketTimeUnixNano: strconv.FormatInt(bucket.Start.UnixNano(), 10), Outcomes: outcomeCountViews(bucket.Outcomes)})
		}
		view := &telem_gen.GatewayConnectionStage{
			Stage:            stage.Stage,
			Attempts:         uint64ToInt64(stage.Attempts),
			Outcomes:         outcomeCountViews(stage.Outcomes),
			Retries:          uint64ToInt64(stage.Retries),
			Recovered:        uint64ToInt64(stage.Recovered),
			LastSeenAt:       timeView(stage.LastSeen),
			LastCodeIssuedAt: nil,
			Buckets:          buckets,
		}
		// One caller's sign-in codes have a meaningful latest time; the whole
		// gateway's do not name anyone.
		if scope.Key != nil || scope.Caller != nil {
			view.LastCodeIssuedAt = timeView(stage.LastCodeIssued)
		}
		stages = append(stages, view)
	}
	notRecorded := make([]*telem_gen.GatewayConnectionCoverage, 0, len(summary.NotRecorded))
	for _, coverage := range summary.NotRecorded {
		notRecorded = append(notRecorded, &telem_gen.GatewayConnectionCoverage{Stage: coverage.Stage, Note: coverage.Note})
	}
	return &telem_gen.GetGatewayConnectionHealthResult{
		From:            window.Start.Format(time.RFC3339Nano),
		To:              window.End.Format(time.RFC3339Nano),
		IntervalSeconds: summary.IntervalSeconds,
		Stages:          stages,
		NotRecorded:     notRecorded,
		Scope:           summary.Scope,
		Caller:          payload.Caller,
	}, nil
}

// ListGatewayConnectionHotspots ranks one gateway's callers, providers, and
// clients within a recorded stage.
func (s *Service) ListGatewayConnectionHotspots(ctx context.Context, payload *telem_gen.ListGatewayConnectionHotspotsPayload) (*telem_gen.ListGatewayConnectionHotspotsResult, error) {
	target, window, err := s.connectionHealthRequest(ctx, payload.MetaMcpServerID, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	hotspots, err := NewConnectionHealth(s.db, s.chConn, s.logsEnabled, s.authz).Hotspots(ctx, target, window, ConnectionHotspotQuery{
		Stage:       payload.Stage,
		ByRate:      payload.RankBy == "rate",
		MinAttempts: int(payload.MinAttempts),
		Limit:       int(payload.Limit),
		Caller:      callerScope(payload.Caller),
	})
	if err != nil {
		return nil, s.connectionHealthError(ctx, err)
	}
	views := make([]*telem_gen.GatewayConnectionHotspot, 0, len(hotspots.Hotspots))
	for _, hotspot := range hotspots.Hotspots {
		reasons := make([]*telem_gen.GatewayConnectionReasonCount, 0, len(hotspot.Reasons))
		for _, reason := range hotspot.Reasons {
			reasons = append(reasons, &telem_gen.GatewayConnectionReasonCount{Reason: reason.Reason, Count: uint64ToInt64(reason.Count)})
		}
		views = append(views, &telem_gen.GatewayConnectionHotspot{
			Subject:    labelView(hotspot.Subject),
			Provider:   labelView(hotspot.Provider),
			Client:     labelView(hotspot.Client),
			Attempts:   uint64ToInt64(hotspot.Attempts),
			Failed:     uint64ToInt64(hotspot.Failed),
			Blocked:    uint64ToInt64(hotspot.Blocked),
			Outcomes:   outcomeCountViews(hotspot.Outcomes),
			Reasons:    reasons,
			LastSeenAt: timeView(hotspot.LastSeen),
		})
	}
	rankBy := "count"
	if hotspots.ByRate {
		rankBy = "rate"
	}
	return &telem_gen.ListGatewayConnectionHotspotsResult{
		From:        window.Start.Format(time.RFC3339Nano),
		To:          window.End.Format(time.RFC3339Nano),
		Stage:       hotspots.Stage,
		RankBy:      rankBy,
		MinAttempts: int64(hotspots.MinAttempts),
		Hotspots:    views,
		Truncated:   hotspots.Truncated,
		Caller:      payload.Caller,
	}, nil
}

func callerScope(caller *telem_gen.GatewayConnectionCaller) *ConnectionCaller {
	if caller == nil {
		return nil
	}
	return &ConnectionCaller{Subject: caller.Subject, ClientKey: caller.Client}
}

// connectionHealthRequest reads the caller's project, the gateway, and the
// window. The reader authorizes the read.
func (s *Service) connectionHealthRequest(ctx context.Context, gatewayID string, from, to *string) (ConnectionHealthTarget, ConnectionHealthWindow, error) {
	var target ConnectionHealthTarget
	var window ConnectionHealthWindow
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return target, window, oops.C(oops.CodeUnauthorized)
	}
	gateway, err := uuid.Parse(gatewayID)
	if err != nil {
		return target, window, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id")
	}
	window, err = ParseConnectionHealthWindow(conv.PtrValOr(from, ""), conv.PtrValOr(to, ""), time.Now())
	if err != nil {
		return target, window, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	return ConnectionHealthTarget{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, GatewayID: gateway}, window, nil
}

func (s *Service) connectionHealthError(ctx context.Context, err error) error {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		return shareable
	}
	switch {
	case errors.Is(err, telemetryerrs.ErrLogsDisabled):
		return oops.E(oops.CodeNotFound, err, "logs are not enabled for this organization")
	case errors.Is(err, ErrConnectionHealthGatewayNotFound):
		return oops.E(oops.CodeNotFound, err, "gateway not found")
	case errors.Is(err, ErrConnectionHealthKeyNotFound):
		return oops.E(oops.CodeNotFound, err, "no visible hotspot with this key")
	case errors.Is(err, ErrConnectionHealthCallerNotFound):
		return oops.E(oops.CodeNotFound, err, "no visible caller with this client")
	case errors.Is(err, ErrConnectionHealthInvalid):
		return oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	return oops.E(oops.CodeUnexpected, err, "error reading gateway connection health").LogError(ctx, s.logger)
}

func outcomeCountViews(counts []ConnectionOutcomeCount) []*telem_gen.GatewayConnectionOutcomeCount {
	views := make([]*telem_gen.GatewayConnectionOutcomeCount, 0, len(counts))
	for _, count := range counts {
		views = append(views, &telem_gen.GatewayConnectionOutcomeCount{Outcome: count.Outcome, Count: uint64ToInt64(count.Count)})
	}
	return views
}

func labelView(label ConnectionLabel) *telem_gen.GatewayConnectionLabel {
	view := &telem_gen.GatewayConnectionLabel{Kind: label.Kind, Label: label.Label, ID: nil, Key: nil}
	if label.ID != "" {
		view.ID = &label.ID
	}
	if label.Selectable {
		view.Key = &label.Key
	}
	return view
}

func timeView(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339Nano)
	return &formatted
}
