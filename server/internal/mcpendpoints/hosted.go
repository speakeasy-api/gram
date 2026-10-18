package mcpendpoints

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// HostedAddressUpdate is an mcpEndpoints.update aimed at the single endpoint of a canonical hosted wrapper.
type HostedAddressUpdate struct {
	// ToolsetID is the backing toolset, which is also the wrapper id.
	ToolsetID uuid.UUID

	// EndpointID is the wrapper endpoint being re-addressed.
	EndpointID uuid.UUID

	// Slug is the requested address slug.
	Slug string

	// CustomDomainID is the requested domain; invalid means the platform domain.
	CustomDomainID uuid.NullUUID
}

// HostedAddressWriter applies hosted endpoint address changes as the equivalent toolset mcp_slug / custom domain change.
type HostedAddressWriter interface {
	UpdateHostedAddress(ctx context.Context, update HostedAddressUpdate) (repo.McpEndpoint, error)
}

// WithHostedAddressWriter lets UpdateMcpEndpoint re-address canonical hosted endpoints; without one they are rejected.
func (s *Service) WithHostedAddressWriter(writer HostedAddressWriter) *Service {
	s.hostedAddresses = writer
	return s
}

func (s *Service) updateHostedEndpoint(ctx context.Context, logger *slog.Logger, existing repo.McpEndpoint, server mcpserversrepo.McpServer, mcpServerID uuid.NullUUID, customDomainID uuid.NullUUID, slug string) (*types.McpEndpoint, error) {
	if s.hostedAddresses == nil {
		return nil, oops.E(oops.CodeInvalid, nil, "manage hosted MCP endpoint addresses through the toolset")
	}
	if !mcpServerID.Valid || mcpServerID.UUID != server.ID {
		return nil, oops.E(oops.CodeInvalid, nil, "a hosted MCP endpoint keeps its server").LogWarn(ctx, logger)
	}

	updated, err := s.hostedAddresses.UpdateHostedAddress(ctx, HostedAddressUpdate{
		ToolsetID:      server.ID,
		EndpointID:     existing.ID,
		Slug:           slug,
		CustomDomainID: customDomainID,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // The writer returns oops errors whose codes reach the caller.
	}
	return mv.BuildMcpEndpointView(updated), nil
}
