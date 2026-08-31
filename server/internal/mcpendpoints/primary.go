package mcpendpoints

import (
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

// PrimaryEndpoint selects the address that stands for a server when a single
// URL must be chosen: its custom-domain endpoint when it has one, else its
// platform endpoint. This mirrors how the toolsets.mcp_slug/custom_domain_id
// column pair summarized a hosted server's address, so surfaces that used to
// read those columns keep advertising the same location once they read
// endpoints instead. Within a scope, the earliest-created endpoint wins so the
// choice is stable as more addresses are added. Returns nil for an empty
// slice.
func PrimaryEndpoint(endpoints []repo.McpEndpoint) *repo.McpEndpoint {
	var primary *repo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case primary == nil:
			primary = candidate
		case candidate.CustomDomainID.Valid != primary.CustomDomainID.Valid:
			if candidate.CustomDomainID.Valid {
				primary = candidate
			}
		case candidate.CreatedAt.Time.Before(primary.CreatedAt.Time):
			primary = candidate
		}
	}
	return primary
}
