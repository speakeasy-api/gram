package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// searchUsersExcludingHostedPage runs one page of the internal people search the
// way Platform MCP's search_users runs it: Speakeasy-hosted hook sources excluded,
// newest activity first, one person per page. cursorLastSeen is the boundary the
// previous page observed; zero leaves the repository to re-derive it.
func searchUsersExcludingHostedPage(t *testing.T, ctx context.Context, queries *repo.Queries, projectID uuid.UUID, window time.Time, cursor string, cursorLastSeen int64) []repo.UserSummary {
	t.Helper()

	rows, err := queries.SearchUsers(ctx, repo.SearchUsersParams{
		ExcludedHookSources:    billing.GramHostedHookSourceNames(),
		GramProjectID:          projectID.String(),
		TimeStart:              window.Add(-time.Hour).UnixNano(),
		TimeEnd:                window.Add(time.Hour).UnixNano(),
		GramDeploymentID:       "",
		EventSource:            "",
		HookSource:             "",
		AccountType:            "",
		ExternalOrgID:          "",
		GroupBy:                "user_id",
		UserIDs:                nil,
		IdentityContains:       "example.com",
		SortOrder:              "desc",
		Cursor:                 cursor,
		CursorLastSeenUnixNano: cursorLastSeen,
		Limit:                  1,
		MetricsDetail:          repo.MetricsDetailBasic,
		CanonicalIdentityOrg:   "",
	})
	require.NoError(t, err)
	return rows
}

// TestSearchUsersPaginatesPastAnExcludedLaterEvent pins that a search excluding
// Speakeasy-hosted hook sources advances past a person whose excluded rows run later
// than their qualifying ones.
//
// The cursor's boundary used to be re-derived inside the repository by looking
// the person's max(time_unix_nano) up again, in a subquery that applies neither
// the outer query's window nor its excluded hook sources. A person placed on
// page 1 by a qualifying event was therefore compared against the later
// timestamp of an event the search had excluded, still satisfied the HAVING
// clause, and came back on page 2 — and on every page after it, until the
// traversal cap, never reaching anyone older.
func TestSearchUsersPaginatesPastAnExcludedLaterEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	projectID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	// The three moments of the reproduction: A's qualifying event, A's excluded
	// one after it, and B's qualifying event before both.
	aQualifying := now.Add(-10 * time.Minute)
	aExcluded := now.Add(-1 * time.Minute)
	bQualifying := now.Add(-20 * time.Minute)

	hostedSource := billing.GramHostedHookSourceNames()[0]
	insertIdentityLog(t, ctx, conn, projectID, aQualifying, `{"user.id":"user-42","user.email":"pat.rivera@example.com","gram.hook.source":"claude-code"}`)
	insertIdentityLog(t, ctx, conn, projectID, aExcluded, `{"user.id":"user-42","user.email":"pat.rivera@example.com","gram.hook.source":"`+hostedSource+`"}`)
	insertIdentityLog(t, ctx, conn, projectID, bQualifying, `{"user.id":"user-77","user.email":"quinn.patel@example.com","gram.hook.source":"claude-code"}`)

	page1 := searchUsersExcludingHostedPage(t, ctx, queries, projectID, now, "", 0)
	require.Len(t, page1, 1)
	require.Equal(t, "pat.rivera@example.com", page1[0].UserID, "the more recent qualifying event leads")
	require.Equal(t, aQualifying.UnixNano(), page1[0].LastSeenUnixNano,
		"last_seen is the qualifying event, not the excluded one that follows it")

	// The fix: the boundary page 1 actually showed is what page 2 resumes after.
	page2 := searchUsersExcludingHostedPage(t, ctx, queries, projectID, now, page1[0].UserID, page1[0].LastSeenUnixNano)
	require.Len(t, page2, 1)
	require.Equal(t, "quinn.patel@example.com", page2[0].UserID,
		"page 2 must reach the next person rather than repeat the first")
	require.Equal(t, bQualifying.UnixNano(), page2[0].LastSeenUnixNano)

	// And the third page ends the traversal rather than cycling.
	require.Empty(t, searchUsersExcludingHostedPage(t, ctx, queries, projectID, now, page2[0].UserID, page2[0].LastSeenUnixNano))

	// The regression itself, kept as the reason the boundary is sealed: asking
	// the repository to re-derive it returns the same person again, because the
	// lookup sees the excluded row this search does not.
	repeated := searchUsersExcludingHostedPage(t, ctx, queries, projectID, now, page1[0].UserID, 0)
	require.Len(t, repeated, 1)
	require.Equal(t, "pat.rivera@example.com", repeated[0].UserID,
		"a re-derived boundary reads the excluded later event and repeats page 1")
}
