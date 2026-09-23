package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminWorkloadIssuer = Type("AdminWorkloadIssuer", func() {
	Description("A workload assertion issuer trusted by one organization.")
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("issuer", String, func() { Meta("struct:tag:json", "issuer") })
	Attribute("jwks_uri", String, func() { Meta("struct:tag:json", "jwks_uri") })
	Attribute("project_id", String, func() {
		Description("Project the issuer is scoped to, absent when it is organization-wide.")
		Meta("struct:tag:json", "project_id")
	})
	Attribute("created_at", String, func() { Meta("struct:tag:json", "created_at"); Format(FormatDateTime) })
	Required("id", "name", "issuer", "jwks_uri", "created_at")
})

var AdminWorkloadSubject = Type("AdminWorkloadSubject", func() {
	Description("An admitted workload subject and the agent supplying its policy. A subject with no assigned agent is refused at the token endpoint, so both are shown together.")
	Attribute("workload_issuer_id", String, func() { Meta("struct:tag:json", "workload_issuer_id") })
	Attribute("subject", String, func() { Meta("struct:tag:json", "subject") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("agent_id", String, func() {
		Description("Assigned agent, absent when the subject is admitted but unassigned.")
		Meta("struct:tag:json", "agent_id")
	})
	Attribute("agent_name", String, func() { Meta("struct:tag:json", "agent_name") })
	Required("workload_issuer_id", "subject")
})

var AdminWorkloadAuthenticationHost = Type("AdminWorkloadAuthenticationHost", func() {
	Description("Whether one user session issuer announces the deployment's authentication host as its OAuth issuer.")
	Attribute("user_session_issuer_id", String, func() { Meta("struct:tag:json", "user_session_issuer_id") })
	Attribute("project_id", String, func() { Meta("struct:tag:json", "project_id") })
	Attribute("use_authentication_host", Boolean, func() { Meta("struct:tag:json", "use_authentication_host") })
	Required("user_session_issuer_id", "use_authentication_host")
})

var AdminWorkloadIdentityState = Type("AdminWorkloadIdentityState", func() {
	Description("An organization's complete workload identity configuration. Every write returns it, so a caller never has to reassemble the state itself.")
	Attribute("organization_id", String, func() { Meta("struct:tag:json", "organization_id") })
	Attribute("issuers", ArrayOf(AdminWorkloadIssuer), func() { Meta("struct:tag:json", "issuers") })
	Attribute("subjects", ArrayOf(AdminWorkloadSubject), func() { Meta("struct:tag:json", "subjects") })
	Attribute("authentication_hosts", ArrayOf(AdminWorkloadAuthenticationHost), func() { Meta("struct:tag:json", "authentication_hosts") })
	Required("organization_id", "issuers", "subjects", "authentication_hosts")
})

// workloadIdentityMethods exposes the workload identity trust policy to admin
// operators. It exists because the tenant-facing management API lands in a
// later milestone, and seeding this by hand means raw SQL against production.
// These endpoints are the same writes behind admin authentication.
func workloadIdentityMethods() {
	Method("getWorkloadIdentity", func() {
		Meta("openapi:operationId", "adminGetWorkloadIdentity")
		Meta("openapi:extension:x-speakeasy-name-override", "getWorkloadIdentity")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetWorkloadIdentity"}`)
		Description("Read an organization's workload issuers, admitted subjects, and authentication host state.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Required("organization_id")
		})
		Result(AdminWorkloadIdentityState)
		HTTP(func() {
			GET("/admin/organization.workloadIdentity")
			Param("organization_id")
			Response(StatusOK)
		})
	})

	Method("createWorkloadIssuer", func() {
		Meta("openapi:operationId", "adminCreateWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "createWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminCreateWorkloadIssuer"}`)
		Description("Trust one workload assertion issuer for an organization. Both URLs must be https, which the grant enforces at verification time; rejecting them here avoids storing a row that can never admit anything.")
		Payload(func() {
			Meta("openapi:typename", "CreateWorkloadIssuerRequestBody")
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Attribute("project_id", String, "Scope the issuer to one project. Omit for organization-wide.")
			Attribute("name", String, func() { MinLength(1); MaxLength(100) })
			Attribute("issuer", String, func() {
				Description("The assertion's iss claim.")
				Pattern(`^https://`)
				MaxLength(2048)
			})
			Attribute("jwks_uri", String, func() {
				Description("Where the issuer publishes its signing keys.")
				Pattern(`^https://`)
				MaxLength(2048)
			})
			Required("organization_id", "name", "issuer", "jwks_uri")
		})
		Result(AdminWorkloadIdentityState)
		HTTP(func() {
			POST("/admin/organization.workloadIssuer")
			Response(StatusOK)
		})
	})

	Method("admitWorkloadSubject", func() {
		Meta("openapi:operationId", "adminAdmitWorkloadSubject")
		Meta("openapi:extension:x-speakeasy-name-override", "admitWorkloadSubject")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminAdmitWorkloadSubject"}`)
		Description("Admit one exact subject under a trusted issuer and assign the agent whose policy it inherits. Trusting the issuer alone never admits a workload, so the subject must be the value the platform actually mints.")
		Payload(func() {
			Meta("openapi:typename", "AdmitWorkloadSubjectRequestBody")
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Attribute("workload_issuer_id", String, func() { Format(FormatUUID) })
			Attribute("subject", String, func() {
				Description("The assertion's sub claim, matched exactly.")
				MinLength(1)
				MaxLength(2048)
			})
			Attribute("name", String, func() { MinLength(1); MaxLength(100) })
			Attribute("agent_id", String, func() {
				Description("Agent supplying the workload's policy.")
				Format(FormatUUID)
			})
			Required("organization_id", "workload_issuer_id", "subject", "agent_id")
		})
		Result(AdminWorkloadIdentityState)
		HTTP(func() {
			POST("/admin/organization.workloadAdmission")
			Response(StatusOK)
		})
	})

	Method("setWorkloadAuthenticationHost", func() {
		Meta("openapi:operationId", "adminSetWorkloadAuthenticationHost")
		Meta("openapi:extension:x-speakeasy-name-override", "setWorkloadAuthenticationHost")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminSetWorkloadAuthenticationHost"}`)
		Description("Announce the deployment's authentication host as this issuer's OAuth issuer and endpoint origin. Required by clients that refuse a token endpoint sharing a host with the API it calls.")
		Payload(func() {
			Meta("openapi:typename", "SetWorkloadAuthenticationHostRequestBody")
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Attribute("user_session_issuer_id", String, func() { Format(FormatUUID) })
			Attribute("enabled", Boolean)
			Required("organization_id", "user_session_issuer_id", "enabled")
		})
		Result(AdminWorkloadIdentityState)
		HTTP(func() {
			POST("/admin/organization.workloadAuthenticationHost")
			Response(StatusOK)
		})
	})

	Method("teardownWorkloadIssuer", func() {
		Meta("openapi:operationId", "adminTeardownWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "teardownWorkloadIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminTeardownWorkloadIssuer"}`)
		Description("Withdraw an issuer with its admissions and agent assignments in one step, so teardown cannot leave a subject admitted under an issuer that is gone.")
		Payload(func() {
			Meta("openapi:typename", "TeardownWorkloadIssuerRequestBody")
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Attribute("workload_issuer_id", String, func() { Format(FormatUUID) })
			Required("organization_id", "workload_issuer_id")
		})
		Result(AdminWorkloadIdentityState)
		HTTP(func() {
			POST("/admin/organization.workloadIdentityTeardown")
			Response(StatusOK)
		})
	})
}
