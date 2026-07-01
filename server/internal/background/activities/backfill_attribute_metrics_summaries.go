package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type BackfillAttributeMetricsSummaries struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	chConn clickhouse.Conn
}

func NewBackfillAttributeMetricsSummaries(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn) *BackfillAttributeMetricsSummaries {
	return &BackfillAttributeMetricsSummaries{
		logger: logger.With(attr.SlogComponent("backfill_attribute_metrics_summaries")),
		db:     db,
		chConn: chConn,
	}
}

type BackfillAttributeMetricsSummariesParams struct {
	OrganizationID string `json:"organization_id"`
}

type BackfillAttributeMetricsSummariesResult struct {
	OrganizationID     string    `json:"organization_id"`
	ProjectCount       int       `json:"project_count"`
	UserLookupCount    int       `json:"user_lookup_count"`
	From               time.Time `json:"from"`
	Cutoff             time.Time `json:"cutoff"`
	RebuiltSummaryRows uint64    `json:"rebuilt_summary_rows"`
}

type attributeMetricsUserLookupItem struct {
	Email      string          `json:"email"`
	Attributes json.RawMessage `json:"attributes"`
}

func (b *BackfillAttributeMetricsSummaries) Do(ctx context.Context, params BackfillAttributeMetricsSummariesParams) (*BackfillAttributeMetricsSummariesResult, error) {
	if strings.TrimSpace(params.OrganizationID) == "" {
		return nil, temporal.NewNonRetryableApplicationError("organization_id is required", "InvalidBackfillRequest", nil)
	}

	cutoff := AttributeMetricsBackfillCutoff()
	from := cutoff.AddDate(0, 0, -AttributeMetricsBackfillWindowDays)
	logger := b.logger.With(
		attr.SlogOrganizationID(params.OrganizationID),
		slog.Time("from", from),
		slog.Time("cutoff", cutoff),
	)

	projects, err := projectsrepo.New(b.db).ListProjectsByOrganization(ctx, params.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list projects by organization: %w", err)
	}
	if len(projects) == 0 {
		return &BackfillAttributeMetricsSummariesResult{
			OrganizationID: params.OrganizationID,
			ProjectCount:   0,
			From:           from,
			Cutoff:         cutoff,
		}, nil
	}

	lookup, err := b.loadUserLookup(ctx, b.db, params.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("load user lookup: %w", err)
	}

	activity.RecordHeartbeat(ctx, "deleting existing attribute metrics summaries")

	lookupJSON, err := json.Marshal(lookup)
	if err != nil {
		return nil, fmt.Errorf("marshal user lookup: %w", err)
	}

	backfillParams := telemetryrepo.BackfillAttributeMetricsSummariesParams{
		ProjectIDs:     projectIDStrings(projects),
		UserLookupJSON: string(lookupJSON),
		FromUnixNano:   from.UnixNano(),
		CutoffUnixNano: cutoff.UnixNano(),
	}

	telemetryQueries := telemetryrepo.New(b.chConn)
	if err := telemetryQueries.DeleteAttributeMetricsSummariesBackfillWindow(ctx, backfillParams); err != nil {
		return nil, fmt.Errorf("delete existing attribute metrics summaries: %w", err)
	}

	if err := waitForAttributeMetricsSummariesBackfillWindowToClear(ctx, telemetryQueries, backfillParams); err != nil {
		return nil, err
	}

	activity.RecordHeartbeat(ctx, "inserting rebuilt attribute metrics summaries")

	if err := telemetryQueries.InsertAttributeMetricsSummariesBackfillWindow(ctx, backfillParams); err != nil {
		return nil, fmt.Errorf("insert rebuilt attribute metrics summaries: %w", err)
	}

	rebuiltRows, err := telemetryQueries.CountAttributeMetricsSummariesBackfillWindow(ctx, backfillParams)
	if err != nil {
		return nil, fmt.Errorf("count rebuilt attribute metrics summaries: %w", err)
	}

	logger.InfoContext(ctx, "backfilled attribute metrics summaries",
		slog.Int("project_count", len(projects)),
		slog.Int("user_lookup_count", len(lookup)),
		slog.Uint64("rebuilt_summary_rows", rebuiltRows),
	)

	return &BackfillAttributeMetricsSummariesResult{
		OrganizationID:     params.OrganizationID,
		ProjectCount:       len(projects),
		UserLookupCount:    len(lookup),
		From:               from,
		Cutoff:             cutoff,
		RebuiltSummaryRows: rebuiltRows,
	}, nil
}

func (b *BackfillAttributeMetricsSummaries) loadUserLookup(ctx context.Context, dbtx activitiesrepo.DBTX, organizationID string) ([]attributeMetricsUserLookupItem, error) {
	rows, err := activitiesrepo.New(dbtx).ListLatestDirectoryUsersForAttributeMetricsBackfill(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("query directory users: %w", err)
	}

	lookup := make([]attributeMetricsUserLookupItem, 0, len(rows))
	var email string
	for _, row := range rows {
		email = conv.NormalizeEmail(conv.FromPGTextOrEmpty[string](row.Email))
		if email == "" {
			continue
		}

		lookup = append(lookup, attributeMetricsUserLookupItem{
			Email:      email,
			Attributes: json.RawMessage(row.Attributes),
		})
	}

	return lookup, nil

}

func waitForAttributeMetricsSummariesBackfillWindowToClear(ctx context.Context, queries *telemetryrepo.Queries, params telemetryrepo.BackfillAttributeMetricsSummariesParams) error {
	deadline := time.Now().Add(attributeMetricsBackfillDeleteMaxWait)

	for {
		rows, err := queries.CountAttributeMetricsSummariesBackfillWindow(ctx, params)
		if err != nil {
			return fmt.Errorf("count attribute metrics summaries during delete wait: %w", err)
		}
		if rows == 0 {
			return nil
		}

		activity.RecordHeartbeat(ctx, fmt.Sprintf("waiting for attribute metrics summaries delete mutation: %d rows remaining", rows))

		sleepFor := attributeMetricsBackfillDeletePollInterval
		if remaining := time.Until(deadline); remaining <= 0 {
			return fmt.Errorf("attribute metrics summaries delete mutation did not clear backfill window within %s: %d rows remaining", attributeMetricsBackfillDeleteMaxWait, rows)
		} else if remaining < sleepFor {
			sleepFor = remaining
		}

		timer := time.NewTimer(sleepFor)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func projectIDStrings(projects []projectsrepo.Project) []string {
	ids := make([]string, 0, len(projects))
	for _, project := range projects {
		ids = append(ids, project.ID.String())
	}
	return ids
}

const (
	AttributeMetricsBackfillWindowDays         = 30
	attributeMetricsBackfillDeleteMaxWait      = 30 * time.Second
	attributeMetricsBackfillDeletePollInterval = 5 * time.Second
)

func AttributeMetricsBackfillCutoff() time.Time {
	return time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC)
}
