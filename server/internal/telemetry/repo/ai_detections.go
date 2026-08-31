package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// UpsertAIDetectionParams is one (organization, target, device, user, signal)
// detection observation from a device-agent AI scan.
type UpsertAIDetectionParams struct {
	OrganizationID string
	TargetID       string
	DeviceSerial   string
	UserEmail      string
	Signal         string
	Category       string
	Version        string
	SeenAt         time.Time
	UpdatedAt      time.Time
}

// AIDetectionRow is the merged state of one detection key, resolved across
// ReplacingMergeTree row versions with argMax/min/max the way the shadow MCP
// inventory reads resolve theirs.
type AIDetectionRow struct {
	TargetID     string    `ch:"target_id"`
	DeviceSerial string    `ch:"device_serial"`
	UserEmail    string    `ch:"user_email"`
	Signal       string    `ch:"signal"`
	Category     string    `ch:"category"`
	Version      string    `ch:"version"`
	FirstSeen    time.Time `ch:"first_seen"`
	LastSeen     time.Time `ch:"last_seen"`
	// UpdatedAt is aliased max_updated_at in queries: an alias equal to the
	// base column name is substituted into sibling argMax aggregates and
	// trips ILLEGAL_AGGREGATION.
	UpdatedAt time.Time `ch:"max_updated_at"`
}

type aiDetectionUpsert struct {
	OrganizationID string
	TargetID       string
	DeviceSerial   string
	UserEmail      string
	Signal         string
	Category       string
	Version        string
	FirstSeen      time.Time
	LastSeen       time.Time
	UpdatedAt      time.Time
}

type aiDetectionScope struct {
	OrganizationID string
	DeviceSerial   string
	UserEmail      string
}

func aiDetectionKey(organizationID, deviceSerial, userEmail, targetID, signal string) string {
	return organizationID + "\x00" + deviceSerial + "\x00" + userEmail + "\x00" + targetID + "\x00" + signal
}

// UpsertAIDetections merges the given detections with any existing rows (one
// batched lookup per reporting device+user scope) and writes them with a
// synchronous insert: the read-merge-write preserves first_seen, so the
// insert must not be deferred by ClickHouse async insert buffering.
func (q *Queries) UpsertAIDetections(ctx context.Context, args []UpsertAIDetectionParams) error {
	if len(args) == 0 {
		return nil
	}

	upserts := make(map[string]*aiDetectionUpsert, len(args))
	for _, arg := range args {
		if arg.OrganizationID == "" || arg.TargetID == "" || arg.UserEmail == "" || arg.Signal == "" {
			continue
		}
		seenAt := arg.SeenAt
		if seenAt.IsZero() {
			seenAt = time.Now()
		}
		updatedAt := arg.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}

		key := aiDetectionKey(arg.OrganizationID, arg.DeviceSerial, arg.UserEmail, arg.TargetID, arg.Signal)
		upsert := upserts[key]
		if upsert == nil {
			upserts[key] = &aiDetectionUpsert{
				OrganizationID: arg.OrganizationID,
				TargetID:       arg.TargetID,
				DeviceSerial:   arg.DeviceSerial,
				UserEmail:      arg.UserEmail,
				Signal:         arg.Signal,
				Category:       arg.Category,
				Version:        arg.Version,
				FirstSeen:      seenAt.UTC(),
				LastSeen:       seenAt.UTC(),
				UpdatedAt:      updatedAt.UTC(),
			}
			continue
		}
		if arg.Category != "" {
			upsert.Category = arg.Category
		}
		if arg.Version != "" {
			upsert.Version = arg.Version
		}
		if seenAt.UTC().Before(upsert.FirstSeen) {
			upsert.FirstSeen = seenAt.UTC()
		}
		if seenAt.UTC().After(upsert.LastSeen) {
			upsert.LastSeen = seenAt.UTC()
		}
		if updatedAt.UTC().After(upsert.UpdatedAt) {
			upsert.UpdatedAt = updatedAt.UTC()
		}
	}

	if len(upserts) == 0 {
		return nil
	}

	// Fetch existing rows with one batched lookup per (org, device, user)
	// scope — a scan report always lands in exactly one scope, so the usual
	// case is a single lookup covering every target in the report.
	targetsByScope := make(map[aiDetectionScope][]string)
	for _, upsert := range upserts {
		scope := aiDetectionScope{
			OrganizationID: upsert.OrganizationID,
			DeviceSerial:   upsert.DeviceSerial,
			UserEmail:      upsert.UserEmail,
		}
		targetsByScope[scope] = append(targetsByScope[scope], upsert.TargetID)
	}
	for scope, targetIDs := range targetsByScope {
		existingRows, err := q.listAIDetectionRowsByScope(ctx, scope, targetIDs)
		if err != nil {
			return err
		}
		for _, existing := range existingRows {
			upsert, ok := upserts[aiDetectionKey(scope.OrganizationID, scope.DeviceSerial, scope.UserEmail, existing.TargetID, existing.Signal)]
			if !ok {
				continue
			}
			if upsert.Category == "" {
				upsert.Category = existing.Category
			}
			if upsert.Version == "" {
				upsert.Version = existing.Version
			}
			if existing.FirstSeen.Before(upsert.FirstSeen) {
				upsert.FirstSeen = existing.FirstSeen
			}
			if existing.LastSeen.After(upsert.LastSeen) {
				upsert.LastSeen = existing.LastSeen
			}
			// The merged row must dominate the state read above or the
			// argMax(_, updated_at) reads resolve against a stale row.
			if !upsert.UpdatedAt.After(existing.UpdatedAt) {
				upsert.UpdatedAt = existing.UpdatedAt.Add(time.Nanosecond)
			}
		}
	}

	rows := make([]*aiDetectionUpsert, 0, len(upserts))
	for _, upsert := range upserts {
		rows = append(rows, upsert)
	}
	if err := q.insertAIDetectionRows(ctx, rows); err != nil {
		return fmt.Errorf("upserting ai detections: %w", err)
	}

	return nil
}

// insertAIDetectionRows writes detection rows synchronously (async_insert=0):
// every caller is a read-merge-write cycle whose next read must see the rows
// written here. Timestamps are sent as fromUnixTimestamp64Nano expressions
// because clickhouse-go's positional binder truncates time.Time arguments to
// whole seconds, which collapses distinct updated_at versions written within
// the same second and makes the argMax(_, updated_at) reads nondeterministic.
func (q *Queries) insertAIDetectionRows(ctx context.Context, rows []*aiDetectionUpsert) error {
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert": 0,
	}))

	builder := sq.Insert("ai_detections").
		Columns(
			"organization_id",
			"target_id",
			"device_serial",
			"user_email",
			"signal",
			"category",
			"version",
			"first_seen",
			"last_seen",
			"updated_at",
		)

	for _, row := range rows {
		builder = builder.Values(
			row.OrganizationID,
			row.TargetID,
			row.DeviceSerial,
			row.UserEmail,
			row.Signal,
			row.Category,
			row.Version,
			squirrel.Expr("fromUnixTimestamp64Nano(?)", row.FirstSeen.UTC().UnixNano()),
			squirrel.Expr("fromUnixTimestamp64Nano(?)", row.LastSeen.UTC().UnixNano()),
			squirrel.Expr("fromUnixTimestamp64Nano(?)", row.UpdatedAt.UTC().UnixNano()),
		)
	}

	query, queryArgs, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building ai detection insert query: %w", err)
	}

	if err := q.conn.Exec(ctx, query, queryArgs...); err != nil {
		return fmt.Errorf("inserting ai detection rows: %w", err)
	}

	return nil
}

func (q *Queries) listAIDetectionRowsByScope(ctx context.Context, scope aiDetectionScope, targetIDs []string) ([]AIDetectionRow, error) {
	if len(targetIDs) == 0 {
		return []AIDetectionRow{}, nil
	}

	sb := sq.Select(
		"target_id",
		"device_serial",
		"user_email",
		"signal",
		"argMax(category, updated_at) AS category",
		"argMaxIf(version, updated_at, version != '') AS version",
		"min(first_seen) AS first_seen",
		"max(last_seen) AS last_seen",
		"max(updated_at) AS max_updated_at",
	).
		From("ai_detections").
		Where("organization_id = ?", scope.OrganizationID).
		Where("device_serial = ?", scope.DeviceSerial).
		// This is a storage-key lookup for the read-merge-write cycle, not an
		// analytics filter: user_email is part of the ai_detections sort key
		// and rows must be matched exactly as written, or merges would move
		// rows between keys as the identity map changes.
		Where("user_email = ?", scope.UserEmail). //nolint:glint // exact sort-key lookup, not identity-scoped analytics
		Where(squirrel.Eq{"target_id": targetIDs}).
		GroupBy("organization_id", "target_id", "device_serial", "user_email", "signal") //nolint:glint // grouping by the exact sort key to resolve row versions, not identity bucketing

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building ai detection scope lookup query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying ai detection scope lookup: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]AIDetectionRow, 0, len(targetIDs))
	for rows.Next() {
		var row AIDetectionRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning ai detection scope lookup row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating ai detection scope lookup rows: %w", err)
	}

	return result, nil
}

// ListAIDetectionSummariesParams filters the per-target aggregation. Both
// filters are optional; empty slices mean no restriction.
type ListAIDetectionSummariesParams struct {
	OrganizationID string
	// Categories restricts to detections stored with these categories.
	Categories []string
	// UserEmails restricts to detections attributed to these normalized
	// emails (the team-filter pushdown).
	UserEmails []string
	// CanonicalIdentityOrg enables the identity fold for the UserEmails
	// filter: set it to the org id when the canonical identity fold is
	// rolled out to the org, "" otherwise (see canonical_identity.go).
	CanonicalIdentityOrg string
}

// AIDetectionSummaryRow aggregates one target's detections across an
// organization. Aggregating over unmerged ReplacingMergeTree versions is
// sound here: the merge preserves min(first_seen)/max(last_seen), so stale
// versions never widen the true range, and the uniq counts key on columns
// that are part of the sort key.
type AIDetectionSummaryRow struct {
	TargetID string `ch:"target_id"`
	// Category is aliased detected_category in the query: an alias equal to
	// the base column name would be substituted into the WHERE category
	// filter and trip ILLEGAL_AGGREGATION.
	Category    string    `ch:"detected_category"`
	UserCount   uint64    `ch:"user_count"`
	DeviceCount uint64    `ch:"device_count"`
	Signals     []string  `ch:"signals"`
	FirstSeen   time.Time `ch:"first_seen"`
	LastSeen    time.Time `ch:"last_seen"`
}

// ListAIDetectionSummaries aggregates detections per target for one
// organization, most recently seen first.
func (q *Queries) ListAIDetectionSummaries(ctx context.Context, arg ListAIDetectionSummariesParams) ([]AIDetectionSummaryRow, error) {
	sb := sq.Select(
		"target_id",
		"argMax(category, updated_at) AS detected_category",
		"uniqExact(user_email) AS user_count",
		"uniqExactIf(device_serial, device_serial != '') AS device_count",
		"groupUniqArray(signal) AS signals",
		"min(first_seen) AS first_seen",
		"max(last_seen) AS last_seen",
	).
		From("ai_detections").
		Where("organization_id = ?", arg.OrganizationID)

	if len(arg.Categories) > 0 {
		sb = sb.Where(squirrel.Eq{"category": arg.Categories})
	}
	if len(arg.UserEmails) > 0 {
		// Fold the team filter through the identity map where rolled out, so
		// detections stored under an employee's linked alias emails still
		// match their directory email.
		if orgLit := canonicalIdentityOrgLiteral(arg.CanonicalIdentityOrg); orgLit != "" {
			sb = sb.Where(canonicalEmailFilter(orgLit, "user_email", arg.UserEmails))
		} else {
			sb = sb.Where(squirrel.Eq{"user_email": arg.UserEmails}) //nolint:glint // fold not rolled out to this org; both sides are lowercase-normalized
		}
	}

	sb = sb.GroupBy("organization_id", "target_id").
		OrderBy("last_seen DESC", "target_id ASC")

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building ai detection summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying ai detection summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []AIDetectionSummaryRow{}
	for rows.Next() {
		var row AIDetectionSummaryRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning ai detection summary row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating ai detection summary rows: %w", err)
	}

	return result, nil
}

// InsertAIScanReceiptParams is one scan receipt: proof a device ran an AI
// scan, posted even when nothing matched.
type InsertAIScanReceiptParams struct {
	OrganizationID    string
	DeviceSerial      string
	UserEmail         string
	ScanStartedAt     time.Time
	ScanCompletedAt   time.Time
	TargetListVersion int32
	MatchCount        uint32
	ReceivedAt        time.Time
}

// InsertAIScanReceipts appends scan receipts with a synchronous insert
// (async_insert=0) so a caller's follow-up read sees them.
func (q *Queries) InsertAIScanReceipts(ctx context.Context, args []InsertAIScanReceiptParams) error {
	if len(args) == 0 {
		return nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert": 0,
	}))

	builder := sq.Insert("ai_scan_receipts").
		Columns(
			"organization_id",
			"device_serial",
			"user_email",
			"scan_started_at",
			"scan_completed_at",
			"target_list_version",
			"match_count",
			"received_at",
		)

	for _, arg := range args {
		receivedAt := arg.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = time.Now()
		}
		builder = builder.Values(
			arg.OrganizationID,
			arg.DeviceSerial,
			arg.UserEmail,
			squirrel.Expr("fromUnixTimestamp64Nano(?)", arg.ScanStartedAt.UTC().UnixNano()),
			squirrel.Expr("fromUnixTimestamp64Nano(?)", arg.ScanCompletedAt.UTC().UnixNano()),
			arg.TargetListVersion,
			arg.MatchCount,
			squirrel.Expr("fromUnixTimestamp64Nano(?)", receivedAt.UTC().UnixNano()),
		)
	}

	query, queryArgs, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building ai scan receipt insert query: %w", err)
	}

	if err := q.conn.Exec(ctx, query, queryArgs...); err != nil {
		return fmt.Errorf("inserting ai scan receipt rows: %w", err)
	}

	return nil
}

// ListAIScanReceiptsParams filters receipt reads. DeviceSerial is optional.
type ListAIScanReceiptsParams struct {
	OrganizationID string
	DeviceSerial   string
	Limit          int
}

// AIScanReceiptRow is one stored scan receipt.
type AIScanReceiptRow struct {
	DeviceSerial      string    `ch:"device_serial"`
	UserEmail         string    `ch:"user_email"`
	ScanStartedAt     time.Time `ch:"scan_started_at"`
	ScanCompletedAt   time.Time `ch:"scan_completed_at"`
	TargetListVersion int32     `ch:"target_list_version"`
	MatchCount        uint32    `ch:"match_count"`
	ReceivedAt        time.Time `ch:"received_at"`
}

// ListAIScanReceipts lists stored scan receipts for one organization, most
// recent first.
func (q *Queries) ListAIScanReceipts(ctx context.Context, arg ListAIScanReceiptsParams) ([]AIScanReceiptRow, error) {
	limit := arg.Limit
	if limit <= 0 {
		limit = 100
	}

	sb := sq.Select(
		"device_serial",
		"user_email",
		"scan_started_at",
		"scan_completed_at",
		"target_list_version",
		"match_count",
		"received_at",
	).
		From("ai_scan_receipts").
		Where("organization_id = ?", arg.OrganizationID)

	if arg.DeviceSerial != "" {
		sb = sb.Where("device_serial = ?", arg.DeviceSerial)
	}

	sb = sb.OrderBy("received_at DESC").Limit(uint64(limit))

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building ai scan receipt list query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying ai scan receipts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []AIScanReceiptRow{}
	for rows.Next() {
		var row AIScanReceiptRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning ai scan receipt row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating ai scan receipt rows: %w", err)
	}

	return result, nil
}
