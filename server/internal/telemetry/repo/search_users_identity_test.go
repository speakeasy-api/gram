package repo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
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
		ExcludedHookSources:    nil,
		GramProjectID:          projectID.String(),
		TimeStart:              window.Add(-time.Hour).UnixNano(),
		TimeEnd:                window.Add(time.Hour).UnixNano(),
		GramDeploymentID:       "",
		EventSource:            "",
		HookSource:             "",
		AccountType:            "",
		ExternalOrgID:          "",
		GroupBy:                groupBy,
		UserIDs:                nil,
		IdentityContains:       contains,
		SortOrder:              "desc",
		Cursor:                 "",
		CursorLastSeenUnixNano: 0,
		Limit:                  10,
		MetricsDetail:          repo.MetricsDetailBasic,
		CanonicalIdentityOrg:   "",
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

// foldedUserSummary runs one person's summary the way Platform MCP's
// get_user_metrics_summary runs it: Gram-hosted hook sources excluded, with
// whichever of the two identity scopes the caller supplies.
func foldedUserSummary(t *testing.T, ctx context.Context, queries *repo.Queries, projectID uuid.UUID, window time.Time, identity repo.UserIdentity, canonical repo.CanonicalUserIdentity) *repo.MetricsSummaryRow {
	t.Helper()

	summary, err := queries.GetUserMetricsSummary(ctx, repo.GetUserMetricsSummaryParams{
		ExcludedHookSources: billing.GramHostedHookSourceNames(),
		GramProjectID:       projectID.String(),
		TimeStart:           window.Add(-time.Hour).UnixNano(),
		TimeEnd:             window.Add(time.Hour).UnixNano(),
		User:                identity,
		CanonicalUser:       canonical,
		ExternalUserID:      "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	})
	require.NoError(t, err)
	return summary
}

// TestGetUserMetricsSummaryFindsAnUnmappedEmailsRows pins that a per-person
// summary agrees with the grouped search about a person whose email the
// identity map does not carry.
//
// The search finds such a person because its group key folds an unmapped email
// back to the recorded address: joinGet returns ” for a missing key, and
// canonicalEmailExpr coalesces that to lowerUTF8 of the column. The summary's
// canonical scope has no such fallback. Its id-keyed arm compares user_id
// against joinGet('canonical_user_id'), which is ” for an unmapped email and
// is guarded by user_id != ”, while its other arm matches only rows with an
// empty user_id. A person whose every row carries a user id therefore matched
// nothing, and the summary reported no activity for someone the search had just
// returned as active. The user ids resolved from the rows themselves are what
// close the gap, so the two scopes are unioned rather than one replacing the
// other.
func TestGetUserMetricsSummaryFindsAnUnmappedEmailsRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	projectID := uuid.New()
	// An organization on the fold whose map carries no entry for this address.
	// Its own id, so that absence holds however the shared identity_map is
	// populated for anyone else.
	orgID := "org" + strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC().Truncate(time.Second)

	const (
		email   = "pat.rivera@example.com"
		theirID = "user-42"
	)
	// Every one of their rows carries a user id, which is what puts them out of
	// reach of both canonical arms. Distinct moments, so the summary's activity
	// window is identifiably theirs.
	theirFirst := now.Add(-40 * time.Minute)
	theirLast := now.Add(-5 * time.Minute)
	insertIdentityLog(t, ctx, conn, projectID, theirFirst, `{"user.id":"`+theirID+`","user.email":"`+email+`","gram.hook.source":"claude-code"}`)
	insertIdentityLog(t, ctx, conn, projectID, theirLast, `{"user.id":"`+theirID+`","user.email":"`+email+`","gram.hook.source":"claude-code"}`)
	// Somebody else, outside that span on the early side: a scope that reads
	// too widely moves first_seen onto their row and is caught below.
	somebodyElse := now.Add(-50 * time.Minute)
	insertIdentityLog(t, ctx, conn, projectID, somebodyElse, `{"user.id":"user-77","user.email":"quinn.patel@example.com","gram.hook.source":"claude-code"}`)

	// What the search says: one summary, keyed by the recorded address, naming
	// the user id their rows carry.
	found, err := queries.SearchUsers(ctx, repo.SearchUsersParams{
		ExcludedHookSources:    billing.GramHostedHookSourceNames(),
		GramProjectID:          projectID.String(),
		TimeStart:              now.Add(-time.Hour).UnixNano(),
		TimeEnd:                now.Add(time.Hour).UnixNano(),
		GramDeploymentID:       "",
		EventSource:            "",
		HookSource:             "",
		AccountType:            "",
		ExternalOrgID:          "",
		GroupBy:                "user_id",
		UserIDs:                nil,
		IdentityContains:       "rivera",
		SortOrder:              "desc",
		Cursor:                 "",
		CursorLastSeenUnixNano: 0,
		Limit:                  10,
		MetricsDetail:          repo.MetricsDetailBasic,
		CanonicalIdentityOrg:   orgID,
	})
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, email, found[0].UserID, "an unmapped email keys the summary at the recorded address")
	require.Equal(t, []string{theirID}, found[0].RawUserIDs)
	require.Equal(t, theirLast.UnixNano(), found[0].LastSeenUnixNano)
	require.Equal(t, theirFirst.UnixNano(), found[0].FirstSeenUnixNano)

	canonical := repo.CanonicalUserIdentity{OrgID: orgID, UserID: "", EmailLower: email}
	require.True(t, canonical.Enabled())

	// The regression itself, kept as the reason the union exists: the fold
	// alone resolves this address to no owner id and names none of their rows.
	foldOnly := foldedUserSummary(t, ctx, queries, projectID, now, repo.UserIdentity{UserIDs: nil, Emails: nil}, canonical)
	require.Zero(t, foldOnly.LastSeenUnixNano, "an unmapped email resolves to no owner, so the fold alone observes nothing")
	require.Zero(t, foldOnly.TotalToolCalls)

	// The fold together with the identities resolved from the rows: the summary
	// now reports the activity span the search reported.
	resolved := repo.UserIdentity{UserIDs: []string{theirID}, Emails: []string{email}}
	summary := foldedUserSummary(t, ctx, queries, projectID, now, resolved, canonical)
	require.Equal(t, found[0].LastSeenUnixNano, summary.LastSeenUnixNano, "the summary's last activity is the one the search showed")
	require.Equal(t, found[0].FirstSeenUnixNano, summary.FirstSeenUnixNano,
		"and its first is too, so the scope reached their earlier row without reaching anyone else's")
	require.NotEqual(t, somebodyElse.UnixNano(), summary.FirstSeenUnixNano, "the other person's earlier row stays out of this summary")
}
