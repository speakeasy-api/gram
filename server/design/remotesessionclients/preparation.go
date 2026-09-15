package remotesessionclients

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

// IdentityChainingPreparation describes recorded configuration, never demonstrated access.
var IdentityChainingPreparation = Type("IdentityChainingPreparation", func() {
	Attribute("state", String, "Preparation state; readiness never proves user access.", func() {
		Enum("unsupported_profile", "incomplete_metadata", "unknown_grants", "manual_setup_required", "configuration_required", "in_progress", "indeterminate", "provider_rejection", "transient_failure", "published_acceptance_unverified", "ready", "unlinked")
	})
	Attribute("stage", String, func() { Enum("selection", "eligibility", "registration", "discovery", "publication") })
	Attribute("remediation", String, "Safe next action without provider bodies or credentials.")
	Attribute("retryable", Boolean)
	Attribute("binding_id", String)
	Attribute("generation", Int64)
	Attribute("client_id", String, "Exact selected remote_session_client row ID, when available.")
	Attribute("external_client_id", String, "Public OAuth client identifier, never a secret.")
	Attribute("issuer", String)
	Attribute("resource", String)
	Attribute("grant_types", ArrayOf(String), "Null means unknown; an empty array means no recorded grants.", func() {
		Meta("struct:tag:json", "grant_types")
	})
	Attribute("scopes", ArrayOf(String))
	Attribute("grant_source", String, "Provenance of recorded grants, not provider trust.")
	Required("state", "stage", "remediation", "retryable", "generation", "resource", "scopes", "grant_source")
})

func identityChainingMethods() {
	for _, name := range []string{"prepareEMA", "readEMA", "unlinkEMA"} {
		Method(name, func() {
			Description("Explicit, tenant-scoped identity-chaining configuration. Does not exchange tokens or establish user access.")
			Payload(func() {
				security.SessionPayload()
				security.ByKeyPayload()
				security.ProjectPayload()
				Attribute("user_session_issuer_id", String, func() { Format(FormatUUID) })
				Attribute("remote_session_issuer_id", String, func() { Format(FormatUUID) })
				Attribute("resource", String, "Canonical intended resource URI.", func() { Format(FormatURI); MinLength(1); MaxLength(2048) })
				if name == "prepareEMA" {
					Attribute("client_id", String, "Explicit selected client row ID; never inferred.", func() { Format(FormatUUID) })
					Attribute("scopes", ArrayOf(String), func() { ScopeAttribute("Requested scope tokens.") })
					Attribute("mechanism", String, func() { Enum("manual", "cimd", "dcr"); Default("manual") })
					Attribute("token_endpoint_auth_method", String, "Explicit DCR authentication method.", func() { Enum("client_secret_basic", "client_secret_post") })
					Attribute("resource_metadata", func() {
						Attribute("resource", String)
						Attribute("authorization_servers", ArrayOf(String))
						Required("resource", "authorization_servers")
					})
					Attribute("confirm_grants", ArrayOf(String), "Administrator-declared grants, not provider verification.")
				}
				// Reads discover the current generation, including before a binding exists.
				// Mutations require that generation to prevent stale writes.
				if name != "readEMA" {
					Attribute("expected_generation", Int64, "Optimistic binding generation.", func() { Minimum(0) })
					Required("expected_generation")
				}
				Required("user_session_issuer_id", "remote_session_issuer_id", "resource")
			})
			Result(IdentityChainingPreparation)
			HTTP(func() {
				POST("/rpc/remoteSessionClients." + name)
				security.SessionHeader()
				security.ByKeyHeader()
				security.ProjectHeader()
				Response(StatusOK)
			})
			Meta("openapi:operationId", name)
			Meta("openapi:extension:x-speakeasy-name-override", name)
		})
	}
}
