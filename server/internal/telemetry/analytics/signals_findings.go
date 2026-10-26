package analytics

import (
	"context"
	"fmt"
	"reflect"
	"time"

	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

const (
	// comparisonAnswers avoids interpreting percentages from a handful of answers.
	comparisonAnswers = 30
	// comparisonSessions prevents one long conversation from masquerading as breadth.
	comparisonSessions = 5
	// comparisonRecentWindow marks live trailing windows, not a completeness watermark.
	comparisonRecentWindow = 5 * time.Minute
)

// ConversationAccess applies the underlying chat's owner, grant and support rules.
type ConversationAccess interface {
	ReadableConversations(ctx context.Context, conversationIDs []string) ([]string, error)
}

// SignalFindingsRequest asks about the selected cohort across conversations.
type SignalFindingsRequest struct {
	// Query uses the same explicit predicate and source scope as a chart.
	Query signalquery.Request `json:"signal_query"`
	// Peer optionally supplies a second measured predicate for co-occurrence.
	Peer *signalquery.Selection `json:"peer,omitempty"`
	// ComparePrevious requests an adjacent equal-duration observation window.
	ComparePrevious bool `json:"compare_previous"`
	// Evidence requests bounded examples after exact conversation authorization.
	Evidence bool `json:"evidence"`
}

// SignalComparison preserves both values and the reasons to qualify a change.
type SignalComparison struct {
	// From is the inclusive previous-window boundary.
	From string `json:"from"`
	// To is the exclusive previous-window boundary.
	To string `json:"to"`
	// Summary contains the previous window's measured counts and definition mix.
	Summary Row `json:"summary"`
	// Comparable requires sufficient, stable measured observations and an older window.
	Comparable bool `json:"comparable"`
	// Reasons identifies failed comparability gates, independently of the numeric delta.
	Reasons []string `json:"reasons"`
	// DeltaShare is a descriptive difference, not a significance test or causal estimate.
	DeltaShare *float64 `json:"delta_share"`
}

// SignalFindings is an exportable, live measurement with permission-checked examples.
type SignalFindings struct {
	// Measurement preserves the primary selection, window, population and outcomes.
	Measurement *SignalResult `json:"measurement"`
	// Concentration divides top-conversation hits by all hits with a conversation link.
	Concentration Row `json:"concentration"`
	// Peer is the second predicate, never implicitly pooled with the primary sensor.
	Peer *signalquery.Selection `json:"peer,omitempty"`
	// PeerSummary reports the second predicate's independent outcomes and definitions.
	PeerSummary Row `json:"peer_summary,omitempty"`
	// Cooccurrence separates same-message and same-session joint-answer populations.
	Cooccurrence []Row `json:"cooccurrence"`
	// Comparison is absent unless explicitly requested.
	Comparison *SignalComparison `json:"comparison,omitempty"`
	// Evidence contains only permitted recent examples, without transcript content.
	Evidence []Row `json:"evidence"`
	// EvidenceScope explains the bounded candidate selection and its non-exhaustiveness.
	EvidenceScope string `json:"evidence_scope"`
}

// SignalFindings executes bounded canonical reads; content is never selected.
func (e *Engine) SignalFindings(ctx context.Context, tenant Tenant, window Window, request SignalFindingsRequest, access ConversationAccess) (*SignalFindings, error) {
	if len(request.Query.GroupBy) != 0 {
		return nil, newError(ErrUnsatisfiable, signalquery.Dataset, "group_by", "", "session findings measure the complete filtered cohort; omit group_by")
	}
	if request.Evidence && access == nil {
		return nil, newError(ErrUnsatisfiable, signalquery.Dataset, "evidence", "", "conversation access checks are unavailable; request aggregate findings without evidence")
	}
	plan, err := e.prepareSignalPlan(ctx, tenant, window, "auto", request.Query)
	if err != nil {
		return nil, err
	}
	var peer, previous *signalquery.Plan
	if request.Peer != nil {
		peerRequest := plan.Request
		peerRequest.Selection = *request.Peer
		peer, err = signalquery.Compile(plan.Context, peerRequest)
		if err != nil {
			return nil, signalFailure(err)
		}
	}
	if request.ComparePrevious {
		previousContext := plan.Context
		previousContext.To, previousContext.From = plan.Context.From, plan.Context.From.Add(-plan.Context.To.Sub(plan.Context.From))
		previous, err = signalquery.Compile(previousContext, plan.Request)
		if err != nil {
			return nil, signalFailure(err)
		}
	}
	summary, err := e.signalSummary(ctx, plan)
	if err != nil {
		return nil, err
	}
	m := SignalMetadata{Selection: plan.Request.Selection, Trigger: plan.Request.Trigger, RunID: request.Query.RunID, Step: plan.Context.Step, PhysicalResolution: "canonical", TimeAxis: "event_occurred_at", Unit: "original_message", Population: "answered original-message readings that measured the selected signal or ordered level", Coverage: "unknown", QueriedAt: plan.Context.Now.Format(time.RFC3339Nano), From: plan.Context.From.Format(time.RFC3339Nano), To: plan.Context.To.Format(time.RFC3339Nano), Filters: append([]signalquery.Filter{}, plan.Request.Filters...), Snapshot: "live", Exact: true, SeriesComplete: false, GroupsTruncated: false, GroupBy: []signalquery.Dimension{}, PointBudget: 0}
	if m.Trigger == "stream" {
		m.RunID = ""
	}
	m.Step = "none"
	result := &SignalFindings{Measurement: &SignalResult{Metadata: m, Summary: summary, Groups: []Row{}, Rows: []Row{}}, Concentration: Row{"linked_matched_messages": int64(0), "top_session_matches": int64(0), "top_five_session_matches": int64(0), "top_session_share": nil, "top_five_session_share": nil}, Peer: request.Peer, PeerSummary: nil, Cooccurrence: []Row{}, Comparison: nil, Evidence: []Row{}, EvidenceScope: "At most twenty recent matching conversations are considered. Only permitted examples are returned; this is not an exhaustive session listing. Transcript access is checked again when opening the conversation."}
	concentration, err := runSignalPlan(ctx, e.ch, plan.Concentration())
	if err != nil {
		return nil, err
	}
	if len(concentration) > 0 {
		total, _ := concentration[0]["linked_matched_messages"].(int64)
		top, _ := concentration[0]["session_matches"].(int64)
		var five int64
		for _, row := range concentration {
			n, _ := row["session_matches"].(int64)
			five += n
		}
		result.Concentration["linked_matched_messages"], result.Concentration["top_session_matches"], result.Concentration["top_five_session_matches"] = total, top, five
		if total > 0 {
			result.Concentration["top_session_share"], result.Concentration["top_five_session_share"] = float64(top)/float64(total), float64(five)/float64(total)
		}
	}
	if peer != nil {
		result.EvidenceScope += " With a peer selected, examples contain both predicates in the same conversation, possibly in different messages. The returned message coordinate is a primary-predicate hit."
		result.PeerSummary, err = e.signalSummary(ctx, peer)
		if err != nil {
			return nil, err
		}
		for _, sessions := range []bool{false, true} {
			rows, err := runSignalPlan(ctx, e.ch, plan.Cooccurrence(peer, sessions))
			if err != nil {
				return nil, err
			}
			if len(rows) != 1 {
				return nil, fmt.Errorf("cooccurrence summary is missing")
			}
			row := rows[0]
			row["unit"] = "same_message"
			if sessions {
				row["unit"] = "same_session"
			}
			answered, _ := row["joint_answered"].(int64)
			matched, _ := row["joint_matches"].(int64)
			row["joint_share"] = nil
			if answered > 0 {
				row["joint_share"] = float64(matched) / float64(answered)
			}
			result.Cooccurrence = append(result.Cooccurrence, row)
		}
	}
	if previous != nil {
		baseline, err := e.signalSummary(ctx, previous)
		if err != nil {
			return nil, err
		}
		result.Comparison = compareSignals(plan.Context, baseline, summary)
	}
	if request.Evidence {
		candidates, err := runSignalPlan(ctx, e.ch, plan.Evidence(peer))
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(candidates))
		for _, row := range candidates {
			id, ok := row["conversation_id"].(string)
			if !ok {
				return nil, fmt.Errorf("signal evidence has no conversation identity")
			}
			ids = append(ids, id)
		}
		readable, err := access.ReadableConversations(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("authorize signal evidence: %w", err)
		}
		allowed := make(map[string]bool, len(readable))
		for _, id := range readable {
			allowed[id] = true
		}
		for i, row := range candidates {
			if allowed[ids[i]] {
				result.Evidence = append(result.Evidence, row)
			}
		}
	}
	return result, nil
}

func (e *Engine) signalSummary(ctx context.Context, plan *signalquery.Plan) (Row, error) {
	rows, err := runSignalPlan(ctx, e.ch, plan.Summary())
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("signal summary is missing")
	}
	if err := normalizeSignalMeasures(rows[0]); err != nil {
		return nil, err
	}
	return rows[0], nil
}

func compareSignals(ctx signalquery.Context, baseline, current Row) *SignalComparison {
	result := &SignalComparison{From: ctx.From.Add(-ctx.To.Sub(ctx.From)).Format(time.RFC3339Nano), To: ctx.From.Format(time.RFC3339Nano), Summary: baseline, Comparable: false, Reasons: []string{}, DeltaShare: nil}
	for _, row := range []Row{baseline, current} {
		answers, _ := row["answered"].(int64)
		sessions, _ := row["evaluated_sessions"].(int64)
		if answers < comparisonAnswers || sessions < comparisonSessions {
			result.Reasons = append(result.Reasons, "insufficient_observations")
			break
		}
	}
	if baseline["definition_count"] != int64(1) || current["definition_count"] != int64(1) || baseline["unknown_measured_definitions"] != int64(0) || current["unknown_measured_definitions"] != int64(0) || !reflect.DeepEqual(baseline["definition_hashes"], current["definition_hashes"]) {
		result.Reasons = append(result.Reasons, "definition_changed_mixed_or_unknown")
	}
	for _, key := range []string{"classifier_models", "classifier_providers", "evaluated_models", "compiler_versions", "input_strategies"} {
		before, beforeOK := baseline[key].([]string)
		after, afterOK := current[key].([]string)
		if !beforeOK || !afterOK || len(before) != 1 || len(after) != 1 || before[0] == "" || !reflect.DeepEqual(before, after) {
			result.Reasons = append(result.Reasons, key+"_changed_mixed_or_unknown")
		}
	}
	if baseline["unknown_measured_models"] != int64(0) || current["unknown_measured_models"] != int64(0) {
		result.Reasons = append(result.Reasons, "evaluated_models_missing")
	}
	if ctx.To.After(ctx.Now.Add(-comparisonRecentWindow)) {
		result.Reasons = append(result.Reasons, "recent_window_unsettled")
	}
	result.Comparable = len(result.Reasons) == 0
	if previous, ok := baseline["match_share"].(float64); ok {
		if current, ok := current["match_share"].(float64); ok {
			result.DeltaShare = new(current - previous)
		}
	}
	return result
}
