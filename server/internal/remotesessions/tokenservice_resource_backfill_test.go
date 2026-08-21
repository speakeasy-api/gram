// A successful refresh stamps the resource it actually used onto legacy NULL
// rows, never overwrites a stored binding, and leaves NULL when underivable.

package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func getRemoteSessionRow(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, subject urn.SessionSubject) repo.RemoteSession {
	t.Helper()

	sess, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	require.NoError(t, err)
	return sess
}

func TestResolveAccessToken_RefreshBackfillsNullResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithAudience(t, pgtype.Text{String: "", Valid: false}, &spy)

	require.False(t, getRemoteSessionRow(t, ctx, ti, clientID, subject).Resource.Valid, "fixture must seed a legacy NULL-resource row")

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "https://mcp.example.com/mcp")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, "https://mcp.example.com/mcp", conv.FromPGTextOrEmpty[string](sess.Resource), "refresh must stamp the resource it used onto a legacy NULL row")
}

func TestResolveAccessToken_RefreshNeverOverwritesStoredResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithAudience(t, pgtype.Text{String: "", Valid: false}, &spy)

	const stored = "https://stored.example.com/mcp"
	err := testrepo.New(ti.conn).SetRemoteSessionResourceFixture(ctx, testrepo.SetRemoteSessionResourceFixtureParams{
		Resource:              conv.ToPGText(stored),
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	require.NoError(t, err)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "https://drifted.example.com/mcp")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Equal(t, stored, spy.form.Get("resource"), "refresh grant must replay the stored binding, not the caller's fallback")
	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, stored, conv.FromPGTextOrEmpty[string](sess.Resource), "a stored resource binding must never be overwritten")
}

func TestResolveAccessToken_RefreshLeavesResourceNullWhenUnderivable(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithAudience(t, pgtype.Text{String: "", Valid: false}, &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, sess.Resource.Valid, "an empty derivation must leave the legacy row's resource NULL")
}
