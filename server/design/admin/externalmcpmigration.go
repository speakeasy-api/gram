package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

// maxExternalMcpMigrationServers bounds one migration request. Every server
// runs in its own transaction, so the bound keeps a request well inside the
// HTTP timeout.
const maxExternalMcpMigrationServers = 100

var AdminExternalMcpMigrationCandidate = Type("AdminExternalMcpMigrationCandidate", func() {
	Description("A toolset backed by an external MCP attachment, with what running it does: migrate its server in place, or delete it.")
	Required("toolset_id", "organization_id", "organization_name", "organization_slug", "project_id", "project_name", "project_slug", "server_name", "action", "delete_reasons", "blockers", "warnings")

	Attribute("toolset_id", String, "The toolset behind the server.", func() { Format(FormatUUID) })
	Attribute("organization_id", String, "Organization ID.")
	Attribute("organization_name", String, "Organization name.")
	Attribute("organization_slug", String, "Organization slug.")
	Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
	Attribute("project_name", String, "Project name.")
	Attribute("project_slug", String, "Project slug.")
	Attribute("server_name", String, "The server's display name.")
	Attribute("mcp_slug", String, "The slug in the server's /mcp/<slug> address. Absent when the server has none.")
	Attribute("remote_url", String, "The attachment's upstream URL. Absent when the server proxies several.")
	Attribute("replacement_url", String, "The upstream URL the server will dial as a remote MCP server: the one resource its signed-in sessions recorded, otherwise the attachment's URL without unfilled template query parameters.")
	Attribute("sign_in_provider", String, "The server's user session issuer and the upstream authorization servers behind it. Absent when the server has none.")
	Attribute("tool_calls", Int64, "Tool calls over the last tool_call_window_days. Absent when telemetry is unavailable.")
	Attribute("action", String, "What running the candidate does: migrate switches its server in place to a remote MCP server; delete removes the toolset because nothing is left to migrate, along with any server, addresses and plugin entries it still owns. A server already migrated in place is kept and keeps working.", func() {
		Enum("migrate", "delete")
	})
	Attribute("delete_reasons", ArrayOf(String), "Why the candidate is deleted rather than migrated. Empty when it migrates.")
	Attribute("blockers", ArrayOf(String), "Why the server cannot be migrated. Empty when it can.")
	Attribute("warnings", ArrayOf(String), "What migrating the server drops or cannot carry over. Warnings do not stop it.")
})

var AdminListExternalMcpMigrationCandidatesResult = Type("AdminListExternalMcpMigrationCandidatesResult", func() {
	Required("candidates", "tool_call_window_days")

	Attribute("candidates", ArrayOf(AdminExternalMcpMigrationCandidate), "Every candidate across all organizations.")
	Attribute("tool_call_window_days", Int, "Days of tool calls each candidate's tool_calls covers.")
})

var AdminExternalMcpMigrationRequest = Type("AdminExternalMcpMigrationRequest", func() {
	Required("toolset_id")

	Attribute("toolset_id", String, "The candidate's toolset.", func() { Format(FormatUUID) })
})

var AdminExternalMcpMigrationOutcome = Type("AdminExternalMcpMigrationOutcome", func() {
	Description("What happened, or in a preview what would happen, to one server.")
	Required("toolset_id", "migration_status", "actions", "warnings")

	Attribute("toolset_id", String, "The candidate's toolset.", func() { Format(FormatUUID) })
	Attribute("action", String, "What the run did, or would do, with the candidate. Absent when it was not attempted.", func() {
		Enum("migrate", "delete")
	})
	Attribute("migration_status", String, "migrated, deleted and previewed completed every step; deleted removed the toolset and any server it still owned, so mcp_server_id, remote_mcp_server_id and remote_url are absent; failed left the server unchanged; not_attempted follows an earlier failure.", func() {
		Enum("migrated", "deleted", "previewed", "failed", "not_attempted")
	})
	Attribute("reason", String, "Why the server failed or was not attempted.")
	Attribute("mcp_slug", String, "The server's /mcp/<slug> address. Migrating keeps it; deleting a server the toolset still owns removes it.")
	Attribute("mcp_server_id", String, "The server, which keeps its ID.", func() { Format(FormatUUID) })
	Attribute("remote_mcp_server_id", String, "The remote MCP source the server proxies once migrated. Absent when the server failed; a preview's source is rolled back.", func() { Format(FormatUUID) })
	Attribute("remote_url", String, "The upstream URL the server dials once migrated. Absent when the server failed.")
	Attribute("actions", ArrayOf(String), "Each change, in order.")
	Attribute("warnings", ArrayOf(String), "What the migration could not carry over.")
})

var AdminExternalMcpMigrationResult = Type("AdminExternalMcpMigrationResult", func() {
	Required("outcomes")

	Attribute("outcomes", ArrayOf(AdminExternalMcpMigrationOutcome), "One outcome per requested server, in request order.")
})

func externalMcpMigrationPayload() {
	security.AdminAuthPayload()
	Required("servers")

	Attribute("servers", ArrayOf(AdminExternalMcpMigrationRequest), "Servers to migrate or delete, in order.", func() {
		MinLength(1)
		MaxLength(maxExternalMcpMigrationServers)
	})
}

func externalMcpMigrationMethods() {
	Method("listExternalMcpMigrationCandidates", func() {
		declareUnavailable()
		Description("Lists every live server across all organizations whose latest toolset version proxies an external MCP attachment on its project's active deployment, with the blockers that stop each one from migrating.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminListExternalMcpMigrationCandidatesResult)

		HTTP(func() {
			declareUnavailableResponse()
			GET("/admin/externalMcpMigration.listCandidates")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListExternalMcpMigrationCandidates")
		Meta("openapi:extension:x-speakeasy-name-override", "listExternalMcpMigrationCandidates")
	})

	Method("previewExternalMcpMigration", func() {
		declareUnavailable()
		Description("Runs each server's migration or deletion inside a transaction that is rolled back, reporting what would happen without changing anything.")

		Payload(func() {
			externalMcpMigrationPayload()
			Meta("openapi:typename", "PreviewExternalMcpMigrationRequestBody")
		})

		Result(AdminExternalMcpMigrationResult)

		HTTP(func() {
			declareUnavailableResponse()
			POST("/admin/externalMcpMigration.preview")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminPreviewExternalMcpMigration")
		Meta("openapi:extension:x-speakeasy-name-override", "previewExternalMcpMigration")
	})

	Method("migrateExternalMcpServers", func() {
		declareUnavailable()
		Description("Switches each server in place from its external MCP attachment to a remote MCP source on the same sign-in provider, keeping its ID, address and everything that references it, and disables the old toolset. A candidate left with nothing to migrate is deleted instead, with its server, addresses and plugin entries, as the toolset delete API does. Servers run one at a time, each in its own transaction, and the run stops at the first failure. Never creates a sign-in provider or a client and never registers one with the upstream.")

		Payload(func() {
			externalMcpMigrationPayload()
			Meta("openapi:typename", "MigrateExternalMcpServersRequestBody")
		})

		Result(AdminExternalMcpMigrationResult)

		HTTP(func() {
			declareUnavailableResponse()
			POST("/admin/externalMcpMigration.migrate")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminMigrateExternalMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "migrateExternalMcpServers")
	})
}
