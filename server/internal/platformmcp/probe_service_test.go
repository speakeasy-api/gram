package platformmcp

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var probeServiceTestTime = time.Unix(1_700_000_100, 0)

const probeServiceTestKeyMaterial = "probe-service-test-key"

// probeFixtureMCPHandler serves a real streamable-HTTP MCP server declaring
// the given tools, the same shape a vendor's hosted server would present.
func probeFixtureMCPHandler(toolNames ...string) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "probe-fixture", Version: "2.3.4"}, nil)
	for _, name := range toolNames {
		mcp.AddTool(server, &mcp.Tool{
			Name:        name,
			Description: "fixture tool " + name,
		}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

// withWellKnownNotFound answers the RFC 9728/8414 well-known probes with a
// clean 404 — the "publishes no OAuth metadata" outcome — and forwards
// everything else.
func withWellKnownNotFound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type probeRequestCounter struct {
	count atomic.Int64
}

func (c *probeRequestCounter) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.count.Add(1)
		next.ServeHTTP(w, r)
	})
}

func startProbeFixture(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server
}

func probeFixturePolicy(t *testing.T, fixture *httptest.Server) *guardian.Policy {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	return policy
}

func newTestRemoteProbeService(t *testing.T, policy *guardian.Policy) *RemoteProbeService {
	t.Helper()
	service, err := NewRemoteProbeService(testenv.NewLogger(t), policy, probeServiceTestKeyMaterial, allowBudget())
	require.NoError(t, err)
	service.now = func() time.Time { return probeServiceTestTime }
	return service
}

func TestRemoteProbeServiceVerifiesCleanHandshake(t *testing.T) {
	t.Parallel()

	fixture := startProbeFixture(t, withWellKnownNotFound(probeFixtureMCPHandler("alpha", "beta")))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))
	principal := registrationServicePrincipal()

	result, err := service.Probe(t.Context(), principal, fixture.URL)
	require.NoError(t, err)

	require.Equal(t, fixture.URL, result.Evidence.NormalizedURL)
	require.Equal(t, "probe-fixture", result.Evidence.ServerName)
	require.Equal(t, "2.3.4", result.Evidence.ServerVersion)
	require.Equal(t, 2, result.Evidence.ToolCount)
	require.ElementsMatch(t, []string{"alpha", "beta"}, result.Evidence.ToolNames)
	require.Equal(t, ProbeAuthPostureOpen, result.Evidence.AuthPosture)
	require.Contains(t, result.Evidence.Gaps, probeGapNoOAuthMetadata)
	require.Equal(t, probeServiceTestTime.Add(probeReceiptTTL), result.ReceiptExpiresAt)

	// The receipt binds the probing caller to exactly this URL and exactly
	// this evidence: the registration tool redeems it without re-validating.
	codec, err := newProbeReceiptCodec(probeServiceTestKeyMaterial)
	require.NoError(t, err)
	receipt, err := codec.Decode(result.Receipt, principal, probeServiceTestTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, fixture.URL, receipt.NormalizedURL)
	digest, err := probeEvidenceDigest(result.Evidence)
	require.NoError(t, err)
	require.Equal(t, digest, receipt.ProbeDigest)
}

func TestRemoteProbeServiceVerifiesAuthWalledServer(t *testing.T) {
	t.Parallel()

	fixture := startProbeFixture(t, withWellKnownNotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))
	principal := registrationServicePrincipal()

	result, err := service.Probe(t.Context(), principal, fixture.URL)
	require.NoError(t, err)

	require.Equal(t, ProbeAuthPostureAuthRequired, result.Evidence.AuthPosture)
	require.Empty(t, result.Evidence.ServerName)
	require.Zero(t, result.Evidence.ToolCount)
	require.Empty(t, result.Evidence.ToolNames)
	require.Contains(t, result.Evidence.Gaps, probeGapInitializeDeclined)
	require.Contains(t, result.Evidence.Gaps, probeGapNoOAuthMetadata)

	codec, err := newProbeReceiptCodec(probeServiceTestKeyMaterial)
	require.NoError(t, err)
	receipt, err := codec.Decode(result.Receipt, principal, probeServiceTestTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, fixture.URL, receipt.NormalizedURL)
}

func TestRemoteProbeServiceDiscoversOAuthMetadataOnAuthWalledServer(t *testing.T) {
	t.Parallel()

	// The fixture needs its own origin inside the metadata it publishes, and
	// the origin is only known after the listener starts; the handler reads it
	// lazily and no request arrives before the store below.
	var origin atomic.Value
	fixture := startProbeFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base, _ := origin.Load().(string)
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              base,
				"authorization_servers": []string{base},
			})
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 base,
				"authorization_endpoint": base + "/authorize",
				"token_endpoint":         base + "/token",
				"registration_endpoint":  base + "/register",
			})
		default:
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
	}))
	origin.Store(fixture.URL)
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.NoError(t, err)

	require.Equal(t, ProbeAuthPostureOAuthDiscovered, result.Evidence.AuthPosture)
	require.Contains(t, result.Evidence.Gaps, probeGapInitializeDeclined)
	require.NotContains(t, result.Evidence.Gaps, probeGapNoOAuthMetadata)
	require.NotContains(t, result.Evidence.Gaps, probeGapOAuthIncomplete)
	require.NotEmpty(t, result.Receipt)
}

// A bare 401/403 with no WWW-Authenticate challenge is what any ordinary
// protected HTTP endpoint answers; it proves nothing MCP-shaped and must not
// verify, in either auth-rejection branch.
func TestRemoteProbeServiceRefusesBareAuthRejectionWithoutChallenge(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		fixture := startProbeFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "denied", status)
		}))
		service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

		result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
		require.ErrorIs(t, err, ErrProbeNotMCPServer, "status %d", status)
		require.Empty(t, result.Receipt, "status %d", status)
	}
}

// A 403 carrying a WWW-Authenticate challenge is the spec's other typed auth
// rejection; it verifies exactly like the 401 form.
func TestRemoteProbeServiceVerifiesForbiddenServerWithChallenge(t *testing.T) {
	t.Parallel()

	fixture := startProbeFixture(t, withWellKnownNotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
		http.Error(w, "forbidden", http.StatusForbidden)
	})))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))
	principal := registrationServicePrincipal()

	result, err := service.Probe(t.Context(), principal, fixture.URL)
	require.NoError(t, err)

	require.Equal(t, ProbeAuthPostureAuthRequired, result.Evidence.AuthPosture)
	require.Contains(t, result.Evidence.Gaps, probeGapInitializeDeclined)
	require.NotEmpty(t, result.Receipt)
}

// When the handshake already verified the server, a bare 401 on tools/list is
// not auth evidence — the listing is simply unusable, recorded as that gap
// rather than as an auth_required posture.
func TestRemoteProbeServiceTreatsBareAuthRejectedToolsListAsUnusableListing(t *testing.T) {
	t.Parallel()

	mcpHandler := probeFixtureMCPHandler("alpha")
	fixture := startProbeFixture(t, withWellKnownNotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if bytes.Contains(body, []byte(`"tools/list"`)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mcpHandler.ServeHTTP(w, r)
	})))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.NoError(t, err)

	require.Equal(t, "probe-fixture", result.Evidence.ServerName)
	require.Equal(t, ProbeAuthPostureOpen, result.Evidence.AuthPosture, "a bare rejection is not auth evidence")
	require.Zero(t, result.Evidence.ToolCount)
	require.Contains(t, result.Evidence.Gaps, probeGapToolListFailed)
	require.NotContains(t, result.Evidence.Gaps, probeGapToolsDeclined)
	require.NotEmpty(t, result.Receipt)
}

// An enormous tools/list must fail as a bounded size refusal before it is
// materialized: the handshake already verified the server, so the oversized
// listing is an explicit evidence gap, never a memory sink.
func TestRemoteProbeServiceBoundsOversizedToolsListResponse(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "probe-fixture", Version: "2.3.4"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "giant",
		Description: strings.Repeat("x", maxProbeResponseBytes+(1<<20)),
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	fixture := startProbeFixture(t, withWellKnownNotFound(handler))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.NoError(t, err)

	require.Equal(t, "probe-fixture", result.Evidence.ServerName)
	require.Zero(t, result.Evidence.ToolCount)
	require.Empty(t, result.Evidence.ToolNames)
	require.Contains(t, result.Evidence.Gaps, probeGapToolListTooLarge)
	require.NotEmpty(t, result.Receipt)
}

func TestRemoteProbeServiceRecordsDeclinedUnauthenticatedToolsList(t *testing.T) {
	t.Parallel()

	mcpHandler := probeFixtureMCPHandler("alpha")
	fixture := startProbeFixture(t, withWellKnownNotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if bytes.Contains(body, []byte(`"tools/list"`)) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mcpHandler.ServeHTTP(w, r)
	})))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.NoError(t, err)

	// The handshake completed, so identity was observed — but the server is
	// auth-walled for actual use and the declined listing is an explicit gap,
	// never a silent zero.
	require.Equal(t, "probe-fixture", result.Evidence.ServerName)
	require.Equal(t, ProbeAuthPostureAuthRequired, result.Evidence.AuthPosture)
	require.Zero(t, result.Evidence.ToolCount)
	require.Contains(t, result.Evidence.Gaps, probeGapToolsDeclined)
	require.NotEmpty(t, result.Receipt)
}

func TestRemoteProbeServiceClipsToolNameEvidence(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, maxProbeEvidenceToolNames+10)
	for i := range maxProbeEvidenceToolNames + 10 {
		names = append(names, fmt.Sprintf("tool-%03d", i))
	}
	fixture := startProbeFixture(t, withWellKnownNotFound(probeFixtureMCPHandler(names...)))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.NoError(t, err)

	// The exact declared count is always reported; only the name listing is
	// bounded, and the clip is disclosed rather than silent.
	require.Equal(t, len(names), result.Evidence.ToolCount)
	require.Len(t, result.Evidence.ToolNames, maxProbeEvidenceToolNames)
	require.Contains(t, result.Evidence.Gaps, fmt.Sprintf("tool names clipped to the first %d of %d declared tools", maxProbeEvidenceToolNames, len(names)))
}

func TestRemoteProbeServiceRefusesNonMCPServer(t *testing.T) {
	t.Parallel()

	fixture := startProbeFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>a perfectly ordinary website</body></html>")
	}))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.ErrorIs(t, err, ErrProbeNotMCPServer)
	require.NotErrorIs(t, err, ErrProbeUnreachable)
	require.Empty(t, result.Receipt)
}

func TestRemoteProbeServiceRefusesUnreachableTarget(t *testing.T) {
	t.Parallel()

	fixture := startProbeFixture(t, probeFixtureMCPHandler())
	policy := probeFixturePolicy(t, fixture)
	target := fixture.URL
	fixture.Close()
	service := newTestRemoteProbeService(t, policy)

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), target)
	require.ErrorIs(t, err, ErrProbeUnreachable)
	require.Empty(t, result.Receipt)
}

func TestRemoteProbeServiceRefusesEgressDeniedTargetWithoutDetail(t *testing.T) {
	t.Parallel()

	counter := &probeRequestCounter{}
	fixture := startProbeFixture(t, counter.wrap(probeFixtureMCPHandler("alpha")))
	// The default guardian policy blocks loopback, so the fixture is exactly
	// the kind of private-range target the probe must refuse.
	service := newTestRemoteProbeService(t, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.ErrorIs(t, err, ErrProbeEgressDenied)
	require.Empty(t, result.Receipt)
	// The refusal names no address, host, or resolver fact, and the target
	// never saw a request.
	require.NotContains(t, err.Error(), "127.0.0.1")
	require.Equal(t, int64(0), counter.count.Load())
}

func TestRemoteProbeServiceRefusesInvalidURLBeforeAnyNetworkIO(t *testing.T) {
	t.Parallel()

	counter := &probeRequestCounter{}
	fixture := startProbeFixture(t, counter.wrap(probeFixtureMCPHandler("alpha")))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	insecure := "http://" + strings.TrimPrefix(fixture.URL, "https://")
	result, err := service.Probe(t.Context(), registrationServicePrincipal(), insecure)
	require.ErrorIs(t, err, ErrRemoteURLInvalid)
	require.Empty(t, result.Receipt)
	require.Equal(t, int64(0), counter.count.Load())
}

func TestRemoteProbeServiceChargesTheProbeBudgetFirst(t *testing.T) {
	t.Parallel()

	counter := &probeRequestCounter{}
	fixture := startProbeFixture(t, counter.wrap(probeFixtureMCPHandler("alpha")))
	service, err := NewRemoteProbeService(testenv.NewLogger(t), probeFixturePolicy(t, fixture), probeServiceTestKeyMaterial, OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
		Organization: allowOperationLimiter{},
	})
	require.NoError(t, err)

	result, probeErr := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.ErrorIs(t, probeErr, ErrOperationRateLimited)
	require.Empty(t, result.Receipt)
	require.Equal(t, int64(0), counter.count.Load())

	// The budget is charged before URL validation, so a throttled caller
	// cannot even spend the validation oracle.
	_, probeErr = service.Probe(t.Context(), registrationServicePrincipal(), "not a url")
	require.ErrorIs(t, probeErr, ErrOperationRateLimited)
}

func TestRemoteProbeServiceRefusesCallerWithoutReceiptIdentity(t *testing.T) {
	t.Parallel()

	counter := &probeRequestCounter{}
	fixture := startProbeFixture(t, counter.wrap(probeFixtureMCPHandler("alpha")))
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))

	// A connection-less principal with no user has nothing a receipt could
	// bind to; the probe's only product is the receipt, so no egress is spent.
	result, err := service.Probe(t.Context(), Principal{OrganizationID: "organization"}, fixture.URL)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	require.Empty(t, result.Receipt)
	require.Equal(t, int64(0), counter.count.Load())
}

func TestRemoteProbeServiceTimesOutHungServer(t *testing.T) {
	t.Parallel()

	// Hold every request open until the probe gives up. The server does not
	// notice an aborted request until it writes, so the handler also waits on
	// an explicit release that cleanup closes — registered after the fixture
	// so it runs before the fixture's Close, which blocks on active handlers.
	release := make(chan struct{})
	fixture := startProbeFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release) })
	service := newTestRemoteProbeService(t, probeFixturePolicy(t, fixture))
	service.timeout = 250 * time.Millisecond

	result, err := service.Probe(t.Context(), registrationServicePrincipal(), fixture.URL)
	require.ErrorIs(t, err, ErrProbeUnreachable)
	require.Empty(t, result.Receipt)
}

func TestRemoteProbeServiceNilReceiverRefusesWithoutPanic(t *testing.T) {
	t.Parallel()

	// A typed-nil service passes the RemoteMCPProber interface nil check at
	// composition, so the receiver must refuse like every sibling boundary
	// rather than panic on its first field access.
	var service *RemoteProbeService
	result, err := service.Probe(t.Context(), registrationServicePrincipal(), "https://remote.example.test/mcp")
	require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
	require.Empty(t, result.Receipt)
}

func TestOperationBudgetsValidRequiresProbeBudget(t *testing.T) {
	t.Parallel()

	budget := allowBudget()
	budgets := OperationBudgets{
		Catalog:      budget,
		Registration: budget,
		Handoff:      budget,
		SetupStart:   budget,
		Repair:       budget,
		Docs:         budget,
		Skills:       budget,
		Probe:        OperationBudget{Connection: nil, Organization: nil},
	}
	require.False(t, budgets.Valid())

	budgets.Probe = budget
	require.True(t, budgets.Valid())
}
