package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// memberRefreshOutageFixture is a gateway member whose user holds an expired
// connection with a live refresh token, and a provider that can be taken down.
type memberRefreshOutageFixture struct {
	ti       *testInstance
	metaSlug string
	bearer   string
	subject  urn.SessionSubject
	clientID uuid.UUID
	down     *atomic.Bool
	upstream *atomic.Value

	// refreshes counts POSTs to the provider's token endpoint.
	refreshes *atomic.Int32
}

func (f memberRefreshOutageFixture) endCooldown(t *testing.T, ctx context.Context) {
	t.Helper()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	require.NoError(t, cache.NewRedisCacheAdapter(client).Delete(ctx, remotesessions.RefreshCooldownKey(f.subject, f.clientID)))
}

func newMemberRefreshOutageFixture(t *testing.T, ctx context.Context, ti *testInstance, resource func(upstreamURL string) string) memberRefreshOutageFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	down := &atomic.Bool{}
	refreshes := &atomic.Int32{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"refreshed-token","token_type":"Bearer","expires_in":3600,"refresh_token":"rotated-refresh"}`)
	}))
	t.Cleanup(provider.Close)

	lastAuthorization := &atomic.Value{}
	lastAuthorization.Store("")
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuthorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(member.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-outage-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "outage-"+uuid.NewString()[:8], provider.URL, []uuid.UUID{issuerID})
	remoteIssuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", member.URL, conv.ToNullUUID(remoteIssuerID), 0)
	subject := createTestUser(t, ctx, ti, "meta-outage-user-"+uuid.NewString())

	access, err := ti.enc.Encrypt([]byte("expired-token"))
	require.NoError(t, err)
	refresh, err := ti.enc.Encrypt([]byte("live-refresh"))
	require.NoError(t, err)
	stored := resource(member.URL)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   issuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  access,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted: conv.ToPGText(refresh),
		RefreshExpiresAt:      pgtype.Timestamptz{Valid: false},
		Scopes:                []string{},
		Resource:              pgtype.Text{String: stored, Valid: stored != ""},
	})
	require.NoError(t, err)

	return memberRefreshOutageFixture{
		ti:       ti,
		metaSlug: metaSlug,
		bearer:   mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject),
		subject:  subject,
		clientID: clientID,
		down:     down,
		upstream: lastAuthorization,

		refreshes: refreshes,
	}
}

func (f memberRefreshOutageFixture) connectionStatus(t *testing.T, ctx context.Context) metamcp.ConnectionStatusResult {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolConnectionStatus, "arguments": map[string]any{}})
	resp, err := servePublicHTTP(t, ctx, f.ti, f.metaSlug, body, f.bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())

	var res struct {
		StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(decodeRPCResponse(t, resp)["result"], &res))
	return res.StructuredContent
}

func TestServePublic_MetaEndpoint_RefreshOutageIsRetryableThenRecovers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	f.down.Store(true)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "Retry in about 30 seconds")
	require.Contains(t, text, "If it keeps happening, reconnect at")
	require.Contains(t, text, "/connect/first-party?server=member", "reconnecting stays offered, since a provider can answer 5xx for one bad token")
	require.NotContains(t, text, "not connected")
	require.Empty(t, f.upstream.Load(), "an outage never turns into an anonymous call")
	require.EqualValues(t, 1, f.refreshes.Load())

	status := f.connectionStatus(t, ctx)
	require.Len(t, status.Servers, 1)
	require.Equal(t, metamcp.SignInUnavailable, status.Servers[0].Status)
	require.NotContains(t, status.Text(), "To sign in, open")
	require.Contains(t, status.Servers[0].Detail, "reconnect at")
	require.EqualValues(t, 1, f.refreshes.Load(), "retries inside the cooldown never reach the provider")

	stored, err := remotesessions_repo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            f.subject,
		RemoteSessionClientID: f.clientID,
	})
	require.NoError(t, err)
	require.True(t, stored.RefreshTokenEncrypted.Valid, "the outage must not delete the refresh token")

	f.down.Store(false)
	f.endCooldown(t, ctx)
	text, isError = metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "rejected the user's credential", "after the outage the member receives the refreshed token")
	require.Equal(t, "Bearer refreshed-token", f.upstream.Load())
	require.Equal(t, metamcp.SignInConnected, f.connectionStatus(t, ctx).Servers[0].Status)
}

// A grant from before resources were recorded still belongs to its member.
func TestServePublic_MetaEndpoint_RefreshOutageWithoutRecordedResourceIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(string) string { return "" })
	f.down.Store(true)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "Retry in about")
	require.NotContains(t, text, "not connected")
}
