package analytics

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/conv"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

// SignalFindings keeps transport authorization separate from shared measurement semantics.
func (s *Service) SignalFindings(ctx context.Context, payload *gen.SignalFindingsPayload) (*gen.AnalyticsSignalFindings, error) {
	auth, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireSignals(ctx, auth.ActiveOrganizationID); err != nil {
		return nil, err
	}
	from, to, err := ParseWindow(payload.From, payload.To)
	if err != nil {
		return nil, s.failure(ctx, err, "could not read findings window")
	}
	query, err := signalRequestFromPayload(payload.SignalQuery)
	if err != nil {
		return nil, s.failure(ctx, err, "could not read signal predicate")
	}
	if query == nil {
		return nil, s.failure(ctx, newError(ErrUnsatisfiable, signalquery.Dataset, "signal_query", "", "a measured predicate is required"), "could not read signal predicate")
	}
	request := SignalFindingsRequest{Query: *query, Peer: nil, ComparePrevious: payload.ComparePrevious, Evidence: payload.Evidence}
	if p := payload.Peer; p != nil {
		request.Peer = &signalquery.Selection{SensorID: p.SensorID, SignalID: deref(p.SignalID), MinimumLevel: p.MinimumLevel, Threshold: p.Threshold}
	}
	result, err := s.engine.SignalFindings(ctx, Tenant{OrganizationID: auth.ActiveOrganizationID, ProjectID: auth.ProjectID.String()}, Window{FromUnixNano: from, ToUnixNano: to}, request, s.conversationAccess)
	if err != nil {
		return nil, s.failure(ctx, err, "could not analyze signal patterns")
	}
	view := &gen.AnalyticsSignalFindings{Measurement: signalResultView(result.Measurement), Concentration: result.Concentration, Peer: nil, PeerSummary: result.PeerSummary, Cooccurrence: signalRowsView(result.Cooccurrence), Comparison: nil, Evidence: signalRowsView(result.Evidence), EvidenceScope: result.EvidenceScope}
	if p := result.Peer; p != nil {
		view.Peer = &gen.AnalyticsSignalSelection{SensorID: p.SensorID, SignalID: conv.PtrEmpty(p.SignalID), MinimumLevel: p.MinimumLevel, Threshold: p.Threshold}
	}
	if c := result.Comparison; c != nil {
		view.Comparison = &gen.AnalyticsSignalComparison{From: c.From, To: c.To, Summary: c.Summary, Comparable: c.Comparable, Reasons: c.Reasons, DeltaShare: c.DeltaShare}
	}
	return view, nil
}

func signalRowsView(rows []Row) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = row
	}
	return out
}
