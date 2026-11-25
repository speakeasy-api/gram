package platformmcp

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestDirectRemoteInspectionInternalCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cidr string
		ip   string
		ok   bool
	}{
		{name: "configured", cidr: "10.23.45.67/32", ip: "10.23.45.67", ok: true},
		{name: "unset", ip: "10.23.45.67"},
		{name: "unexpected private DNS", cidr: "10.23.45.67/32", ip: "10.23.45.68"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inspector, methods := directRemoteProtocolFixture(t, "auth401")
			inspector.inspector.policy = internalCatalogTestPolicy(t, tc.cidr, tc.ip)

			// The existing fixture supplies TLS transport; this exercises the
			// inspection and OAuth metadata URL guards with a default policy.
			result, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, "authentication_required", result.Authentication)
				require.Equal(t, "available_dcr", result.OAuthDiscovery)
				require.NotEmpty(t, methods())
			} else {
				require.ErrorIs(t, err, ErrDirectRemoteRejected)
				require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(err))
				require.Empty(t, methods(), "blocked DNS must prevent MCP requests")
			}
		})
	}
}

func TestDirectRemoteInternalCatalogRejectsPrivateRedirect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host == "private.example.test" {
			t.Error("redirect to another private address reached upstream")
		}
		http.Redirect(w, r, "https://private.example.test/mcp", http.StatusTemporaryRedirect)
	}))
	option, err := guardian.WithHostedMCPFrontEndCIDR("10.23.45.67/32")
	require.NoError(t, err)
	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "private.example.test" {
			return []net.IP{net.ParseIP("10.23.45.68")}, nil
		}
		return []net.IP{net.ParseIP("10.23.45.67")}, nil
	}})
	inspector.inspector.policy = guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), option, guardian.WithResolver(resolver))

	_, err = inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.ErrorIs(t, err, ErrDirectRemoteRejected)
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(err))
	require.Positive(t, requests.Load(), "the configured destination must pass validation before redirecting")
}

func TestRemoteMCPReadinessInternalCatalogDeniesPrivateDNS(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cidr string
		ip   string
	}{
		{name: "unset", ip: "10.23.45.67"},
		{name: "unexpected private DNS", cidr: "10.23.45.67/32", ip: "10.23.45.68"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			prober := &RemoteMCPReadinessProber{
				logger: slog.New(slog.NewTextHandler(&logs, nil)),
				policy: internalCatalogTestPolicy(t, tc.cidr, tc.ip),
			}

			state, evidence := prober.probe(t.Context(), "https://remote.example.test/mcp", nil, "")
			require.Equal(t, ReadinessUnreachable, state)
			require.Equal(t, "probe_unreachable", evidence)
			require.Contains(t, logs.String(), guardian.ErrBlockedIP.Error(), "failure must come from Guardian, not the network")
		})
	}
}

func internalCatalogTestPolicy(t *testing.T, cidr, ip string) *guardian.Policy {
	t.Helper()
	option, err := guardian.WithHostedMCPFrontEndCIDR(cidr)
	require.NoError(t, err)
	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP(ip)}, nil
	}})
	return guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), option, guardian.WithResolver(resolver))
}
