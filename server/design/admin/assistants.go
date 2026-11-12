package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminOrganizationAssistant = Type("AdminOrganizationAssistant", func() {
	Description("One live assistant of an organization, with where it stands in the move to agent identity.")
	Required("id", "name", "project_id", "project_slug", "project_name", "status", "identity_state", "runtime_version", "creator_active", "created_at")

	Attribute("id", String, "The assistant ID.", func() { Format(FormatUUID) })
	Attribute("name", String, "The assistant name.")
	Attribute("project_id", String, "The assistant's project ID.", func() { Format(FormatUUID) })
	Attribute("project_slug", String, "The assistant's project slug.")
	Attribute("project_name", String, "The assistant's project name.")
	Attribute("status", String, "The assistant status.")
	Attribute("identity_state", String, "NEVER_CONFIGURED when the assistant has no dedicated agent and still runs as its owner, ACTIVE when its dedicated agent is usable, and UNAVAILABLE when that agent is suspended, revoked, or deleted.", func() {
		Enum("NEVER_CONFIGURED", "ACTIVE", "UNAVAILABLE")
	})
	Attribute("agent_id", String, "The dedicated agent, when the assistant has one.", func() { Format(FormatUUID) })
	Attribute("runtime_version", Int, "The assistant's selected runtime: 2 for the default runtime, 3 for Kit.")
	Attribute("created_by_user_id", String, "The user who created the assistant, when known.")
	Attribute("created_by_user_email", String, "The creator's email, when known.")
	Attribute("creator_active", Boolean, "Whether the creator is still an active member of the organization. Migration gives the creator ownership of the new agent, so an assistant whose creator left cannot be migrated.")
	Attribute("created_at", String, "When the assistant was created.", func() { Format(FormatDateTime) })
})

var AdminOrganizationAssistants = Type("AdminOrganizationAssistants", func() {
	Required("organization_id", "assistants")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("assistants", ArrayOf(AdminOrganizationAssistant), "Every live assistant of the organization, by project and name.")
})

var AdminAssistantIdentityMigrationResult = Type("AdminAssistantIdentityMigrationResult", func() {
	Description("What the identity migration did to one assistant.")
	Required("assistant_id", "project_id", "name", "outcome")

	Attribute("assistant_id", String, "The assistant ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The assistant's project ID.", func() { Format(FormatUUID) })
	Attribute("name", String, "The assistant name.")
	Attribute("outcome", String, "migrated when the run gave the assistant a dedicated agent, already_migrated when it already had one, needs_owner when its creator is no longer an active organization member, and failed when provisioning failed.", func() {
		Enum("migrated", "already_migrated", "needs_owner", "failed")
	})
})

var AdminAssistantIdentityMigrationRun = Type("AdminAssistantIdentityMigrationRun", func() {
	Description("The result of migrating one organization's assistants to agent identity.")
	Required("organization_id", "migrated", "already_migrated", "needs_owner", "failed", "results")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("migrated", Int64, "Assistants the run gave a dedicated agent.")
	Attribute("already_migrated", Int64, "Assistants that already had a dedicated agent.")
	Attribute("needs_owner", Int64, "Assistants left unmigrated because their creator is no longer an active member.")
	Attribute("failed", Int64, "Assistants whose provisioning failed. Running again retries them.")
	Attribute("results", ArrayOf(AdminAssistantIdentityMigrationResult), "One result per live assistant.")
})

func assistantMethods() {
	Method("listOrganizationAssistants", func() {
		Description("Lists every live assistant of one organization with its identity state, runtime, and whether its creator is still a member.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")
		})

		Result(AdminOrganizationAssistants)

		HTTP(func() {
			GET("/admin/organization.assistants")
			Param("organization_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListOrganizationAssistants")
		Meta("openapi:extension:x-speakeasy-name-override", "listOrganizationAssistants")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminListOrganizationAssistants"}`)
	})

	Method("migrateOrganizationAssistantIdentities", func() {
		Description("Gives every assistant of one organization that has no dedicated agent its own agent, owned by the assistant's creator, and binds its triggers to workload identities. The agent starts with access to every MCP server and skill in the assistant's project, administration of the assistant, and project inference only when the creator has it. Assistants whose creator is no longer a member are skipped and reported. Idempotent, so it is safe to run again.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")

			// Goa's OpenAPI emitter deduplicates request bodies by shape, so
			// without an explicit typename this body takes another method's
			// name in the generated SDK.
			Meta("openapi:typename", "MigrateOrganizationAssistantIdentitiesRequestBody")
		})

		Result(AdminAssistantIdentityMigrationRun)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/organization.migrateAssistantIdentities")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminMigrateOrganizationAssistantIdentities")
		Meta("openapi:extension:x-speakeasy-name-override", "migrateOrganizationAssistantIdentities")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminMigrateOrganizationAssistantIdentities"}`)
	})
}
