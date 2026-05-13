// Package catalog implements the platform_search_catalog and
// platform_install_catalog_server tools that expose the MCP catalog to
// assistants so they can discover and install external MCP servers on the
// caller's behalf.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	deploymentsgen "github.com/speakeasy-api/gram/server/gen/deployments"
	registriesgen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
)

const (
	SourceCatalog              = "catalog"
	ToolNameSearchCatalog      = "platform_search_catalog"
	ToolNameInstallCatalogTool = "platform_install_catalog_server"
)

// Installer is the subset of deployments + toolsets + external MCP behavior
// the install tool depends on. The live wiring constructs an adapter that
// forwards to the running services; tests substitute a fake.
type Installer interface {
	Evolve(ctx context.Context, payload *deploymentsgen.EvolvePayload) (*deploymentsgen.EvolveResult, error)
	CreateToolset(ctx context.Context, payload *toolsetsgen.CreateToolsetPayload) (*types.Toolset, error)
	UpdateToolset(ctx context.Context, payload *toolsetsgen.UpdateToolsetPayload) (*types.Toolset, error)
	GetCatalogServerDetails(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error)
}

// FuncInstaller adapts the deployments.Service, toolsets.Service, and
// externalmcp.Service method values to the Installer interface without
// forcing catalog to depend on the concrete service packages (some of them
// pull platformtools, which would form a cycle).
type FuncInstaller struct {
	EvolveFn                  func(ctx context.Context, payload *deploymentsgen.EvolvePayload) (*deploymentsgen.EvolveResult, error)
	CreateToolsetFn           func(ctx context.Context, payload *toolsetsgen.CreateToolsetPayload) (*types.Toolset, error)
	UpdateToolsetFn           func(ctx context.Context, payload *toolsetsgen.UpdateToolsetPayload) (*types.Toolset, error)
	GetCatalogServerDetailsFn func(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error)
}

func (f *FuncInstaller) Evolve(ctx context.Context, payload *deploymentsgen.EvolvePayload) (*deploymentsgen.EvolveResult, error) {
	return f.EvolveFn(ctx, payload)
}

func (f *FuncInstaller) CreateToolset(ctx context.Context, payload *toolsetsgen.CreateToolsetPayload) (*types.Toolset, error) {
	return f.CreateToolsetFn(ctx, payload)
}

func (f *FuncInstaller) UpdateToolset(ctx context.Context, payload *toolsetsgen.UpdateToolsetPayload) (*types.Toolset, error) {
	return f.UpdateToolsetFn(ctx, payload)
}

func (f *FuncInstaller) GetCatalogServerDetails(ctx context.Context, payload *registriesgen.GetServerDetailsPayload) (*types.ExternalMCPServer, error) {
	return f.GetCatalogServerDetailsFn(ctx, payload)
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
