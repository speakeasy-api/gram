package identityproviders

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("identityProviders", func() {
	Description("Manage organization identity provider connections used by Speakeasy onboarding.")
	Security(security.Session)
	Security(security.ByKey, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("create", func() {
		Description("Create the organization's identity provider connection and Speakeasy-held signing key.")
		Payload(func() {
			Attribute("kind", String, "Identity provider kind.", func() { Enum("okta") })
			Attribute("tenant_url", String, "HTTPS URL of the identity provider tenant.")
			Required("kind", "tenant_url")
			security.SessionPayload()
			security.ByKeyPayload()
		})
		Result(IdentityProviderConnection)
		HTTP(func() {
			POST("/rpc/identityProviders.create")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createIdentityProvider")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CreateIdentityProvider"}`)
	})

	Method("get", func() {
		Description("Get the organization's live identity provider connection, when configured.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
		})
		Result(GetIdentityProviderResult)
		HTTP(func() {
			GET("/rpc/identityProviders.get")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getIdentityProvider")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"IdentityProvider"}`)
	})

	Method("listApplications", func() {
		Description("List applications from the organization's identity provider, using a five-minute in-memory cache unless force is true.")
		Payload(func() {
			Attribute("force", Boolean, "Bypass the cached application inventory and read it again from the identity provider.")
			security.SessionPayload()
			security.ByKeyPayload()
		})
		Result(ListIdentityProviderApplicationsResult)
		HTTP(func() {
			GET("/rpc/identityProviders.listApplications")
			Param("force")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listIdentityProviderApplications")
		Meta("openapi:extension:x-speakeasy-name-override", "listApplications")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"ListIdentityProviderApplications"}`)
	})

	Method("describeSetup", func() {
		Description("Describe the guided setup steps for the organization's identity provider connection.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
		})
		Result(IdentityProviderSetup)
		HTTP(func() {
			GET("/rpc/identityProviders.describeSetup")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "describeIdentityProviderSetup")
		Meta("openapi:extension:x-speakeasy-name-override", "describeSetup")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"IdentityProviderSetup"}`)
	})

	Method("submitSetupStep", func() {
		Description("Submit administrator-provided values for an identity provider setup step.")
		Payload(func() {
			Attribute("step_key", String, "Setup step key.")
			Attribute("values", ArrayOf(IdentityProviderSetupValue), "Values collected for this step.")
			Required("step_key", "values")
			security.SessionPayload()
			security.ByKeyPayload()
		})
		Result(SubmitSetupStepResult)
		HTTP(func() {
			POST("/rpc/identityProviders.submitSetupStep")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "submitIdentityProviderSetupStep")
		Meta("openapi:extension:x-speakeasy-name-override", "submitSetupStep")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SubmitIdentityProviderSetupStep"}`)
	})

	Method("verifySetupStep", func() {
		Description("Verify an identity provider setup step.")
		Payload(func() {
			Attribute("step_key", String, "Setup step key.")
			Required("step_key")
			security.SessionPayload()
			security.ByKeyPayload()
		})
		Result(IdentityProviderVerifyResult)
		HTTP(func() {
			POST("/rpc/identityProviders.verifySetupStep")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "verifyIdentityProviderSetupStep")
		Meta("openapi:extension:x-speakeasy-name-override", "verifySetupStep")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"VerifyIdentityProviderSetupStep"}`)
	})

	Method("delete", func() {
		Description("Delete an identity provider connection and its signing keys.")
		Payload(func() {
			Attribute("id", String, "Identity provider connection id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})
		HTTP(func() {
			DELETE("/rpc/identityProviders.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteIdentityProvider")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DeleteIdentityProvider"}`)
	})
})

var IdentityProviderConnection = Type("IdentityProviderConnection", func() {
	Description("An organization identity provider connection. Credentials and private signing material are never returned.")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("kind", String, func() { Enum("okta") })
	Attribute("tenant_identifier", String)
	Attribute("display_name", String)
	Attribute("client_id", String, "Identifier of the configured identity provider application.")
	Attribute("status", String, func() { Enum("pending", "awaiting_verification", "active", "failed") })
	Attribute("status_detail", String)
	Attribute("capabilities", ArrayOf(String))
	Attribute("granted_scopes", ArrayOf(String))
	Attribute("jwks_url", String, "Absolute URL of the Speakeasy-hosted public JSON Web Key Set.", func() { Format(FormatURI) })
	Attribute("signing_key_kid", String, "RFC 7638 thumbprint of the active signing key.")
	Attribute("last_verified_at", String, func() { Format(FormatDateTime) })
	Attribute("verify_evidence", IdentityProviderVerifyEvidence)
	Attribute("sign_in_state", String, func() { Enum("not_started", "application_created", "passed", "failed") })
	Attribute("sign_in_connection_id", String, "Identifier of the WorkOS connection used for sign-in.")
	Attribute("groups_source", String, func() { Enum("token", "directory") })
	Attribute("groups_claim_confirmed", Boolean, "Whether an administrator confirmed the Okta groups claim filter is configured.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "kind", "tenant_identifier", "status", "capabilities", "granted_scopes", "jwks_url", "signing_key_kid", "created_at", "updated_at")
})

var GetIdentityProviderResult = Type("GetIdentityProviderResult", func() {
	Description("The organization's live identity provider connection, when configured.")
	Attribute("connection", IdentityProviderConnection)
})

var IdentityProviderApplication = Type("IdentityProviderApplication", func() {
	Description("An application read directly from an identity provider.")
	Attribute("source_application_id", String, "Provider-assigned application identifier.")
	Attribute("label", String, "Application display label.")
	Attribute("provider_status", String, "Provider lifecycle status.")
	Attribute("sign_on_url", String, "Application launch URL.", func() { Format(FormatURI) })
	Attribute("logo_url", String, "Application logo URL.", func() { Format(FormatURI) })
	Attribute("group_assignment_count", Int, "Number of directly assigned groups, when read.")
	Attribute("assigned_groups", ArrayOf(IdentityProviderAssignedGroup), "Up to 10 directly assigned groups, when read.")
	Attribute("assigned_group_overflow", Int, "Number of additional directly assigned groups omitted from assigned_groups, when read.")
	Attribute("user_assignment_count", Int, "Number of distinct users assigned to the application, including users assigned through groups, when read.")
	Attribute("direct_user_assignment_count", Int, "Number of distinct users assigned directly to the application rather than through a group, when read.")
	Attribute("match", IdentityProviderApplicationMatch, "Speakeasy catalogue match for this application, when one has a concrete MCP server endpoint.")
	Attribute("pickable", Boolean, "Whether Speakeasy can create an MCP server draft for this application.")
	Attribute("unpickable_reason", String, "Why this application cannot be selected.", func() { Enum("inactive", "no_match") })
	Required("source_application_id", "label", "pickable")
})

var IdentityProviderApplicationMatch = Type("IdentityProviderApplicationMatch", func() {
	Description("A name-based match between an identity provider application and the Speakeasy MCP server catalogue.")
	Attribute("provider_key", String, "Opaque catalogue provider key.")
	Attribute("catalog_ref", String, "Catalogue entry reference.")
	Attribute("name", String, "Catalogue entry name.")
	Attribute("remote_url", String, "Streamable HTTP endpoint from the inspected catalogue entry.", func() { Format(FormatURI) })
	Attribute("basis", String, "Evidence used to produce the match.", func() { Enum("name", "catalog_identifier", "sign_on_domain") })
	Attribute("confidence", String, "Confidence in the catalogue match.", func() { Enum("exact", "likely", "none") })
	Required("provider_key", "catalog_ref", "name", "remote_url", "basis", "confidence")
})

var IdentityProviderAssignedGroup = Type("IdentityProviderAssignedGroup", func() {
	Description("An identity provider group assigned directly to an application.")
	Attribute("source_group_id", String, "Provider-assigned group identifier.")
	Attribute("name", String, "Group display name.")
	Required("source_group_id", "name")
})

var ListIdentityProviderApplicationsResult = Type("ListIdentityProviderApplicationsResult", func() {
	Description("A capped live application inventory from the identity provider.")
	Attribute("applications", ArrayOf(IdentityProviderApplication))
	Attribute("read_at", String, func() { Format(FormatDateTime) })
	Attribute("application_count", Int)
	Attribute("truncated", Boolean)
	Attribute("detail", String)
	Required("applications", "read_at", "application_count", "truncated", "detail")
})

var IdentityProviderSetup = Type("IdentityProviderSetup", func() {
	Attribute("connection_id", String, func() { Format(FormatUUID) })
	Attribute("steps", ArrayOf(IdentityProviderSetupStep))
	Required("connection_id", "steps")
})

var IdentityProviderSetupStep = Type("IdentityProviderSetupStep", func() {
	Attribute("key", String)
	Attribute("title", String)
	Attribute("where", String, func() { Enum("their_console", "our_page") })
	Attribute("instructions", ArrayOf(String))
	Attribute("deep_link", String, func() { Format(FormatURI) })
	Attribute("printed_values", ArrayOf(IdentityProviderPrintedValue))
	Attribute("expected_values", ArrayOf(IdentityProviderExpectedValue))
	Attribute("claims", ArrayOf(IdentityProviderClaim), "Claims Speakeasy intends sign-in to carry.")
	Attribute("repair", IdentityProviderRepair)
	Attribute("portal_intent", String, "WorkOS Admin Portal intent the dashboard should open for this step.", func() { Enum("sso") })
	Attribute("state", String, func() { Enum("not_started", "awaiting_values", "awaiting_verification", "passed", "failed") })
	Attribute("last_outcome", IdentityProviderVerifyResult)
	Required("key", "title", "where", "instructions", "printed_values", "expected_values", "state")
})

var IdentityProviderClaim = Type("IdentityProviderClaim", func() {
	Attribute("name", String)
	Attribute("purpose", String)
	Attribute("carries_access", Boolean)
	Attribute("provisioned", Boolean, "Whether Speakeasy provisioned this claim in the identity provider.")
	Required("name", "purpose", "carries_access", "provisioned")
})

var IdentityProviderRepair = Type("IdentityProviderRepair", func() {
	Attribute("title", String)
	Attribute("instructions", ArrayOf(String))
	Attribute("deep_link", String, func() { Format(FormatURI) })
	Attribute("fallback_available", Boolean)
	Required("title", "instructions", "fallback_available")
})

var IdentityProviderPrintedValue = Type("IdentityProviderPrintedValue", func() {
	Attribute("label", String)
	Attribute("value", String)
	Attribute("copyable", Boolean)
	Required("label", "value", "copyable")
})

var IdentityProviderExpectedValue = Type("IdentityProviderExpectedValue", func() {
	Attribute("key", String)
	Attribute("label", String)
	Attribute("secret", Boolean)
	Attribute("current_value", String, "Previously submitted value. Always omitted for secret values.")
	Required("key", "label", "secret")
})

var IdentityProviderSetupValue = Type("IdentityProviderSetupValue", func() {
	Attribute("key", String)
	Attribute("value", String)
	Required("key", "value")
})

var SubmitSetupStepResult = Type("SubmitSetupStepResult", func() {
	Attribute("step", IdentityProviderSetupStep)
	Attribute("field_outcomes", ArrayOf(IdentityProviderFieldOutcome))
	Attribute("next_step_key", String)
	Required("step", "field_outcomes")
})

var IdentityProviderFieldOutcome = Type("IdentityProviderFieldOutcome", func() {
	Attribute("key", String)
	Attribute("outcome", String, func() { Enum("accepted", "rejected") })
	Attribute("detail", String)
	Required("key", "outcome", "detail")
})

var IdentityProviderVerifyResult = Type("IdentityProviderVerifyResult", func() {
	Attribute("outcome", String, func() {
		Enum("passed", "pending_validation", "unreachable", "refused", "mismatched_value", "capability_missing")
	})
	Attribute("detail", String)
	Attribute("capabilities", ArrayOf(String))
	Attribute("granted_scopes", ArrayOf(String))
	Attribute("evidence", IdentityProviderVerifyEvidence)
	Required("outcome", "detail", "capabilities", "granted_scopes", "evidence")
})

var IdentityProviderVerifyEvidence = Type("IdentityProviderVerifyEvidence", func() {
	Attribute("checked_at", String, func() { Format(FormatDateTime) })
	Attribute("reads", ArrayOf(IdentityProviderCapabilityRead))
	Required("checked_at", "reads")
})

var IdentityProviderCapabilityRead = Type("IdentityProviderCapabilityRead", func() {
	Attribute("capability", String)
	Attribute("resource", String, func() {
		Enum("groups", "users", "apps", "authorization_servers", "sign_in_application", "sign_in_connection")
	})
	Attribute("ok", Boolean)
	Attribute("count", Int)
	Attribute("detail", String)
	Required("capability", "resource", "ok")
})
