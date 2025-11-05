package shared

import (
	. "goa.design/goa/v3/dsl"
)

var OrganizationFeature = Type("OrganizationFeature", func() {
	Description("Organization-level feature flag configuration")

	Required("organization_id", "feature_name", "enabled", "created_at", "updated_at")

	Attribute("organization_id", String, "The organization that owns the feature flag")
	Attribute("feature_name", String, "The feature name", func() {
		MaxLength(60)
	})
	Attribute("enabled", Boolean, "Indicates if the feature is enabled for the organization")
	Attribute("created_at", String, "Timestamp when the flag was first created", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "Timestamp of the most recent update", func() {
		Format(FormatDateTime)
	})
	Attribute("deleted_at", String, "Timestamp when the feature was disabled", func() {
		Format(FormatDateTime)
	})
})
