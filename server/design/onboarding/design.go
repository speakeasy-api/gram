package onboarding

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var useCaseEnum = []any{"observability", "cost-tracking", "security", "mcp-gateway"}
var mdmVendorEnum = []any{"jamf", "intune", "iru", "none"}
var stageEnum = []any{"stack", "use-case", "steps", "done"}

var Plan = Type("OnboardingPlan", func() {
	Description("A plan a provider sells. It applies to every product of that provider.")
	Attribute("slug", String, "Stable plan key, e.g. anthropic-enterprise")
	Attribute("name", String, "Display name")
	Required("slug", "name")
})

var Product = Type("OnboardingProduct", func() {
	Description("A product surface the platform can cover.")
	Attribute("slug", String, "Stable product key")
	Attribute("name", String, "Display name")
	Attribute("source_ids", ArrayOf(String), "hook_source ids whose events belong to this product")
	Required("slug", "name", "source_ids")
})

var Provider = Type("OnboardingProvider", func() {
	Description("A vendor whose products the platform can cover, with the plans it sells and the products it makes.")
	Attribute("slug", String, "Stable provider key")
	Attribute("name", String, "Display name")
	Attribute("plans", ArrayOf(Plan), "Plans an admin can declare for this provider. Empty when the provider has no plans.")
	Attribute("products", ArrayOf(Product), "Products of this provider, in display order")
	Required("slug", "name", "plans", "products")
})

var Option = Type("OnboardingOption", func() {
	Description("A fixed choice offered by the wizard.")
	Attribute("slug", String, "Stable key")
	Attribute("name", String, "Display name")
	Required("slug", "name")
})

var ReferenceData = Type("OnboardingReferenceData", func() {
	Description("Everything the wizard needs to render its questions.")
	Attribute("providers", ArrayOf(Provider), "Providers the admin can pick, in display order, each with its plans and products")
	Attribute("use_cases", ArrayOf(Option), "Use cases the admin can pick exactly one of")
	Attribute("mdm_vendors", ArrayOf(Option), "MDM vendors the admin can declare")
	Required("providers", "use_cases", "mdm_vendors")
})

var SelectedProvider = Type("OnboardingSelectedProvider", func() {
	Attribute("provider_slug", String, "Slug of a provider from the reference data")
	Attribute("plan_slug", String, "Slug of one of the provider's plans. Omitted only when the provider has no plans.")
	Required("provider_slug")
})

var Answers = Type("OnboardingAnswers", func() {
	Description("What an organization admin told us during onboarding.")
	Attribute("providers", ArrayOf(SelectedProvider), "Providers the organization uses, each with the plan it is on")
	Attribute("product_slugs", ArrayOf(String), "Products the organization uses. Each belongs to one of the selected providers.")
	Attribute("mdm_vendor", String, "MDM vendor the organization uses", func() { Enum(mdmVendorEnum...) })
	Attribute("use_case", String, "The single use case the admin picked. Omitted until the admin picks one.", func() { Enum(useCaseEnum...) })
	Attribute("completed_at", String, "Set once every step for the use case has been verified", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("providers", "product_slugs", "mdm_vendor", "updated_at")
})

var Step = Type("OnboardingStep", func() {
	Description("One recommended action, verified against real evidence.")
	Attribute("slug", String, "Stable step key, unique within an organization's plan")
	Attribute("title", String, "Short imperative title")
	Attribute("description", String, "What to do and why it is the quickest path")
	Attribute("technique_slug", String, "Coverage technique this step sets up")
	Attribute("product_slug", String, "Product this step covers. Omitted for steps that cover the whole organization.")
	Attribute("destination", String, "Dashboard area where the work happens", func() {
		Enum("plugins", "integrations", "policies", "mcp", "devices", "settings")
	})
	Attribute("evidence", String, "What must be observed in the last 30 days for the step to count as verified")
	Attribute("verified_at", String, "When the step's evidence was last confirmed", func() { Format(FormatDateTime) })
	Required("slug", "title", "description", "technique_slug", "destination", "evidence")
})

var State = Type("OnboardingState", func() {
	Description("Where an organization is in onboarding.")
	Attribute("stage", String, "stack: the organization's stack has not been saved. use-case: the stack is saved and a use case is needed. steps: a use case is picked and steps remain. done: the use case is covered.", func() {
		Enum(stageEnum...)
	})
	Attribute("answers", Answers, "Omitted until an admin has saved the stack")
	Attribute("next_step", Step, "The one step to do next. Omitted unless the stage is steps.")
	Attribute("steps", ArrayOf(Step), "Every step planned for the picked use case, in order, with verification state")
	Attribute("done", Boolean, "True once the selected use case is covered")
	Required("stage", "steps", "done")
})

var VerifyStepResult = Type("OnboardingVerifyStepResult", func() {
	Attribute("verified", Boolean, "Whether the step's evidence was found")
	Attribute("evidence", String, "Human-readable summary of what was or was not found")
	Attribute("state", State, "Onboarding state after the check")
	Required("verified", "evidence", "state")
})

var UseCaseStatus = Type("OnboardingUseCaseStatus", func() {
	Attribute("use_case", String, func() { Enum(useCaseEnum...) })
	Attribute("verified", Boolean, "Whether the organization has evidence for this use case in the last 30 days")
	Attribute("selected", Boolean, "Whether this is the use case the admin picked")
	Attribute("next_step", Step, "The next step when this is the selected use case and it is not covered yet")
	Required("use_case", "verified", "selected")
})

var UseCaseStatusResult = Type("OnboardingUseCaseStatusResult", func() {
	Attribute("statuses", ArrayOf(UseCaseStatus), "One entry per use case")
	Required("statuses")
})

var _ = Service("onboarding", func() {
	Description("Organization onboarding: the organization's stack, the one use case to reach first, the one next step to get there, and verification against real evidence.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listReferenceData", func() {
		Description("List the providers with their plans and products, the use cases and the MDM vendors the wizard offers.")
		Payload(func() { security.SessionPayload() })
		Result(ReferenceData)
		HTTP(func() {
			GET("/rpc/onboarding.listReferenceData")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listOnboardingReferenceData")
		Meta("openapi:extension:x-speakeasy-name-override", "listReferenceData")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OnboardingReferenceData"}`)
	})

	Method("getOnboarding", func() {
		Description("Get the organization's onboarding answers, stage, planned steps and next step.")
		Payload(func() { security.SessionPayload() })
		Result(State)
		HTTP(func() {
			GET("/rpc/onboarding.get")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOnboarding")
		Meta("openapi:extension:x-speakeasy-name-override", "getOnboarding")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Onboarding"}`)
	})

	Method("saveStack", func() {
		Description("Save the organization's stack: the providers it uses with their plans, its products and its MDM vendor. Keeps a previously picked use case and recomputes the next step. Organization admins only.")
		Payload(func() {
			security.SessionPayload()
			Attribute("providers", ArrayOf(SelectedProvider), "Providers the organization uses, each with the plan it is on")
			Attribute("product_slugs", ArrayOf(String), "Products the organization uses. Each must belong to a selected provider.")
			Attribute("mdm_vendor", String, func() { Enum(mdmVendorEnum...) })
			Required("providers", "product_slugs", "mdm_vendor")
		})
		Result(State)
		HTTP(func() {
			POST("/rpc/onboarding.saveStack")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "saveOnboardingStack")
		Meta("openapi:extension:x-speakeasy-name-override", "saveStack")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SaveOnboardingStack"}`)
	})

	Method("selectUseCase", func() {
		Description("Pick the one use case to reach first. Requires a saved stack. Changing the use case starts a new plan. Organization admins only.")
		Payload(func() {
			security.SessionPayload()
			Attribute("use_case", String, func() { Enum(useCaseEnum...) })
			Required("use_case")
		})
		Result(State)
		HTTP(func() {
			POST("/rpc/onboarding.selectUseCase")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "selectOnboardingUseCase")
		Meta("openapi:extension:x-speakeasy-name-override", "selectUseCase")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SelectOnboardingUseCase"}`)
	})

	Method("verifyStep", func() {
		Description("Check a planned step against the last 30 days of evidence. A passing check records the step as verified and moves to the next one. Organization admins only.")
		Payload(func() {
			security.SessionPayload()
			Attribute("step_slug", String, "Slug of a step in the organization's plan")
			Required("step_slug")
		})
		Result(VerifyStepResult)
		HTTP(func() {
			POST("/rpc/onboarding.verifyStep")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "verifyOnboardingStep")
		Meta("openapi:extension:x-speakeasy-name-override", "verifyStep")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "VerifyOnboardingStep"}`)
	})

	Method("getUseCaseStatus", func() {
		Description("Report, for every use case, whether the organization has evidence for it in the last 30 days. Product pages use this to decide whether their surface is set up.")
		Payload(func() { security.SessionPayload() })
		Result(UseCaseStatusResult)
		HTTP(func() {
			GET("/rpc/onboarding.getUseCaseStatus")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOnboardingUseCaseStatus")
		Meta("openapi:extension:x-speakeasy-name-override", "getUseCaseStatus")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OnboardingUseCaseStatus"}`)
	})
})
