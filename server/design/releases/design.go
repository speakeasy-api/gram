package releases

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("releases", func() {
	Description("Manage toolset releases and versioning")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})

	shared.DeclareErrorResponses()

	Method("createRelease", func() {
		Description("Create a new release from the current staging toolset state")

		Payload(func() {
			Required("toolset_slug")
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset to create a release for")
			Attribute("notes", String, "Optional release notes")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ToolsetRelease)

		HTTP(func() {
			POST("/rpc/releases.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRelease")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRelease"}`)
	})

	Method("listReleases", func() {
		Description("List all releases for a toolset")

		Payload(func() {
			Required("toolset_slug")
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset")
			Attribute("limit", Int32, "Maximum number of releases to return")
			Attribute("offset", Int32, "Offset for pagination")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListReleasesResult)

		HTTP(func() {
			GET("/rpc/releases.list")
			Param("toolset_slug")
			Param("limit")
			Param("offset")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listReleases")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListReleases"}`)
	})

	Method("getRelease", func() {
		Description("Get a specific release by ID")

		Payload(func() {
			Required("release_id")
			Attribute("release_id", String, "The ID of the release")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ToolsetRelease)

		HTTP(func() {
			GET("/rpc/releases.get")
			Param("release_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRelease")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
	})

	Method("getReleaseByNumber", func() {
		Description("Get a specific release by toolset and release number")

		Payload(func() {
			Required("toolset_slug", "release_number")
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset")
			Attribute("release_number", Int64, "The release number")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ToolsetRelease)

		HTTP(func() {
			GET("/rpc/releases.getByNumber")
			Param("toolset_slug")
			Param("release_number")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getReleaseByNumber")
		Meta("openapi:extension:x-speakeasy-name-override", "getByNumber")
	})

	Method("getLatestRelease", func() {
		Description("Get the latest release for a toolset")

		Payload(func() {
			Required("toolset_slug")
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ToolsetRelease)

		HTTP(func() {
			GET("/rpc/releases.getLatest")
			Param("toolset_slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getLatestRelease")
		Meta("openapi:extension:x-speakeasy-name-override", "getLatest")
	})

	Method("rollbackToRelease", func() {
		Description("Rollback a toolset to a specific release")

		Payload(func() {
			Required("toolset_slug", "release_number")
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset")
			Attribute("release_number", Int64, "The release number to rollback to")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			POST("/rpc/releases.rollback")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rollbackToRelease")
		Meta("openapi:extension:x-speakeasy-name-override", "rollback")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RollbackToRelease"}`)
	})
})

var ToolsetRelease = Type("ToolsetRelease", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the release")
	Attribute("toolset_id", String, "The toolset this release belongs to")
	Attribute("release_number", Int64, "The sequential release number")
	Attribute("source_state_id", String, "The source state ID captured in this release")
	Attribute("toolset_version_id", String, "The toolset version ID captured in this release")
	Attribute("global_variations_version_id", String, "The global variations version ID (optional)")
	Attribute("toolset_variations_version_id", String, "The toolset-scoped variations version ID (optional)")
	Attribute("notes", String, "Release notes")
	Attribute("released_by_user_id", String, "The user who created this release")
	Attribute("created_at", String, func() {
		Description("When the release was created")
		Format(FormatDateTime)
	})

	Required("id", "toolset_id", "release_number", "toolset_version_id", "released_by_user_id", "created_at")
})

var ListReleasesResult = Type("ListReleasesResult", func() {
	Attribute("releases", ArrayOf(ToolsetRelease), "List of releases")
	Attribute("total", Int64, "Total number of releases")
	Required("releases", "total")
})
