package activities

// PromoteStagedTelemetry moves Claude api_request rows out of
// telemetry_logs_staging into telemetry_logs. Rows land in staging when
// Claude redacted their MCP attribution to "custom" (see the fork in
// server/internal/hooks/otel.go); the Stop/SubagentStop hooks ship the
// unredacted (request_id -> server/tool) tuples from the local transcript
// into Redis. One promotion pass, per project:
//
//  1. Load the project's staged rows (oldest first, capped — the promotion
//     workflow's drain loop runs further passes while pages keep promoting,
//     and the next sweep tick picks up anything beyond its budget).
//  2. Per row: tuple in Redis -> patch attributes.mcp_server.name /
//     attributes.mcp_tool.name; no tuple but row older than the timeout ->
//     promote verbatim (stays "custom" — today's behavior); otherwise leave
//     the row for a later pass.
//  3. Existence guard: skip rows whose id already exists in telemetry_logs
//     (sequentially-consistent read), so a fresh pass after a crash between
//     insert and delete does not re-insert — it just finishes the staging
//     delete.
//  4. Promotion claim: claim each remaining row in Redis (SET NX) before
//     inserting, so only one attempt ever inserts a given id. Insert the
//     claimed rows into telemetry_logs (this is when
//     attribute_metrics_summaries_mv aggregates the row), then delete the
//     rows now confirmed in telemetry_logs from staging.
//
// Passes are serialized per project by the promotion workflow's ID, so the
// check-insert sequence normally has a single writer. The existence guard
// catches a row an earlier completed pass already committed; the claim covers
// the residual race the guard cannot see — a timed-out activity attempt whose
// insert lands after the retrying attempt's existence check — because
// insert_deduplication_token is inert on the non-replicated MergeTree
// deployment. (The insert still carries the token as a no-cost belt for any
// replicated/Cloud engine.)

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

// promoteStagedTelemetryClaimTTL bounds a per-row promotion claim: long enough
// to outlast an insert plus the activity's retry gap so a live winner's claim
// is not stolen mid-insert, short enough that a winner which crashed before
// inserting frees the row for a later pass quickly.
const promoteStagedTelemetryClaimTTL = 5 * time.Minute

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
	// Deduped counts rows the dedup guard skipped because an earlier
	// (crashed) pass already inserted them into telemetry_logs; this pass
	// only finished their staging delete. Progress all the same: the
	// workflow's drain loop keeps going when a pass deduped rows even
	// though it inserted nothing, since the next page may be promotable.
	Deduped int
}

func (p *PromoteStagedTelemetry) Do(ctx context.Context, args PromoteStagedTelemetryArgs) (*PromoteStagedTelemetryResult, error) {
	result := &PromoteStagedTelemetryResult{Promoted: 0, Rewritten: 0, Remaining: 0, Deduped: 0}

	rows, err := p.telemetry.ListStagedTelemetryLogs(ctx, args.ProjectID.String())
	if err != nil {
		return nil, fmt.Errorf("list staged telemetry logs: %w", err)
	}
	if len(rows) == 0 {
		return result, nil
	}

	cutoff := p.now().Add(-p.timeout)
	promote := make([]telemetryrepo.InsertTelemetryLogParams, 0, len(rows))
	rewrittenByID := make(map[string]bool, len(rows))
	// Memoize tuple lookups so each distinct lookup costs one Redis
	// round-trip even when several staged rows share it. The memo key must
	// mirror the Redis key's (org, request id) scope: a pass is per-project
	// so two real orgs never meet in one batch, but a row staged before
	// org_id existed carries an empty org, and memoizing its nil result
	// under the request id alone would starve a later row whose populated
	// org does have a tuple. seen guards against duplicate staged copies of
	// the same row id (a retried ingest insert on an engine without a dedup
	// window) entering the promote list twice.
	type tupleKey struct {
		orgID     string
		requestID string
	}
	tuples := make(map[tupleKey]*telemetry.MCPAttributionTuple, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, dup := seen[row.ID]; dup {
			continue
		}
		seen[row.ID] = struct{}{}

		// The tuple key is org-scoped, not project-scoped: the hooks key that
		// wrote the tuple and the OTEL exporter key that staged this row can
		// resolve different projects (org-wide plugin keys), but both agree
		// on the org, which the row carries as a materialized column.
		key := tupleKey{orgID: row.OrgID, requestID: row.RequestID}
		tuple, ok := tuples[key]
		if !ok {
			tuple = p.lookupTuple(ctx, row.OrgID, row.RequestID)
			tuples[key] = tuple
		}

		attributes := row.Attributes
		switch {
		case tuple != nil:
			patched, err := patchMCPAttribution(row.Attributes, *tuple)
			if err != nil {
				// A row whose JSON cannot be patched still promotes verbatim
				// at the timeout; do not wedge the whole pass on it.
				p.logger.WarnContext(ctx, "failed to patch staged telemetry attribution",
					attr.SlogEvent("staged_telemetry_patch_failed"),
					attr.SlogError(err),
					attr.SlogProjectID(args.ProjectID.String()),
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

	// Existence guard: a crash after insert but before delete leaves the row
	// in both tables; a fresh pass must not re-insert (and double-count in the
	// MVs). Ids are preserved across promotion, so existence in telemetry_logs
	// marks the row done — finish its staging delete instead of re-inserting.
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

	// Promotion claim: the existence guard cannot see an insert a timed-out
	// attempt commits after this attempt's check. Claim each row (SET NX)
	// before inserting so only one attempt inserts a given id; an attempt that
	// loses the claim defers the row rather than racing a double insert. Only
	// rows now confirmed in telemetry_logs — either already there (existing)
	// or inserted by this pass — are deleted from staging; deferred rows stay
	// for a later pass.
	insert := make([]telemetryrepo.InsertTelemetryLogParams, 0, len(promote))
	claimedIDs := make([]string, 0, len(promote))
	deleteIDs := make([]string, 0, len(promote))
	deferred := 0
	for _, row := range promote {
		if _, ok := existingSet[row.ID]; ok {
			deleteIDs = append(deleteIDs, row.ID)
			continue
		}
		claimed, err := p.cache.Add(ctx, telemetry.MCPPromotionClaimKey(args.ProjectID.String(), row.ID), promoteStagedTelemetryClaimTTL)
		if err != nil {
			// Cannot establish exclusivity — defer rather than risk a double
			// insert. The row stays staged and promotes on a later pass.
			p.logger.WarnContext(ctx, "failed to claim staged telemetry promotion",
				attr.SlogEvent("staged_telemetry_claim_failed"),
				attr.SlogError(err),
				attr.SlogProjectID(args.ProjectID.String()),
			)
			deferred++
			continue
		}
		if !claimed {
			// A concurrent (e.g. timed-out) attempt already claimed this row's
			// promotion and may have an in-doubt insert. Defer to avoid a
			// double insert; the winner finishes it, or a later pass retries
			// once the claim's TTL lapses.
			deferred++
			continue
		}
		claimedIDs = append(claimedIDs, row.ID)
		insert = append(insert, row)
	}

	if len(insert) > 0 {
		if err := p.telemetry.InsertPromotedTelemetryLogs(ctx, insert); err != nil {
			// Release this attempt's claims so the retry can re-insert
			// immediately instead of waiting out the TTL. Rows that did commit
			// before the error are caught by the existence guard on the retry,
			// so releasing cannot cause a double insert.
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			p.releaseClaims(releaseCtx, args.ProjectID.String(), claimedIDs)
			cancel()
			return nil, fmt.Errorf("insert promoted telemetry logs: %w", err)
		}
		deleteIDs = append(deleteIDs, claimedIDs...)
	}

	if len(deleteIDs) > 0 {
		if err := p.telemetry.DeleteStagedTelemetryLogs(ctx, args.ProjectID.String(), deleteIDs); err != nil {
			return nil, fmt.Errorf("delete promoted staged telemetry logs: %w", err)
		}
	}

	// Counts: Promoted is what this pass inserted. Deduped is the
	// already-promoted rows whose staging delete this pass finished — progress
	// the drain loop counts even though nothing was inserted. Deferred rows
	// made no progress (staging did not shrink), so they surface as Remaining
	// and the loop yields to the next tick instead of re-scanning them.
	result.Promoted = len(insert)
	result.Deduped = len(deleteIDs) - len(insert)
	result.Remaining += deferred
	for _, row := range insert {
		if rewrittenByID[row.ID] {
			result.Rewritten++
		}
	}
	return result, nil
}

// releaseClaims best-effort deletes the given promotion claims. Failures are
// non-fatal: a leaked claim only delays the affected row's next promotion
// attempt until the claim's TTL lapses.
func (p *PromoteStagedTelemetry) releaseClaims(ctx context.Context, projectID string, ids []string) {
	for _, id := range ids {
		if err := p.cache.Delete(ctx, telemetry.MCPPromotionClaimKey(projectID, id)); err != nil {
			p.logger.WarnContext(ctx, "failed to release staged telemetry promotion claim",
				attr.SlogEvent("staged_telemetry_claim_release_failed"),
				attr.SlogError(err),
				attr.SlogProjectID(projectID),
			)
		}
	}
}

// lookupTuple fetches the transcript-derived attribution for one request id,
// or nil when no usable tuple is stored (missing key, cache error, or an
// empty server name). An empty org id (a row staged without gram.org.id in
// its attributes) never matches a key, so such rows promote verbatim at the
// timeout — today's behavior.
func (p *PromoteStagedTelemetry) lookupTuple(ctx context.Context, orgID string, requestID string) *telemetry.MCPAttributionTuple {
	if orgID == "" || requestID == "" {
		return nil
	}
	var tuple telemetry.MCPAttributionTuple
	if err := p.cache.Get(ctx, telemetry.MCPAttributionTupleKey(orgID, requestID), &tuple); err != nil {
		return nil
	}
	if tuple.Server == "" {
		return nil
	}
	return &tuple
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
