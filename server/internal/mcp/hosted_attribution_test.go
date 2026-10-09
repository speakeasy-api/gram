package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	posthoggo "github.com/posthog/posthog-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type capturingPosthogClient struct {
	posthoggo.Client
	mu       sync.Mutex
	captures []posthoggo.Capture
}

func (c *capturingPosthogClient) Enqueue(msg posthoggo.Message) error {
	if capture, ok := msg.(posthoggo.Capture); ok {
		c.mu.Lock()
		c.captures = append(c.captures, capture)
		c.mu.Unlock()
	}
	return nil
}

func (c *capturingPosthogClient) event(t *testing.T, name string) posthoggo.Properties {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, capture := range c.captures {
		if capture.Event == name {
			return capture.Properties
		}
	}
	t.Fatalf("posthog event %q not captured", name)
	return nil
}

func mcpEndpointIDForServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   projectID,
		McpServerID: serverID,
	})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	return endpoints[0].ID
}

func requireHostedAttributionRow(t *testing.T, eventSource telemetry.EventSource, serverID, endpointID uuid.UUID) {
	t.Helper()
	requireTelemetryRowCount(t, `event_source = ?
		   AND mcp_server_id = ?
		   AND toString(attributes.gram.mcp_endpoint.id) = ?`,
		1, string(eventSource), serverID.String(), endpointID.String())
}

// Hosted endpoint traffic carries the server and endpoint ids on events and rows.
func TestServePublic_HostedEndpoint_AttributesServerAndEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	captured := &capturingPosthogClient{Client: nil, mu: sync.Mutex{}, captures: nil}
	ti.service.SetPosthog(posthog.NewWithClient(ti.logger, captured))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	slug := "attr-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, slug)
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, projectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil)
	addHTTPTools(t, ctx, ti, toolset.ID, projectID, authCtx.ActiveOrganizationID, "attr_tool")
	endpointID := mcpEndpointIDForServer(t, ctx, ti, projectID, server.ID)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	reqCtx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{
		ReqID:  "",
		ReqURL: "/mcp/" + slug,
		Host:   "mcp.example.test",
		Method: http.MethodPost,
	})
	headers := map[string]string{"Mcp-Test-Server-Url": upstream.URL}
	for _, body := range [][]byte{
		makeMetaRPCBody(t, "initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "attr-client", "version": "1.0.0"},
		}),
		makeMetaRPCBody(t, "tools/list", map[string]any{}),
		makeMetaRPCBody(t, "tools/call", map[string]any{"name": "attr_tool", "arguments": map[string]any{}}),
	} {
		response, err := servePublicHTTP(t, reqCtx, ti, slug, body, "", headers)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}

	initialized := captured.event(t, "mcp_initialized")
	require.Equal(t, server.ID.String(), initialized["mcp_server_id"])
	require.Equal(t, endpointID.String(), initialized["mcp_endpoint_id"])

	listed := captured.event(t, "mcp_server_count")
	require.Equal(t, server.ID.String(), listed["mcp_server_id"])
	require.Equal(t, endpointID.String(), listed["mcp_endpoint_id"])
	require.Equal(t, toolset.ID.String(), listed["toolset_id"])
	require.EqualValues(t, toolset.Slug, listed["toolset_slug"])

	requireHostedAttributionRow(t, telemetry.EventSourceToolCall, server.ID, endpointID)
}
