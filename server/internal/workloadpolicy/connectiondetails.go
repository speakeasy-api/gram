package workloadpolicy

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// FederationResolver resolves what a federating platform is pointed at for an
// MCP server. The MCP service implements it from the same derivation that
// serves each endpoint's authorization server metadata, so these values
// cannot drift from what a client discovers.
type FederationResolver interface {
	FederationEndpoints(ctx context.Context, organizationID string, server *mcpservers_repo.McpServer) ([]mcp.FederationEndpoint, error)
	OrganizationAuthorizationServer(ctx context.Context, organizationID string) (mcp.OrganizationAuthorizationServer, error)
}

func (s *Service) ConnectionDetails(ctx context.Context, payload *gen.ConnectionDetailsPayload) (*gen.WorkloadConnectionDetails, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadRead)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(t.organizationID), attr.SlogMcpServerID(payload.McpServerID))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, logger)
	}

	// A server outside the caller's organization, or outside the project an
	// API key names, reads as missing rather than forbidden, so the id of a
	// server in another tenant cannot be confirmed.
	servers := mcpservers_repo.New(s.db)
	projectID := t.projectID
	if !projectID.Valid {
		server, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpservers_repo.GetMCPServerByIDAndOrganizationIDParams{
			ID:             serverID,
			OrganizationID: t.organizationID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogInfo(ctx, logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, logger)
		}
		projectID = uuid.NullUUID{UUID: server.ProjectID, Valid: true}
	}
	server, err := servers.GetMCPServerByLiveProjectForOrganization(ctx, mcpservers_repo.GetMCPServerByLiveProjectForOrganizationParams{
		ID:             serverID,
		ProjectID:      projectID.UUID,
		OrganizationID: t.organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogInfo(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, logger)
	}

	// A toolset-backed server's grants are written against its toolset.
	resourceID := server.ID
	if server.ToolsetID.Valid {
		resourceID = server.ToolsetID.UUID
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, resourceID.String(), server.ProjectID.String())); err != nil {
		return nil, err
	}

	federation, err := s.federation.FederationEndpoints(ctx, t.organizationID, &server)
	if err != nil {
		return nil, fmt.Errorf("resolve federation endpoints: %w", err)
	}

	endpoints := make([]*gen.WorkloadConnectionEndpoint, 0, len(federation))
	for _, endpoint := range federation {
		resource, err := url.Parse(endpoint.ResourceURL)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse resource URL").LogError(ctx, logger)
		}
		var reason *string
		if endpoint.NotReady != mcp.FederationReady {
			reason = new(string(endpoint.NotReady))
		}
		endpoints = append(endpoints, &gen.WorkloadConnectionEndpoint{
			ResourceURL:             endpoint.ResourceURL,
			APIHost:                 resource.Host,
			Issuer:                  endpoint.Issuer,
			TokenEndpoint:           endpoint.TokenEndpoint,
			OnAuthenticationHost:    endpoint.OnAuthenticationHost,
			GrantTypesSupported:     endpoint.GrantTypesSupported,
			WorkloadGrantAdvertised: endpoint.WorkloadGrantAdvertised,
			Ready:                   endpoint.NotReady == mcp.FederationReady,
			NotReadyReason:          reason,
		})
	}

	return &gen.WorkloadConnectionDetails{
		McpServerID:   server.ID.String(),
		McpServerName: conv.FromPGTextOrEmpty[string](server.Name),
		Endpoints:     endpoints,
	}, nil
}

// OrganizationConnectionDetails reads the organization's own token endpoint.
// It is a sibling of ConnectionDetails rather than a mode of it: it names no
// MCP server, so it needs no mcp:read and reads the same for every caller in
// the organization, including one whose API key names a project.
func (s *Service) OrganizationConnectionDetails(ctx context.Context, _ *gen.OrganizationConnectionDetailsPayload) (*gen.WorkloadOrganizationConnectionDetails, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadRead)
	if err != nil {
		return nil, err
	}

	server, err := s.federation.OrganizationAuthorizationServer(ctx, t.organizationID)
	if err != nil {
		return nil, fmt.Errorf("resolve organization authorization server: %w", err)
	}

	var reason *string
	if server.NotReady != mcp.FederationReady {
		reason = new(string(server.NotReady))
	}
	return &gen.WorkloadOrganizationConnectionDetails{
		Available:               server.Enabled,
		Issuer:                  server.Issuer,
		TokenEndpoint:           server.TokenEndpoint,
		MetadataURL:             server.MetadataURL,
		OnAuthenticationHost:    server.OnAuthenticationHost,
		GrantTypesSupported:     server.GrantTypesSupported,
		WorkloadGrantAdvertised: server.WorkloadGrantAdvertised,
		Ready:                   server.NotReady == mcp.FederationReady,
		NotReadyReason:          reason,
	}, nil
}
