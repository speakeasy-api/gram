package skills

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("skills", func() {
	Description("Manage project skills and their immutable versions. Methods are gated by the skills product feature and skill read or write scopes.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("create", func() {
		Description("Record an uploaded SKILL.md. The implementation requires the skills product feature and skill write scope, and may create a skill, add a version to an existing skill, or return an existing canonical version as a no-op.")

		Payload(func() {
			Attribute("content", String, "The complete uploaded SKILL.md content. Handlers enforce a maximum size of 65,536 UTF-8 bytes.")
			Required("content")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RecordSkillResult)

		HTTP(func() {
			POST("/rpc/skills.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(CreateSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateSkill"}`)
	})

	Method("addVersion", func() {
		Description("Record an uploaded SKILL.md as a version of an existing skill. The implementation requires the skills product feature and skill write scope, and returns the existing canonical version as a no-op when appropriate.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() {
				Format(FormatUUID)
			})
			Attribute("content", String, "The complete uploaded SKILL.md content. Handlers enforce a maximum size of 65,536 UTF-8 bytes.")
			Required("id", "content")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RecordSkillResult)

		HTTP(func() {
			POST("/rpc/skills.addVersion")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(AddSkillVersionRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "addSkillVersion")
		Meta("openapi:extension:x-speakeasy-name-override", "addVersion")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddSkillVersion"}`)
	})

	Method("list", func() {
		Description("List active skills in the project. The implementation requires the skills product feature and skill read scope.")

		Payload(func() {
			Attribute("cursor", String, "Cursor for the next page of skills.")
			Attribute("limit", Int, "The number of skills to return per page.", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillsResult)

		HTTP(func() {
			GET("/rpc/skills.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listSkills")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Skills"}`)
	})

	Method("get", func() {
		Description("Get an active skill and its latest version. The implementation requires the skills product feature and skill read scope.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(GetSkillResult)

		HTTP(func() {
			GET("/rpc/skills.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Skill"}`)
	})

	Method("listVersions", func() {
		Description("List immutable versions of an active skill, newest first. The implementation requires the skills product feature and skill read scope.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Cursor for the next page of skill versions.")
			Attribute("limit", Int, "The number of skill versions to return per page.", func() {
				Default(20)
				Minimum(1)
				Maximum(50)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillVersionsResult)

		HTTP(func() {
			GET("/rpc/skills.listVersions")
			Param("id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listSkillVersions")
		Meta("openapi:extension:x-speakeasy-name-override", "listVersions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillVersions"}`)
	})

	Method("archive", func() {
		Description("Idempotently archive a skill. The implementation requires the skills product feature and skill write scope. Repeated requests for the same skill succeed without creating another state transition.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/skills.archive")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(ArchiveSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "archiveSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "archive")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ArchiveSkill"}`)
	})
})

var CreateSkillRequestBody = Type("CreateSkillRequestBody", func() {
	Meta("openapi:typename", "CreateSkillRequestBody")

	Attribute("content", String, "The complete uploaded SKILL.md content. Handlers enforce a maximum size of 65,536 UTF-8 bytes.")
	Required("content")
})

var AddSkillVersionRequestBody = Type("AddSkillVersionRequestBody", func() {
	Meta("openapi:typename", "AddSkillVersionRequestBody")

	Attribute("id", String, "The skill ID.", func() {
		Format(FormatUUID)
	})
	Attribute("content", String, "The complete uploaded SKILL.md content. Handlers enforce a maximum size of 65,536 UTF-8 bytes.")
	Required("id", "content")
})

var ArchiveSkillRequestBody = Type("ArchiveSkillRequestBody", func() {
	Meta("openapi:typename", "ArchiveSkillRequestBody")

	Attribute("id", String, "The skill ID.", func() {
		Format(FormatUUID)
	})
	Required("id")
})

var SkillValidationError = Type("SkillValidationError", func() {
	Meta("struct:pkg:path", "types")
	Description("A validation problem found in an uploaded skill manifest.")

	Attribute("code", String, "A stable validation error code.")
	Attribute("field", String, "The manifest field associated with the problem.")
	Attribute("message", String, "A human-readable explanation of the problem.")
	Required("code", "field", "message")
})

var Skill = Type("Skill", func() {
	Meta("struct:pkg:path", "types")
	Description("An active project skill. All API reads return active skills, and archive returns an empty response.")

	Attribute("id", String, "The skill ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project that owns the skill.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The normalized project-unique skill name.")
	Attribute("display_name", String, "The display name from the latest recorded manifest.")
	Attribute("summary", String, "The optional summary from the latest recorded manifest.")
	Attribute("source_kind", String, "How the skill entered the registry.")
	Attribute("classification", String, "The skill classification.")
	Attribute("latest_version_id", String, "The derived latest version ID, selected from immutable version creation order.", func() {
		Format(FormatUUID)
	})
	Attribute("version_count", Int64, "The number of immutable versions recorded for the skill.")
	Attribute("created_at", String, "When the skill was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the skill was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "name", "display_name", "source_kind", "classification", "latest_version_id", "version_count", "created_at", "updated_at")
})

var SkillVersion = Type("SkillVersion", func() {
	Meta("struct:pkg:path", "types")
	Description("An immutable version of a skill manifest.")

	Attribute("id", String, "The skill version ID.", func() {
		Format(FormatUUID)
	})
	Attribute("skill_id", String, "The skill that owns this version.", func() {
		Format(FormatUUID)
	})
	Attribute("content", String, "The exact uploaded SKILL.md content.")
	Attribute("canonical_sha256", String, "The SHA-256 manifest digest derived from canonicalized SKILL.md content.")
	Attribute("raw_sha256", String, "The SHA-256 digest of the exact uploaded SKILL.md content.")
	Attribute("description", String, "The optional description from this manifest version.")
	Attribute("metadata", MapOf(String, Any), "Metadata parsed from this manifest version.")
	Attribute("spec_valid", Boolean, "Whether this manifest version conforms to the Agent Skills specification.")
	Attribute("validation_errors", ArrayOf(SkillValidationError), "Specification validation problems recorded for this manifest version.")
	Attribute("created_at", String, "When this immutable version was recorded.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_by_user_id", String, "The user that recorded this version.")

	Required("id", "skill_id", "content", "canonical_sha256", "raw_sha256", "metadata", "spec_valid", "validation_errors", "created_at", "created_by_user_id")
})

var RecordSkillResult = Type("RecordSkillResult", func() {
	Description("The result of recording an uploaded skill manifest, including whether the operation created either resource.")

	Attribute("skill", Skill, "The recorded skill.")
	Attribute("version", SkillVersion, "The resulting immutable skill version.")
	Attribute("created_skill", Boolean, "Whether this request created the skill.")
	Attribute("created_version", Boolean, "Whether this request created a new immutable version rather than resolving to an existing canonical version.")
	Required("skill", "version", "created_skill", "created_version")
})

var GetSkillResult = Type("GetSkillResult", func() {
	Description("An active skill and its derived latest version.")

	Attribute("skill", Skill, "The skill.")
	Attribute("latest_version", SkillVersion, "The latest immutable version by creation order.")
	Required("skill", "latest_version")
})

var ListSkillsResult = Type("ListSkillsResult", func() {
	Description("A page of active project skills.")

	Attribute("skills", ArrayOf(Skill), "The active skills in this page.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("skills")
})

var ListSkillVersionsResult = Type("ListSkillVersionsResult", func() {
	Description("A page of immutable skill versions.")

	Attribute("versions", ArrayOf(SkillVersion), "The skill versions in this page.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("versions")
})
