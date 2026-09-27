package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// BuildWorkloadIssuerView renders one trusted workload issuer.
//
// project_id is rendered as the empty string at the organization tier rather
// than omitted, so a client never has to distinguish "absent" from "shared".
func BuildWorkloadIssuerView(row repo.WorkloadIssuer) *types.WorkloadIssuer {
	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID.String()
	}

	return &types.WorkloadIssuer{
		ID:                     row.ID.String(),
		OrganizationID:         row.OrganizationID,
		ProjectID:              projectID,
		Name:                   row.Name,
		Issuer:                 row.Issuer,
		JwksURI:                row.JwksUri,
		AllowWildcardAdmission: row.AllowWildcardAdmission,
		CreatedAt:              row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildWorkloadIssuerListView renders the trusted issuers in list order.
func BuildWorkloadIssuerListView(rows []repo.WorkloadIssuer) []*types.WorkloadIssuer {
	views := make([]*types.WorkloadIssuer, 0, len(rows))
	for _, row := range rows {
		views = append(views, BuildWorkloadIssuerView(row))
	}
	return views
}

// BuildWorkloadAdmissionView renders one admitted subject with the agent it
// resolves to.
//
// An empty agent_id is a real state, not a rendering gap: the assignment is
// keyed independently of the admission, so a row can exist without one. The
// token endpoint refuses such a workload for having no agent, which is why the
// field is surfaced rather than hidden.
//
// wildcard_active reports whether a wildcard rule is currently in force. A rule
// written while its issuer permitted wildcard matching stays stored after the
// permission is cleared, and matches nothing until it is restored. Showing the
// row without that distinction would read as working configuration.
func BuildWorkloadAdmissionView(row repo.ListWorkloadAdmissionsRow) *types.WorkloadAdmission {
	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID.String()
	}

	agentID := ""
	if row.AgentID.Valid {
		agentID = row.AgentID.UUID.String()
	}

	wildcardActive := true
	if workloadidentity.MatchKind(row.MatchKind) == workloadidentity.MatchKindWildcard {
		wildcardActive = row.AllowWildcardAdmission
	}

	return &types.WorkloadAdmission{
		ID:               row.ID.String(),
		OrganizationID:   row.OrganizationID,
		ProjectID:        projectID,
		WorkloadIssuerID: row.WorkloadIssuerID.String(),
		Issuer:           row.IssuerUrl,
		IssuerName:       row.IssuerName,
		Subject:          row.Subject,
		MatchKind:        row.MatchKind,
		Name:             conv.PtrValOrEmpty(conv.FromPGText[string](row.Name), ""),
		AgentID:          agentID,
		AgentName:        conv.PtrValOrEmpty(conv.FromPGText[string](row.AgentName), ""),
		WildcardActive:   wildcardActive,
		CreatedAt:        row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildWorkloadAdmissionListView renders the admitted set in list order.
func BuildWorkloadAdmissionListView(rows []repo.ListWorkloadAdmissionsRow) []*types.WorkloadAdmission {
	views := make([]*types.WorkloadAdmission, 0, len(rows))
	for _, row := range rows {
		views = append(views, BuildWorkloadAdmissionView(row))
	}
	return views
}
