package onboarding

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var useCaseEnum = []any{"observability", "cost-tracking", "security", "mcp-gateway"}
var mdmVendorEnum = []any{"jamf", "intune", "iru", "none"}

var Plan = Type("OnboardingPlan", func() {
	Description("A vendor plan an admin can declare for a product.")
	Attribute("slug", String, "Stable plan key, e.g. anthropic-enterprise")
	Attribute("vendor", String, "Vendor that sells the plan")
	Attribute("name", String, "Display name")
	Required("slug", "vendor", "name")
})

var Product = Type("OnboardingProduct", func() {
	Description("A product surface the platform can cover.")
	Attribute("slug", String, "Stable product key")
	Attribute("name", String, "Display name")
	Attribute("vendor", String, "Vendor that sells the product")
	Attribute("source_ids", ArrayOf(String), "hook_source ids whose events belong to this product")
	Attribute("plans", ArrayOf(Plan), "Plans an admin can declare for this product. Empty when the product has no plan.")
	Required("slug", "name", "vendor", "source_ids", "plans")
})

var Option = Type("OnboardingOption", func() {
	Description("A fixed choice offered by the wizard.")
	Attribute("slug", String, "Stable key")
	Attribute("name", String, "Display name")
	Required("slug", "name")
})

var ReferenceData = Type("OnboardingReferenceData", func() {
	Description("Everything the wizard needs to render its questions.")
	Attribute("products", ArrayOf(Product), "Products the admin can pick, in display order")
	Attribute("use_cases", ArrayOf(Option), "Use cases the admin can pick exactly one of")
	Attribute("mdm_vendors", ArrayOf(Option), "MDM vendors the admin can declare")
	Required("products", "use_cases", "mdm_vendors")
})

var SelectedProduct = Type("OnboardingSelectedProduct", func() {
	Attribute("product_slug", String, "Slug of a product from the reference data")
	Attribute("plan_slug", String, "Slug of one of the product's plans. Omitted only when the product has no plans.")
	Required("product_slug")
})

var Answers = Type("OnboardingAnswers", func() {
	Description("What an organization admin told us during onboarding.")
	Attribute("products", ArrayOf(SelectedProduct), "Selected products, each with its declared plan")
	Attribute("mdm_vendor", String, "MDM vendor the organization uses", func() { Enum(mdmVendorEnum...) })
	Attribute("use_case", String, "The single use case the admin picked", func() { Enum(useCaseEnum...) })
	Attribute("completed_at", String, "Set once every step for the use case has been verified", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("products", "mdm_vendor", "use_case", "updated_at")
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
	Attribute("answers", Answers, "Omitted until an admin has saved answers")
	Attribute("next_step", Step, "The one step to do next. Omitted when onboarding is done or has not started.")
	Attribute("steps", ArrayOf(Step), "Every step planned for the answers, in order, with verification state")
	Attribute("done", Boolean, "True once the selected use case is covered")
	Required("steps", "done")
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
	Description("Organization onboarding: what the admin wants to cover, the one next step to get there, and verification against real evidence.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listReferenceData", func() {
		Description("List the products, plans, use cases and MDM vendors the wizard offers.")
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
		Description("Get the organization's onboarding answers, planned steps and next step.")
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

	Method("saveAnswers", func() {
		Description("Save the organization's onboarding answers and recompute the next step. Organization admins only.")
		Payload(func() {
			security.SessionPayload()
			Attribute("products", ArrayOf(SelectedProduct), "Selected products, each with its declared plan")
			Attribute("mdm_vendor", String, func() { Enum(mdmVendorEnum...) })
			Attribute("use_case", String, func() { Enum(useCaseEnum...) })
			Required("products", "mdm_vendor", "use_case")
		})
		Result(State)
		HTTP(func() {
			POST("/rpc/onboarding.saveAnswers")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "saveOnboardingAnswers")
		Meta("openapi:extension:x-speakeasy-name-override", "saveAnswers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SaveOnboardingAnswers"}`)
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
