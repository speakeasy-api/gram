package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// insertIdentityLog writes one telemetry row attributed to a person. The
// identity columns are materialized from attributes, so they are set there.
func insertIdentityLog(t *testing.T, ctx context.Context, conn driver.Conn, projectID uuid.UUID, at time.Time, attributes string) {
	t.Helper()

	err := conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, 'INFO', '', NULL, NULL, ?, '{}', ?, 'claude-code:otel:logs', 'claude-code')
	`, uuid.NewString(), at.UnixNano(), at.UnixNano(), attributes, projectID)
	require.NoError(t, err)
}

func searchIdentities(t *testing.T, ctx context.Context, queries *repo.Queries, projectID uuid.UUID, window time.Time, groupBy, contains string) []repo.UserSummary {
	t.Helper()

	rows, err := queries.SearchUsers(ctx, repo.SearchUsersParams{
		ExcludedHookSources:  nil,
		GramProjectID:        projectID.String(),
		TimeStart:            window.Add(-time.Hour).UnixNano(),
		TimeEnd:              window.Add(time.Hour).UnixNano(),
		GramDeploymentID:     "",
		EventSource:          "",
		HookSource:           "",
		AccountType:          "",
		ExternalOrgID:        "",
		GroupBy:              groupBy,
		UserIDs:              nil,
		IdentityContains:     contains,
		SortOrder:            "desc",
		Cursor:               "",
		Limit:                10,
		MetricsDetail:        repo.MetricsDetailBasic,
		CanonicalIdentityOrg: "",
	})
	require.NoError(t, err)
	return rows
}

// TestSearchUsersIdentityContainsMatchesTheWholeSummary pins that the partial
// identity predicate is applied after grouping: a person matched by part of
// their email or by part of a raw user id folded into their summary is returned
// with every row they aggregate, and nobody else is.
func TestSearchUsersIdentityContainsMatchesTheWholeSummary(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	projectID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	earlier := now.Add(-30 * time.Minute)
	// One person under two row shapes: a hook row carrying both id and email,
	// and an earlier token-bearing row carrying only the email.
	insertIdentityLog(t, ctx, conn, projectID, now, `{"user.id":"user-42","user.email":"pat.rivera@example.com"}`)
	insertIdentityLog(t, ctx, conn, projectID, earlier, `{"user.email":"pat.rivera@example.com"}`)
	// A second person, and an external end user.
	insertIdentityLog(t, ctx, conn, projectID, now, `{"user.id":"user-77","user.email":"quinn.patel@example.com"}`)
	insertIdentityLog(t, ctx, conn, projectID, now, `{"gram.external_user.id":"customer-7781"}`)

	byEmail := searchIdentities(t, ctx, queries, projectID, now, "user_id", "RIVERA")
	require.Len(t, byEmail, 1)
	require.Equal(t, "pat.rivera@example.com", byEmail[0].UserID)
	require.Equal(t, []string{"user-42"}, byEmail[0].RawUserIDs)
	require.Equal(t, earlier.UnixNano(), byEmail[0].FirstSeenUnixNano, "the email-only row stays in the matched summary")
	require.Equal(t, now.UnixNano(), byEmail[0].LastSeenUnixNano)

	byRawID := searchIdentities(t, ctx, queries, projectID, now, "user_id", "user-4")
	require.Len(t, byRawID, 1)
	require.Equal(t, "pat.rivera@example.com", byRawID[0].UserID, "a partial user id finds the email-keyed summary that folded it")
	require.Equal(t, earlier.UnixNano(), byRawID[0].FirstSeenUnixNano, "matching on a raw id keeps the summary whole rather than only the rows carrying the id")

	both := searchIdentities(t, ctx, queries, projectID, now, "user_id", "pat")
	require.Len(t, both, 2, "pat.rivera and quinn.patel both contain the text")

	require.Empty(t, searchIdentities(t, ctx, queries, projectID, now, "user_id", "7781"), "an external id is not an internal identity")
	external := searchIdentities(t, ctx, queries, projectID, now, "external_user_id", "7781")
	require.Len(t, external, 1)
	require.Equal(t, "customer-7781", external[0].UserID)
	require.Empty(t, searchIdentities(t, ctx, queries, projectID, now, "user_id", "nobody"))
}
