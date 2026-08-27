package mv

import (
	gen "github.com/speakeasy-api/gram/server/gen/identity"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identity"
)

// BuildIdentityView converts a resolved identity into the API response type.
//
// Attributes the directory did not supply are returned as null rather than as
// empty strings, so a client renders a missing job title as absent instead of
// as a blank value.
func BuildIdentityView(resolved identity.Record) *gen.IdentityModel {
	return &gen.IdentityModel{
		Kind:            string(resolved.Kind),
		CanonicalUrn:    resolved.CanonicalURN.String(),
		UserIds:         resolved.UserIDs,
		Emails:          resolved.Emails,
		ExternalUserIds: resolved.ExternalUserIDs,
		WorkosUserID:    conv.PtrEmpty(resolved.WorkosUserID),
		DisplayName:     resolved.DisplayName,
		PhotoURL:        conv.PtrEmpty(resolved.PhotoURL),
		Directory: &gen.IdentityDirectory{
			DepartmentName: conv.PtrEmpty(resolved.Directory.DepartmentName),
			JobTitle:       conv.PtrEmpty(resolved.Directory.JobTitle),
			EmployeeType:   conv.PtrEmpty(resolved.Directory.EmployeeType),
			DivisionName:   conv.PtrEmpty(resolved.Directory.DivisionName),
			CostCenterName: conv.PtrEmpty(resolved.Directory.CostCenterName),
			Groups:         resolved.Directory.Groups,
		},
	}
}
