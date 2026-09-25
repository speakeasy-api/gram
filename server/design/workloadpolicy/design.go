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
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("list", func() {
		Description("Read the whole trust policy: every trusted issuer and every admitted subject, at both the organization and project tiers, with the agent each subject resolves to. Requires workload:read.")

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
	Attribute("allow_wildcard_admission", Boolean, "Whether subjects under this issuer may be admitted by a wildcard rule. Defaults to true. Re-checked on every lookup rather than at write time, so clearing it makes wildcard rules already written inert immediately — an incident control rather than a setup step, which is why the dashboard does not ask for it at registration.", func() {
		Default(true)
	})
	Attribute("project_scoped", Boolean, "Register the issuer for the selected project alone rather than the whole organization. Defaults to false.", func() {
		Default(false)
	})

	Required("name", "issuer", "jwks_uri")
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
	Attribute("agent_id", String, "The agent whose policy the admitted workload inherits.", func() {
		Format(FormatUUID)
	})
	Attribute("project_scoped", Boolean, "Admit the subject for the selected project alone rather than the whole organization. Defaults to false.", func() {
		Default(false)
	})

	Required("issuer", "subject", "agent_id")
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
	Attribute("allow_wildcard_admission", Boolean, "Whether subjects under this issuer may be admitted by a wildcard rule.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "project_id", "name", "issuer", "jwks_uri", "allow_wildcard_admission", "created_at", "updated_at")
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

	Required("id", "organization_id", "project_id", "workload_issuer_id", "issuer", "issuer_name", "subject", "match_kind", "name", "agent_id", "agent_name", "wildcard_active", "created_at", "updated_at")
})

var WorkloadIdentityPolicy = Type("WorkloadIdentityPolicy", func() {
	Description("The whole trust policy visible to the caller. Every write returns this, so a client replaces its view rather than merging into it and cannot show a stale list after a mutation.")

	Attribute("issuers", ArrayOf(WorkloadIssuer), "Trusted issuers, organization tier first.")
	Attribute("admissions", ArrayOf(WorkloadAdmission), "Admitted subjects.")

	Required("issuers", "admissions")
})
