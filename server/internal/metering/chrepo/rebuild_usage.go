package chrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const (
	usageSummaryOrganizationPageSize = 256
	usageSummaryCleanupTimeout       = 30 * time.Second
	usageSummaryInitialSlice         = 6 * time.Hour
	usageSummaryMinimumSlice         = time.Nanosecond
	usageSummaryBuildSettings        = " SETTINGS do_not_merge_across_partitions_select_final = 1, max_threads = 2, max_final_threads = 2, max_block_size = 1024, max_memory_usage = 536870912, max_rows_to_read = 10000000, max_bytes_to_read = 2147483648, max_execution_time = 20, timeout_before_checking_execution_speed = 0, max_bytes_before_external_group_by = 134217728, max_bytes_ratio_before_external_group_by = 0.5"
)

type usageSummaryFamily struct {
	name           string
	meterPredicate string
}

var usageSummaryFamilies = [...]usageSummaryFamily{
	{name: "agent_session_storage", meterPredicate: "meter_id = 'gram.agent_session.storage'"},
	{name: "mcp_bandwidth", meterPredicate: "meter_id IN ('gram.mcp.bandwidth.ingress', 'gram.mcp.bandwidth.egress')"},
	{name: "risk_content_scans", meterPredicate: "meter_id IN ('gram.risk.scan.gitleaks', 'gram.risk.scan.presidio', 'gram.risk.scan.prompt_injection', 'gram.risk.scan.prompt_policy', 'gram.risk.scan.custom_rules', 'gram.risk.scan.cli_destructive')"},
}

// RebuildUsageSummaries recomputes every retained complete UTC day and atomically
// publishes one complete generation. Every pass revisits full retained history,
// so readings that arrive after an earlier pass repair old daily summaries.
func (q *Queries) RebuildUsageSummaries(ctx context.Context, snapshotAt time.Time) (returnErr error) {
	if snapshotAt.IsZero() {
		return errors.New("rebuild usage summaries: snapshot time is required")
	}
	buildSettings := clickhouse.Settings{
		"async_insert":                             0,
		"max_threads":                              2,
		"max_final_threads":                        2,
		"max_block_size":                           1024,
		"max_memory_usage":                         536870912,
		"max_rows_to_read":                         10000000,
		"max_bytes_to_read":                        2147483648,
		"max_execution_time":                       20,
		"timeout_before_checking_execution_speed":  0,
		"max_bytes_before_external_group_by":       134217728,
		"max_bytes_ratio_before_external_group_by": 0.5,
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(buildSettings))
	snapshotAt = snapshotAt.UTC()
	publishedBefore := utcUsageDay(snapshotAt.Add(-time.Hour))
	publication, err := q.requireAtomicUsageSummaryExchange(ctx)
	if err != nil {
		return err
	}
	if publication.replicas != 0 {
		buildSettings["insert_quorum"] = publication.replicas
		buildSettings["insert_quorum_timeout"] = 15000
		ctx = clickhouse.Context(ctx, clickhouse.WithSettings(buildSettings))
		if err := q.verifyUsageSummaryReplicaCatalog(ctx, publication); err != nil {
			return err
		}
	}

	if err := q.clearUsageSummaryWorkTables(ctx); err != nil {
		return fmt.Errorf("prepare usage summary rebuild: %w", err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), usageSummaryCleanupTimeout)
		defer cancel()
		if err := q.clearUsageSummaryWorkTables(cleanupCtx); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("discard unpublished usage summary generation: %w", err))
		}
	}()

	firstDay, lastDay, hasReadings, err := q.retainedUsageDayRange(ctx)
	if err != nil {
		return err
	}
	if publishedBefore.Before(lastDay) {
		lastDay = publishedBefore
	}
	if hasReadings {
		for day := firstDay; day.Before(lastDay); day = day.AddDate(0, 0, 1) {
			nextDay := day.AddDate(0, 0, 1)
			for _, family := range usageSummaryFamilies {
				for from := day; from.Before(nextDay); from = from.Add(usageSummaryInitialSlice) {
					if err := q.rebuildUsageSummarySlice(ctx, family, from, from.Add(usageSummaryInitialSlice)); err != nil {
						return fmt.Errorf("rebuild %s usage summary for %s: %w", family.name, from.Format(time.RFC3339Nano), err)
					}
				}
				if err := q.consolidateUsageSummaryParts(ctx, family, day); err != nil {
					return fmt.Errorf("consolidate %s usage summary for %s: %w", family.name, day.Format(time.DateOnly), err)
				}
				if err := q.conn.Exec(ctx, "TRUNCATE TABLE billing_meter_daily_summary_parts"); err != nil {
					return fmt.Errorf("clear consolidated usage summary parts: %w", err)
				}
			}
		}
	}

	if err := q.conn.Exec(ctx, `
		INSERT INTO billing_meter_daily_summaries_staging VALUES
		('', '', '', '', toDate(?), '', '', '', '', '', toInt128(0), toUInt64(0), toUInt8(1), toDateTime64(?, 9, 'UTC'), toDateTime64(?, 9, 'UTC'))
	`, publishedBefore.Format(time.DateOnly), usageSummaryDateTime64(publishedBefore), usageSummaryDateTime64(snapshotAt)); err != nil {
		return fmt.Errorf("stage usage summary publication marker: %w", err)
	}
	if err := q.verifyUsageSummaryReplicaCatalog(ctx, publication); err != nil {
		return fmt.Errorf("verify usage summary publication topology: %w", err)
	}
	// A timeout can follow a successful local exchange. Never discard a table
	// that a lagging replica may still expose as its live generation.
	published = true
	if err := q.conn.Exec(ctx, "EXCHANGE TABLES billing_meter_daily_summaries_staging AND billing_meter_daily_summaries"); err != nil {
		return fmt.Errorf("publish usage summary generation: %w", err)
	}
	publication.liveUUID, publication.stagingUUID = publication.stagingUUID, publication.liveUUID
	if err := q.verifyUsageSummaryReplicaCatalog(ctx, publication); err != nil {
		return fmt.Errorf("await usage summary publication: %w", err)
	}
	if err := q.clearUsageSummaryWorkTables(ctx); err != nil {
		return fmt.Errorf("clear usage summary work tables after publication: %w", err)
	}
	return nil
}

type usageSummaryPublication struct {
	database    string
	replicas    uint64
	liveUUID    string
	stagingUUID string
}

func (q *Queries) requireAtomicUsageSummaryExchange(ctx context.Context) (usageSummaryPublication, error) {
	var publication usageSummaryPublication
	var engine string
	if err := q.conn.QueryRow(ctx, "SELECT name, engine FROM system.databases WHERE name = currentDatabase()").Scan(&publication.database, &engine); err != nil {
		return publication, fmt.Errorf("inspect usage summary database engine: %w", err)
	}
	if engine != "Atomic" && engine != "Shared" && engine != "Replicated" {
		return publication, fmt.Errorf("rebuild usage summaries: database engine %q does not support the required atomic table exchange", engine)
	}
	var replicatedTables uint64
	if err := q.conn.QueryRow(ctx, `
		SELECT
			countIf(startsWith(engine, 'Replicated')),
			anyIf(toString(uuid), name = 'billing_meter_daily_summaries'),
			anyIf(toString(uuid), name = 'billing_meter_daily_summaries_staging')
		FROM system.tables
		WHERE database = currentDatabase()
			AND name IN ('billing_meter_daily_summaries', 'billing_meter_daily_summaries_staging')
	`).Scan(&replicatedTables, &publication.liveUUID, &publication.stagingUUID); err != nil {
		return publication, fmt.Errorf("inspect usage summary publication tables: %w", err)
	}
	if publication.liveUUID == "" || publication.stagingUUID == "" {
		return publication, errors.New("rebuild usage summaries: publication tables are missing")
	}
	if engine != "Replicated" {
		if replicatedTables != 0 {
			return publication, errors.New("rebuild usage summaries: replicated tables require replicated database metadata for atomic publication")
		}
		return publication, nil
	}
	if replicatedTables != 2 {
		return publication, errors.New("rebuild usage summaries: replicated database requires both publication tables to replicate data")
	}
	var shards uint64
	if err := q.conn.QueryRow(ctx, `
		SELECT count(), uniqExact(shard_num)
		FROM system.clusters WHERE cluster = ?
	`, publication.database).Scan(&publication.replicas, &shards); err != nil {
		return publication, fmt.Errorf("inspect usage summary replicas: %w", err)
	}
	if publication.replicas == 0 || shards != 1 {
		return publication, errors.New("rebuild usage summaries: publication requires one complete shard with known replicas")
	}
	return publication, nil
}

func (q *Queries) verifyUsageSummaryReplicaCatalog(ctx context.Context, publication usageSummaryPublication) error {
	if publication.replicas == 0 {
		return nil
	}
	var matchingTables uint64
	if err := q.conn.QueryRow(ctx, `
		SELECT countIf(
			(name = 'billing_meter_daily_summaries' AND toString(uuid) = ?)
			OR (name = 'billing_meter_daily_summaries_staging' AND toString(uuid) = ?)
		)
		FROM clusterAllReplicas(?, system.tables)
		WHERE database = ?
			AND name IN ('billing_meter_daily_summaries', 'billing_meter_daily_summaries_staging')
		SETTINGS skip_unavailable_shards = 0,
			connect_timeout_with_failover_ms = 1000, receive_timeout = 10
	`, publication.liveUUID, publication.stagingUUID, publication.database, publication.database).Scan(&matchingTables); err != nil {
		return fmt.Errorf("verify usage summary replica catalog: %w", err)
	}
	if matchingTables != 2*publication.replicas {
		return errors.New("usage summary replica catalogs have not converged; preserving both generations")
	}
	return nil
}

func (q *Queries) retainedUsageDayRange(ctx context.Context) (time.Time, time.Time, bool, error) {
	var partCount uint64
	var firstPart, lastPart time.Time
	if err := q.conn.QueryRow(ctx, `
		SELECT count(), min(min_time), max(max_time)
		FROM system.parts
		WHERE active AND database = currentDatabase() AND table = 'billing_meter_readings_by_time'
	`).Scan(&partCount, &firstPart, &lastPart); err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("inspect retained usage parts: %w", err)
	}
	if partCount == 0 {
		return time.Time{}, time.Time{}, false, nil
	}
	return utcUsageDay(firstPart), utcUsageDay(lastPart).AddDate(0, 0, 1), true, nil
}

func (q *Queries) rebuildUsageSummarySlice(ctx context.Context, family usageSummaryFamily, from, to time.Time) error {
	cursor := ""
	for {
		organizations, err := q.usageSummaryOrganizationPage(ctx, family, from, to, cursor)
		if err != nil {
			if cursor == "" && isUsageSummarySliceLimit(err) && canSplitUsageSummarySlice(from, to) {
				middle := usageSummarySliceMiddle(from, to)
				if err := q.rebuildUsageSummarySlice(ctx, family, from, middle); err != nil {
					return err
				}
				return q.rebuildUsageSummarySlice(ctx, family, middle, to)
			}
			return err
		}
		for _, organizationID := range organizations {
			if err := q.rebuildOrganizationUsageSummarySlice(ctx, family, organizationID, from, to); err != nil {
				return err
			}
		}
		if len(organizations) < usageSummaryOrganizationPageSize {
			return nil
		}
		cursor = organizations[len(organizations)-1]
	}
}

func (q *Queries) usageSummaryOrganizationPage(ctx context.Context, family usageSummaryFamily, from, to time.Time, cursor string) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT organization_id
		FROM billing_meter_readings_by_time
		WHERE organization_id > ? AND %s
			AND occurred_at >= toDateTime64(?, 9, 'UTC')
			AND occurred_at < toDateTime64(?, 9, 'UTC')
		ORDER BY organization_id
		LIMIT %d%s`, family.meterPredicate, usageSummaryOrganizationPageSize, usageSummaryBuildSettings)
	rows, err := q.conn.Query(ctx, query, cursor, usageSummaryDateTime64(from), usageSummaryDateTime64(to))
	if err != nil {
		return nil, fmt.Errorf("enumerate usage summary organizations: %w", err)
	}
	organizations := make([]string, 0, usageSummaryOrganizationPageSize)
	for rows.Next() {
		var organizationID string
		if err := rows.Scan(&organizationID); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan usage summary organization: %w", err)
		}
		organizations = append(organizations, organizationID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("read usage summary organizations: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close usage summary organizations: %w", err)
	}
	return organizations, nil
}

func (q *Queries) rebuildOrganizationUsageSummarySlice(ctx context.Context, family usageSummaryFamily, organizationID string, from, to time.Time) error {
	if err := q.conn.Exec(ctx, "TRUNCATE TABLE billing_meter_daily_summary_attempt"); err != nil {
		return fmt.Errorf("clear usage summary attempt: %w", err)
	}
	query := `
		INSERT INTO billing_meter_daily_summary_attempt
		SELECT *
		FROM billing_meter_daily_summary_source_slice(
			summary_organization_id = ?,
			summary_family = ?,
			summary_from = toDateTime64(?, 9, 'UTC'),
			summary_to = toDateTime64(?, 9, 'UTC')
		)` + usageSummaryBuildSettings
	if err := q.conn.Exec(ctx, query, organizationID, family.name, usageSummaryDateTime64(from), usageSummaryDateTime64(to)); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), usageSummaryCleanupTimeout)
		clearErr := q.conn.Exec(cleanupCtx, "TRUNCATE TABLE billing_meter_daily_summary_attempt")
		cancel()
		if clearErr != nil {
			return errors.Join(fmt.Errorf("build organization usage summary slice: %w", err), fmt.Errorf("discard failed usage summary attempt: %w", clearErr))
		}
		if isUsageSummarySliceLimit(err) && canSplitUsageSummarySlice(from, to) {
			middle := usageSummarySliceMiddle(from, to)
			if err := q.rebuildOrganizationUsageSummarySlice(ctx, family, organizationID, from, middle); err != nil {
				return err
			}
			return q.rebuildOrganizationUsageSummarySlice(ctx, family, organizationID, middle, to)
		}
		return fmt.Errorf("build organization usage summary slice: %w", err)
	}
	if err := q.conn.Exec(ctx, "INSERT INTO billing_meter_daily_summary_parts SELECT * FROM billing_meter_daily_summary_attempt"); err != nil {
		return fmt.Errorf("retain successful usage summary attempt: %w", err)
	}
	if err := q.conn.Exec(ctx, "TRUNCATE TABLE billing_meter_daily_summary_attempt"); err != nil {
		return fmt.Errorf("clear successful usage summary attempt: %w", err)
	}
	return nil
}

func (q *Queries) consolidateUsageSummaryParts(ctx context.Context, family usageSummaryFamily, day time.Time) error {
	cursor := ""
	for {
		rows, err := q.conn.Query(ctx, fmt.Sprintf(`
			SELECT DISTINCT organization_id
			FROM billing_meter_daily_summary_parts
			WHERE organization_id > ? AND family = ? AND day = ? AND is_publication = 0
			ORDER BY organization_id
			LIMIT %d%s`, usageSummaryOrganizationPageSize, usageSummaryBuildSettings), cursor, family.name, day)
		if err != nil {
			return fmt.Errorf("enumerate usage summary part organizations: %w", err)
		}
		organizations := make([]string, 0, usageSummaryOrganizationPageSize)
		for rows.Next() {
			var organizationID string
			if err := rows.Scan(&organizationID); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan usage summary part organization: %w", err)
			}
			organizations = append(organizations, organizationID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read usage summary part organizations: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close usage summary part organizations: %w", err)
		}
		for _, organizationID := range organizations {
			query := `
				INSERT INTO billing_meter_daily_summaries_staging
				SELECT organization_id, family, reading_kind, facet, day, series_kind, series_key,
					max(label), unit, measurement_method, sum(quantity), sum(reading_count),
					toUInt8(0), toDateTime64(0, 9, 'UTC'), toDateTime64(0, 9, 'UTC')
				FROM billing_meter_daily_summary_parts
				WHERE organization_id = ? AND family = ? AND day = ? AND is_publication = 0
				GROUP BY organization_id, family, reading_kind, facet, day, series_kind, series_key, unit, measurement_method` + usageSummaryBuildSettings
			if err := q.conn.Exec(ctx, query, organizationID, family.name, day); err != nil {
				return fmt.Errorf("write consolidated organization usage summaries: %w", err)
			}
		}
		if len(organizations) < usageSummaryOrganizationPageSize {
			return nil
		}
		cursor = organizations[len(organizations)-1]
	}
}

func (q *Queries) clearUsageSummaryWorkTables(ctx context.Context) error {
	var result error
	if err := q.conn.Exec(ctx, "TRUNCATE TABLE billing_meter_daily_summaries_staging"); err != nil {
		result = errors.Join(result, fmt.Errorf("truncate usage summary staging: %w", err))
	}
	if err := q.conn.Exec(ctx, "TRUNCATE TABLE billing_meter_daily_summary_parts"); err != nil {
		result = errors.Join(result, fmt.Errorf("truncate usage summary parts: %w", err))
	}
	if err := q.conn.Exec(ctx, "TRUNCATE TABLE billing_meter_daily_summary_attempt"); err != nil {
		result = errors.Join(result, fmt.Errorf("truncate usage summary attempt: %w", err))
	}
	return result
}

func isUsageSummarySliceLimit(err error) bool {
	var exception *clickhouse.Exception
	if !errors.As(err, &exception) {
		return false
	}
	switch exception.Code {
	case 158, 159, 160, 241:
		return true
	default:
		return false
	}
}

func canSplitUsageSummarySlice(from, to time.Time) bool {
	return to.Sub(from) > usageSummaryMinimumSlice
}

func usageSummarySliceMiddle(from, to time.Time) time.Time {
	return from.Add(to.Sub(from) / 2)
}

func usageSummaryDateTime64(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05.999999999")
}
