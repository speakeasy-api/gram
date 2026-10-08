package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// BuildWorkloadTokenEndpointView renders the shared authorization server of a
// user session issuer. project_id and project_name are the empty string for an
// organization-level issuer rather than omitted.
func BuildWorkloadTokenEndpointView(issuer usersessions_repo.UserSessionIssuer, projectName string, issuerURL string, tokenEndpoint string, mcpHost string) *types.WorkloadTokenEndpoint {
	projectID := ""
	if issuer.ProjectID.Valid {
		projectID = issuer.ProjectID.UUID.String()
	}

	return &types.WorkloadTokenEndpoint{
		UserSessionIssuerID:   issuer.ID.String(),
		UserSessionIssuerSlug: issuer.Slug,
		ProjectID:             projectID,
		ProjectName:           projectName,
		Issuer:                issuerURL,
		TokenEndpoint:         tokenEndpoint,
		McpHost:               mcpHost,
	}
}
