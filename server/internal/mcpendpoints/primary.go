package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
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

// PrimaryEndpointURL resolves a server's primary endpoint to its public URL,
// skipping endpoints whose custom domain was deleted concurrently or is not
// owned by organizationID. Returns "" when no endpoint is addressable.
func PrimaryEndpointURL(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, mcpServerID uuid.UUID, serverURL string) (string, error) {
	endpoints, err := repo.New(db).ListMCPEndpointsByMCPServerID(ctx, repo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   projectID,
		McpServerID: mcpServerID,
	})
	if err != nil {
		return "", fmt.Errorf("list mcp server endpoints: %w", err)
	}

	for {
		primary := PrimaryEndpoint(endpoints)
		if primary == nil {
			return "", nil
		}
		domain := ""
		if primary.CustomDomainID.Valid {
			row, err := customdomainsrepo.New(db).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
				ID:             primary.CustomDomainID.UUID,
				OrganizationID: organizationID,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				id := primary.ID
				endpoints = slices.DeleteFunc(endpoints, func(e repo.McpEndpoint) bool { return e.ID == id })
				continue
			case err != nil:
				return "", fmt.Errorf("load custom domain: %w", err)
			}
			domain = row.Domain
		}
		return EndpointURL(primary, domain, serverURL)
	}
}
