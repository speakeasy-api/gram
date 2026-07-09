package mv

import (
	"time"

	extcred "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/conv"
	repo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
)

// BuildExternalCredentialSummaryView converts a supertype row into the
// provider-independent summary returned by the generic list endpoint.
func BuildExternalCredentialSummaryView(ec repo.ExternalCredential) *extcred.ExternalCredentialSummary {
	return &extcred.ExternalCredentialSummary{
		ID:             ec.ID.String(),
		OrganizationID: ec.OrganizationID.String,
		Provider:       ec.Provider,
		Name:           ec.Name,
		CreatedAt:      ec.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      ec.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildExternalCredentialSummaryListView converts supertype rows into summaries.
func BuildExternalCredentialSummaryListView(ecs []repo.ExternalCredential) []*extcred.ExternalCredentialSummary {
	result := make([]*extcred.ExternalCredentialSummary, len(ecs))
	for i, ec := range ecs {
		result[i] = BuildExternalCredentialSummaryView(ec)
	}
	return result
}

// BuildAwsIamCredentialView joins the supertype and AWS subtype rows into the
// full AWS credential view. The authentication mode is derived from which
// columns are populated.
func BuildAwsIamCredentialView(ec repo.ExternalCredential, aws repo.AwsIamCredential) *extcred.AwsIamCredential {
	return &extcred.AwsIamCredential{
		AssumeRoleArn:  conv.FromPGText[string](aws.AssumeRoleArn),
		ExternalID:     conv.FromPGText[string](aws.ExternalID),
		OidcAudience:   conv.FromPGText[string](aws.OidcAudience),
		OidcSubject:    conv.FromPGText[string](aws.OidcSubject),
		StsRegion:      conv.FromPGText[string](aws.StsRegion),
		ID:             ec.ID.String(),
		OrganizationID: ec.OrganizationID.String,
		Provider:       ec.Provider,
		Name:           ec.Name,
		CreatedAt:      ec.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      ec.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildGcpIamCredentialView joins the supertype and GCP subtype rows into the
// full GCP credential view. The authentication mode is derived from which
// columns are populated.
func BuildGcpIamCredentialView(ec repo.ExternalCredential, gcp repo.GcpIamCredential) *extcred.GcpIamCredential {
	return &extcred.GcpIamCredential{
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
