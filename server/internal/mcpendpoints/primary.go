package mcpendpoints

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

// PrimaryEndpoint selects the address that stands for a server when a single
// URL must be chosen: domain-root endpoints first, then other custom-domain
// endpoints, then platform endpoints; ties break by age then id.
func PrimaryEndpoint(endpoints []repo.McpEndpoint) *repo.McpEndpoint {
	var primary *repo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		if primary == nil || endpointBefore(candidate, primary) {
			primary = candidate
		}
	}
	return primary
}

func endpointBefore(a, b *repo.McpEndpoint) bool {
	if ra, rb := endpointRank(a), endpointRank(b); ra != rb {
		return ra < rb
	}
	if !a.CreatedAt.Time.Equal(b.CreatedAt.Time) {
		return a.CreatedAt.Time.Before(b.CreatedAt.Time)
	}
	return a.ID.String() < b.ID.String()
}

func endpointRank(e *repo.McpEndpoint) int {
	switch {
	case e.CustomDomainID.Valid && e.IsDomainRoot.Valid && e.IsDomainRoot.Bool:
		return 0
	case e.CustomDomainID.Valid:
		return 1
	default:
		return 2
	}
}

// EndpointURL renders an endpoint's public URL. domain is the resolved
// custom-domain name, empty for platform endpoints served under serverURL.
func EndpointURL(endpoint *repo.McpEndpoint, domain string, serverURL string) (string, error) {
	base := serverURL
	if domain != "" {
		if endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool {
			return "https://" + domain, nil
		}
		base = "https://" + domain
	}
	u, err := url.JoinPath(base, "mcp", endpoint.Slug)
	if err != nil {
		return "", fmt.Errorf("build endpoint URL: %w", err)
	}
	return u, nil
}

// PrimaryEndpointURL resolves a server's primary addressable endpoint to its
// public URL; "" when the server has none.
func PrimaryEndpointURL(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, mcpServerID uuid.UUID, serverURL string) (string, error) {
	rows, err := repo.New(db).ListAddressableMCPEndpointsByMCPServerID(ctx, repo.ListAddressableMCPEndpointsByMCPServerIDParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		McpServerID:    mcpServerID,
	})
	if err != nil {
		return "", fmt.Errorf("list addressable mcp server endpoints: %w", err)
	}

	endpoints := make([]repo.McpEndpoint, 0, len(rows))
	domains := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, row.McpEndpoint)
		if row.CustomDomain.Valid {
			domains[row.McpEndpoint.ID] = row.CustomDomain.String
		}
	}

	primary := PrimaryEndpoint(endpoints)
	if primary == nil {
		return "", nil
	}
	return EndpointURL(primary, domains[primary.ID], serverURL)
}
