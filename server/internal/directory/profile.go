package directory

import (
	"strings"

	"github.com/google/uuid"
)

// Group describes an active directory group assigned to a user.
type Group struct {
	// ExternalID is the group identifier assigned by the directory provider.
	ExternalID string `json:"external_id"`

	// Name is the display name supplied by the directory provider.
	Name string `json:"name"`
}

// UserProfile is the current directory state associated with a Gram user.
type UserProfile struct {
	// ID is the internal identifier for the selected directory user row.
	ID uuid.UUID

	// UserID is the Gram user identifier linked to the directory profile.
	UserID string

	// ExternalID is the user identifier assigned by the directory provider.
	ExternalID string

	// Email is the email supplied by the directory provider.
	Email string

	// DepartmentName is the directory department attribute.
	DepartmentName string

	// JobTitle is the directory job title attribute.
	JobTitle string

	// EmployeeType is the directory employment type attribute.
	EmployeeType string

	// DivisionName is the directory division attribute.
	DivisionName string

	// CostCenterName is the directory cost centre attribute.
	CostCenterName string

	// RawAttributes contains the complete directory provider payload.
	RawAttributes map[string]any

	// Groups contains the active groups assigned to the selected profile.
	Groups []Group
}

// UserAttributes is the provider-independent allowlist of directory
// attributes used to enrich telemetry and plugin attribution. The fields are
// predefined directory attributes with consistent meanings across providers;
// customer-defined attributes remain available through
// UserProfile.RawAttributes.
type UserAttributes struct {
	DepartmentName string `json:"department_name,omitempty"`
	JobTitle       string `json:"job_title,omitempty"`
	EmployeeType   string `json:"employee_type,omitempty"`
	DivisionName   string `json:"division_name,omitempty"`
	CostCenterName string `json:"cost_center_name,omitempty"`
}

// IsZero reports whether the profile has any supported enrichment attributes.
func (a UserAttributes) IsZero() bool {
	var zero UserAttributes
	return a == zero
}

// Attributes returns the supported enrichment attributes of a profile.
func (p UserProfile) Attributes() UserAttributes {
	return UserAttributes{
		DepartmentName: p.DepartmentName,
		JobTitle:       p.JobTitle,
		EmployeeType:   p.EmployeeType,
		DivisionName:   p.DivisionName,
		CostCenterName: p.CostCenterName,
	}
}

// GroupNames returns the active directory group names in profile order.
func (p UserProfile) GroupNames() []string {
	names := make([]string, len(p.Groups))
	for i, group := range p.Groups {
		names[i] = group.Name
	}
	return names
}

// stringAttribute reads one supported attribute from the raw directory
// payload. Mappings are customer-controlled, so anything that is not a string
// is ignored, and a value that is only whitespace is the same as an absent
// one.
func stringAttribute(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return strings.TrimSpace(value)
}
