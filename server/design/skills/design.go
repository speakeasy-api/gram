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
			Attribute("derived_from_version_id", String, "The optional source version this new version was derived from.", func() { Format(FormatUUID) })
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

	Method("update", func() {
		Description("Rename an active skill or update its display name and summary. The implementation requires the skills product feature and skill write scope.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Attribute("name", String, "The canonical skill name.", func() { MaxLength(64) })
			Attribute("display_name", String, "The user-facing skill name.", func() { MaxLength(256) })
			Attribute("summary", String, "The optional skill summary.", func() { MaxLength(1024) })
			Required("id", "name", "display_name")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Skill)

		HTTP(func() {
			POST("/rpc/skills.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(UpdateSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateSkill"}`)
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

	Method("listUnknownActivations", func() {
		Description("List terminal skill activations that could not be attributed to a skill version.")

		Payload(func() {
			Attribute("cursor", String, "Cursor for the next page of unknown activations.")
			Attribute("limit", Int, "The number of unknown activations to return per page.", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListUnknownSkillActivationsResult)

		HTTP(func() {
			GET("/rpc/skills.listUnknownActivations")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUnknownSkillActivations")
		Meta("openapi:extension:x-speakeasy-name-override", "listUnknownActivations")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UnknownSkillActivations"}`)
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

	Method("distribute", func() {
		Description("Create or update the active distribution of a skill to exactly one plugin or assistant. Repeating the request for the same target updates the version pin or is a no-op.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Attribute("plugin_id", String, "The plugin that carries the skill.", func() { Format(FormatUUID) })
			Attribute("assistant_id", String, "The assistant that carries the skill.", func() { Format(FormatUUID) })
			Attribute("pinned_version_id", String, "An optional valid version to pin instead of tracking the latest valid version.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(SkillDistribution)

		HTTP(func() {
			POST("/rpc/skills.distribute")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(DistributeSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "distributeSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "distribute")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DistributeSkill"}`)
	})

	Method("undistribute", func() {
		Description("Revoke a skill's active distribution to exactly one plugin or assistant. Repeated requests are a no-op.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Attribute("plugin_id", String, "The plugin the skill was distributed to.", func() { Format(FormatUUID) })
			Attribute("assistant_id", String, "The assistant the skill was distributed to.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/skills.undistribute")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(UndistributeSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "undistributeSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "undistribute")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UndistributeSkill"}`)
	})

	Method("listDistributions", func() {
		Description("List active plugin skill distributions for the current project.")

		Payload(func() {
			Attribute("skill_id", String, "Only return distributions of this skill.", func() { Format(FormatUUID) })
			Attribute("plugin_id", String, "Only return distributions carried by this plugin.", func() { Format(FormatUUID) })
			Attribute("cursor", String, "Cursor for the next page of skill distributions.")
			Attribute("limit", Int, "The number of skill distributions to return per page.", func() {
				Default(20)
				Minimum(1)
				Maximum(50)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillDistributionsResult)

		HTTP(func() {
			GET("/rpc/skills.listDistributions")
			Param("skill_id")
			Param("plugin_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listSkillDistributions")
		Meta("openapi:extension:x-speakeasy-name-override", "listDistributions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillDistributions"}`)
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
	Attribute("derived_from_version_id", String, "The optional source version this new version was derived from.", func() { Format(FormatUUID) })
	Required("id", "content")
})

var UpdateSkillRequestBody = Type("UpdateSkillRequestBody", func() {
	Meta("openapi:typename", "UpdateSkillRequestBody")

	Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
	Attribute("name", String, "The canonical skill name.", func() { MaxLength(64) })
	Attribute("display_name", String, "The user-facing skill name.", func() { MaxLength(256) })
	Attribute("summary", String, "The optional skill summary.", func() { MaxLength(1024) })
	Required("id", "name", "display_name")
})

var ArchiveSkillRequestBody = Type("ArchiveSkillRequestBody", func() {
	Meta("openapi:typename", "ArchiveSkillRequestBody")

	Attribute("id", String, "The skill ID.", func() {
		Format(FormatUUID)
	})
	Required("id")
})

var DistributeSkillRequestBody = Type("DistributeSkillRequestBody", func() {
	Meta("openapi:typename", "DistributeSkillRequestBody")

	Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
	Attribute("plugin_id", String, "The plugin that carries the skill.", func() { Format(FormatUUID) })
	Attribute("assistant_id", String, "The assistant that carries the skill.", func() { Format(FormatUUID) })
	Attribute("pinned_version_id", String, "An optional valid version to pin instead of tracking the latest valid version.", func() { Format(FormatUUID) })
	Required("id")
	Example(Val{
		"id":        "550e8400-e29b-41d4-a716-446655440000",
		"plugin_id": "550e8400-e29b-41d4-a716-446655440001",
	})
})

var UndistributeSkillRequestBody = Type("UndistributeSkillRequestBody", func() {
	Meta("openapi:typename", "UndistributeSkillRequestBody")

	Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
	Attribute("plugin_id", String, "The plugin the skill was distributed to.", func() { Format(FormatUUID) })
	Attribute("assistant_id", String, "The assistant the skill was distributed to.", func() { Format(FormatUUID) })
	Required("id")
	Example(Val{
		"id":        "550e8400-e29b-41d4-a716-446655440000",
		"plugin_id": "550e8400-e29b-41d4-a716-446655440001",
	})
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
	Attribute("display_name", String, "The user-facing registry name.")
	Attribute("summary", String, "The optional registry summary.")
	Attribute("source_kind", String, "How the skill entered the registry.")
	Attribute("classification", String, "The skill classification.")
	Attribute("latest_version_id", String, "The derived latest version ID, selected from immutable version creation order.", func() {
		Format(FormatUUID)
	})
	Attribute("version_count", Int64, "The number of immutable versions recorded for the skill.")
	Attribute("has_valid_version", Boolean, "Whether the skill has at least one valid version available to distribute.")
	Attribute("first_seen_at", String, "When this skill was first activated.", func() { Format(FormatDateTime) })
	Attribute("last_seen_at", String, "When this skill was most recently activated.", func() { Format(FormatDateTime) })
	Attribute("seen_count", Int64, "The number of reconciled activations observed for this skill.")
	Attribute("created_at", String, "When the skill was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the skill was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "name", "display_name", "source_kind", "classification", "version_count", "has_valid_version", "seen_count", "created_at", "updated_at")
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
	Attribute("frontmatter", MapOf(String, Any), "All top-level frontmatter fields parsed from this manifest version.")
	Attribute("spec_valid", Boolean, "Whether this manifest version conforms to the Agent Skills specification.")
	Attribute("validation_errors", ArrayOf(SkillValidationError), "Specification validation problems recorded for this manifest version.")
	Attribute("derived_from_version_id", String, "The source version this version was derived from.", func() { Format(FormatUUID) })
	Attribute("created_at", String, "When this immutable version was recorded.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_by_user_id", String, "The user that recorded this version.")
	Attribute("first_seen_at", String, "When this exact version was first activated.", func() { Format(FormatDateTime) })
	Attribute("last_seen_at", String, "When this exact version was most recently activated.", func() { Format(FormatDateTime) })
	Attribute("seen_count", Int64, "The number of activations attributed to this exact version.")

	Required("id", "skill_id", "content", "canonical_sha256", "raw_sha256", "metadata", "frontmatter", "spec_valid", "validation_errors", "created_at", "created_by_user_id", "seen_count")
})

var SkillAdoption = Type("SkillAdoption", func() {
	Description("Activation adoption metrics for a skill.")
	Attribute("window_start", String, "Start of the rolling adoption window.", func() { Format(FormatDateTime) })
	Attribute("window_end", String, "End of the rolling adoption window.", func() { Format(FormatDateTime) })
	Attribute("distinct_hostnames", Int64, "Distinct non-empty hostnames that activated the skill during the rolling window.")
	Attribute("activations_in_window", Int64, "Activations observed during the rolling window.")
	Required("window_start", "window_end", "distinct_hostnames", "activations_in_window")
})

var SkillSightingTimelinePoint = Type("SkillSightingTimelinePoint", func() {
	Description("A UTC-day activation bucket for a skill.")
	Attribute("bucket_start", String, "Start of the UTC day.", func() { Format(FormatDateTime) })
	Attribute("activation_count", Int64, "Activations observed during the day.")
	Required("bucket_start", "activation_count")
})

var SkillDrift = Type("SkillDrift", func() {
	Description("Active-machine convergence against the skill's plugin distribution target.")
	Attribute("window_start", String, "Start of the active-machine window.", func() { Format(FormatDateTime) })
	Attribute("window_end", String, "End of the active-machine window.", func() { Format(FormatDateTime) })
	Attribute("target_state", String, "Whether the skill has no distribution target, one target, or conflicting targets.", func() {
		Enum("not_distributed", "single", "ambiguous")
	})
	Attribute("target_version_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Distinct versions targeted by active plugin distributions.")
	Attribute("active_machines", Int64, "Machines that activated the skill during the window.")
	Attribute("on_target_machines", Int64, "Active machines whose latest activation used the target version.")
	Attribute("drifted_machines", Int64, "Active machines whose latest attributed activation used another version.")
	Attribute("indeterminate_machines", Int64, "Active machines without a version or without one unambiguous target.")
	Required("window_start", "window_end", "target_state", "target_version_ids", "active_machines", "on_target_machines", "drifted_machines", "indeterminate_machines")
})

var UnknownSkillActivation = Type("UnknownSkillActivation", func() {
	Description("A completed activation that could not be attributed to a skill version.")
	Attribute("id", String, "The activation observation ID.", func() { Format(FormatUUID) })
	Attribute("skill_name", String, "The skill name reported by the agent.")
	Attribute("provider", String, "The agent provider that reported the activation.")
	Attribute("source", String, "The optional provider-specific source.")
	Attribute("source_level", String, "The optional source precedence level.")
	Attribute("seen_at", String, "When the activation occurred.", func() { Format(FormatDateTime) })
	Attribute("reason", String, "Why exact version attribution failed.", func() {
		Enum("invalid_name", "unresolved_hash", "ambiguous_hash")
	})
	Required("id", "skill_name", "provider", "seen_at", "reason")
})

var SkillDistribution = Type("SkillDistribution", func() {
	Meta("struct:pkg:path", "types")
	Description("An active plugin or assistant distribution of a project skill.")

	Attribute("id", String, "The distribution ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The project that owns the distribution.", func() { Format(FormatUUID) })
	Attribute("skill_id", String, "The distributed skill ID.", func() { Format(FormatUUID) })
	Attribute("skill_name", String, "The canonical name of the distributed skill.")
	Attribute("skill_display_name", String, "The display name of the distributed skill.")
	Attribute("plugin_id", String, "The plugin that carries the skill.", func() { Format(FormatUUID) })
	Attribute("plugin_name", String, "The name of the plugin that carries the skill.")
	Attribute("assistant_id", String, "The assistant that carries the skill.", func() { Format(FormatUUID) })
	Attribute("assistant_name", String, "The name of the assistant that carries the skill.")
	Attribute("pinned_version_id", String, "The pinned version, absent when tracking the latest valid version.", func() { Format(FormatUUID) })
	Attribute("resolved_version_id", String, "The version currently targeted by this distribution.", func() { Format(FormatUUID) })
	Attribute("channel", String, "The distribution channel.", func() { Enum("plugin", "assistant") })
	Attribute("created_by_user_id", String, "The user that created the distribution.")
	Attribute("created_at", String, "When the distribution was created.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "When the distribution configuration last changed.", func() { Format(FormatDateTime) })

	Required("id", "project_id", "skill_id", "skill_name", "skill_display_name", "resolved_version_id", "channel", "created_by_user_id", "created_at", "updated_at")
})

var PluginSkillDistribution = Type("PluginSkillDistribution", func() {
	Meta("struct:pkg:path", "types")
	Description("An active plugin distribution of a project skill.")

	Attribute("id", String, "The distribution ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The project that owns the distribution.", func() { Format(FormatUUID) })
	Attribute("skill_id", String, "The distributed skill ID.", func() { Format(FormatUUID) })
	Attribute("skill_name", String, "The canonical name of the distributed skill.")
	Attribute("skill_display_name", String, "The display name of the distributed skill.")
	Attribute("plugin_id", String, "The plugin that carries the skill.", func() { Format(FormatUUID) })
	Attribute("plugin_name", String, "The name of the plugin that carries the skill.")
	Attribute("pinned_version_id", String, "The pinned version, absent when tracking the latest valid version.", func() { Format(FormatUUID) })
	Attribute("resolved_version_id", String, "The version currently targeted by this distribution.", func() { Format(FormatUUID) })
	Attribute("channel", String, "The distribution channel.", func() { Enum("plugin") })
	Attribute("created_by_user_id", String, "The user that created the distribution.")
	Attribute("created_at", String, "When the distribution was created.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "When the distribution configuration last changed.", func() { Format(FormatDateTime) })

	Required("id", "project_id", "skill_id", "skill_name", "skill_display_name", "plugin_id", "plugin_name", "resolved_version_id", "channel", "created_by_user_id", "created_at", "updated_at")
})

var ListSkillDistributionsResult = Type("ListSkillDistributionsResult", func() {
	Description("A page of active plugin skill distributions for the current project.")

	Attribute("distributions", ArrayOf(PluginSkillDistribution), "The active plugin skill distributions in this page.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("distributions")
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
	Attribute("adoption", SkillAdoption, "Activation adoption metrics.")
	Attribute("sighting_timeline", ArrayOf(SkillSightingTimelinePoint), "Daily activations in the adoption window.")
	Attribute("drift", SkillDrift, "Active-machine version convergence.")
	Attribute("assistant_count", Int64, "The number of active, non-deleted assistants using the skill.")
	Required("skill", "adoption", "sighting_timeline", "drift", "assistant_count")
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

var ListUnknownSkillActivationsResult = Type("ListUnknownSkillActivationsResult", func() {
	Description("A page of terminal skill activations without exact version attribution.")
	Attribute("activations", ArrayOf(UnknownSkillActivation), "Unknown activations in this page.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("activations")
})
