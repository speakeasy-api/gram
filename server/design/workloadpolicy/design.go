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

	Method("listPlatforms", func() {
		Description("List the platforms the catalog offers to trust without looking anything up, each with the guided setup that connects it. The same for every organization. Requires workload:read.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadPlatformCatalog)

		HTTP(func() {
			GET("/rpc/workloadIdentities.listPlatforms")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listWorkloadPlatforms")
		Meta("openapi:extension:x-speakeasy-name-override", "listPlatforms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WorkloadPlatforms"}`)
	})

	Method("getCustomFlows", func() {
		Description("Get the forms for trusting a platform the catalog does not list and allowing its workloads: registering and editing a trusted platform, and allowing and editing access. The same for every organization. Requires workload:read.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadCustomFlows)

		HTTP(func() {
			GET("/rpc/workloadIdentities.getCustomFlows")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getWorkloadCustomFlows")
		Meta("openapi:extension:x-speakeasy-name-override", "getCustomFlows")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WorkloadCustomFlows"}`)
	})

	Method("listTokenEndpoints", func() {
		Description("List the token endpoints an external platform can be pointed at: one per user session issuer in shared mode, at the organization level and in each project, with the issuer it serves. Issuers this deployment does not serve a shared authorization server for are left out. Requires workload:read.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(WorkloadTokenEndpoints)

		HTTP(func() {
			GET("/rpc/workloadIdentities.listTokenEndpoints")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listWorkloadTokenEndpoints")
		Meta("openapi:extension:x-speakeasy-name-override", "listTokenEndpoints")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "WorkloadTokenEndpoints"}`)
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
	Attribute("tags", ArrayOf(String), "Replaces the issuer's tags; an empty list clears them. Trimmed and de-duplicated on write, then limited to 40 tags of at most 64 characters each.", func() {
		// omitzero, not omitempty: an empty list is the instruction to clear
		// the tags and has to reach the wire, while a nil one stays omitted.
		Meta("struct:tag:json", "tags,omitzero")
	})

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
	Attribute("tags", ArrayOf(String), "Replaces the admission's tags; an empty list clears them. Trimmed and de-duplicated on write, then limited to 40 tags of at most 64 characters each.", func() {
		// omitzero, not omitempty: an empty list is the instruction to clear
		// the tags and has to reach the wire, while a nil one stays omitted.
		Meta("struct:tag:json", "tags,omitzero")
	})
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

var WorkloadPlatformConstant = Type("WorkloadPlatformConstant", func() {
	Description("A value a catalog platform supplies, the same for every customer.")

	Attribute("value", String, "The value, with {key} placeholders for platform-tier variables.")
	Attribute("visibility", String, "Whether the operator sees the value.", func() {
		Enum("hidden", "read-only")
	})

	Required("value", "visibility")
})

var WorkloadPlatformVariable = Type("WorkloadPlatformVariable", func() {
	Description("Something the operator supplies when connecting a catalog platform.")

	Attribute("key", String, "Names the variable in templates, as {key}.")
	Attribute("tier", String, "Whether it is supplied once per trusted platform or once per access rule.", func() {
		Enum("platform", "rule")
	})
	Attribute("label", String, "The field's label.")
	Attribute("help", String, "Where the operator finds the value. Empty where there is no help.")
	Attribute("placeholder", String, "Shown in the empty field. Empty where there is none.")
	Attribute("pattern", String, "A regular expression the whole value must match.")
	Attribute("pattern_message", String, "Shown when the value does not match. Empty where there is none.")

	Required("key", "tier", "label", "help", "placeholder", "pattern", "pattern_message")
})

var WorkloadPlatformSubject = Type("WorkloadPlatformSubject", func() {
	Description("The access rule a catalog platform produces.")

	Attribute("template", String, "The subject with {key} placeholders for rule-tier variables; the stem, for a wildcard rule.")
	Attribute("wildcard", Boolean, "Whether the rule is the filled template followed by *.")

	Required("template", "wildcard")
})

var WorkloadPlatformBlock = Type("WorkloadPlatformBlock", func() {
	Description("One piece of a guided setup step. Which fields are set depends on type; the rest are empty.")

	Attribute("type", String, "The kind of content.", func() {
		Enum("text", "image", "link", "field", "subject_rule", "agent_picker", "tags", "computed_status", "computed", "checklist_item")
	})
	Attribute("markdown", String, "A text block's Markdown, or what a checklist item without a value asks the operator to do. Raw HTML in it must not be rendered.")
	Attribute("src", String, "An image's path on the dashboard's origin.")
	Attribute("alt", String, "An image's alternative text.")
	Attribute("caption", String, "Shown under an image.")
	Attribute("href", String, "A link's https target.")
	Attribute("label", String, "A link's or computed value's label, or the console field or control a checklist item names.")
	Attribute("variable", String, "The variable key a field collects.")
	Attribute("value", String, "The value a computed block or checklist item shows.", func() {
		Enum("", "token_endpoint", "issuer_url", "mcp_host")
	})
	Attribute("help", String, "Shown under a computed value or checklist item.")

	Required("type", "markdown", "src", "alt", "caption", "href", "label", "variable", "value", "help")
})

var WorkloadPlatformStep = Type("WorkloadPlatformStep", func() {
	Description("One screen of a guided setup.")

	Attribute("id", String, "Stable within the platform; used in the dashboard URL.")
	Attribute("title", String, "Heads the step.")
	Attribute("phase", String, "collect steps gather values, the create step writes the rows, connect steps describe the platform's side.", func() {
		Enum("collect", "create", "connect")
	})
	Attribute("blocks", ArrayOf(WorkloadPlatformBlock), "Rendered in order.")

	Required("id", "title", "phase", "blocks")
})

var WorkloadPlatform = Type("WorkloadPlatform", func() {
	Description("A platform the catalog offers to trust, with what the operator supplies and the guided setup that connects it.")

	Attribute("key", String, "Identifies the entry permanently.")
	Attribute("display_name", String, "What the operator sees.")
	Attribute("description", String, "What connecting the platform does.")
	Attribute("icon", String, "The platform's logo, a path on the dashboard's origin. Empty where it has none.")
	Attribute("enabled", Boolean, "False for a platform listed but not offered.")
	Attribute("issuer", WorkloadPlatformConstant, "The issuer identifier its tokens carry.")
	Attribute("jwks_uri", WorkloadPlatformConstant, "Where it publishes its signing keys.")
	Attribute("variables", ArrayOf(WorkloadPlatformVariable), "What the operator supplies.")
	Attribute("subject", WorkloadPlatformSubject, "The access rule it produces.")
	Attribute("steps", ArrayOf(WorkloadPlatformStep), "The guided setup. Empty for a platform without one, which is listed as coming soon and cannot be opened.")

	Required("key", "display_name", "description", "icon", "enabled", "issuer", "jwks_uri", "variables", "subject", "steps")
})

var WorkloadPlatformCatalog = Type("WorkloadPlatformCatalog", func() {
	Description("Every platform the catalog offers, ordered by key.")

	Attribute("platforms", ArrayOf(WorkloadPlatform), "The catalog entries.")

	Required("platforms")
})

var WorkloadFormBlock = Type("WorkloadFormBlock", func() {
	Description("One piece of a custom flow's form. Which fields are set depends on type; the rest are empty.")

	Attribute("type", String, "The kind of content. wildcard_caution marks where the form warns that a wildcard rule admits more than one identity; the dashboard writes that warning, since it names the rule and the agent.", func() {
		Enum("text", "link", "input", "agent_picker", "tags", "wildcard_caution")
	})
	Attribute("markdown", String, "A text block's Markdown. Raw HTML in it must not be rendered.")
	Attribute("href", String, "A link's https target.")
	Attribute("label", String, "A link's label, or a form control's.")
	Attribute("field", String, "The form value an input collects. label is submitted as an admission's name.", func() {
		Enum("", "name", "description", "issuer", "jwks_uri", "subject", "label")
	})
	Attribute("placeholder", String, "Shown in an empty form control.")
	Attribute("help", String, "Markdown shown under a form control while it has no validation message. Raw HTML in it must not be rendered.")
	Attribute("multiline", Boolean, "Whether an input is a text area.")
	Attribute("read_only", Boolean, "Whether an input shows its value without letting it change. A read-only value is not submitted.")
	Attribute("format", String, "The dashboard validator an input's value must pass. The server applies the same rules when the form is submitted.", func() {
		Enum("", "issuer_url", "jwks_uri", "platform_name", "platform_description", "subject_rule", "none")
	})

	Required("type", "markdown", "href", "label", "field", "placeholder", "help", "multiline", "read_only", "format")
})

var WorkloadFormStep = Type("WorkloadFormStep", func() {
	Description("One screen of a custom flow's form.")

	Attribute("id", String, "Stable within the form.")
	Attribute("title", String, "Heads the step.")
	Attribute("blocks", ArrayOf(WorkloadFormBlock), "Rendered in order.")

	Required("id", "title", "blocks")
})

var WorkloadForm = Type("WorkloadForm", func() {
	Description("A custom flow's form, held to the management API form it submits.")

	Attribute("title", String, "Heads the form.")
	Attribute("description", String, "Shown under the title.")
	Attribute("submit_label", String, "The submit button's label.")
	Attribute("pending_label", String, "The submit button's label while the form submits.")
	Attribute("steps", ArrayOf(WorkloadFormStep), "The form's steps, in order.")

	Required("title", "description", "submit_label", "pending_label", "steps")
})

var WorkloadCustomFlows = Type("WorkloadCustomFlows", func() {
	Description("The forms for trusting a platform the catalog does not list and allowing its workloads.")

	Attribute("register_platform", WorkloadForm, "Trusts a new platform; submits registerIssuer.")
	Attribute("edit_platform", WorkloadForm, "Edits a trusted platform; submits updateIssuer.")
	Attribute("allow_access", WorkloadForm, "Allows a subject under a trusted platform; submits admitSubject.")
	Attribute("edit_access", WorkloadForm, "Edits allowed access; submits updateSubject.")

	Required("register_platform", "edit_platform", "allow_access", "edit_access")
})

var WorkloadTokenEndpoint = Type("WorkloadTokenEndpoint", func() {
	Meta("struct:pkg:path", "types")

	Description("The shared authorization server of a user session issuer, which an external platform exchanges its workload token at.")

	Attribute("user_session_issuer_id", String, "The user session issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_slug", String, "The user session issuer slug.")
	Attribute("project_id", String, "The owning project id; empty for an organization-level issuer.")
	Attribute("project_name", String, "The owning project name; empty for an organization-level issuer.")
	Attribute("issuer", String, "The authorization server's issuer identifier, as its RFC 8414 metadata publishes it.")
	Attribute("token_endpoint", String, "The authorization server's token endpoint.")
	Attribute("mcp_host", String, "The host MCP servers are served on, which a platform calls with the tokens it is issued.")

	Required("user_session_issuer_id", "user_session_issuer_slug", "project_id", "project_name", "issuer", "token_endpoint", "mcp_host")
})

var WorkloadTokenEndpoints = Type("WorkloadTokenEndpoints", func() {
	Meta("struct:pkg:path", "types")

	Attribute("items", ArrayOf(WorkloadTokenEndpoint), "Organization-level issuers first, then by project name and issuer slug.")

	Required("items")
})
