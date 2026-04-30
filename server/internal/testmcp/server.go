// Package testmcp provides an in-process mock MCP server backed by the
// official MCP Go SDK. It is used by tests that need to exercise proxy or
// client code paths against a real MCP endpoint without depending on an
// external process.
package testmcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// NewStreamableHTTPServer returns a running [httptest.Server] that speaks
// the MCP Streamable HTTP transport and exposes the tools registered on s.
func NewStreamableHTTPServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()

	server, err := s.streamableHTTPServer()
	require.NoError(t, err)
	return server
}

// NewSSEServer returns a running [httptest.Server] that speaks the legacy
// MCP HTTP+SSE transport and exposes the tools registered on s.
func NewSSEServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()

	server, err := s.sseServer()
	require.NoError(t, err)
	return server
}

// Server collects the tools a mock MCP server should expose. Populate Tools,
// then hand the Server to [NewStreamableHTTPServer] or [NewSSEServer] to get
// a running [httptest.Server] speaking the corresponding MCP transport.
type Server struct {
	// Tools are the tools the mock server will register before starting.
	// Appending to Tools after a transport server has been constructed has
	// no effect — registration happens once, at server start.
	Tools []Tool
}

// mcpServer constructs an [mcp.Server] populated with s.Tools. It is shared
// by streamableHTTPServer and sseServer so both transports expose the same
// tool surface without duplicating registration code. Any error converting
// a Tool to its MCP representation is returned to the caller.
func (s *Server) mcpServer() (*mcp.Server, error) {
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Icons:      nil,
		Name:       "testmcp-server",
		Title:      "",
		Version:    "1.0.0",
		WebsiteURL: "",
	}, nil)

	for _, tool := range s.Tools {
		mcpTool, err := tool.mcpTool()
		if err != nil {
			return nil, err
		}

		mcpServer.AddTool(mcpTool, tool.mcpToolHandler())
	}

	return mcpServer, nil
}

// sseServer returns a running [httptest.Server] that speaks the legacy MCP
// HTTP+SSE transport and exposes the tools registered on s. Callers are
// responsible for calling Close on the returned server.
func (s *Server) sseServer() (*httptest.Server, error) {
	mcpServer, err := s.mcpServer()
	if err != nil {
		return nil, err
	}

	handler := mcp.NewSSEHandler(func(_ *http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	return httptest.NewServer(handler), nil
}

// streamableHTTPServer returns a running [httptest.Server] that speaks the
// MCP Streamable HTTP transport and exposes the tools registered on s.
// Callers are responsible for calling Close on the returned server.
func (s *Server) streamableHTTPServer() (*httptest.Server, error) {
	mcpServer, err := s.mcpServer()
	if err != nil {
		return nil, err
	}

	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	return httptest.NewServer(handler), nil
}
