package shared

import (
	. "goa.design/goa/v3/dsl"
)

var Organization = Type("Organization", func() {
	Required("id", "name", "slug", "account_type", "webhooks_onboarded", "webhooks_enabled", "created_at", "updated_at")

	Attribute("id", String, "The ID of the organization")
	Attribute("name", String, "The name of the organization")
	Attribute("slug", Slug, "The slug of the organization")
	Attribute("account_type", String, "The account type of the organization")
	Attribute("webhooks_onboarded", Boolean, "Whether webhooks support is initialized for the organization")
	Attribute("webhooks_enabled", Boolean, "Whether webhooks are enabled for the organization")

	Attribute("created_at", String, func() {
		Description("The creation date of the organization.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the organization.")
		Format(FormatDateTime)
	})
})
