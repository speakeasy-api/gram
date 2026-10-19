package identitychaining

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGovernor_ResourceMetadataRestrictsAuthorizationServer(t *testing.T) {
	t.Parallel()
	for _, allowed := range []bool{true, false} {
		name := "unadvertised issuer"
		if allowed {
			name = "advertised issuer"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/oauth-protected-resource" {
					http.NotFound(w, r)
					return
				}
				issuer := "https://another-issuer.example.test"
				if allowed {
					issuer = f.sel.issuer
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": "https://" + r.Host, "authorization_servers": []string{issuer}})
			}))
			t.Cleanup(server.Close)
			bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, server.URL, remotesessions.PreparationStateReady)
			f.setBinding(t, remotesessions.PreparationStateUnlinked)
			f.req.UpstreamResource = server.URL + "/mcp"
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
			require.NoError(t, err)
			manager := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.db, f.chainer.enc, policy, nil, nil, nil)
			f.chainer.SetResourceResolver(manager.ResolveEMAResource)
			selected, outcome := f.chainer.selectBinding(t.Context(), f.chainer.logger, f.req)
			if allowed {
				require.True(t, outcome.Succeeded(), "%+v", outcome)
				require.Equal(t, server.URL, selected.resource)
			} else {
				require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
				require.Empty(t, selected.resource)
			}
			require.Equal(t, server.URL+"/mcp", f.req.UpstreamResource, "transport identity is preserved")
		})
	}
}

func TestGovernor_InvalidMetadataRetainsOnlyBoundOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, endpoint string
		bound          bool
	}{
		{"bound origin alias", "https://bound.example.test/mcp", true},
		{"equivalent default port", "https://bound.example.test:443/mcp", true},
		{"unrelated origin", "https://unrelated.example.test/mcp", false},
		{"different port", "https://bound.example.test:444/mcp", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			f.setBinding(t, remotesessions.PreparationStateUnlinked)
			bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, "https://bound.example.test", remotesessions.PreparationStateReady)
			f.req.UpstreamResource = tc.endpoint
			f.chainer.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
				return protectedresource.EndpointResource{}, protectedresource.ErrEndpointResourceInvalid
			})
			_, outcome := f.chainer.selectBinding(t.Context(), f.chainer.logger, f.req)
			if tc.bound {
				require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
			} else {
				require.Equal(t, ReasonNotApplicable, outcome.Reason)
			}
			require.Equal(t, tc.bound, f.chainer.Governs(t.Context(), f.req))
		})
	}
}

func TestGovernor_DiscoveryFailureGovernsOnlyABoundOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, endpoint string
		bound          bool
	}{
		{"bound origin", "https://bound.example.test/mcp", true},
		{"unrelated origin", "https://unrelated.example.test/mcp", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			f.setBinding(t, remotesessions.PreparationStateUnlinked)
			bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, "https://bound.example.test", remotesessions.PreparationStateReady)
			f.req.UpstreamResource = tc.endpoint
			f.chainer.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
				return protectedresource.EndpointResource{}, errors.New("metadata probe timed out")
			})
			_, outcome := f.chainer.selectBinding(t.Context(), f.chainer.logger, f.req)
			if tc.bound {
				require.Equal(t, ReasonTransientFailure, outcome.Reason, "a failed probe is never read as no metadata")
				require.True(t, outcome.Retryable)
			} else {
				require.Equal(t, ReasonNotApplicable, outcome.Reason, "an endpoint without a binding keeps its interactive behavior")
			}
			require.Equal(t, tc.bound, f.chainer.Governs(t.Context(), f.req), "a bound origin reports retryable instead of asking to reconnect")
			_, outcome = f.chainer.Acquire(t.Context(), f.req)
			require.Equal(t, tc.bound, outcome.Applicable())
		})
	}
}

func TestGovernor_CandidateLookupFailureKeepsTheInteractivePath(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.chainer.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
		f.db.Close()
		return protectedresource.EndpointResource{}, protectedresource.ErrEndpointResourceInvalid
	})
	_, outcome := f.chainer.selectBinding(t.Context(), f.chainer.logger, f.req)
	require.Equal(t, ReasonNotApplicable, outcome.Reason, "unknown configuration never fails an endpoint closed")
}

func TestSameIssuerComparesCanonicalForms(t *testing.T) {
	t.Parallel()
	require.True(t, sameIssuer("https://auth.vendor.example", "https://auth.vendor.example/"))
	require.True(t, sameIssuer("https://AUTH.vendor.example:443", "https://auth.vendor.example"))
	require.False(t, sameIssuer("https://auth.vendor.example/tenant-a", "https://auth.vendor.example/tenant-b"))
	require.False(t, sameIssuer("https://auth.vendor.example", "https://other.vendor.example"))
	require.False(t, sameIssuer("not an issuer", "https://auth.vendor.example"))
}
