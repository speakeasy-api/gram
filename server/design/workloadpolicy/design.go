// Package workloadpolicy designs the management API for an organization's
// workload identity trust policy: which external issuers it trusts, which
// subjects those issuers may present, and which agent each admitted workload
// inherits its policy from.
//
// The Goa service is named workloadIdentities because that is the product term.
// "Trusted issuer" is reserved for the enterprise-managed authorization concept,
// and "identity provider" and "remote" are both spent on the outbound concept.
package workloadpolicy

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("workloadIdentities", func() {
	Description("Configure which workloads an organization recognises as its own: the issuers it trusts, the subjects those issuers may present, and the agent each admitted workload inherits its policy from.")
	// The trust policy belongs to the organization, so a dashboard session
	// needs no project. An API key is issued to a project and keeps naming one.
	Security(security.Session)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("list", func() {
		Description("Read the whole trust policy: every trusted issuer and every admitted subject at the organization tier, plus the selected project's tier when the caller names a project, with the agent each subject resolves to. Requires workload:read.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			GET("/rpc/workloadIdentities.list")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listWorkloadIdentities")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WorkloadIdentities"}`)
	})

	Method("registerIssuer", func() {
		Description("Trust an external issuer to vouch for workloads. Requires workload:write. Returns the whole policy, so a caller replaces its view rather than merging into it.")

		Payload(func() {
			Extend(RegisterWorkloadIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			POST("/rpc/workloadIdentities.registerIssuer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "registerWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "registerIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RegisterWorkloadIssuer"}`)
	})

	Method("updateIssuer", func() {
		Description("Edit a trusted issuer's name, description, tags, or JWKS URI. Omitted fields are left unchanged. The issuer URL and the wildcard admission setting are fixed at registration. Requires workload:write. Returns the whole policy, so a caller replaces its view rather than merging into it.")

		Payload(func() {
			Extend(UpdateWorkloadIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			POST("/rpc/workloadIdentities.updateIssuer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateWorkloadIssuer"}`)
	})

	Method("withdrawIssuer", func() {
		Description("Stop trusting an issuer. Every subject admitted under it is withdrawn in the same transaction, so no admission can outlive the issuer it names. Requires workload:write.")

		Payload(func() {
			Attribute("id", String, "The workload issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			DELETE("/rpc/workloadIdentities.withdrawIssuer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "withdrawWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "withdrawIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WithdrawWorkloadIssuer"}`)
	})

	Method("admitSubject", func() {
		Description("Admit a subject one of the trusted issuers asserts, and assign the agent it inherits its policy from. Both happen in one transaction: a subject admitted without an agent is refused at the token endpoint, so that half-configured state is not reachable. Requires workload:write.")

		Payload(func() {
			Extend(AdmitWorkloadSubjectForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			POST("/rpc/workloadIdentities.admitSubject")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "admitWorkloadSubject")
		Meta("openapi:extension:x-speakeasy-name-override", "admitSubject")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AdmitWorkloadSubject"}`)
	})

	Method("updateSubject", func() {
		Description("Edit an admitted subject's label, tags, or assigned agent. Omitted fields are left unchanged. The subject, match kind, issuer, and tier are fixed at admission. The agent assignment is shared by every admission of the same subject under the same issuer, at either tier, so reassigning it through one admission reassigns it for both. Requires workload:write. Returns the whole policy, so a caller replaces its view rather than merging into it.")

		Payload(func() {
			Extend(UpdateWorkloadSubjectForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			POST("/rpc/workloadIdentities.updateSubject")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateWorkloadSubject")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSubject")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateWorkloadSubject"}`)
	})

	Method("withdrawSubject", func() {
		Description("Withdraw an admitted subject and its agent assignment, stopping it authenticating. Requires workload:write.")

		Payload(func() {
			Attribute("id", String, "The admission id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadIdentityPolicy)

		HTTP(func() {
			DELETE("/rpc/workloadIdentities.withdrawSubject")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "withdrawWorkloadSubject")
		Meta("openapi:extension:x-speakeasy-name-override", "withdrawSubject")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WithdrawWorkloadSubject"}`)
	})

	Method("connectionDetails", func() {
		Description("Read the values an external platform must be configured with to exchange its workload identity tokens at an MCP server: for each of the server's addresses, the token endpoint, the authorization server's issuer identifier, and the resource URL, as the address's authorization server metadata serves them, plus whether an exchange there can succeed. Requires workload:read and mcp:read on the server. A caller that names a project can read only that project's servers.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The MCP server the platform will call.", func() {
				Format(FormatUUID)
			})
			Required("mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadConnectionDetails)

		HTTP(func() {
			GET("/rpc/workloadIdentities.connectionDetails")
			Param("mcp_server_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getWorkloadConnectionDetails")
		Meta("openapi:extension:x-speakeasy-name-override", "connectionDetails")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WorkloadConnectionDetails"}`)
	})

	Method("organizationConnectionDetails", func() {
		Description("Read the organization's own token endpoint for workload identity tokens: one endpoint at which a platform exchanges an assertion for a session on any of the organization's MCP servers its workload may reach, naming the server by resource. Returns the values the organization's authorization server metadata serves, and whether an exchange there can succeed. Requires workload:read.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadOrganizationConnectionDetails)

		HTTP(func() {
			GET("/rpc/workloadIdentities.organizationConnectionDetails")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getWorkloadOrganizationConnectionDetails")
		Meta("openapi:extension:x-speakeasy-name-override", "organizationConnectionDetails")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WorkloadOrganizationConnectionDetails"}`)
	})
})

var RegisterWorkloadIssuerForm = Type("RegisterWorkloadIssuerForm", func() {
	Description("Form for trusting an external workload issuer.")

	Attribute("name", String, "The label an operator works with. Unique within its tier.", func() {
		MinLength(1)
		MaxLength(100)
	})
	// FormatURI rejects a value that is not a URI at all, in generated clients and
	// in the contract. It deliberately does not try to express the https and
	// fully-qualified-domain rules: those are enforced on the write path, where
	// the refusal can name which rule was broken, and a regex here would be a
	// second copy of them free to drift.
	Attribute("issuer", String, "The issuer identifier the assertion's iss claim must carry. Must be an https URL on a fully qualified domain name, with no query or fragment.", func() {
		Format(FormatURI)
	})
	Attribute("jwks_uri", String, "Where the issuer publishes the keys its assertions are signed with. Must be an https URL on a fully qualified domain name.", func() {
		Format(FormatURI)
	})
	Attribute("allow_wildcard_admission", Boolean, "Whether subjects under this issuer may be admitted by a wildcard rule. Defaults to true. Checked when a wildcard rule is admitted and again on every lookup, so clearing it makes wildcard rules already written inert immediately.")
	Attribute("description", String, "What the platform is and what runs on it, in the operator's words. Trimmed on write; blank is stored as none. At most 500 characters after trimming.")
	Attribute("tags", ArrayOf(String), "Free-form labels for grouping and filtering trusted platforms. Flat strings, not key/value pairs. Trimmed and de-duplicated on write, then limited to 40 tags of at most 64 characters each.")
	Attribute("project_scoped", Boolean, "Register the issuer for the selected project alone rather than the whole organization. Requires a caller that names a project; a dashboard session does not. Defaults to false.", func() {
		Default(false)
	})

	Required("name", "issuer", "jwks_uri")
})

var UpdateWorkloadIssuerForm = Type("UpdateWorkloadIssuerForm", func() {
	Description("Form for editing a trusted workload issuer. Every field but id is optional; an omitted field is left unchanged.")

	Attribute("id", String, "The workload issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The label an operator works with. Unique within its tier.", func() {
		MinLength(1)
		MaxLength(100)
	})
	Attribute("jwks_uri", String, "Where the issuer publishes the keys its assertions are signed with. Must be an https URL on a fully qualified domain name.", func() {
		Format(FormatURI)
	})
	Attribute("description", String, "What the platform is and what runs on it, in the operator's words. Trimmed on write; blank clears it. At most 500 characters after trimming.")
	Attribute("tags", ArrayOf(String), "Replaces the issuer's tags; an empty list clears them. Trimmed and de-duplicated on write, then limited to 40 tags of at most 64 characters each.")

	Required("id")
})

var AdmitWorkloadSubjectForm = Type("AdmitWorkloadSubjectForm", func() {
	Description("Form for admitting a workload subject and assigning its agent.")

	Attribute("issuer", String, "The issuer identifier of an already-trusted issuer, resolved within the caller's organization. Naming one the caller cannot see is a not-found, not a permission error.", func() {
		Format(FormatURI)
	})
	Attribute("subject", String, "The sub claim the issuer must assert, stored and compared exactly as supplied.")
	Attribute("match_kind", String, "How the subject is compared: exact compares the whole value; wildcard requires a trailing * and matches anything beginning with the value before it. Wildcard additionally requires the issuer to permit it. Defaults to exact.", func() {
		Enum("exact", "wildcard")
		Default("exact")
	})
	Attribute("name", String, "Optional label, for platforms whose subjects are not self-describing.", func() {
		MinLength(1)
	})
	Attribute("tags", ArrayOf(String), "Free-form labels for finding the admitted workload in a long list. Flat strings, not key/value pairs. Trimmed and de-duplicated on write, then limited to 40 tags of at most 64 characters each.")
	Attribute("agent_id", String, "The agent whose policy the admitted workload inherits.", func() {
		Format(FormatUUID)
	})
	Attribute("project_scoped", Boolean, "Admit the subject for the selected project alone rather than the whole organization. Requires a caller that names a project; a dashboard session does not. Defaults to false.", func() {
		Default(false)
	})

	Required("issuer", "subject", "agent_id")
})

var UpdateWorkloadSubjectForm = Type("UpdateWorkloadSubjectForm", func() {
	Description("Form for editing an admitted workload subject. Every field but id is optional; an omitted field is left unchanged.")

	Attribute("id", String, "The admission id.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Optional label, for platforms whose subjects are not self-describing. Trimmed on write; blank clears it.")
	Attribute("tags", ArrayOf(String), "Replaces the admission's tags; an empty list clears them. Trimmed and de-duplicated on write, then limited to 40 tags of at most 64 characters each.")
	Attribute("agent_id", String, "The agent whose policy the admitted workload inherits. Shared with any admission of the same subject under the same issuer at the other tier.", func() {
		Format(FormatUUID)
	})

	Required("id")
})

var WorkloadIssuer = Type("WorkloadIssuer", func() {
	Meta("struct:pkg:path", "types")

	Description("An external issuer an organization trusts to vouch for its workloads.")

	Attribute("id", String, "The workload issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The owning organization id.")
	Attribute("project_id", String, "The owning project id; empty for an organization-tier issuer.")
	Attribute("name", String, "The label an operator works with.")
	Attribute("issuer", String, "The issuer identifier the assertion's iss claim must carry.")
	Attribute("jwks_uri", String, "Where the issuer publishes its signing keys.")
	Attribute("description", String, "What the platform is and what runs on it. Empty rather than absent where none is set.")
	Attribute("allow_wildcard_admission", Boolean, "Whether subjects under this issuer may be admitted by a wildcard rule.")
	Attribute("tags", ArrayOf(String), "Free-form labels for grouping and filtering trusted platforms. Empty rather than absent where none are set.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "project_id", "name", "issuer", "jwks_uri", "description", "allow_wildcard_admission", "tags", "created_at", "updated_at")
})

var WorkloadAdmission = Type("WorkloadAdmission", func() {
	Meta("struct:pkg:path", "types")

	Description("A subject an organization admits, with the agent it inherits its policy from.")

	Attribute("id", String, "The admission id.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The owning organization id.")
	Attribute("project_id", String, "The owning project id; empty for an organization-tier admission.")
	Attribute("workload_issuer_id", String, "The issuer that must assert this subject.", func() {
		Format(FormatUUID)
	})
	Attribute("issuer", String, "The issuer identifier, denormalized so a list does not need a second lookup.")
	Attribute("issuer_name", String, "The issuer's operator-facing label.")
	Attribute("subject", String, "The sub claim, exactly as stored.")
	Attribute("match_kind", String, "How the subject is compared: exact | wildcard.", func() {
		Enum("exact", "wildcard")
	})
	Attribute("name", String, "Optional label; empty when none was supplied.")
	Attribute("tags", ArrayOf(String), "Free-form labels for finding the admitted workload. Empty rather than absent where none are set.")
	Attribute("agent_id", String, "The agent whose policy this workload inherits. Empty when the assignment is missing, which the token endpoint refuses.", func() {
		Format(FormatUUID)
	})
	Attribute("agent_name", String, "The assigned agent's name; empty when the assignment is missing.")
	Attribute("wildcard_active", Boolean, "False when this is a wildcard rule under an issuer that no longer permits wildcard matching. Such a rule is inert: it is stored, listed, and matches nothing.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "project_id", "workload_issuer_id", "issuer", "issuer_name", "subject", "match_kind", "name", "tags", "agent_id", "agent_name", "wildcard_active", "created_at", "updated_at")
})

var WorkloadIdentityPolicy = Type("WorkloadIdentityPolicy", func() {
	Description("The whole trust policy visible to the caller. Every write returns this, so a client replaces its view rather than merging into it and cannot show a stale list after a mutation.")

	Attribute("issuers", ArrayOf(WorkloadIssuer), "Trusted issuers, organization tier first.")
	Attribute("admissions", ArrayOf(WorkloadAdmission), "Admitted subjects.")

	Required("issuers", "admissions")
})

var WorkloadConnectionEndpoint = Type("WorkloadConnectionEndpoint", func() {
	Description("One address of an MCP server, with the values a federating platform is configured with. Issuer and token endpoint are the ones the address's authorization server metadata serves, so a client that discovers them reads the same values.")

	Attribute("resource_url", String, "The MCP server URL: the resource the exchanged session is for.")
	Attribute("api_host", String, "The host of resource_url, which a platform lists among the API hosts its token may be sent to.")
	Attribute("issuer", String, "The authorization server's issuer identifier. An assertion's aud must be exactly this or token_endpoint. Empty when Gram is not this address's authorization server.")
	Attribute("token_endpoint", String, "Where the platform sends its assertion. Empty when Gram is not this address's authorization server.")
	Attribute("on_authentication_host", Boolean, "Whether token_endpoint is on Gram's dedicated authentication host, a different host from api_host.")
	Attribute("grant_types_supported", ArrayOf(String), "grant_types_supported as the authorization server metadata lists it. Empty when Gram is not this address's authorization server.")
	Attribute("workload_grant_advertised", Boolean, "Whether the metadata lists the jwt-bearer grant because the clientless workload assertion exchange is available here.")
	Attribute("ready", Boolean, "Whether nothing Gram knows of stops an exchange at this address. The platform's own configuration and the trust policy are not checked.")
	Attribute("not_ready_reason", String, "Why an exchange here cannot succeed; absent when ready. not_publicly_reachable: the address does not resolve publicly (disabled, or private network only). no_authorization_server: the server is not gated on a Gram user session issuer, or is an anonymous public tunnel, which serves no OAuth metadata even when it has one. workload_grant_unavailable: the metadata does not advertise the workload grant. agent_rollout_disabled: the organization is outside the agent authorization rollout the token endpoint requires.", func() {
		Enum("not_publicly_reachable", "no_authorization_server", "workload_grant_unavailable", "agent_rollout_disabled")
	})

	Required("resource_url", "api_host", "issuer", "token_endpoint", "on_authentication_host", "grant_types_supported", "workload_grant_advertised", "ready")
})

var WorkloadConnectionDetails = Type("WorkloadConnectionDetails", func() {
	Description("The values a federating platform is configured with for one MCP server.")

	Attribute("mcp_server_id", String, "The MCP server id.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_name", String, "The MCP server's display name; empty when it has none.")
	Attribute("endpoints", ArrayOf(WorkloadConnectionEndpoint), "The server's addresses, platform-host addresses first. Empty when the server has no address.")

	Required("mcp_server_id", "mcp_server_name", "endpoints")
})

var WorkloadOrganizationConnectionDetails = Type("WorkloadOrganizationConnectionDetails", func() {
	Description("The values a federating platform is configured with to exchange workload identity tokens at the organization's own token endpoint.")

	Attribute("available", Boolean, "Whether the organization token endpoint is served for this organization. When false its routes answer 404 and a platform must use a server's own token endpoint.")
	Attribute("issuer", String, "The organization authorization server's issuer identifier, and the iss of the sessions it mints. An assertion's aud must be exactly this or token_endpoint.")
	Attribute("token_endpoint", String, "Where the platform sends its assertion, naming the MCP server it wants a session for as resource: that server's URL.")
	Attribute("metadata_url", String, "Where the organization authorization server's RFC 8414 metadata is served.")
	Attribute("on_authentication_host", Boolean, "Whether token_endpoint is on Gram's dedicated authentication host rather than the platform host.")
	Attribute("grant_types_supported", ArrayOf(String), "grant_types_supported as the metadata lists it. Empty when the endpoint is not available or the workload grant is not.")
	Attribute("workload_grant_advertised", Boolean, "Whether the metadata lists the jwt-bearer grant because the clientless workload assertion exchange is available here.")
	Attribute("ready", Boolean, "Whether nothing Gram knows of stops an exchange here. The platform's own configuration, the trust policy, and which servers the workload's agent may reach are not checked.")
	Attribute("not_ready_reason", String, "Why an exchange here cannot succeed; absent when ready. organization_endpoint_disabled: the organization is outside the organization token endpoint's rollout. workload_grant_unavailable: the deployment does not serve the workload grant. agent_rollout_disabled: the organization is outside the agent authorization rollout the token endpoint requires.", func() {
		Enum("organization_endpoint_disabled", "workload_grant_unavailable", "agent_rollout_disabled")
	})

	Required("available", "issuer", "token_endpoint", "metadata_url", "on_authentication_host", "grant_types_supported", "workload_grant_advertised", "ready")
})
