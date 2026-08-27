package identity

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("identity", func() {
	Description("Resolves the identifiers Gram records activity under into a single identity.")

	Security(security.ByKey, func() {
		Scope("producer")
	})
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("resolve", func() {
		Description("Resolve an identity URN into every identifier the subject's activity is recorded under.")

		Payload(func() {
			Extend(ResolveIdentityForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})
		Result(IdentityModel)

		HTTP(func() {
			GET("/rpc/identity.resolve")
			security.ByKeyHeader()
			security.SessionHeader()
			Param("urn")
		})

		Meta("openapi:operationId", "resolveIdentity")
		Meta("openapi:extension:x-speakeasy-name-override", "resolve")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Identity"}`)
	})
})

var ResolveIdentityForm = Type("ResolveIdentityForm", func() {
	Required("urn")
	Attribute("urn", String, func() {
		Description("The identity URN to resolve, in the form '<kind>:<id>'. Kind is one of 'user' (Gram user id), 'email', 'external' (the external user id an agent reported), 'apikey', or 'agent'. Callers pass whichever identifier they hold; every URN for the same subject resolves to the same identity.")
		Example("user:user_01abc")
	})
})

var IdentityDirectory = Type("IdentityDirectory", func() {
	Description("WorkOS Directory Sync attributes for an identity. Every field is mapped by the customer's IdP and may be absent.")

	Required("groups")

	Attribute("department_name", String, "The directory department.")
	Attribute("job_title", String, "The directory job title.")
	Attribute("employee_type", String, "The directory employment type.")
	Attribute("division_name", String, "The directory division.")
	Attribute("cost_center_name", String, "The directory cost centre.")
	Attribute("groups", ArrayOf(String), "The directory groups the identity currently belongs to.")
})

var IdentityModel = Type("IdentityModel", func() {
	Description("One subject with every identifier its activity is keyed under, so a client can query each subsystem with the identifier that subsystem expects.")

	Required("kind", "canonical_urn", "display_name", "user_ids", "emails", "external_user_ids", "directory")

	Attribute("kind", String, func() {
		Description("What sort of subject this is: 'human' for a directory user, 'apikey' for a subject acting under an API key, 'agent' for an agent identity, or 'unattributed' for activity whose identifier matches no directory row.")
		Enum("human", "apikey", "agent", "unattributed")
	})
	Attribute("canonical_urn", String, func() {
		Description("The identity URN clients should navigate to. Resolving any URN for the same subject returns this one, so links built from different identifiers converge on a single page.")
	})

	Attribute("user_ids", ArrayOf(String), func() {
		Description("The Gram user ids this identity resolves to, the first being the directory owner. Empty when the subject matches no directory row. Audit logs, chats, user sessions and plugin assignments key on these.")
	})
	Attribute("emails", ArrayOf(String), func() {
		Description("Every address the subject is known by — directory email first, then linked AI account emails. Telemetry and cost aggregate over this set.")
	})
	Attribute("external_user_ids", ArrayOf(String), func() {
		Description("The identifiers agents report for this subject. Risk findings key on these.")
	})
	Attribute("workos_user_id", String, func() {
		Description("The WorkOS user id, which RBAC role assignments key on. Absent for subjects with no WorkOS link.")
	})

	Attribute("display_name", String, "The subject's name, falling back to its primary email.")
	Attribute("photo_url", String, "The subject's avatar, when the directory supplied one.")

	Attribute("directory", IdentityDirectory, "Directory Sync attributes, empty when the subject has no directory row.")
})
