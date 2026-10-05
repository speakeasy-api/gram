package mcp

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/stretchr/testify/require"
)

func TestMethodDispatchByRevision(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"hosted", "platform", "meta"} {
		for _, version := range mcpversions.All() {
			for _, method := range []string{mcpversions.MethodInitialize, mcpversions.MethodNotificationsInitialized, mcpversions.MethodPing, mcpversions.MethodServerDiscover, "unknown/method"} {
				t.Run(surface+"/"+version+"/"+method, func(t *testing.T) {
					t.Parallel()
					_, payload := newClientIdentityFixture(t)
					payload.protocolVersion = mcpversions.Resolution{Declared: version, InEffect: version}
					req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(7), Method: method,
						Params: json.RawMessage(`{"protocolVersion":"` + version + `"}`)}
					logger := testenv.NewLogger(t)
					service := &Service{logger: logger, sessionClientInfo: sessionclientinfo.NewStore(nil, 1),
						toolsetsRepo: toolsets_repo.New(failingDBTX{}), mcpMetadataRepo: metadata_repo.New(failingDBTX{})}
					if method == mcpversions.MethodServerDiscover {
						// A discovery path must not invoke the session store.
						service.sessionClientInfo = nil
					}
					var body json.RawMessage
					var err error
					switch surface {
					case "hosted":
						body, err = service.handleRequest(t.Context(), payload, req)
					case "platform":
						body, err = service.handlePlatformToolsetRequest(t.Context(), nil, platformtools.Toolset{}, req, "", &payload.protocolVersion)
					case "meta":
						body, err = service.handleMetaMCPRequest(t.Context(), logger, nil, &metamcprepo.MetaMcpServer{},
							&metaGateContext{protocolVersion: payload.protocolVersion}, req, version)
					}
					declares20260728 := version == mcpversions.Version20260728
					allowed := method != "unknown/method" && ((method == mcpversions.MethodServerDiscover) == declares20260728)
					if !allowed {
						require.Error(t, err)
						var shareable *oops.ShareableError
						require.ErrorAs(t, err, &shareable)
						require.Equal(t, oops.CodeNotImplemented, shareable.Code)
						require.Empty(t, body)
						return
					}
					require.NoError(t, err)
					if method == mcpversions.MethodNotificationsInitialized {
						require.Empty(t, body)
						return
					}
					var response struct {
						Result map[string]json.RawMessage `json:"result"`
					}
					require.NoError(t, json.Unmarshal(body, &response))
					if method == mcpversions.MethodServerDiscover {
						require.Contains(t, response.Result, "supportedVersions")
						var supported []string
						require.NoError(t, json.Unmarshal(response.Result["supportedVersions"], &supported))
						expected := mcpversions.SupportedHostedToolset()
						if surface == "platform" {
							expected = mcpversions.SupportedPlatformToolset()
						}
						if surface == "meta" {
							expected = mcpversions.SupportedMetaServer()
						}
						require.Equal(t, expected, supported)
						require.NotContains(t, response.Result, "protocolVersions")
						require.NotContains(t, response.Result, "serverInfo")
						require.NotContains(t, response.Result, "protocolVersion")
						require.JSONEq(t, `0`, string(response.Result["ttlMs"]))
						require.JSONEq(t, `"complete"`, string(response.Result["resultType"]))
						require.Contains(t, string(response.Result["_meta"]), metaKeyServerInfo)
					}
				})
			}
		}
	}
}

func TestInitializeRequiresNegotiableRevision(t *testing.T) {
	t.Parallel()
	req := &rawRequest{Method: mcpversions.MethodInitialize, ID: mcpjsonrpc.NumberID(1), Params: json.RawMessage(`{"protocolVersion":"2025-11-25"}`)}
	resolution := mcpversions.Resolution{Declared: "", InEffect: mcpversions.DefaultInEffect}
	require.True(t, methodAvailable(req, resolution, mcpversions.SupportedHostedToolset()))
	// A surface serving only handshake-free revisions has nothing to
	// negotiate, so initialize is unavailable rather than answered with an
	// empty protocolVersion.
	require.False(t, methodAvailable(req, resolution, []string{mcpversions.Version20260728}))
}

func TestUnavailableMethodNotificationHasNoResponse(t *testing.T) {
	t.Parallel()
	for _, method := range []string{mcpversions.MethodNotificationsInitialized, mcpversions.MethodPing, "unknown/method"} {
		req := &rawRequest{Method: method}
		body, err := (&Service{}).handlePlatformToolsetRequest(t.Context(), nil, platformtools.Toolset{}, req, "",
			&mcpversions.Resolution{Declared: mcpversions.Version20260728, InEffect: mcpversions.Version20260728})
		require.NoError(t, err)
		require.Empty(t, body)
	}
}

func TestInitializeDeclarationControlsVersionValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, params, declared string
		// rejected is the expected error code, or zero when the handshake
		// proceeds.
		rejected oops.MCPCode
	}{
		{name: "handshake proposes 2026-07-28", params: `{"protocolVersion":"2026-07-28"}`},
		{name: "2025-03-26 metadata", params: `{"protocolVersion":"2025-11-25","_meta":{"io.modelcontextprotocol/protocolVersion":"2025-03-26"}}`, declared: mcpversions.Version20250326},
		{name: "unrecognized header", params: `{"protocolVersion":"2025-06-18"}`, declared: "2025-12-01"},
		{name: "unrecognized metadata", params: `{"protocolVersion":"2025-06-18","_meta":{"io.modelcontextprotocol/protocolVersion":"2025-12-01"}}`},
		{name: "2026-07-28 header", declared: mcpversions.Version20260728, rejected: oops.MCPCodeUnsupportedProtocolVersion},
		{name: "2026-07-28 metadata", params: `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`, declared: mcpversions.Version20260728, rejected: oops.MCPCodeUnsupportedProtocolVersion},
		{name: "2025-11-25 header with 2026-07-28 metadata", params: `{"protocolVersion":"2025-11-25","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`, declared: mcpversions.Version20251125, rejected: oops.MCPCodeHeaderMismatch},
		{name: "2026-07-28 header with 2025-11-25 metadata", params: `{"protocolVersion":"2025-11-25","_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25"}}`, declared: mcpversions.Version20260728, rejected: oops.MCPCodeHeaderMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := &rawRequest{Method: mcpversions.MethodInitialize, ID: mcpjsonrpc.NumberID(1), Params: json.RawMessage(tc.params)}
			resolution := mcpversions.Resolve(mcprequests.DeclaredProtocolVersion(tc.declared, req.Params), mcpversions.SupportedHostedToolset())
			for surface, err := range map[string]error{
				"hosted": validateSupportedProtocolVersion(req, resolution, mcpversions.SupportedHostedToolset()),
				"meta":   validateMetaDeclaredProtocolVersion(req, tc.declared),
			} {
				if tc.rejected == 0 {
					require.NoError(t, err, surface)
					continue
				}
				var mcpErr *oops.MCPError
				require.ErrorAs(t, err, &mcpErr, surface)
				require.Equal(t, tc.rejected, mcpErr.Code, surface)
			}
		})
	}
}

func TestInitializeDeclaring20260728CannotStartHandshake(t *testing.T) {
	t.Parallel()
	req := &rawRequest{
		Method: mcpversions.MethodInitialize,
		ID:     mcpjsonrpc.NumberID(1),
		Params: json.RawMessage(`{"protocolVersion":"2025-11-25","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`),
	}
	// A 2025-11-25 header must not let a request carrying 2026-07-28 metadata
	// invoke the handshake, even before the general mirrored-header validator runs.
	resolution := mcpversions.Resolve(mcpversions.Version20251125, mcpversions.SupportedHostedToolset())
	require.False(t, initializeNegotiable(req, resolution))
	body, err := (&Service{}).handlePlatformToolsetRequest(t.Context(), nil, platformtools.Toolset{}, req, "", &resolution)
	require.Error(t, err)
	require.Empty(t, body)
	require.Equal(t, mcpversions.Version20251125, resolution.InEffect)
}

func TestRequestDeclarationsMustAgreeBeforeDispatch(t *testing.T) {
	t.Parallel()
	for _, method := range []string{mcpversions.MethodServerDiscover, mcpversions.MethodToolsList, mcpversions.MethodPing, mcpversions.MethodNotificationsInitialized} {
		for _, tc := range []struct {
			name, header, meta string
			conflict           bool
		}{
			{name: "earlier header", header: mcpversions.Version20251125, meta: mcpversions.Version20260728, conflict: true},
			{name: "later header", header: mcpversions.Version20260728, meta: mcpversions.Version20251125, conflict: true},
			{name: "matching", header: mcpversions.Version20260728, meta: mcpversions.Version20260728},
			{name: "metadata only", meta: mcpversions.Version20260728},
			{name: "header only", header: mcpversions.Version20260728},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				params, err := json.Marshal(map[string]any{"_meta": map[string]string{"io.modelcontextprotocol/protocolVersion": tc.meta}})
				require.NoError(t, err)
				req := &rawRequest{Method: method, ID: mcpjsonrpc.NumberID(1), Params: params}
				// Exercise the shared hosted/platform gate with discovery enabled,
				// so unsupported-version rejection cannot mask a conflict.
				supported := append(mcpversions.SupportedHostedToolset(), mcpversions.Version20260728)
				resolution := mcpversions.Resolve(mcprequests.DeclaredProtocolVersion(tc.header, params), supported)
				err = validateSupportedProtocolVersion(req, resolution, supported)
				if !tc.conflict {
					require.NoError(t, err)
					return
				}
				var rpcErr *oops.MCPError
				require.ErrorAs(t, err, &rpcErr)
				require.Equal(t, oops.MCPCodeHeaderMismatch, rpcErr.Code)
				// The declarations name no single revision, so the
				// response follows the latest specification's rules.
				var declErr *declarationError
				require.ErrorAs(t, err, &declErr)
				require.Equal(t, mcpversions.Latest(), declErr.revision)
			})
		}
	}
}
