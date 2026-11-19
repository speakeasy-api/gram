package inferenceprofiles

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// ProviderEnum applies the allowed-values constraint to an inference
// provider attribute.
func ProviderEnum() {
	Enum("bedrock", "vertex", "gateway")
}

// HarnessEnum applies the allowed-values constraint to a harness attribute.
func HarnessEnum() {
	Enum("claude_code", "codex")
}

var _ = Service("inferenceProfiles", func() {
	Description("Manage the inference endpoints (Amazon Bedrock, Google Vertex AI, or a gateway) the device agent points each person's AI harnesses at. Requires an organization administrator session.")
	Meta("openapi:extension:x-speakeasy-group", "inferenceProfiles")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listProfiles", func() {
		Description("List the organization's inference profiles, by name.")
		Payload(func() {
			security.SessionPayload()
		})
		Result(ListInferenceProfilesResult)
		HTTP(func() {
			GET("/rpc/inferenceProfiles.listProfiles")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listInferenceProfiles")
		Meta("openapi:extension:x-speakeasy-name-override", "listProfiles")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "InferenceProfiles"}`)
	})

	Method("createProfile", func() {
		Description("Create an inference profile. A profile names where inference goes and never carries a credential.")
		Payload(func() {
			security.SessionPayload()
			profileFormAttributes()
			Required("name", "provider", "settings")
		})
		Result(InferenceProfileModel)
		HTTP(func() {
			POST("/rpc/inferenceProfiles.createProfile")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createInferenceProfile")
		Meta("openapi:extension:x-speakeasy-name-override", "createProfile")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateInferenceProfile", "type": "mutation"}`)
	})

	Method("updateProfile", func() {
		Description("Replace an inference profile's name, provider and settings.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, "The profile ID.", func() { Format(FormatUUID) })
			profileFormAttributes()
			Required("id", "name", "provider", "settings")
		})
		Result(InferenceProfileModel)
		HTTP(func() {
			POST("/rpc/inferenceProfiles.updateProfile")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateInferenceProfile")
		Meta("openapi:extension:x-speakeasy-name-override", "updateProfile")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateInferenceProfile", "type": "mutation"}`)
	})

	Method("deleteProfile", func() {
		Description("Delete an inference profile and every rule that uses it.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, "The profile ID.", func() { Format(FormatUUID) })
			Required("id")
		})
		HTTP(func() {
			DELETE("/rpc/inferenceProfiles.deleteProfile")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "deleteInferenceProfile")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteProfile")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteInferenceProfile", "type": "mutation"}`)
	})

	Method("listRules", func() {
		Description("List the organization's inference profile rules, by position.")
		Payload(func() {
			security.SessionPayload()
		})
		Result(ListInferenceProfileRulesResult)
		HTTP(func() {
			GET("/rpc/inferenceProfiles.listRules")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listInferenceProfileRules")
		Meta("openapi:extension:x-speakeasy-name-override", "listRules")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "InferenceProfileRules"}`)
	})

	Method("createRule", func() {
		Description("Bind an inference profile to a principal, for one harness or all of them.")
		Payload(func() {
			security.SessionPayload()
			ruleFormAttributes()
			Required("profile_id", "principal_urn")
		})
		Result(InferenceProfileRuleModel)
		HTTP(func() {
			POST("/rpc/inferenceProfiles.createRule")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createInferenceProfileRule")
		Meta("openapi:extension:x-speakeasy-name-override", "createRule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateInferenceProfileRule", "type": "mutation"}`)
	})

	Method("updateRule", func() {
		Description("Replace a rule's profile, principal, harness and position. Reorder rules by updating their positions.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, "The rule ID.", func() { Format(FormatUUID) })
			ruleFormAttributes()
			Required("id", "profile_id", "principal_urn")
		})
		Result(InferenceProfileRuleModel)
		HTTP(func() {
			POST("/rpc/inferenceProfiles.updateRule")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateInferenceProfileRule")
		Meta("openapi:extension:x-speakeasy-name-override", "updateRule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateInferenceProfileRule", "type": "mutation"}`)
	})

	Method("deleteRule", func() {
		Description("Delete an inference profile rule.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, "The rule ID.", func() { Format(FormatUUID) })
			Required("id")
		})
		HTTP(func() {
			DELETE("/rpc/inferenceProfiles.deleteRule")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "deleteInferenceProfileRule")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteRule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteInferenceProfileRule", "type": "mutation"}`)
	})

	Method("preview", func() {
		Description("Show which profile each harness resolves to for a person, the rule that decided it, and the settings the device agent would receive.")
		Payload(func() {
			security.SessionPayload()
			Attribute("email", String, "The person's email address.", func() { Format(FormatEmail) })
			Required("email")
		})
		Result(PreviewInferenceProfilesResult)
		HTTP(func() {
			GET("/rpc/inferenceProfiles.preview")
			Param("email")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "previewInferenceProfiles")
		Meta("openapi:extension:x-speakeasy-name-override", "preview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PreviewInferenceProfiles"}`)
	})
})

func profileFormAttributes() {
	Attribute("name", String, "Display name, unique within the organization.", func() {
		MinLength(1)
		MaxLength(100)
	})
	Attribute("provider", String, "Where inference goes.", func() { ProviderEnum() })
	Attribute("settings", InferenceProfileSettingsModel)
}

func ruleFormAttributes() {
	Attribute("profile_id", String, "The profile the rule applies.", func() { Format(FormatUUID) })
	Attribute("principal_urn", String, "Who the rule applies to: `user:<id>`, `user:all`, `email:<address>`, `role:<kind>:<id>`, `directory_group:<id>` or `directory_attribute:<key>:<value>`.")
	Attribute("harness", String, "The harness the rule applies to. Absent applies it to every harness that supports the profile's provider.", func() { HarnessEnum() })
	Attribute("position", Int, "Breaks ties between matching rules at the same level; lower wins.", func() {
		Minimum(0)
		Default(0)
	})
}

var InferenceProfileModelsModel = Type("InferenceProfileModels", func() {
	Description("Model IDs to pin, in the provider's own naming. Each is optional.")
	Attribute("default", String, "The model the harness uses by default.")
	Attribute("opus", String, "The model Claude Code uses for its Opus tier.")
	Attribute("sonnet", String, "The model Claude Code uses for its Sonnet tier.")
	Attribute("haiku", String, "The model Claude Code uses for its Haiku tier.")
})

var InferenceProfileSettingsModel = Type("InferenceProfileSettings", func() {
	Description("Provider settings. Bedrock requires region; Vertex requires region and gcp_project_id; a gateway requires base_url. Never a credential.")
	Attribute("region", String, "Cloud region, for example `us-west-2` (Bedrock) or `us-east5` (Vertex).")
	Attribute("aws_profile", String, "AWS profile name developers sign in with, for example an IAM Identity Center profile. Bedrock only.")
	Attribute("gcp_project_id", String, "Google Cloud project ID. Vertex only.")
	Attribute("base_url", String, "HTTPS endpoint. Required for a gateway; optional for Bedrock and Vertex.", func() { Format(FormatURI) })
	Attribute("models", InferenceProfileModelsModel)
})

var InferenceProfileModel = Type("InferenceProfile", func() {
	Required("id", "name", "provider", "settings", "created_at", "updated_at")
	Attribute("id", String, "The profile ID.", func() { Format(FormatUUID) })
	Attribute("name", String, "Display name.")
	Attribute("provider", String, "Where inference goes.", func() { ProviderEnum() })
	Attribute("settings", InferenceProfileSettingsModel)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var InferenceProfileRuleModel = Type("InferenceProfileRule", func() {
	Required("id", "profile_id", "principal_urn", "position", "created_at", "updated_at")
	Attribute("id", String, "The rule ID.", func() { Format(FormatUUID) })
	Attribute("profile_id", String, "The profile the rule applies.", func() { Format(FormatUUID) })
	Attribute("principal_urn", String, "Who the rule applies to.")
	Attribute("harness", String, "The harness the rule applies to. Absent means every harness.", func() { HarnessEnum() })
	Attribute("position", Int, "Tiebreak within a level; lower wins.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var ListInferenceProfilesResult = Type("ListInferenceProfilesResult", func() {
	Required("profiles")
	Attribute("profiles", ArrayOf(InferenceProfileModel))
})

var ListInferenceProfileRulesResult = Type("ListInferenceProfileRulesResult", func() {
	Required("rules")
	Attribute("rules", ArrayOf(InferenceProfileRuleModel))
})

var InferenceProfileResolutionModel = Type("InferenceProfileResolution", func() {
	Required("harness")
	Attribute("harness", String, func() { HarnessEnum() })
	Attribute("profile", InferenceProfileModel, "The profile the harness resolves to. Absent when no rule matches, leaving the harness on its own default.")
	Attribute("rule", InferenceProfileRuleModel, "The rule that decided it.")
})

var PreviewInferenceProfilesResult = Type("PreviewInferenceProfilesResult", func() {
	Required("resolutions", "tool_config")
	Attribute("resolutions", ArrayOf(InferenceProfileResolutionModel), "One entry per supported harness.")
	Attribute("tool_config", MapOf(String, Any), "The tool_config the device agent would receive, keyed by harness.")
})
