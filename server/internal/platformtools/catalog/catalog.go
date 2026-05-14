// Package catalog implements the platform_search_catalog and
// platform_install_catalog_server tools that expose the MCP catalog to
// assistants so they can discover and register external MCP servers on the
// caller's behalf.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	registriesgen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	mcpserversgen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	remotemcpgen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
)

const (
	SourceCatalog              = "catalog"
	ToolNameSearchCatalog      = "platform_search_catalog"
	ToolNameInstallCatalogTool = "platform_install_catalog_server"
)

// Catalog is the subset of mcp_registries + remote_mcp + mcp_servers
// behavior the install tool depends on. The live wiring constructs an
// adapter that forwards to the running services; tests substitute a fake.
//
// The install flow mirrors the dashboard's "Add remote MCP" path
// (client/dashboard/src/pages/sources/remote-mcp/hooks.ts): create the
// remote MCP server, then create a disabled mcp_servers row that links to
// it. Without the link row the dashboard treats the source as orphaned.
type Catalog interface {
	ListCatalog(ctx context.Context, payload *registriesgen.ListCatalogPayload) (*registriesgen.ListCatalogResult, error)
	GetServerDetails(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error)
	CreateRemoteServer(ctx context.Context, payload *remotemcpgen.CreateServerPayload) (*types.RemoteMcpServer, error)
	DeleteRemoteServer(ctx context.Context, payload *remotemcpgen.DeleteServerPayload) error
	CreateMCPServer(ctx context.Context, payload *mcpserversgen.CreateMcpServerPayload) (*types.McpServer, error)
}

// FuncCatalog adapts the underlying Goa service method values to the
// Catalog interface without forcing this package to import the concrete
// service packages (some transitively pull platformtools, which would
// form a cycle).
type FuncCatalog struct {
	ListCatalogFn        func(ctx context.Context, payload *registriesgen.ListCatalogPayload) (*registriesgen.ListCatalogResult, error)
	GetServerDetailsFn   func(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error)
	CreateRemoteServerFn func(ctx context.Context, payload *remotemcpgen.CreateServerPayload) (*types.RemoteMcpServer, error)
	DeleteRemoteServerFn func(ctx context.Context, payload *remotemcpgen.DeleteServerPayload) error
	CreateMCPServerFn    func(ctx context.Context, payload *mcpserversgen.CreateMcpServerPayload) (*types.McpServer, error)
}

func (f *FuncCatalog) ListCatalog(ctx context.Context, payload *registriesgen.ListCatalogPayload) (*registriesgen.ListCatalogResult, error) {
	return f.ListCatalogFn(ctx, payload)
}

func (f *FuncCatalog) GetServerDetails(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error) {
	return f.GetServerDetailsFn(ctx, payload)
}

func (f *FuncCatalog) CreateRemoteServer(ctx context.Context, payload *remotemcpgen.CreateServerPayload) (*types.RemoteMcpServer, error) {
	return f.CreateRemoteServerFn(ctx, payload)
}

func (f *FuncCatalog) DeleteRemoteServer(ctx context.Context, payload *remotemcpgen.DeleteServerPayload) error {
	return f.DeleteRemoteServerFn(ctx, payload)
}

func (f *FuncCatalog) CreateMCPServer(ctx context.Context, payload *mcpserversgen.CreateMcpServerPayload) (*types.McpServer, error) {
	return f.CreateMCPServerFn(ctx, payload)
}

func decodePayload(payload io.Reader, target any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func writeJSON(wr io.Writer, value any) error {
	if err := json.NewEncoder(wr).Encode(value); err != nil {
		return fmt.Errorf("encode response body: %w", err)
	}
	return nil
}

func catalogToolAnnotations(readOnly, destructive, idempotent, openWorld bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
