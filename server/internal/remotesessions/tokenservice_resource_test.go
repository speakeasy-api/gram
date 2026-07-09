// tokenservice_resource_test.go drives the refresh path against an httptest
// token endpoint and asserts that the RFC 8707 resource passed to
// ResolveAccessToken is included in the refresh grant, and omitted when
// empty.

package remotesessions_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestResolveAccessToken_RefreshIncludesResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, clientID, subject := setupRefreshFixtureWithAudience(t, pgtype.Text{String: "", Valid: false}, &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "https://mcp.example.com/mcp")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Equal(t, "https://mcp.example.com/mcp", spy.form.Get("resource"), "refresh body must carry the caller's resource")
}

func TestResolveAccessToken_RefreshOmitsResourceWhenEmpty(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, clientID, subject := setupRefreshFixtureWithAudience(t, conv.ToPGText("https://api.example.com"), &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.False(t, spy.form.Has("resource"), "refresh body must omit resource when the caller passes none")
}
