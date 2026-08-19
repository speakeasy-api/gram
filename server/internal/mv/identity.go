package mv

import (
	gen "github.com/speakeasy-api/gram/server/gen/identity"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identity"
)

// BuildIdentityView converts a resolved identity into the API response type.
//
// Absent attributes are returned as null rather than as empty strings so a
// client can tell "the directory has no job title for this person" from "the
// directory recorded a blank job title".
func BuildIdentityView(resolved identity.Identity) *gen.IdentityModel {
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
