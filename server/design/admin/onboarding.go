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

var AdminOnboardingUseCase = Type("AdminOnboardingUseCase", func() {
	Attribute("id", String)
	Attribute("slug", String, "Stable name the onboarding survey uses.")
	Attribute("name", String)
	Attribute("description", String)
	Attribute("default_playbook_id", String, "The default playbook, once one is marked.")
	Required("id", "slug", "name", "description")
})

var AdminOnboardingUseCaseList = Type("AdminOnboardingUseCaseList", func() {
	Attribute("use_cases", ArrayOf(AdminOnboardingUseCase))
	Required("use_cases")
})

var AdminOnboardingPlaybookStep = Type("AdminOnboardingPlaybookStep", func() {
	Attribute("slug", String)
	Attribute("title", String)
	Required("slug", "title")
})

var AdminOnboardingPlaybook = Type("AdminOnboardingPlaybook", func() {
	Attribute("id", String)
	Attribute("use_case_id", String, "Set for a shared playbook: the use case it belongs to.")
	Attribute("use_case_slug", String)
	Attribute("use_case_name", String)
	Attribute("organization_id", String, "Set for a custom playbook: the one organization it belongs to. A playbook has a use case or an organization, never both.")
	Attribute("organization_name", String, "That organization's name.")
	Attribute("name", String)
	Attribute("description", String)
	Attribute("is_default", Boolean, "The default playbook of its use case. Only a shared playbook can be one.")
	Attribute("steps", ArrayOf(AdminOnboardingPlaybookStep), "Top-level steps in walking order; a group brings its cards.")
	Required("id", "name", "description", "is_default", "steps")
})

var AdminOnboardingPlaybookList = Type("AdminOnboardingPlaybookList", func() {
	Attribute("playbooks", ArrayOf(AdminOnboardingPlaybook))
	Required("playbooks")
})

var AdminOnboardingStepApplicability = Type("AdminOnboardingStepApplicability", func() {
	Attribute("slug", String)
	Attribute("title", String)
	Attribute("applies", Boolean, "Whether the recorded stack supports the step.")
	Attribute("reason", String, "Why not, when it does not.")
	Required("slug", "title", "applies", "reason")
})

var AdminOrganizationOnboardingPlaybook = Type("AdminOrganizationOnboardingPlaybook", func() {
	Attribute("organization_id", String)
	Attribute("playbook", AdminOnboardingPlaybook, "The assigned playbook. Absent until staff assign one.")
	Attribute("applicability", ArrayOf(AdminOnboardingStepApplicability), "Each step of the assigned playbook against the recorded stack.")
	Required("organization_id", "applicability")
})

func onboardingPlaybookMethods() {
	Method("listOnboardingUseCases", func() {
		Description("Read the use cases staff defined, each with its default playbook.")
		Payload(func() { security.AdminAuthPayload() })
		Result(AdminOnboardingUseCaseList)
		HTTP(func() { GET("/admin/onboarding.useCases"); Response(StatusOK) })
		Meta("openapi:operationId", "adminListOnboardingUseCases")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOnboardingUseCases"}`)
	})

	Method("createOnboardingUseCase", func() {
		Description("Define a use case.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("slug", String, "Lowercase letters, digits and dashes.")
			Attribute("name", String)
			Attribute("description", String)
			Required("slug", "name")
		})
		Result(AdminOnboardingUseCase)
		HTTP(func() { POST("/admin/onboarding.useCases.create"); Response(StatusOK) })
		Meta("openapi:operationId", "adminCreateOnboardingUseCase")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CreateAdminOnboardingUseCase"}`)
	})

	Method("updateOnboardingUseCase", func() {
		Description("Rename or describe a use case. The slug never changes.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("use_case_id", String)
			Attribute("name", String)
			Attribute("description", String)
			Required("use_case_id", "name")
		})
		Result(AdminOnboardingUseCase)
		HTTP(func() { POST("/admin/onboarding.useCases.update"); Response(StatusOK) })
		Meta("openapi:operationId", "adminUpdateOnboardingUseCase")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"UpdateAdminOnboardingUseCase"}`)
	})

	Method("deleteOnboardingUseCase", func() {
		Description("Retire a use case and its playbooks. Organizations assigned one of them fall back to their setup task selection.")
		Payload(func() { security.AdminAuthPayload(); Attribute("use_case_id", String); Required("use_case_id") })
		Result(AdminOnboardingUseCaseList)
		HTTP(func() { POST("/admin/onboarding.useCases.delete"); Response(StatusOK) })
		Meta("openapi:operationId", "adminDeleteOnboardingUseCase")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DeleteAdminOnboardingUseCase"}`)
	})

	Method("listOnboardingPlaybooks", func() {
		Description("Read every playbook, or, when an organization is named, the shared ones and its own.")
		Payload(func() { security.AdminAuthPayload(); Attribute("organization_id", String) })
		Result(AdminOnboardingPlaybookList)
		HTTP(func() { GET("/admin/onboarding.playbooks"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminListOnboardingPlaybooks")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOnboardingPlaybooks"}`)
	})

	Method("createOnboardingPlaybook", func() {
		Description("Create a playbook from top-level steps, for a use case or for one organization, not both. Every prerequisite of a step must be in the playbook, before it. Marking a default replaces the use case's previous default.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("use_case_id", String, "The use case a shared playbook belongs to.")
			Attribute("organization_id", String, "The one organization a custom playbook belongs to.")
			Attribute("name", String)
			Attribute("description", String)
			Attribute("is_default", Boolean)
			Attribute("step_slugs", ArrayOf(String), "Top-level step slugs in walking order.")
			Required("name", "step_slugs")
		})
		Result(AdminOnboardingPlaybook)
		HTTP(func() { POST("/admin/onboarding.playbooks.create"); Response(StatusOK) })
		Meta("openapi:operationId", "adminCreateOnboardingPlaybook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CreateAdminOnboardingPlaybook"}`)
	})

	Method("updateOnboardingPlaybook", func() {
		Description("Replace a playbook's name, description, default mark and steps. A custom playbook is also checked against its organization's stack.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("playbook_id", String)
			Attribute("name", String)
			Attribute("description", String)
			Attribute("is_default", Boolean)
			Attribute("step_slugs", ArrayOf(String))
			Required("playbook_id", "name", "step_slugs")
		})
		Result(AdminOnboardingPlaybook)
		HTTP(func() { POST("/admin/onboarding.playbooks.update"); Response(StatusOK) })
		Meta("openapi:operationId", "adminUpdateOnboardingPlaybook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"UpdateAdminOnboardingPlaybook"}`)
	})

	Method("deleteOnboardingPlaybook", func() {
		Description("Retire a playbook. Organizations assigned it fall back to their setup task selection.")
		Payload(func() { security.AdminAuthPayload(); Attribute("playbook_id", String); Required("playbook_id") })
		Result(AdminOnboardingPlaybookList)
		HTTP(func() { POST("/admin/onboarding.playbooks.delete"); Response(StatusOK) })
		Meta("openapi:operationId", "adminDeleteOnboardingPlaybook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DeleteAdminOnboardingPlaybook"}`)
	})

	Method("cloneOnboardingPlaybook", func() {
		Description("Copy a playbook into a custom one for an organization, so staff can edit it for that organization alone.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("playbook_id", String)
			Attribute("name", String, "Defaults to the source name.")
			Required("organization_id", "playbook_id")
		})
		Result(AdminOnboardingPlaybook)
		HTTP(func() { POST("/admin/onboarding.playbooks.clone"); Response(StatusOK) })
		Meta("openapi:operationId", "adminCloneOnboardingPlaybook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CloneAdminOnboardingPlaybook"}`)
	})

	Method("getOrganizationOnboardingPlaybook", func() {
		Description("Read the playbook assigned to an organization and how each of its steps fares against the recorded stack.")
		Payload(func() { security.AdminAuthPayload(); Attribute("organization_id", String); Required("organization_id") })
		Result(AdminOrganizationOnboardingPlaybook)
		HTTP(func() { GET("/admin/organization.onboardingPlaybook"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetOrganizationOnboardingPlaybook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOrganizationOnboardingPlaybook"}`)
	})

	Method("assignOrganizationOnboardingPlaybook", func() {
		Description("Assign a playbook to an organization, or clear it. Rejected when a step's methods do not apply to the recorded stack; the error names the steps.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("playbook_id", String, "Omit to clear the assignment.")
			Required("organization_id")
		})
		Result(AdminOrganizationOnboardingPlaybook)
		HTTP(func() { POST("/admin/organization.onboardingPlaybook"); Response(StatusOK) })
		Meta("openapi:operationId", "adminAssignOrganizationOnboardingPlaybook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AssignAdminOrganizationOnboardingPlaybook"}`)
	})
}
