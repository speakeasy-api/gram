package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/netgateway/wire"
)

// fakeNode implements Node with a scripted identity response.
type fakeNode struct {
	identity    *PeerIdentity
	identityErr error
}

func (f *fakeNode) Start(context.Context) error { return nil }
func (f *fakeNode) Listener(context.Context) (net.Listener, error) {
	return nil, errors.New("not used")
}
func (f *fakeNode) Status(context.Context) NodeStatus {
	return NodeStatus{Online: true, NetworkName: "", DNSName: "", NodeID: "", Err: ""}
}
func (f *fakeNode) Close(context.Context) error { return nil }
func (f *fakeNode) Identity(context.Context, string) (*PeerIdentity, error) {
	return f.identity, f.identityErr
}

func testIngressConfig(identityRequired bool) IngressConfig {
	return IngressConfig{
		ID:               uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID:   "org_test",
		Provider:         "tailscale",
		Hostname:         "gram-mcp",
		Tags:             []string{"tag:gram"},
		IdentityRequired: identityRequired,
		UpdatedAt:        time.Unix(0, 0),
		Credential:       Credential{Kind: CredentialKindAuthKey, AuthKey: "", OAuthClientID: "", OAuthClientSecret: ""},
	}
}

func TestProxySetsTrustHeadersAndStripsForgeries(t *testing.T) {
	t.Parallel()

	var got http.Header
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	node := &fakeNode{
		identity: &PeerIdentity{
			Login:       "user@example.com",
			DisplayName: "A User",
			Device:      "a-device",
			Tags:        []string{"tag:eng"},
			Caps:        []string{"example.com/cap/mcp"},
		},
		identityErr: nil,
	}
	handler := NewProxyHandler(testIngressConfig(true), node, upstreamURL, "sekrit", slog.New(slog.DiscardHandler))

	req := httptest.NewRequest(http.MethodGet, "http://gram-mcp.example.ts.net/mcp/foo", nil)
	// Forged inbound trust headers must never survive.
	req.Header.Set(wire.HeaderForwardToken, "forged")
	req.Header.Set(wire.HeaderUserLogin, "attacker@example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "sekrit", got.Get(wire.HeaderForwardToken))
	require.Equal(t, "11111111-1111-1111-1111-111111111111", got.Get(wire.HeaderIngressID))
	require.Equal(t, "tailscale", got.Get(wire.HeaderProvider))
	require.Equal(t, "user@example.com", got.Get(wire.HeaderUserLogin))
	require.Equal(t, "A User", got.Get(wire.HeaderUserName))
	require.Equal(t, "a-device", got.Get(wire.HeaderUserNode))
	require.Equal(t, "example.com/cap/mcp", got.Get(wire.HeaderUserCaps))
	// Host is preserved so issuer/metadata URLs derive as the private name.
	require.Equal(t, "gram-mcp.example.ts.net", gotHost)
}

func TestProxyRejectsUnattributedWhenIdentityRequired(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not reach upstream")
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	node := &fakeNode{identity: nil, identityErr: nil}
	handler := NewProxyHandler(testIngressConfig(true), node, upstreamURL, "sekrit", slog.New(slog.DiscardHandler))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/foo", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestProxyAllowsUnattributedWhenIdentityOptional(t *testing.T) {
	t.Parallel()

	var got http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	// Identity lookup failures degrade to unattributed rather than erroring
	// the request.
	node := &fakeNode{identity: nil, identityErr: errors.New("control plane hiccup")}
	handler := NewProxyHandler(testIngressConfig(false), node, upstreamURL, "sekrit", slog.New(slog.DiscardHandler))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/foo", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "sekrit", got.Get(wire.HeaderForwardToken))
	require.Empty(t, got.Get(wire.HeaderUserLogin))
}
