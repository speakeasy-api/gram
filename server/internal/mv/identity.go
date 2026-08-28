package mv

import (
	gen "github.com/speakeasy-api/gram/server/gen/identity"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/identity"
)

// BuildIdentityView converts a resolved identity into the API response type.
//
// A directory attribute that is unset, or blank once trimmed, is returned as
// null rather than as an empty string, so a client renders it as absent
// instead of as a blank value.
func BuildIdentityView(resolved identity.Record) *gen.IdentityModel {
	// A subject with no directory row still renders the section, empty.
	var empty directory.UserProfile
	profile := resolved.Directory
	if profile == nil {
		profile = &empty
	}

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
			DepartmentName: conv.PtrEmpty(profile.DepartmentName),
			JobTitle:       conv.PtrEmpty(profile.JobTitle),
			EmployeeType:   conv.PtrEmpty(profile.EmployeeType),
			DivisionName:   conv.PtrEmpty(profile.DivisionName),
			CostCenterName: conv.PtrEmpty(profile.CostCenterName),
			Groups:         profile.GroupNames(),
		},
	}
}
