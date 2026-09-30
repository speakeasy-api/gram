package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminOnboardingStep = Type("AdminOnboardingStep", func() {
	Attribute("slug", String)
	Attribute("title", String)
	Attribute("description", String)
	Attribute("parent_slug", String, "The group this card sits under. Absent for a top-level step.")
	Attribute("completion", String, "How the step completes: manual, fact, or children for a group.")
	Attribute("hidden_by_default", Boolean, "Whether an organization that never saved a selection sees the step.")
	Attribute("method_slugs", ArrayOf(String), "Support matrix integration methods the step configures. Empty means the step applies to every stack.")
	Attribute("requires", ArrayOf(String), "Slugs of the steps that must be done before this one.")
	Required("slug", "title", "description", "completion", "hidden_by_default", "method_slugs", "requires")
})

var AdminOnboardingStepList = Type("AdminOnboardingStepList", func() {
	Attribute("steps", ArrayOf(AdminOnboardingStep), "Every step in wizard order; a group precedes its cards.")
	Required("steps")
})

var AdminOnboardingPlan = Type("AdminOnboardingPlan", func() {
	Attribute("slug", String)
	Attribute("name", String)
	Required("slug", "name")
})

var AdminOnboardingPlatform = Type("AdminOnboardingPlatform", func() {
	Attribute("slug", String)
	Attribute("name", String)
	Attribute("family", String)
	Attribute("surface", String)
	Required("slug", "name", "family", "surface")
})

var AdminOnboardingVendorOption = Type("AdminOnboardingVendorOption", func() {
	Attribute("vendor", String, "Vendor name as the support matrix spells it.")
	Attribute("plans", ArrayOf(AdminOnboardingPlan), "Plans the vendor sells. Empty for a vendor with no plans.")
	Attribute("platforms", ArrayOf(AdminOnboardingPlatform), "The vendor's products, all implied when the vendor is selected.")
	Required("vendor", "plans", "platforms")
})

var AdminMdmVendorOption = Type("AdminMdmVendorOption", func() {
	Attribute("slug", String)
	Attribute("name", String)
	Required("slug", "name")
})

var AdminOnboardingStackOptions = Type("AdminOnboardingStackOptions", func() {
	Attribute("vendors", ArrayOf(AdminOnboardingVendorOption), "From the support matrix catalog, in catalog order.")
	Attribute("mdm_vendors", ArrayOf(AdminMdmVendorOption), "Device management software the form offers, ending with other.")
	Required("vendors", "mdm_vendors")
})

var AdminOnboardingStackVendor = Type("AdminOnboardingStackVendor", func() {
	Attribute("vendor", String)
	Attribute("plan_slug", String, "The plan the organization is on with this vendor. Absent for a vendor with no plans.")
	Required("vendor")
})

var AdminOnboardingStack = Type("AdminOnboardingStack", func() {
	Attribute("organization_id", String)
	Attribute("vendors", ArrayOf(AdminOnboardingStackVendor), "The vendors the organization uses. Empty until staff record the stack.")
	Attribute("mdm_vendor", String, "jamf, intune, iru, other or none. Absent until staff record the stack.")
	Attribute("mdm_vendor_name", String, "The software's name when mdm_vendor is other.")
	Required("organization_id", "vendors")
})

func onboardingStackMethods() {
	Method("listOnboardingSteps", func() {
		Description("Read the onboarding steps the code defines, mirrored into the database, with their groups, methods and prerequisites.")
		Payload(func() { security.AdminAuthPayload() })
		Result(AdminOnboardingStepList)
		HTTP(func() { GET("/admin/onboarding.steps"); Response(StatusOK) })
		Meta("openapi:operationId", "adminListOnboardingSteps")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOnboardingSteps"}`)
	})

	Method("getOnboardingStackOptions", func() {
		Description("Read the vendors, plans and platforms the stack form offers, from the support matrix catalog.")
		Payload(func() { security.AdminAuthPayload() })
		Result(AdminOnboardingStackOptions)
		HTTP(func() { GET("/admin/onboarding.stackOptions"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetOnboardingStackOptions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOnboardingStackOptions"}`)
	})

	Method("getOrganizationOnboardingStack", func() {
		Description("Read the stack staff recorded for an organization: its vendors with plans and its device management.")
		Payload(func() { security.AdminAuthPayload(); Attribute("organization_id", String); Required("organization_id") })
		Result(AdminOnboardingStack)
		HTTP(func() { GET("/admin/organization.onboardingStack"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetOrganizationOnboardingStack")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOrganizationOnboardingStack"}`)
	})

	Method("setOrganizationOnboardingStack", func() {
		Description("Replace the stack recorded for an organization. Vendors and plans must come from the catalog; other needs a name and none clears it.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("vendors", ArrayOf(AdminOnboardingStackVendor), "Complete explicit list; an empty array records no vendors.")
			Attribute("mdm_vendor", String, "jamf, intune, iru, other or none.")
			Attribute("mdm_vendor_name", String, "Required when mdm_vendor is other, ignored otherwise.")
			Required("organization_id", "vendors", "mdm_vendor")
		})
		Result(AdminOnboardingStack)
		HTTP(func() { POST("/admin/organization.onboardingStack"); Response(StatusOK) })
		Meta("openapi:operationId", "adminSetOrganizationOnboardingStack")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SetAdminOrganizationOnboardingStack"}`)
	})
}
