package activities

// PromoteStagedTelemetry moves Claude api_request rows out of
// telemetry_logs_staging into telemetry_logs. Rows land in staging when
// Claude redacted their MCP attribution to "custom" (see the fork in
// server/internal/hooks/otel.go); the Stop/SubagentStop hooks ship the
// unredacted (request_id -> server/tool) tuples from the local transcript
// into Redis. One promotion pass, per session:
//
//  1. Load every staged row for the session.
//  2. Per row: tuple in Redis -> patch attributes.mcp_server.name /
//     attributes.mcp_tool.name; no tuple but row older than the timeout ->
//     promote verbatim (stays "custom" — today's behavior); otherwise leave
//     the row for a later trigger or the sweep.
//  3. Dedup guard: drop rows whose id already exists in telemetry_logs.
//     Promotion preserves the staged row's id, so telemetry_logs itself is
//     the exactly-once ledger across crashes between insert and delete.
//  4. Insert the batch into telemetry_logs (this is when
//     attribute_metrics_summaries_mv aggregates the row), then delete the
//     promoted ids from staging.
//
// Passes are serialized per session by the promotion workflow's ID, so the
// read-check-insert sequence has a single writer.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// promoteStagedTelemetryTimeout is how long a staged row waits for its
// attribution tuple before it is promoted verbatim.
const promoteStagedTelemetryTimeout = 30 * time.Minute

type PromoteStagedTelemetry struct {
	logger    *slog.Logger
	telemetry *telemetryrepo.Queries
	cache     cache.Cache
	timeout   time.Duration
	now       func() time.Time
}

func NewPromoteStagedTelemetry(logger *slog.Logger, chConn clickhouse.Conn, cacheAdapter cache.Cache) *PromoteStagedTelemetry {
	return &PromoteStagedTelemetry{
		logger:    logger.With(attr.SlogComponent("promote_staged_telemetry")),
		telemetry: telemetryrepo.New(chConn),
		cache:     cacheAdapter,
		timeout:   promoteStagedTelemetryTimeout,
		now:       time.Now,
	}
}

type PromoteStagedTelemetryArgs struct {
	ProjectID uuid.UUID
	SessionID string
}

type PromoteStagedTelemetryResult struct {
	// Promoted counts rows inserted into telemetry_logs by this pass
	// (rewritten plus verbatim timeouts).
	Promoted int
	// Rewritten counts promoted rows whose attribution was restored from a
	// transcript tuple.
	Rewritten int
	// Remaining counts rows left in staging awaiting a tuple or the timeout.
	Remaining int
}

func (p *PromoteStagedTelemetry) Do(ctx context.Context, args PromoteStagedTelemetryArgs) (*PromoteStagedTelemetryResult, error) {
	result := &PromoteStagedTelemetryResult{Promoted: 0, Rewritten: 0, Remaining: 0}

	rows, err := p.telemetry.ListStagedTelemetryLogs(ctx, args.ProjectID.String(), args.SessionID)
	if err != nil {
		return nil, fmt.Errorf("list staged telemetry logs: %w", err)
	}
	if len(rows) == 0 {
		return result, nil
	}

	cutoff := p.now().Add(-p.timeout)
	promote := make([]telemetryrepo.InsertTelemetryLogParams, 0, len(rows))
	rewrittenByID := make(map[string]bool, len(rows))
	for _, row := range rows {
		var tuple telemetry.MCPAttributionTuple
		hasTuple := row.RequestID != "" &&
			p.cache.Get(ctx, telemetry.MCPAttributionTupleKey(args.ProjectID.String(), row.RequestID), &tuple) == nil &&
			tuple.Server != ""

		attributes := row.Attributes
		switch {
		case hasTuple:
			patched, err := patchMCPAttribution(row.Attributes, tuple)
			if err != nil {
				// A row whose JSON cannot be patched still promotes verbatim
				// at the timeout; do not wedge the whole pass on it.
				p.logger.WarnContext(ctx, "failed to patch staged telemetry attribution",
					attr.SlogEvent("staged_telemetry_patch_failed"),
					attr.SlogError(err),
					attr.SlogGenAIConversationID(args.SessionID),
				)
				result.Remaining++
				continue
			}
			attributes = patched
			rewrittenByID[row.ID] = true
		case time.Unix(0, row.ObservedTimeUnixNano).Before(cutoff):
			// Timeout: promote verbatim so the row is not withheld from
			// dashboards forever. It keeps its native "custom" attribution.
		default:
			result.Remaining++
			continue
		}

		promote = append(promote, telemetryrepo.InsertTelemetryLogParams{
			ID:                   row.ID,
			TimeUnixNano:         row.TimeUnixNano,
			ObservedTimeUnixNano: row.ObservedTimeUnixNano,
			SeverityText:         row.SeverityText,
			Body:                 row.Body,
			TraceID:              row.TraceID,
			SpanID:               row.SpanID,
			Attributes:           attributes,
			ResourceAttributes:   row.ResourceAttributes,
			GramProjectID:        row.GramProjectID,
			GramDeploymentID:     row.GramDeploymentID,
			GramFunctionID:       row.GramFunctionID,
			GramURN:              row.GramURN,
			ServiceName:          row.ServiceName,
			ServiceVersion:       row.ServiceVersion,
			GramChatID:           row.GramChatID,
		})
	}

	if len(promote) == 0 {
		return result, nil
	}

	// Dedup guard: a crash after insert but before delete leaves the row in
	// both tables; the retry must not insert (and double-count in the MVs)
	// again. Ids are preserved across promotion, so existence in
	// telemetry_logs marks the row done — finish its delete instead.
	ids := make([]string, 0, len(promote))
	minTime, maxTime := promote[0].TimeUnixNano, promote[0].TimeUnixNano
	for _, row := range promote {
		ids = append(ids, row.ID)
		minTime = min(minTime, row.TimeUnixNano)
		maxTime = max(maxTime, row.TimeUnixNano)
	}
	existing, err := p.telemetry.ListExistingTelemetryLogIDs(ctx, args.ProjectID.String(), ids, minTime, maxTime)
	if err != nil {
		return nil, fmt.Errorf("list existing telemetry log ids: %w", err)
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, id := range existing {
		existingSet[id] = struct{}{}
	}

	insert := make([]telemetryrepo.InsertTelemetryLogParams, 0, len(promote))
	for _, row := range promote {
		if _, ok := existingSet[row.ID]; ok {
			continue
		}
		insert = append(insert, row)
	}

	if err := p.telemetry.InsertTelemetryLogs(ctx, insert); err != nil {
		return nil, fmt.Errorf("insert promoted telemetry logs: %w", err)
	}
	if err := p.telemetry.DeleteStagedTelemetryLogs(ctx, args.ProjectID.String(), ids); err != nil {
		return nil, fmt.Errorf("delete promoted staged telemetry logs: %w", err)
	}

	// Count only rows this pass actually inserted: rows the dedup guard
	// skipped were promoted by an earlier crashed pass, not this one.
	result.Promoted = len(insert)
	for _, row := range insert {
		if rewrittenByID[row.ID] {
			result.Rewritten++
		}
	}
	return result, nil
}

// ListStagedSessions lists the (project, session) pairs with rows waiting in
// staging, for the sweep.
func (p *PromoteStagedTelemetry) ListStagedSessions(ctx context.Context) ([]PromoteStagedTelemetryArgs, error) {
	sessions, err := p.telemetry.ListStagedTelemetrySessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list staged telemetry sessions: %w", err)
	}
	out := make([]PromoteStagedTelemetryArgs, 0, len(sessions))
	for _, session := range sessions {
		projectID, err := uuid.Parse(session.ProjectID)
		if err != nil {
			continue
		}
		out = append(out, PromoteStagedTelemetryArgs{ProjectID: projectID, SessionID: session.SessionID})
	}
	return out, nil
}

// patchMCPAttribution rewrites mcp_server.name / mcp_tool.name inside the
// row's attributes JSON. ClickHouse's JSON type treats dotted keys as nested
// paths, so toJSONString renders them as nested objects and the patch writes
// the nested form; on re-insert the paths are identical.
func patchMCPAttribution(attributesJSON string, tuple telemetry.MCPAttributionTuple) (string, error) {
	var attrs map[string]any
	if err := json.Unmarshal([]byte(attributesJSON), &attrs); err != nil {
		return "", fmt.Errorf("unmarshal staged attributes: %w", err)
	}

	setNested(attrs, "mcp_server", "name", tuple.Server)
	if tuple.Tool != "" {
		setNested(attrs, "mcp_tool", "name", tuple.Tool)
	}

	patched, err := json.Marshal(attrs)
	if err != nil {
		return "", fmt.Errorf("marshal patched attributes: %w", err)
	}
	return string(patched), nil
}

func setNested(attrs map[string]any, outer string, inner string, value string) {
	obj, ok := attrs[outer].(map[string]any)
	if !ok {
		obj = make(map[string]any, 1)
		attrs[outer] = obj
	}
	obj[inner] = value
}
