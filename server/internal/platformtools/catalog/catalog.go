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
	remotemcpgen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
)

const (
	SourceCatalog              = "catalog"
	ToolNameSearchCatalog      = "platform_search_catalog"
	ToolNameInstallCatalogTool = "platform_install_catalog_server"
)

// Catalog is the subset of mcp_registries + remote_mcp behavior the install
// tool depends on. The live wiring constructs an adapter that forwards to the
// running services; tests substitute a fake.
type Catalog interface {
	GetServerDetails(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error)
	CreateRemoteServer(ctx context.Context, payload *remotemcpgen.CreateServerPayload) (*types.RemoteMcpServer, error)
}

// FuncCatalog adapts mcp_registries.Service.GetServerDetails and
// remote_mcp.Service.CreateServer method values to the Catalog interface
// without forcing this package to depend on the concrete service packages
// (some of them transitively pull platformtools, which would form a cycle).
type FuncCatalog struct {
	GetServerDetailsFn   func(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error)
	CreateRemoteServerFn func(ctx context.Context, payload *remotemcpgen.CreateServerPayload) (*types.RemoteMcpServer, error)
}

func (f *FuncCatalog) GetServerDetails(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error) {
	return f.GetServerDetailsFn(ctx, payload)
}

func (f *FuncCatalog) CreateRemoteServer(ctx context.Context, payload *remotemcpgen.CreateServerPayload) (*types.RemoteMcpServer, error) {
	return f.CreateRemoteServerFn(ctx, payload)
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
