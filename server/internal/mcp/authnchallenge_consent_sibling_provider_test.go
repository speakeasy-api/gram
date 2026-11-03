package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// siblingProviderUpstream is a streamable HTTP MCP upstream that records the
// bearer each MCP request carried.
type siblingProviderUpstream struct {
	url string

	mu     sync.Mutex
	bearer []string
}

func (u *siblingProviderUpstream) bearers() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.bearer...)
}

func newSiblingProviderUpstream(t *testing.T) *siblingProviderUpstream {
	t.Helper()
	upstream := &siblingProviderUpstream{url: "", mu: sync.Mutex{}, bearer: nil}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// Metadata discovery probes are anonymous by design.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		upstream.mu.Lock()
		upstream.bearer = append(upstream.bearer, r.Header.Get("Authorization"))
		upstream.mu.Unlock()
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Method != "tools/list" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{
			"tools": []map[string]any{{"name": "list_issues", "inputSchema": map[string]any{"type": "object"}}},
		}})
	}))
	t.Cleanup(server.Close)
	upstream.url = server.URL
	return upstream
}

func attachConsentStreamableMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, issuerID uuid.UUID, slug, serverURL string) uuid.UUID {
	t.Helper()
	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           serverURL,
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
	return server.ID
}

// seedSiblingProviderConsent is a remote-backed endpoint whose user session
// issuer binds two providers: clientA signs in to this server's upstream and
// clientB to an unrelated one.
func seedSiblingProviderConsent(t *testing.T, tag string) (context.Context, consentActionFixture, *siblingProviderUpstream) {
	t.Helper()
	upstream := newSiblingProviderUpstream(t)
	ctx, fx := standaloneConsent(t, tag, upstream.url, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentStreamableMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, upstream.url)
	})
	fx.clientA = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-a", "", []uuid.UUID{fx.shared})
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-b", "", []uuid.UUID{fx.shared})
	return ctx, fx, upstream
}

func TestConsentInventory_UnconnectedSiblingProviderDoesNotBlockConnectedUpstream(t *testing.T) {
	t.Parallel()

	ctx, fx, upstream := seedSiblingProviderConsent(t, "sibling-unconnected")
	grant(t, ctx, fx, fx.clientA, upstream.url)

	w := serveConsentMCPRequest(t, ctx, fx.ti, fx.endpoint, fx.stateID, "csrf-token", uuid.NewString(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	require.Contains(t, w.Body.String(), "list_issues")
	require.Equal(t, []string{"Bearer token-" + fx.clientA.String()}, upstream.bearers(), "only this server's own credential is forwarded")
}

func TestConsentInventory_UnconnectedOwnProviderStillConflicts(t *testing.T) {
	t.Parallel()

	ctx, fx, upstream := seedSiblingProviderConsent(t, "sibling-own-missing")
	// Only the unrelated provider is connected.
	grant(t, ctx, fx, fx.clientB, consentUpstreamB)

	requireConsentInventoryConflict(t, fx)
	require.Empty(t, upstream.bearers(), "a sibling's credential is never sent to this upstream")
}
