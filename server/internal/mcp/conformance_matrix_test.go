package mcp_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// The conformance matrix runs one fixed request set against every surface on
// which Speakeasy terminates MCP, once for each protocol revision the surface
// supports plus an absent and an unsupported declaration, and asserts the wire
// behavior the specification of the governing revision requires. Requests go
// through the production router, so error wrapping is exercised as clients see
// it.
//
// The expectations below are written out rather than derived from production
// code, so conformanceRequests doubles as the per-revision behavior table. Only
// the revision axis is derived, from each surface's supported set, so a newly
// supported revision is exercised without editing this file and fails here if
// its era's expectations do not hold for it.

// conformanceEra classifies a request's declared revision by the wire
// behavior the specification gives it.
type conformanceEra string

const (
	// eraHandshake covers the revisions opening with an initialize handshake,
	// 2024-11-05 through 2025-11-25, and a request declaring no revision at
	// all, which the 2026-07-28 Streamable HTTP transport lets a server treat
	// as 2025-03-26.
	eraHandshake conformanceEra = "handshake"

	// eraPerRequest covers 2026-07-28: per-request metadata instead of a
	// handshake, mandated HTTP statuses for protocol errors, and no
	// protocol-level sessions.
	eraPerRequest conformanceEra = "per-request"

	// eraUnsupported covers a well-formed declaration outside the surface's
	// supported set.
	eraUnsupported conformanceEra = "unsupported"
)

// conformanceOutcome is the expected wire response to one request.
type conformanceOutcome struct {
	// status is the HTTP status code.
	status int

	// code is the expected JSON-RPC error code, or zero when the response
	// must carry a result.
	code oops.MCPCode

	// noBody marks a response that must carry no body: the acknowledgement of
	// a JSON-RPC notification.
	noBody bool

	// statusOnly marks a refusal at the HTTP layer, before any JSON-RPC
	// processing, whose body is not part of the contract.
	statusOnly bool
}

// conformanceRequest is one row of the behavior table: a request and the
// outcome each era requires of it.
type conformanceRequest struct {
	// name labels the row in subtest names.
	name string

	// httpMethod is the HTTP method, POST unless a row exercises another verb.
	httpMethod string

	// method is the JSON-RPC method, sent as a notification when notification
	// is set.
	method string

	// notification sends the request without an id.
	notification bool

	// params are the request params before any per-request `_meta` is added.
	params map[string]any

	// mcpName is the Mcp-Name header a per-request client mirrors from params.
	mcpName string

	// expect is the required outcome per era.
	expect map[conformanceEra]conformanceOutcome
}

// conformanceMissingTool names a tool no fixture defines.
const conformanceMissingTool = "conformance-missing-tool"

// Shorthands for the outcomes the table repeats.
var (
	resultOK           = conformanceOutcome{status: http.StatusOK, code: 0, noBody: false, statusOnly: false}
	accepted           = conformanceOutcome{status: http.StatusAccepted, code: 0, noBody: true, statusOnly: false}
	methodNotAllowed   = conformanceOutcome{status: http.StatusMethodNotAllowed, code: 0, noBody: false, statusOnly: true}
	unsupportedRev     = conformanceOutcome{status: http.StatusBadRequest, code: oops.MCPCodeUnsupportedProtocolVersion, noBody: false, statusOnly: false}
	methodNotFoundOK   = conformanceOutcome{status: http.StatusOK, code: oops.MCPCodeMethodNotFound, noBody: false, statusOnly: false}
	methodNotFound404  = conformanceOutcome{status: http.StatusNotFound, code: oops.MCPCodeMethodNotFound, noBody: false, statusOnly: false}
	invalidParams400   = conformanceOutcome{status: http.StatusBadRequest, code: oops.MCPCodeInvalidParams, noBody: false, statusOnly: false}
	resourceNotFoundOK = conformanceOutcome{status: http.StatusOK, code: oops.MCPCodeResourceNotFound, noBody: false, statusOnly: false}
)

// conformanceRequests is the behavior table. Handshake-era revisions answer
// every JSON-RPC error with HTTP 200, define initialize, and may assign a
// session at initialize; 2026-07-28 removes the handshake, answers protocol
// errors with their mandated status, and adds server/discover. GET and DELETE
// are answered 405 on every revision: no surface offers a standalone SSE
// stream or session termination for its own backend.
var conformanceRequests = []conformanceRequest{
	{
		name:   "initialize",
		method: mcpversions.MethodInitialize,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:  resultOK,
			eraPerRequest: methodNotFound404,
			// A handshake proposing an unrecognized revision is negotiated
			// down rather than rejected.
			eraUnsupported: resultOK,
		},
	},
	{
		name:         "notifications/initialized",
		method:       mcpversions.MethodNotificationsInitialized,
		notification: true,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake: accepted,
			// A notification the revision does not define is acknowledged
			// and dropped: a notification never receives a JSON-RPC response.
			eraPerRequest: accepted,
			// A notification the server cannot accept must still be refused
			// with an HTTP error status, under a null id.
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:   "ping",
		method: mcpversions.MethodPing,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   resultOK,
			eraPerRequest:  methodNotFound404,
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:   "server/discover",
		method: mcpversions.MethodServerDiscover,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   methodNotFoundOK,
			eraPerRequest:  resultOK,
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:   "tools/list",
		method: mcpversions.MethodToolsList,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   resultOK,
			eraPerRequest:  resultOK,
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:    "tools/call unknown tool",
		method:  mcpversions.MethodToolsCall,
		params:  map[string]any{"name": conformanceMissingTool, "arguments": map[string]any{}},
		mcpName: conformanceMissingTool,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   resourceNotFoundOK,
			eraPerRequest:  invalidParams400,
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:   "subscriptions/listen",
		method: mcpversions.MethodSubscriptionsListen,
		expect: map[conformanceEra]conformanceOutcome{
			// Speakeasy advertises no list-change notifications, so it implements
			// no listen stream to deliver them on.
			eraHandshake:   methodNotFoundOK,
			eraPerRequest:  methodNotFound404,
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:   "unknown method",
		method: "conformance/unknown",
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   methodNotFoundOK,
			eraPerRequest:  methodNotFound404,
			eraUnsupported: unsupportedRev,
		},
	},
	{
		name:       "GET",
		httpMethod: http.MethodGet,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   methodNotAllowed,
			eraPerRequest:  methodNotAllowed,
			eraUnsupported: methodNotAllowed,
		},
	},
	{
		name:       "DELETE",
		httpMethod: http.MethodDelete,
		expect: map[conformanceEra]conformanceOutcome{
			eraHandshake:   methodNotAllowed,
			eraPerRequest:  methodNotAllowed,
			eraUnsupported: methodNotAllowed,
		},
	},
}

// conformanceTarget is one surface under test, served by the production
// router.
type conformanceTarget struct {
	// router serves every surface's routes.
	router http.Handler

	// path is the MCP endpoint path.
	path string

	// authorization is the Authorization header value, or empty for an
	// anonymous surface.
	authorization string

	// supported is the surface's supported revision set.
	supported []string

	// assignsSessions reports whether the surface assigns a session id at a
	// handshake-era initialize.
	assignsSessions bool
}

// conformanceDeclaration is one point on the revision axis.
type conformanceDeclaration struct {
	// label names the declaration in subtest names.
	label string

	// revision is the declared revision, empty for none.
	revision string

	// era is the behavior class the declaration falls in.
	era conformanceEra
}

// declarationsFor derives the revision axis from a supported set: each
// supported revision, an absent declaration, and an unsupported one.
func declarationsFor(supported []string) []conformanceDeclaration {
	declarations := []conformanceDeclaration{
		{label: "absent", revision: "", era: eraHandshake},
		{label: unservedProtocolVersion, revision: unservedProtocolVersion, era: eraUnsupported},
	}
	for _, revision := range supported {
		era := eraHandshake
		if mcpversions.AtLeast(revision, mcpversions.Version20260728) {
			era = eraPerRequest
		}
		declarations = append(declarations, conformanceDeclaration{label: revision, revision: revision, era: era})
	}
	return declarations
}

// negotiatedRevision is the revision a handshake-era initialize proposing
// proposed must be answered with: a supported proposal is echoed, an absent
// one gets the unversioned default, and anything else gets the newest
// revision defining the handshake.
func negotiatedRevision(proposed string, supported []string) string {
	switch {
	case proposed == "":
		return mcpversions.DefaultInEffect
	case slices.Contains(supported, proposed):
		return proposed
	default:
		return mcpversions.Version20251125
	}
}

// buildConformanceRequest renders row for declaration the way a conforming
// client of that era sends it. A handshake-era client proposes its revision in
// the initialize body and declares it in MCP-Protocol-Version afterwards. A
// 2026-07-28 client, and a client of an unrecognized later revision, declares
// it on every request in the header and in `_meta`, alongside the mirrored
// Mcp-Method and Mcp-Name headers and the required client metadata.
func buildConformanceRequest(t *testing.T, target conformanceTarget, row conformanceRequest, declaration conformanceDeclaration) *http.Request {
	t.Helper()

	httpMethod := row.httpMethod
	if httpMethod == "" {
		httpMethod = http.MethodPost
	}
	isHandshake := row.method == mcpversions.MethodInitialize && declaration.era != eraPerRequest
	perRequest := httpMethod == http.MethodPost && declaration.era != eraHandshake && !isHandshake

	var body []byte
	if httpMethod == http.MethodPost {
		params := map[string]any{}
		maps.Copy(params, row.params)
		if isHandshake {
			if declaration.revision != "" {
				params["protocolVersion"] = declaration.revision
			}
			params["capabilities"] = map[string]any{}
			params["clientInfo"] = map[string]any{"name": "conformance-client", "version": "1.0.0"}
		}
		if perRequest {
			params["_meta"] = map[string]any{
				"io.modelcontextprotocol/protocolVersion":    declaration.revision,
				"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "conformance-client", "version": "1.0.0"},
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			}
		}

		envelope := map[string]any{"jsonrpc": "2.0", "method": row.method, "params": params}
		if !row.notification {
			envelope["id"] = 1
		}
		var err error
		body, err = json.Marshal(envelope)
		require.NoError(t, err)
	}

	req := httptest.NewRequestWithContext(t.Context(), httpMethod, target.path, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	if httpMethod == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if target.authorization != "" {
		req.Header.Set("Authorization", target.authorization)
	}

	if declaration.revision != "" && !isHandshake {
		req.Header.Set(mcpversions.HTTPHeader, declaration.revision)
	}
	if perRequest {
		req.Header.Set("Mcp-Method", row.method)
		if row.mcpName != "" {
			req.Header.Set("Mcp-Name", row.mcpName)
		}
	}
	// Every request but a handshake carries a session id, as a handshake-era
	// client holding one would. 2026-07-28 servers must ignore it, and no
	// surface echoes it outside initialize.
	if row.method != mcpversions.MethodInitialize {
		req.Header.Set("Mcp-Session-Id", "conformance-session")
	}

	return req
}

func runConformanceMatrix(t *testing.T, target conformanceTarget) {
	t.Helper()

	for _, declaration := range declarationsFor(target.supported) {
		for _, row := range conformanceRequests {
			t.Run(declaration.label+"/"+row.name, func(t *testing.T) {
				t.Parallel()

				want, ok := row.expect[declaration.era]
				require.True(t, ok, "row %q has no expectation for era %q", row.name, declaration.era)

				w := httptest.NewRecorder()
				target.router.ServeHTTP(w, buildConformanceRequest(t, target, row, declaration))

				require.Equal(t, want.status, w.Code, "body=%s", w.Body.String())
				require.Empty(t, w.Header().Get(mcpversions.HTTPHeader), "MCP-Protocol-Version is a request header in every revision")

				wantSession := target.assignsSessions && row.method == mcpversions.MethodInitialize && want == resultOK
				if wantSession {
					require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"), "a handshake-era initialize is assigned a session")
				} else {
					require.Empty(t, w.Header().Get("Mcp-Session-Id"), "only a handshake-era initialize may be assigned a session")
				}

				switch {
				case want.statusOnly:
					return
				case want.noBody:
					require.Empty(t, w.Body.String())
					return
				}

				var response struct {
					JSONRPC string                     `json:"jsonrpc"`
					ID      json.RawMessage            `json:"id"`
					Result  map[string]json.RawMessage `json:"result"`
					Error   *struct {
						Code oops.MCPCode `json:"code"`
						Data struct {
							Supported []string `json:"supported"`
							Requested string   `json:"requested"`
						} `json:"data"`
					} `json:"error"`
				}
				require.Equal(t, "application/json", w.Header().Get("Content-Type"))
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "body=%s", w.Body.String())
				require.Equal(t, "2.0", response.JSONRPC)
				wantID := `1`
				if row.notification {
					wantID = `null`
				}
				require.JSONEq(t, wantID, string(response.ID), "the response must echo the request id")

				if want.code != 0 {
					require.NotNil(t, response.Error, "body=%s", w.Body.String())
					require.Equal(t, want.code, response.Error.Code, "body=%s", w.Body.String())
					if want.code == oops.MCPCodeUnsupportedProtocolVersion {
						require.Equal(t, target.supported, response.Error.Data.Supported)
						require.Equal(t, declaration.revision, response.Error.Data.Requested)
					}
					return
				}

				require.Nil(t, response.Error, "body=%s", w.Body.String())
				require.JSONEq(t, `"complete"`, string(response.Result["resultType"]), "every result carries resultType")
				switch row.method {
				case mcpversions.MethodInitialize:
					var negotiated string
					require.NoError(t, json.Unmarshal(response.Result["protocolVersion"], &negotiated))
					require.Equal(t, negotiatedRevision(declaration.revision, target.supported), negotiated)

					var capabilities struct {
						Tools struct {
							ListChanged bool `json:"listChanged"`
						} `json:"tools"`
					}
					require.NoError(t, json.Unmarshal(response.Result["capabilities"], &capabilities))
					require.False(t, capabilities.Tools.ListChanged, "list-change notifications need a listen stream no surface implements")
				case mcpversions.MethodServerDiscover:
					var supported []string
					require.NoError(t, json.Unmarshal(response.Result["supportedVersions"], &supported))
					require.Equal(t, target.supported, supported)
				}
			})
		}
	}
}

func TestConformanceMatrix_HostedToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "conformance-"+uuid.NewString()[:8])

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	runConformanceMatrix(t, conformanceTarget{
		router:          router,
		path:            "/mcp/" + toolset.McpSlug.String,
		authorization:   "",
		supported:       mcpversions.SupportedHostedToolset(),
		assignsSessions: true,
	})
}

func TestConformanceMatrix_HostedToolsetEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "conformance-toolset-"+uuid.NewString()[:8])
	endpointSlug := "conformance-endpoint-" + uuid.NewString()[:8]
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	runConformanceMatrix(t, conformanceTarget{
		router:          router,
		path:            "/mcp/" + endpointSlug,
		authorization:   "",
		supported:       mcpversions.SupportedHostedToolset(),
		assignsSessions: true,
	})
}

func TestConformanceMatrix_Meta(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "conformance-meta-" + uuid.NewString()[:8]
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	runConformanceMatrix(t, conformanceTarget{
		router:          router,
		path:            "/mcp/" + slug,
		authorization:   "Bearer " + ti.createTestAPIKey(ctx, t),
		supported:       mcpversions.SupportedMetaServer(),
		assignsSessions: true,
	})
}

func TestConformanceMatrix_Platform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	managedID := createAssistant(t, ti, authCtx, "Managed")
	err := assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	})
	require.NoError(t, err)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	runConformanceMatrix(t, conformanceTarget{
		router:          router,
		path:            "/platform/mcp/" + platformtools.ManagedAssistantPlatformToolsetSlug,
		authorization:   "Bearer " + mintAssistantToken(t, ti, authCtx, managedID),
		supported:       mcpversions.SupportedPlatformToolset(),
		assignsSessions: false,
	})
}

func TestConformanceMatrix_AgentGateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fixture := seedAgentGateway(t, ctx, ti)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	runConformanceMatrix(t, conformanceTarget{
		router:          router,
		path:            "/agent-mcp/" + fixture.agent.ID.String(),
		authorization:   "Bearer " + fixture.token,
		supported:       mcpversions.SupportedMetaServer(),
		assignsSessions: true,
	})
}
