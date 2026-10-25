package repo

import (
	"context"
	"fmt"
	"time"
)

// GatewayConnectionAttemptsParams selects one gateway's connection attempts in
// exactly [TimeStart, TimeEnd).
type GatewayConnectionAttemptsParams struct {
	GramProjectID   string
	MetaMCPServerID string
	TimeStart       int64
	TimeEnd         int64

	// IntervalSeconds is the timeline bucket width: a multiple of 30 minutes,
	// aligned to the UTC epoch.
	IntervalSeconds int64

	// Key, when set, counts only one stage's caller × provider × client.
	Key *GatewayConnectionKey
}

// GatewayConnectionKey is one hotspot's raw key within a stage. An empty
// value matches attempts that recorded none.
type GatewayConnectionKey struct {
	Stage      string
	Subject    string
	ProviderID string
	ClientKey  string
}

// GatewayConnectionAttemptBucket is one stage and outcome's attempts in one
// timeline bucket.
type GatewayConnectionAttemptBucket struct {
	Stage              string `ch:"stage"`
	Outcome            string `ch:"outcome"`
	BucketTimeUnixNano int64  `ch:"interval_start"`
	Attempts           uint64 `ch:"attempts"`
	Retries            uint64 `ch:"retries"`
	Recovered          uint64 `ch:"recovered"`
	LastSeenUnixNano   int64  `ch:"last_seen"`
}

// GatewayConnectionAttempts counts a gateway's attempts per stage, outcome,
// and timeline bucket. Whole half-hours come from the rollup and the partial
// edges from raw rows, as discoveryFailures reads them.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GatewayConnectionAttempts(ctx context.Context, arg GatewayConnectionAttemptsParams) ([]GatewayConnectionAttemptBucket, error) {
	if arg.IntervalSeconds <= 0 || arg.IntervalSeconds%1800 != 0 {
		return nil, fmt.Errorf("connection attempt interval must be a positive multiple of 30 minutes")
	}
	slices := connectionAttemptSlicesFor(arg.TimeStart, arg.TimeEnd)
	intervalNanos := arg.IntervalSeconds * int64(time.Second)
	keyed, key := uint8(0), GatewayConnectionKey{Stage: "", Subject: "", ProviderID: "", ClientKey: ""}
	if arg.Key != nil {
		keyed, key = 1, *arg.Key
	}
	rows, err := q.conn.Query(ctx,
		`SELECT stage, outcome, interval_start,
		        sum(attempts) AS attempts, sum(retries) AS retries, sum(recovered) AS recovered, max(last_seen) AS last_seen
		 FROM (
		   SELECT toString(stage) AS stage, toString(outcome) AS outcome,
		          toInt64(intDiv(toUnixTimestamp(bucket), ?) * ?) * 1000000000 AS interval_start,
		          sum(attempts) AS attempts, sum(retries) AS retries, sum(recovered) AS recovered,
		          toInt64(toUnixTimestamp64Nano(max(last_seen))) AS last_seen
		   FROM mcp_connection_attempt_summaries
		   WHERE gram_project_id = toUUID(?) AND server_kind = 'meta' AND server_id = ?
		     AND bucket >= fromUnixTimestamp(?) AND bucket < fromUnixTimestamp(?)
		     AND (? = 0 OR (stage = ? AND subject = ? AND provider_id = ? AND client_key = ?))
		   GROUP BY stage, outcome, interval_start
		   UNION ALL
		   SELECT toString(attributes.gram.connection_attempt.stage) AS stage,
		          toString(attributes.gram.connection_attempt.outcome) AS outcome,
		          toInt64(intDiv(time_unix_nano, ?) * ?) AS interval_start,
		          count() AS attempts,
		          sum(toUInt64OrZero(toString(attributes.gram.connection_attempt.retries))) AS retries,
		          countIf(stage = 'discovery_member' AND outcome = 'listed' AND toUInt64OrZero(toString(attributes.gram.connection_attempt.retries)) > 0) AS recovered,
		          max(time_unix_nano) AS last_seen
		   FROM telemetry_logs
		   WHERE gram_project_id = ?
		     AND ((time_unix_nano >= ? AND time_unix_nano < ?) OR (time_unix_nano >= ? AND time_unix_nano < ?))
		     AND event_urn = 'urn:telemetry:gram_service:log:mcp_connection_attempt'
		     AND meta_mcp_server_id = ?
		     AND stage != ''
		     AND (? = 0 OR (stage = ?
		       AND toString(attributes.gram.connection_attempt.subject) = ?
		       AND toString(attributes.gram.connection_attempt.provider_id) = ?
		       AND toString(attributes.gram.connection_attempt.client_key) = ?))
		   GROUP BY stage, outcome, interval_start
		 )
		 GROUP BY stage, outcome, interval_start
		 ORDER BY stage, interval_start, outcome`,
		arg.IntervalSeconds, arg.IntervalSeconds, arg.GramProjectID, arg.MetaMCPServerID,
		slices.interiorStart/int64(time.Second), slices.interiorEnd/int64(time.Second),
		keyed, key.Stage, key.Subject, key.ProviderID, key.ClientKey,
		intervalNanos, intervalNanos, arg.GramProjectID,
		slices.edges[0][0], slices.edges[0][1], slices.edges[1][0], slices.edges[1][1], arg.MetaMCPServerID,
		keyed, key.Stage, key.Subject, key.ProviderID, key.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("query gateway connection attempts: %w", err)
	}
	defer rows.Close()
	buckets := []GatewayConnectionAttemptBucket{}
	for rows.Next() {
		var bucket GatewayConnectionAttemptBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scan gateway connection attempt bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}

// GatewayConnectionHotspotsParams selects one stage's attempts for one
// gateway in exactly [TimeStart, TimeEnd), ranked by caller.
type GatewayConnectionHotspotsParams struct {
	GramProjectID   string
	MetaMCPServerID string
	TimeStart       int64
	TimeEnd         int64
	Stage           string

	// ByRate ranks by failed / attempts instead of failed attempts.
	ByRate      bool
	MinAttempts uint64
	Limit       uint64
}

// GatewayConnectionOutcomeCount is the attempts with one outcome and reason.
type GatewayConnectionOutcomeCount struct {
	Outcome string
	Reason  string
	Count   uint64
}

// GatewayConnectionHotspot is one caller × provider × client's attempts in a
// stage. Its keys are raw attempt values; callers resolve labels.
type GatewayConnectionHotspot struct {
	Subject          string
	ProviderID       string
	ClientKey        string
	Attempts         uint64
	Failed           uint64
	Blocked          uint64
	Outcomes         []GatewayConnectionOutcomeCount
	LastSeenUnixNano int64
}

// GatewayConnectionHotspots ranks a stage's caller × provider × client keys
// with at least MinAttempts attempts, by failed attempts or by failure rate,
// with ties broken by failures, attempts, then the keys.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GatewayConnectionHotspots(ctx context.Context, arg GatewayConnectionHotspotsParams) ([]GatewayConnectionHotspot, error) {
	order := "failed DESC, attempts DESC"
	if arg.ByRate {
		order = "failed / attempts DESC, failed DESC, attempts DESC"
	}
	slices := connectionAttemptSlicesFor(arg.TimeStart, arg.TimeEnd)
	rows, err := q.conn.Query(ctx,
		`SELECT subject, provider_id, client_key,
		        sum(n) AS attempts,
		        sumIf(n, outcome = 'failed') AS failed,
		        sumIf(n, outcome IN ('credential_blocked', 'consent_blocked')) AS blocked,
		        groupArray(outcome) AS outcomes, groupArray(reason) AS reasons, groupArray(n) AS counts,
		        max(last_seen) AS last_seen
		 FROM (
		   SELECT subject, provider_id, client_key, outcome, reason, sum(n) AS n, max(last_seen) AS last_seen
		   FROM (
		     SELECT subject, provider_id, client_key, toString(outcome) AS outcome, toString(reason) AS reason,
		            sum(attempts) AS n, toInt64(toUnixTimestamp64Nano(max(last_seen))) AS last_seen
		     FROM mcp_connection_attempt_summaries
		     WHERE gram_project_id = toUUID(?) AND server_kind = 'meta' AND server_id = ? AND stage = ?
		       AND bucket >= fromUnixTimestamp(?) AND bucket < fromUnixTimestamp(?)
		     GROUP BY subject, provider_id, client_key, outcome, reason
		     UNION ALL
		     SELECT toString(attributes.gram.connection_attempt.subject) AS subject,
		            toString(attributes.gram.connection_attempt.provider_id) AS provider_id,
		            toString(attributes.gram.connection_attempt.client_key) AS client_key,
		            toString(attributes.gram.connection_attempt.outcome) AS outcome,
		            toString(attributes.gram.connection_attempt.reason) AS reason,
		            count() AS n, max(time_unix_nano) AS last_seen
		     FROM telemetry_logs
		     WHERE gram_project_id = ?
		       AND ((time_unix_nano >= ? AND time_unix_nano < ?) OR (time_unix_nano >= ? AND time_unix_nano < ?))
		       AND event_urn = 'urn:telemetry:gram_service:log:mcp_connection_attempt'
		       AND meta_mcp_server_id = ?
		       AND toString(attributes.gram.connection_attempt.stage) = ?
		     GROUP BY subject, provider_id, client_key, outcome, reason
		   )
		   GROUP BY subject, provider_id, client_key, outcome, reason
		 )
		 GROUP BY subject, provider_id, client_key
		 HAVING attempts >= ?
		 ORDER BY `+order+`, subject, provider_id, client_key
		 LIMIT ?`,
		arg.GramProjectID, arg.MetaMCPServerID, arg.Stage,
		slices.interiorStart/int64(time.Second), slices.interiorEnd/int64(time.Second),
		arg.GramProjectID, slices.edges[0][0], slices.edges[0][1], slices.edges[1][0], slices.edges[1][1], arg.MetaMCPServerID, arg.Stage,
		arg.MinAttempts, arg.Limit)
	if err != nil {
		return nil, fmt.Errorf("query gateway connection hotspots: %w", err)
	}
	defer rows.Close()
	hotspots := []GatewayConnectionHotspot{}
	for rows.Next() {
		var hotspot GatewayConnectionHotspot
		var outcomes, reasons []string
		var counts []uint64
		if err := rows.Scan(&hotspot.Subject, &hotspot.ProviderID, &hotspot.ClientKey, &hotspot.Attempts, &hotspot.Failed, &hotspot.Blocked, &outcomes, &reasons, &counts, &hotspot.LastSeenUnixNano); err != nil {
			return nil, fmt.Errorf("scan gateway connection hotspot: %w", err)
		}
		// One aggregation fills the three arrays in the same row order.
		if len(reasons) != len(outcomes) || len(counts) != len(outcomes) {
			return nil, fmt.Errorf("scan gateway connection hotspot: outcome arrays differ in length")
		}
		for i := range outcomes {
			hotspot.Outcomes = append(hotspot.Outcomes, GatewayConnectionOutcomeCount{Outcome: outcomes[i], Reason: reasons[i], Count: counts[i]})
		}
		hotspots = append(hotspots, hotspot)
	}
	return hotspots, rows.Err()
}
