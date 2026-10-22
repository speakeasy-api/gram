package remotesessions_test

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/organization_remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestListClientSessionsFiltersAndStableOrdering(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuerID := createRemoteIssuer(t, ctx, ti, "filter-provider", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "filter-issuer")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuer.String(), "filter-client")
	now := time.Now().UTC().Truncate(time.Second)
	var ids []string
	for i, subject := range []string{"refreshable", "terminal-expired", "no-deadline"} {
		row := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(subject), userIssuer.String(), clientID)
		params := repo.SetRemoteSessionListFixtureParams{
			ID: row.ID, OrganizationID: conv.ToPGText(activeOrganizationID(t, ctx)),
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}
		if i < 2 {
			params.AccessExpiresAt = pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
			params.RefreshExpiresAt = pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}
			params.RefreshTokenEncrypted = conv.ToPGText("inert-refresh-token")
		}
		if i == 1 {
			params.AuthorizationExpiresAt = pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true}
		}
		require.NoError(t, repo.New(ti.conn).SetRemoteSessionListFixture(ctx, params))
		ids = append(ids, row.ID.String())
	}
	result, err := ti.service.ListClientSessions(ctx, &gen.ListClientSessionsPayload{ClientID: clientID, Status: new("active"), Sort: new("expiry")})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, ids[0], result.Items[0].ID, "expired access token is refreshable, expiry ordering uses the usable grant deadline")
	require.Equal(t, ids[2], result.Items[1].ID, "unknown expiry sorts last")
	expired, err := ti.service.ListClientSessions(ctx, &gen.ListClientSessionsPayload{ClientID: clientID, Status: new("expired")})
	require.NoError(t, err)
	require.Len(t, expired.Items, 1)
	require.Equal(t, ids[1], expired.Items[0].ID)
	expected := slices.Clone(ids)
	slices.Sort(expected)
	request := gen.ListClientSessionsPayload{ClientID: clientID, Limit: new(1), Sort: new("oldest")}
	var actual []string
	for {
		page, err := ti.service.ListClientSessions(ctx, &request)
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		actual = append(actual, page.Items[0].ID)
		if page.NextCursor == nil {
			break
		}
		request.Cursor = page.NextCursor
	}
	require.Equal(t, expected, actual)
	request.Search = new("refreshable")
	_, err = ti.service.ListClientSessions(ctx, &request)
	requireOopsCode(t, err, oops.CodeBadRequest)
	request.Cursor = nil
	filtered, err := ti.service.ListClientSessions(ctx, &request)
	require.NoError(t, err)
	require.Len(t, filtered.Items, 1)
	require.Equal(t, ids[0], filtered.Items[0].ID)
	request.Search = new("%")
	none, err := ti.service.ListClientSessions(ctx, &request)
	require.NoError(t, err)
	require.Empty(t, none.Items)
	request.Search = nil
	request.CreatedBefore = new(now.Format(time.RFC3339))
	none, err = ti.service.ListClientSessions(ctx, &request)
	require.NoError(t, err)
	require.Empty(t, none.Items, "upper bound is exclusive")
	// Existing callers can keep following UUID cursors without opting into sorting.
	legacy, err := ti.service.ListClientSessions(ctx, &gen.ListClientSessionsPayload{ClientID: clientID, Limit: new(1)})
	require.NoError(t, err)
	require.Equal(t, expected[2], *legacy.NextCursor)
	second, err := ti.service.ListClientSessions(ctx, &gen.ListClientSessionsPayload{ClientID: clientID, Limit: new(2), Cursor: legacy.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 2)
	require.Nil(t, second.NextCursor)
}
