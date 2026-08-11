package mv

import (
	"time"

	adminec "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/conv"
	repo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
)

// BuildPlatformExternalCredentialSummaryView converts a supertype row into the
// provider-independent summary returned by the platform-admin list endpoint.
// Platform rows carry no organization, so OrganizationID is empty.
func BuildPlatformExternalCredentialSummaryView(ec repo.ExternalCredential) *adminec.ExternalCredentialSummary {
	return &adminec.ExternalCredentialSummary{
		ID:             ec.ID.String(),
		OrganizationID: ec.OrganizationID.String,
		Provider:       ec.Provider,
		Name:           ec.Name,
		CreatedAt:      ec.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      ec.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildPlatformExternalCredentialSummaryListView converts supertype rows into
// platform-admin summaries.
func BuildPlatformExternalCredentialSummaryListView(ecs []repo.ExternalCredential) []*adminec.ExternalCredentialSummary {
	result := make([]*adminec.ExternalCredentialSummary, len(ecs))
	for i, ec := range ecs {
		result[i] = BuildPlatformExternalCredentialSummaryView(ec)
	}
	return result
}

// BuildPlatformGcpIamCredentialView joins the supertype and GCP subtype rows
// into the full platform GCP credential view. The authentication mode is derived
// from which columns are populated.
func BuildPlatformGcpIamCredentialView(ec repo.ExternalCredential, gcp repo.GcpIamCredential) *adminec.GcpIamCredential {
	return &adminec.GcpIamCredential{
		ImpersonateServiceAccount: conv.FromPGText[string](gcp.ImpersonateServiceAccount),
		WifPoolID:                 conv.FromPGText[string](gcp.WifPoolID),
		WifProviderID:             conv.FromPGText[string](gcp.WifProviderID),
		WifProjectNumber:          conv.FromPGText[string](gcp.WifProjectNumber),
		ID:                        ec.ID.String(),
		OrganizationID:            ec.OrganizationID.String,
		Provider:                  ec.Provider,
		Name:                      ec.Name,
		CreatedAt:                 ec.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                 ec.UpdatedAt.Time.Format(time.RFC3339),
	}
}
