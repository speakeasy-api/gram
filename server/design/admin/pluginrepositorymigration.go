package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminPluginRepositoryMigration = Type("AdminPluginRepositoryMigration", func() {
	Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
	Attribute("organization_id", String, "Organization that owns the project.")
	Attribute("repo_owner", String, "Stored GitHub repository owner.")
	Attribute("repo_name", String, "Stored GitHub repository name.")
	Attribute("bound", Boolean, "Whether the project has a trusted repository ID binding.")
	Required("project_id", "organization_id", "repo_owner", "repo_name", "bound")
})

var AdminPluginRepositoryMigrationList = Type("AdminPluginRepositoryMigrationList", func() {
	Attribute("items", ArrayOf(AdminPluginRepositoryMigration), "Projects with a plugin repository, ordered by project ID.")
	Attribute("next_cursor", String, "Pass as after for the next page; absent on the last page.", func() { Format(FormatUUID) })
	Required("items")
})

var AdminPluginRepositoryMigrationEntry = Type("AdminPluginRepositoryMigrationEntry", func() {
	Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
	Attribute("repository_id", String, "The historically verified GitHub repository ID as a decimal string.", func() { Pattern(`^[0-9]+$`) })
	Attribute("evidence", String, "Reference to trusted historical evidence that identifies this repository for the project.", func() { Pattern(`\S`) })
	Required("project_id", "repository_id", "evidence")
})

var AdminPluginRepositoryMigrationOutcome = Type("AdminPluginRepositoryMigrationOutcome", func() {
	Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
	Attribute("status", String, "Outcome of verifying or applying the binding.", func() {
		Enum("verified", "bound", "already_bound", "conflict", "unavailable")
	})
	Attribute("message", String, "Details of the outcome.")
	Required("project_id", "status", "message")
})

var AdminPluginRepositoryMigrationApplyResult = Type("AdminPluginRepositoryMigrationApplyResult", func() {
	Attribute("items", ArrayOf(AdminPluginRepositoryMigrationOutcome), "Outcome for each entry, in request order.")
	Required("items")
})

var AdminPluginRepositoryReprovisionResult = Type("AdminPluginRepositoryReprovisionResult", func() {
	Attribute("status", String, "Reprovisioning outcome.")
	Attribute("message", String, "Details of the outcome.")
	Required("status", "message")
})

func pluginRepositoryMigrationMethods() {
	Method("listPluginRepositoryMigrations", func() {
		Description("Lists plugin repositories and reports whether each is bound to a trusted GitHub repository ID.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("after", String, "Continue after this project ID.", func() { Format(FormatUUID) })
		})
		Result(AdminPluginRepositoryMigrationList)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/plugin-repository-migrations")
			Param("after")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminListPluginRepositoryMigrations")
		Meta("openapi:extension:x-speakeasy-name-override", "listPluginRepositoryMigrations")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminListPluginRepositoryMigrations"}`)
	})

	Method("applyPluginRepositoryMigrations", func() {
		Description("Verifies historical repository bindings and, unless dry_run is true, records them for the specified projects.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "ApplyPluginRepositoryMigrationsRequestBody")
			Attribute("entries", ArrayOf(AdminPluginRepositoryMigrationEntry), "Repository bindings to verify or apply.", func() { MinLength(1); MaxLength(50) })
			Attribute("dry_run", Boolean, "Verify bindings without recording them.")
			Required("entries", "dry_run")
		})
		Result(AdminPluginRepositoryMigrationApplyResult)
		declareUnavailable()
		HTTP(func() {
			POST("/admin/plugin-repository-migrations/apply")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminApplyPluginRepositoryMigrations")
		Meta("openapi:extension:x-speakeasy-name-override", "applyPluginRepositoryMigrations")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminApplyPluginRepositoryMigrations"}`)
	})

	Method("reprovisionPluginRepository", func() {
		Description("Reprovisions the plugin repository for one project as a separate recovery operation.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "ReprovisionPluginRepositoryRequestBody")
			Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
			Attribute("reason", String, "Staff reason for reprovisioning.", func() { Pattern(`\S`) })
			Required("project_id", "reason")
		})
		Result(AdminPluginRepositoryReprovisionResult)
		declareUnavailable()
		HTTP(func() {
			POST("/admin/plugin-repository-migrations/reprovision")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminReprovisionPluginRepository")
		Meta("openapi:extension:x-speakeasy-name-override", "reprovisionPluginRepository")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminReprovisionPluginRepository"}`)
	})
}
