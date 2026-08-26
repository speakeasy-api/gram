// A successful refresh stamps the resource it actually used onto legacy NULL
// rows, never overwrites a stored binding, and leaves NULL when underivable.

package remotesessions_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-stamp", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

	require.False(t, getRemoteSessionRow(t, ctx, ti, clientID, subject).Resource.Valid, "fixture must seed a legacy NULL-resource row")

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "https://mcp.example.com/mcp")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, "https://mcp.example.com/mcp", conv.FromPGTextOrEmpty[string](sess.Resource), "refresh must stamp the resource it used onto a legacy NULL row")
	// The stamped value is exactly the wire value the refresh grant carried.
	require.Equal(t, spy.form.Get("resource"), conv.FromPGTextOrEmpty[string](sess.Resource), "the stored binding must equal the form resource the grant used")
}

func TestResolveAccessToken_RefreshNeverOverwritesStoredResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-keep-stored", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

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
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-underivable", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, sess.Resource.Valid, "an empty derivation must leave the legacy row's resource NULL")
}

// A refresh that loses the CAS (the row rotated mid-POST) must be a full
// no-op: no resource stamped, no token overwritten, winner's token adopted.
func TestResolveAccessToken_RefreshCASLossStampsNothing(t *testing.T) {
	t.Parallel()

	refreshArrived := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRefresh) }) }
	t.Cleanup(release)

	// Block mid-refresh so the test can rotate the row before the CAS write.
	handler := func(w http.ResponseWriter, r *http.Request) {
		close(refreshArrived)
		<-releaseRefresh
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-access","token_type":"Bearer","expires_in":3600,"refresh_token":"refreshed-refresh"}`))
	}
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-cas-loss", pgtype.Text{String: "", Valid: false}, handler)
	before := getRemoteSessionRow(t, ctx, ti, clientID, subject)

	type resolveResult struct {
		token string
		err   error
	}
	resolved := make(chan resolveResult, 1)
	go func() {
		tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "https://mcp.example.com/mcp")
		resolved <- resolveResult{token: tok, err: err}
	}()

	<-refreshArrived

	// Concurrent winner rotates tokens + updated_at, leaving resource NULL.
	enc := testenv.NewEncryptionClient(t)
	winnerAccess, err := enc.Encrypt([]byte("winner-access"))
	require.NoError(t, err)
	winnerRefresh, err := enc.Encrypt([]byte("winner-refresh"))
	require.NoError(t, err)
	winner, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   before.UserSessionIssuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  winnerAccess,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText(winnerRefresh),
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                before.Scopes,
		Resource:              pgtype.Text{},
	})
	require.NoError(t, err)
	require.Equal(t, before.ID, winner.ID, "the winner rotates the row in place")

	release()

	got := <-resolved
	require.NoError(t, got.err)
	require.Equal(t, "winner-access", got.token, "a CAS loss adopts the concurrent winner's token")

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, sess.Resource.Valid, "a losing refresh write must not stamp its resource")
	require.Equal(t, winner.UpdatedAt.Time, sess.UpdatedAt.Time, "the losing CAS write must not touch the row")
	require.Equal(t, winner.AccessTokenEncrypted, sess.AccessTokenEncrypted, "the winner's access token must survive")
	require.Equal(t, winner.RefreshTokenEncrypted.String, sess.RefreshTokenEncrypted.String, "the winner's refresh token must survive")
}

// A refresh rejected with invalid_grant clears only the refresh grant; the
// failed refresh must not stamp the fallback resource it attempted with.
func TestResolveAccessToken_InvalidGrantLeavesResourceNull(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	}
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-invalid-grant", pgtype.Text{String: "", Valid: false}, handler)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "https://mcp.example.com/mcp")
	require.NoError(t, err)
	require.Empty(t, tok)

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, sess.Resource.Valid, "a failed refresh must not stamp a resource")
	require.False(t, sess.RefreshTokenEncrypted.Valid, "invalid_grant must clear the dead refresh grant")
	require.False(t, sess.RefreshExpiresAt.Valid)
}

// A second refresh after the stamp keeps the same value: COALESCE is a no-op
// and the grant replays the now-stored binding.
func TestResolveAccessToken_RefreshBackfillIsIdempotent(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-idempotent", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

	const used = "https://mcp.example.com/mcp"
	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, used)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	stamped := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, used, conv.FromPGTextOrEmpty[string](stamped.Resource))

	// Age the fresh access token out so the next resolve refreshes again.
	require.NoError(t, testrepo.New(ti.conn).ExpireRemoteSessionAccessTokenFixture(ctx, stamped.ID))

	tok, err = mgr.ResolveAccessToken(ctx, clientID, subject, used)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, used, conv.FromPGTextOrEmpty[string](sess.Resource), "a second refresh must keep the stamped value")
	require.Equal(t, used, spy.form.Get("resource"), "the second grant must replay the stamped binding")
}

// The live-E2E blip: with a second bound credential present, per-upstream
// routing depends on each entry's Resource, so the request whose refresh
// backfills a legacy NULL resource must itself return the backfilled value.
// Returning the pre-refresh empty resource fails routing closed
// (legacy_null_resource) on exactly the request that healed the row, leaving
// only the NEXT request to succeed.
func TestResolveAccessTokens_RefreshingRequestCarriesBackfilledResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-same-request", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	row := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, row.Resource.Valid, "fixture must seed a legacy NULL-resource row")

	refreshedClient, err := repo.New(ti.conn).GetRemoteSessionClientWithIssuerByID(ctx, clientID)
	require.NoError(t, err)

	// Second bound credential on a distinct remote issuer with a still-valid
	// token — the multi-upstream shape that makes routing depend on Resource.
	enc := testenv.NewEncryptionClient(t)
	secondClient, secondIssuer := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, row.UserSessionIssuerID, authCtx.ActiveOrganizationID, "rsi-backfill-same-request")
	secondAccess, err := enc.Encrypt([]byte("second-token"))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   row.UserSessionIssuerID,
		RemoteSessionClientID: secondClient,
		AccessTokenEncrypted:  secondAccess,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		Scopes:                []string{},
		Resource:              conv.ToPGText("https://second.example.com/mcp"),
	})
	require.NoError(t, err)

	const derived = "https://mcp.example.com/mcp"
	// The refreshing client's sole attached upstream, so RefreshNow derives
	// this resource: the value under test is never a caller-supplied string.
	seedRemoteMCPServerForIssuer(t, ctx, ti, row.UserSessionIssuerID, "backfill-same-request-mcp", derived)

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, row.UserSessionIssuerID, subject)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, derived, spy.form.Get("resource"), "the refresh grant must have carried the derived resource")

	require.Equal(t, remotesessions.UpstreamToken{Token: "refreshed-access", Resource: derived, RemoteSessionClientID: clientID}, tokens[refreshedClient.RemoteSessionIssuerID],
		"the request that backfills the resource must itself carry it, not the pre-refresh empty value")
	require.Equal(t, remotesessions.UpstreamToken{Token: "second-token", Resource: "https://second.example.com/mcp", RemoteSessionClientID: secondClient}, tokens[secondIssuer])

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, derived, conv.FromPGTextOrEmpty[string](sess.Resource), "the backfill must also have been persisted")
}

// seedRemoteMCPServerForIssuer binds one remote MCP server at mcpURL to the
// user session issuer so FallbackResourceForClient derives it unambiguously.
func seedRemoteMCPServerForIssuer(t *testing.T, ctx context.Context, ti *testInstance, userIssuerID uuid.UUID, slug, mcpURL string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	remoteServer, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     *authCtx.ProjectID,
		TransportType: "sse",
		Url:           mcpURL,
	})
	require.NoError(t, err)

	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
	})
	require.NoError(t, err)
}

// attachClientUpstream gives one client a private user session issuer carrying
// a single remote MCP server, so the client derives mcpURL and its siblings on
// the shared issuer derive nothing from it.
func attachClientUpstream(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, slug, mcpURL string) {
	t.Helper()

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+slug)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   issuerID,
	}))
	seedRemoteMCPServerForIssuer(t, ctx, ti, issuerID, slug+"-mcp", mcpURL)
}

// Two clients bound to one issuer, each fronting a different upstream: the
// backfill must stamp the refreshed client's own derived resource. Before the
// derivation was per-client one endpoint-level resource reached every client,
// so refreshing one could stamp a sibling's upstream onto its row — a
// permanent wrong-audience binding that then routes every later request.
func TestResolveAccessTokens_BackfillStampsTheRefreshedClientsOwnResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-wrong-client", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	row := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, row.Resource.Valid, "fixture must seed a legacy NULL-resource row")

	q := repo.New(ti.conn)
	refreshedClient, err := q.GetRemoteSessionClientWithIssuerByID(ctx, clientID)
	require.NoError(t, err)

	// The sibling holds a still-valid token minted for its own upstream, so
	// nothing but a misattribution can put its resource on the refreshed row.
	const siblingResource = "https://sibling.example.com/mcp"
	enc := testenv.NewEncryptionClient(t)
	siblingClient, siblingIssuer := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, row.UserSessionIssuerID, authCtx.ActiveOrganizationID, "rsi-backfill-wrong-client")
	siblingAccess, err := enc.Encrypt([]byte("sibling-token"))
	require.NoError(t, err)
	_, err = q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   row.UserSessionIssuerID,
		RemoteSessionClientID: siblingClient,
		AccessTokenEncrypted:  siblingAccess,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		Scopes:                []string{},
		Resource:              conv.ToPGText(siblingResource),
	})
	require.NoError(t, err)

	// Each client reaches its upstream through its own issuer, so the two
	// derivations differ and the shared issuer derives nothing: any resource
	// that is not the refreshed client's own is visibly the wrong one.
	const derived = "https://own.example.com/mcp"
	attachClientUpstream(t, ctx, ti, clientID, "backfill-wrong-client-own", derived)
	attachClientUpstream(t, ctx, ti, siblingClient, "backfill-wrong-client-sibling", siblingResource)

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, row.UserSessionIssuerID, subject)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, derived, spy.form.Get("resource"), "the refresh grant must carry the refreshed client's own derived resource")

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, derived, conv.FromPGTextOrEmpty[string](sess.Resource), "the backfill must stamp the refreshed client's own resource, never a sibling's")

	require.Equal(t, remotesessions.UpstreamToken{Token: "refreshed-access", Resource: derived, RemoteSessionClientID: clientID}, tokens[refreshedClient.RemoteSessionIssuerID])
	require.Equal(t, remotesessions.UpstreamToken{Token: "sibling-token", Resource: siblingResource, RemoteSessionClientID: siblingClient}, tokens[siblingIssuer])

	siblingSess := getRemoteSessionRow(t, ctx, ti, siblingClient, subject)
	require.Equal(t, siblingResource, conv.FromPGTextOrEmpty[string](siblingSess.Resource), "the sibling's own binding must survive a neighbour's backfill")
}

// The scheduled sweep converges through the same CAS write: refreshing a
// NULL-resource row stamps the client-derived fallback it sent upstream.
func TestRemoteSessionRefreshActivity_SweepBackfillsNullResource(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, _, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "backfill-sweep", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	row := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.False(t, row.Resource.Valid)

	const mcpURL = "https://mcp.example.com/mcp"
	seedRemoteMCPServerForIssuer(t, ctx, ti, row.UserSessionIssuerID, "backfill-sweep-mcp", mcpURL)

	// Make the row a due keepalive candidate: stale, org-enforced, live Gram session.
	require.NoError(t, repo.New(ti.conn).SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        row.ID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
	}))
	seedGramSession(t, ctx, ti, subject, row.UserSessionIssuerID, "backfill-sweep", 24*time.Hour)
	enableOrgAutoRefreshFeature(t, ctx, ti, authCtx.ActiveOrganizationID, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	refresher := remotesessions.NewRefreshService(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, testenv.NewEncryptionClient(t), policy, cache.NoopCache)
	activity := activities.NewRemoteSessionRefresh(testenv.NewLogger(t), ti.conn, refresher)

	res, err := activity.Do(ctx, activities.RefreshRemoteSessionInput{
		SessionID:      row.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.False(t, res.RateLimited)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refresh_token", spy.form.Get("grant_type"), "the sweep must have executed the refresh grant")

	sess := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.Equal(t, mcpURL, conv.FromPGTextOrEmpty[string](sess.Resource), "the sweep must stamp the client-derived fallback")
	require.Equal(t, spy.form.Get("resource"), conv.FromPGTextOrEmpty[string](sess.Resource), "the stored binding must equal the form resource the grant used")
}
