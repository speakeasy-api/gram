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

	Method("restoreVersion", func() {
		Description("Restore a historical valid version as the skill's current version without changing the immutable version record or explicit distribution pins.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Attribute("version_id", String, "The historical version to restore.", func() { Format(FormatUUID) })
			Required("id", "version_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RecordSkillResult)

		HTTP(func() {
			POST("/rpc/skills.restoreVersion")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(RestoreSkillVersionRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "restoreSkillVersion")
		Meta("openapi:extension:x-speakeasy-name-override", "restoreVersion")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RestoreSkillVersion"}`)
	})

	Method("update", func() {
		Description("Rename an active skill or update its display name, summary, and tags. The implementation requires the skills product feature and skill write scope.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Attribute("name", String, "The canonical skill name.", func() { MaxLength(64) })
			Attribute("display_name", String, "The user-facing skill name.", func() { MaxLength(256) })
			Attribute("summary", String, "The optional skill summary.", func() { MaxLength(1024) })
			Attribute("tags", ArrayOf(String, func() { MaxLength(64) }), "Registry tags for categorizing the skill. At most 40 tags.", func() {
				MaxLength(40)
			})
			Required("id", "name", "display_name", "tags")
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
			Attribute("search", String, "Search skill names, display names, and summaries.", func() { MaxLength(256) })
			Attribute("source_kinds", ArrayOf(String, func() { Enum("manual", "captured") }), "Only return skills from these sources.")
			Attribute("classifications", ArrayOf(String, func() { Enum("custom", "built_in") }), "Only return skills with these classifications.")
			Attribute("tags", ArrayOf(String, func() { MaxLength(64) }), "Only return skills that have any of these tags.")
			Attribute("sort", String, "How to order skills.", func() {
				Enum("name", "updated")
				Default("name")
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
			Param("search")
			Param("source_kinds")
			Param("classifications")
			Param("tags")
			Param("sort")
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

	Method("listTags", func() {
		Description("List distinct tags used by active skills in the project. The implementation requires the skills product feature and skill read scope.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillTagsResult)

		HTTP(func() {
			GET("/rpc/skills.listTags")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listSkillTags")
		Meta("openapi:extension:x-speakeasy-name-override", "listTags")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillTags"}`)
	})

	Method("listSuggestions", func() {
		Description("List open skill edit suggestions in the project, newest first. The implementation requires the skills product feature and skill read scope.")

		Payload(func() {
			Attribute("skill_id", String, "Only return suggestions for this skill.", func() { Format(FormatUUID) })
			Attribute("cursor", String, "Cursor for the next page of suggestions.")
			Attribute("limit", Int, "The number of suggestions to return per page.", func() {
				Default(20)
				Minimum(1)
				Maximum(50)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillSuggestionsResult)

		HTTP(func() {
			GET("/rpc/skills.listSuggestions")
			Param("skill_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listSkillSuggestions")
		Meta("openapi:extension:x-speakeasy-name-override", "listSuggestions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillSuggestions"}`)
	})

	Method("listFeedback", func() {
		Description("List outcome counts, collection metrics, volume, and recent resolved feedback for a skill. Name-only feedback is excluded.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Attribute("cursor", String, "Cursor for the next page of feedback.")
			Attribute("limit", Int, "The number of feedback rows to return per page.", func() {
				Default(20)
				Minimum(1)
				Maximum(50)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillFeedbackResult)

		HTTP(func() {
			GET("/rpc/skills.listFeedback")
			Param("id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listSkillFeedback")
		Meta("openapi:extension:x-speakeasy-name-override", "listFeedback")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillFeedback"}`)
	})

	Method("triggerSuggestion", func() {
		Description("Manually run suggestion analysis for a skill, bypassing automatic feedback and efficacy thresholds while preserving the one-open-suggestion invariant.")

		Payload(func() {
			Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/skills.triggerSuggestion")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(TriggerSkillSuggestionRequestBody)
			Response(StatusAccepted)
		})

		Meta("openapi:operationId", "triggerSkillSuggestion")
		Meta("openapi:extension:x-speakeasy-name-override", "triggerSuggestion")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TriggerSkillSuggestion", "type": "mutation"}`)
	})

	Method("approveSuggestion", func() {
		Description("Approve an open skill edit suggestion, optionally replacing its proposed SKILL.md content or taking only a subset of its proposed changes. Stale suggestions are superseded instead.")

		Payload(func() {
			Attribute("id", String, "The suggestion ID.", func() { Format(FormatUUID) })
			Attribute("content", String, "Optional edited complete SKILL.md content. Handlers enforce a maximum size of 65,536 UTF-8 bytes.")
			Attribute("change_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Optional IDs of the proposed changes to take together as one new version. The suggestion stays open carrying whatever is left. Cannot be combined with edited content.")
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ApproveSkillSuggestionResult)

		HTTP(func() {
			POST("/rpc/skills.approveSuggestion")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(ApproveSkillSuggestionRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "approveSkillSuggestion")
		Meta("openapi:extension:x-speakeasy-name-override", "approveSuggestion")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ApproveSkillSuggestion"}`)
	})

	Method("dismissSuggestion", func() {
		Description("Idempotently dismiss an open skill edit suggestion. Approved and superseded suggestions conflict.")

		Payload(func() {
			Attribute("id", String, "The suggestion ID.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(SkillEditSuggestion)

		HTTP(func() {
			POST("/rpc/skills.dismissSuggestion")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(DismissSkillSuggestionRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "dismissSkillSuggestion")
		Meta("openapi:extension:x-speakeasy-name-override", "dismissSuggestion")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DismissSkillSuggestion"}`)
	})

	Method("listSuggestionFeedback", func() {
		Description("List the agent feedback cited as the reason for one proposed change, newest first.")

		Payload(func() {
			Attribute("id", String, "The proposed change ID.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "The number of feedback records to return.", func() {
				Default(50)
				Minimum(1)
				Maximum(50)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListSkillSuggestionFeedbackResult)

		HTTP(func() {
			GET("/rpc/skills.listSuggestionFeedback")
			Param("id")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listSkillSuggestionFeedback")
		Meta("openapi:extension:x-speakeasy-name-override", "listSuggestionFeedback")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillSuggestionFeedback"}`)
	})

	Method("approveAllSuggestions", func() {
		Description("Snapshot and independently process selected skill edit suggestions, or every open suggestion when no IDs are supplied. One conflict or failure does not stop the remaining approvals.")

		Payload(func() {
			Attribute("suggestion_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Optional suggestion IDs to approve. Omitted or empty approves every currently open suggestion.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ApproveAllSkillSuggestionsResult)

		HTTP(func() {
			POST("/rpc/skills.approveAllSuggestions")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(ApproveAllSkillSuggestionsRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "approveAllSkillSuggestions")
		Meta("openapi:extension:x-speakeasy-name-override", "approveAllSuggestions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ApproveAllSkillSuggestions"}`)
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

	Method("share", func() {
		Description("Create a public share link for a skill. Repeated requests return the existing active link, so each skill has at most one active share token.")

		Payload(func() {
			Attribute("skill_id", String, "The skill ID.", func() { Format(FormatUUID) })
			Required("skill_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(SkillShareLink)

		HTTP(func() {
			POST("/rpc/skills.share")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(ShareSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "shareSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "share")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ShareSkill"}`)
	})

	Method("unshare", func() {
		Description("Revoke a skill's active public share link. Repeated requests are a no-op.")

		Payload(func() {
			Attribute("skill_id", String, "The skill ID.", func() { Format(FormatUUID) })
			Required("skill_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/skills.unshare")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(UnshareSkillRequestBody)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "unshareSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "unshare")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UnshareSkill"}`)
	})

	Method("getShared", func() {
		Description("Fetch the publicly shared view of a skill by its share token. This endpoint is unauthenticated and only ever exposes the skill name, display name, summary, and latest content.")

		Payload(func() {
			Attribute("token", String, "The public share token.", func() {
				MinLength(32)
				MaxLength(128)
			})
			Required("token")
		})

		Result(SharedSkill)

		// Share tokens are unguessable capability URLs, so this endpoint is
		// public by design (same pattern as assets.serveImage).
		NoSecurity()

		HTTP(func() {
			GET("/rpc/skills.getShared")
			Param("token")

			Response(StatusOK, func() {
				Header("cache_control:Cache-Control")
				Header("x_robots_tag:X-Robots-Tag")
				Header("referrer_policy:Referrer-Policy")
			})
		})

		Meta("openapi:operationId", "getSharedSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "getShared")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SharedSkill"}`)
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

var RestoreSkillVersionRequestBody = Type("RestoreSkillVersionRequestBody", func() {
	Meta("openapi:typename", "RestoreSkillVersionRequestBody")

	Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
	Attribute("version_id", String, "The historical version to restore.", func() { Format(FormatUUID) })
	Required("id", "version_id")
})

var ApproveSkillSuggestionRequestBody = Type("ApproveSkillSuggestionRequestBody", func() {
	Meta("openapi:typename", "ApproveSkillSuggestionRequestBody")

	Attribute("id", String, "The suggestion ID.", func() { Format(FormatUUID) })
	Attribute("content", String, "Optional edited complete SKILL.md content. Handlers enforce a maximum size of 65,536 UTF-8 bytes.")
	Attribute("change_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Optional IDs of the proposed changes to take together as one new version. The suggestion stays open carrying whatever is left. Cannot be combined with edited content.")
	Required("id")
})

var DismissSkillSuggestionRequestBody = Type("DismissSkillSuggestionRequestBody", func() {
	Meta("openapi:typename", "DismissSkillSuggestionRequestBody")

	Attribute("id", String, "The suggestion ID.", func() { Format(FormatUUID) })
	Required("id")
})

var TriggerSkillSuggestionRequestBody = Type("TriggerSkillSuggestionRequestBody", func() {
	Meta("openapi:typename", "TriggerSkillSuggestionRequestBody")

	Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
	Required("id")
})

var ApproveAllSkillSuggestionsRequestBody = Type("ApproveAllSkillSuggestionsRequestBody", func() {
	Meta("openapi:typename", "ApproveAllSkillSuggestionsRequestBody")

	Attribute("suggestion_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Optional suggestion IDs to approve. Omitted or empty approves every currently open suggestion.")
})

var UpdateSkillRequestBody = Type("UpdateSkillRequestBody", func() {
	Meta("openapi:typename", "UpdateSkillRequestBody")

	Attribute("id", String, "The skill ID.", func() { Format(FormatUUID) })
	Attribute("name", String, "The canonical skill name.", func() { MaxLength(64) })
	Attribute("display_name", String, "The user-facing skill name.", func() { MaxLength(256) })
	Attribute("summary", String, "The optional skill summary.", func() { MaxLength(1024) })
	Attribute("tags", ArrayOf(String, func() { MaxLength(64) }), "Registry tags for categorizing the skill. At most 40 tags.", func() {
		MaxLength(40)
	})
	Required("id", "name", "display_name", "tags")
})

var ListSkillTagsResult = Type("ListSkillTagsResult", func() {
	Attribute("tags", ArrayOf(String), "Distinct tags used by active skills in the project, sorted lexicographically.")
	Required("tags")
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

var ShareSkillRequestBody = Type("ShareSkillRequestBody", func() {
	Meta("openapi:typename", "ShareSkillRequestBody")

	Attribute("skill_id", String, "The skill ID.", func() { Format(FormatUUID) })
	Required("skill_id")
})

var UnshareSkillRequestBody = Type("UnshareSkillRequestBody", func() {
	Meta("openapi:typename", "UnshareSkillRequestBody")

	Attribute("skill_id", String, "The skill ID.", func() { Format(FormatUUID) })
	Required("skill_id")
})

var SkillShareLink = Type("SkillShareLink", func() {
	Meta("struct:pkg:path", "types")
	Description("An active public share link for a skill.")

	Attribute("token", String, "The public share token.")
	Attribute("created_at", String, "When the share link was created.", func() {
		Format(FormatDateTime)
	})
	Required("token", "created_at")
})

var SharedSkill = Type("SharedSkill", func() {
	Description("The public view of a shared skill. It deliberately carries no project, organization, or user identifiers.")

	Attribute("name", String, "The normalized skill name.")
	Attribute("display_name", String, "The user-facing skill name.")
	Attribute("summary", String, "The optional skill summary.")
	Attribute("content", String, "The latest SKILL.md content.")
	Attribute("updated_at", String, "When the shared content was last updated.", func() {
		Format(FormatDateTime)
	})
	Attribute("cache_control", String, "The Cache-Control response header.")
	Attribute("x_robots_tag", String, "The X-Robots-Tag response header.")
	Attribute("referrer_policy", String, "The Referrer-Policy response header.")
	Required("name", "display_name", "content", "updated_at")
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
	Attribute("tags", ArrayOf(String), "Registry tags for categorizing the skill.")
	Attribute("latest_version_id", String, "The current version ID, selected by effective promotion time.", func() {
		Format(FormatUUID)
	})
	Attribute("version_count", Int64, "The number of immutable versions recorded for the skill.")
	Attribute("has_valid_version", Boolean, "Whether the skill has at least one valid version available to distribute.")
	Attribute("first_seen_at", String, "When this skill was first activated.", func() { Format(FormatDateTime) })
	Attribute("last_seen_at", String, "When this skill was most recently activated.", func() { Format(FormatDateTime) })
	Attribute("seen_count", Int64, "The number of reconciled activations observed for this skill.")
	Attribute("share_token", String, "The active public share token, absent when the skill is not shared.")
	Attribute("created_at", String, "When the skill was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the skill was last updated.", func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "name", "display_name", "source_kind", "classification", "tags", "version_count", "has_valid_version", "seen_count", "created_at", "updated_at")
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

var SkillEditSuggestion = Type("SkillEditSuggestion", func() {
	Meta("struct:pkg:path", "types")
	Description("A proposed edit to an existing project skill.")

	Attribute("id", String, "The suggestion ID.", func() { Format(FormatUUID) })
	Attribute("skill_id", String, "The skill targeted by the suggestion.", func() { Format(FormatUUID) })
	Attribute("skill_name", String, "The canonical skill name.")
	Attribute("skill_display_name", String, "The user-facing skill name.")
	Attribute("base_version_id", String, "The version the suggestion was generated from.", func() { Format(FormatUUID) })
	Attribute("changes", ArrayOf(SkillEditSuggestionChange), "The separate changes proposed, each reviewable on its own.")
	Attribute("proposed_content", String, "The complete SKILL.md content produced by taking every proposed change.")
	Attribute("applies_cleanly", Boolean, "Whether every proposed change still applies to the base version.")
	Attribute("rationale", String, "Why the edit was proposed, covering the suggestion as a whole.")
	Attribute("status", String, "The suggestion state.", func() { Enum("open", "approved", "dismissed", "superseded") })
	Attribute("feedback_count", Int64, "Feedback records the suggestion was generated from.")
	Attribute("feedback_session_count", Int64, "Distinct sessions that reported the feedback behind the suggestion.")
	Attribute("scored_session_count", Int64, "Scored sessions considered by the suggestion.")
	Attribute("approved_by_user_id", String, "The user that approved the suggestion, when present.")
	Attribute("approved_at", String, "When the suggestion was approved, when present.", func() { Format(FormatDateTime) })
	Attribute("created_at", String, "When the suggestion was created.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "When the suggestion was last updated.", func() { Format(FormatDateTime) })

	Required("id", "skill_id", "skill_name", "skill_display_name", "base_version_id", "changes", "proposed_content", "applies_cleanly", "rationale", "status", "feedback_count", "feedback_session_count", "scored_session_count", "created_at", "updated_at")
})

var SkillEditSuggestionChange = Type("SkillEditSuggestionChange", func() {
	Meta("struct:pkg:path", "types")
	Description("One self-contained edit proposed by a suggestion, applied and reviewed on its own.")

	Attribute("id", String, "The change ID.", func() { Format(FormatUUID) })
	Attribute("suggestion_id", String, "The suggestion the change belongs to.", func() { Format(FormatUUID) })
	Attribute("proposed_diff", String, "The change as a unified diff against the content the changes before it produce.")
	Attribute("rationale", String, "Why this change alone was proposed.")
	Attribute("applies_cleanly", Boolean, "Whether the change still applies.")
	Attribute("feedback_count", Int64, "Feedback records cited as the reason for this change.")
	Attribute("feedback_session_count", Int64, "Distinct sessions that reported the feedback behind this change.")
	Attribute("created_at", String, "When the change was recorded.", func() { Format(FormatDateTime) })

	Required("id", "suggestion_id", "proposed_diff", "rationale", "applies_cleanly", "feedback_count", "feedback_session_count", "created_at")
})

var SkillFeedbackSource = Type("SkillFeedbackSource", String, func() {
	Description("Where skill feedback was recorded.")
	Meta("openapi:typename", "SkillFeedbackSource")
})

var SkillFeedbackOutcome = Type("SkillFeedbackOutcome", String, func() {
	Description("The reported skill feedback outcome.")
	Enum("helped", "partially_helped", "did_not_help", "misleading", "harmful")
	Meta("openapi:typename", "SkillFeedbackOutcome")
})

var SkillFeedback = Type("SkillFeedback", func() {
	Description("A privacy-minimized feedback row for a skill.")
	Attribute("id", String, "The feedback ID.", func() { Format(FormatUUID) })
	Attribute("source", SkillFeedbackSource, "Where the feedback was recorded.")
	Attribute("outcome", SkillFeedbackOutcome, "The reported outcome.")
	Attribute("note", String, "An optional feedback note.")
	Attribute("skill_version_id", String, "The attributed skill version, when known.", func() { Format(FormatUUID) })
	Attribute("reviewed_at", String, "When automated suggestion analysis reviewed this feedback.", func() { Format(FormatDateTime) })
	Attribute("created_at", String, "When the feedback was recorded.", func() { Format(FormatDateTime) })
	Required("id", "source", "outcome", "created_at")
})

var SkillFeedbackCounts = Type("SkillFeedbackCounts", func() {
	Description("All-time outcome counts for resolved feedback on a skill.")
	Attribute("total", Int64)
	Attribute("helped", Int64)
	Attribute("partially_helped", Int64)
	Attribute("did_not_help", Int64)
	Attribute("misleading", Int64)
	Attribute("harmful", Int64)
	Required("total", "helped", "partially_helped", "did_not_help", "misleading", "harmful")
})

var SkillFeedbackMetrics = Type("SkillFeedbackMetrics", func() {
	Description("Feedback collection and suggestion conversion metrics for a skill.")
	Attribute("window_start", String, "The start of the rolling collection window.", func() { Format(FormatDateTime) })
	Attribute("window_end", String, "The end of the rolling collection window.", func() { Format(FormatDateTime) })
	Attribute("feedback_in_window", Int64, "Feedback recorded during the collection window.")
	Attribute("activations_in_window", Int64, "Resolved skill activations during the collection window.")
	Attribute("feedback_activations_in_window", Int64, "Resolved activations paired to feedback during the collection window.")
	Attribute("unreviewed", Int64, "Feedback not yet reviewed by suggestion analysis.")
	Attribute("converted", Int64, "All-time feedback linked to a generated suggestion.")
	Required("window_start", "window_end", "feedback_in_window", "activations_in_window", "feedback_activations_in_window", "unreviewed", "converted")
})

var SkillFeedbackTimelinePoint = Type("SkillFeedbackTimelinePoint", func() {
	Description("Feedback volume for one UTC day.")
	Attribute("bucket_start", String, "The start of the UTC day.", func() { Format(FormatDateTime) })
	Attribute("feedback_count", Int64)
	Required("bucket_start", "feedback_count")
})

var ListSkillFeedbackResult = Type("ListSkillFeedbackResult", func() {
	Description("Outcome counts, collection metrics, a 30-day timeline, and a newest-first page of feedback for a skill.")
	Attribute("counts", SkillFeedbackCounts)
	Attribute("metrics", SkillFeedbackMetrics)
	Attribute("timeline", ArrayOf(SkillFeedbackTimelinePoint))
	Attribute("feedback", ArrayOf(SkillFeedback))
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("counts", "metrics", "timeline", "feedback")
})

var ListSkillSuggestionFeedbackResult = Type("ListSkillSuggestionFeedbackResult", func() {
	Description("The agent feedback a suggestion was generated from, newest first.")
	Attribute("feedback", ArrayOf(SkillFeedback), "The feedback records linked to the suggestion.")
	Required("feedback")
})

var ListSkillSuggestionsResult = Type("ListSkillSuggestionsResult", func() {
	Description("A page of open skill edit suggestions.")
	Attribute("suggestions", ArrayOf(SkillEditSuggestion), "The open suggestions in this page.")
	Attribute("total_open_count", Int64, "The total number of matching open suggestions, independent of pagination.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("suggestions", "total_open_count")
})

var ApproveSkillSuggestionResult = Type("ApproveSkillSuggestionResult", func() {
	Description("The result of approving one suggestion.")
	Attribute("suggestion", SkillEditSuggestion, "The resulting suggestion state.")
	Attribute("outcome", String, "Whether the suggestion created a version, created one and stayed open carrying its remaining changes, or was stale.", func() { Enum("applied", "partially_applied", "superseded") })
	Attribute("version", SkillVersion, "The created version for an applied approval.")
	Required("suggestion", "outcome")
})

var SkillSuggestionApprovalItem = Type("SkillSuggestionApprovalItem", func() {
	Description("The result of one item in a bulk suggestion approval.")
	Attribute("suggestion_id", String, "The suggestion ID.", func() { Format(FormatUUID) })
	Attribute("skill_id", String, "The targeted skill ID.", func() { Format(FormatUUID) })
	Attribute("skill_name", String, "The canonical skill name.")
	Attribute("skill_display_name", String, "The user-facing skill name.")
	Attribute("outcome", String, "The item's processing outcome.", func() { Enum("applied", "superseded", "conflict", "failed") })
	Attribute("resulting_version_id", String, "The created version for an applied item.", func() { Format(FormatUUID) })
	Attribute("message", String, "A safe explanation for a conflict or failure.")
	Required("suggestion_id", "skill_id", "skill_name", "skill_display_name", "outcome")
})

var ApproveAllSkillSuggestionsResult = Type("ApproveAllSkillSuggestionsResult", func() {
	Description("Per-item outcomes for the snapshotted open suggestions.")
	Attribute("items", ArrayOf(SkillSuggestionApprovalItem), "The outcomes in snapshot order.")
	Required("items")
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
	Description("A UTC-day activation bucket for one attributed skill version.")
	Attribute("bucket_start", String, "Start of the UTC day.", func() { Format(FormatDateTime) })
	Attribute("skill_version_id", String, "The attributed skill version, absent when the observation could not be resolved to a version.", func() { Format(FormatUUID) })
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

var SkillPromptInjectionFinding = Type("SkillPromptInjectionFinding", func() {
	Description("A prompt-injection finding for the current skill version. Raw matched content is intentionally omitted.")

	Attribute("rule_id", String, "The rule that produced the finding.")
	Attribute("description", String, "Why the current skill version was flagged.")
	Attribute("confidence", Float64, "The classifier confidence from 0 to 1.", func() {
		Minimum(0)
		Maximum(1)
	})
	Required("rule_id", "description", "confidence")
})

var GetSkillResult = Type("GetSkillResult", func() {
	Description("An active skill and its current version.")

	Attribute("skill", Skill, "The skill.")
	Attribute("latest_version", SkillVersion, "The current immutable version by effective promotion time.")
	Attribute("adoption", SkillAdoption, "Activation adoption metrics.")
	Attribute("sighting_timeline", ArrayOf(SkillSightingTimelinePoint), "Daily activations by attributed version in the adoption window.")
	Attribute("drift", SkillDrift, "Active-machine version convergence.")
	Attribute("assistant_count", Int64, "The number of active, non-deleted assistants using the skill.")
	Attribute("prompt_injection_findings", ArrayOf(SkillPromptInjectionFinding), "Open prompt-injection findings for the current skill version.")
	Required("skill", "adoption", "sighting_timeline", "drift", "assistant_count", "prompt_injection_findings")
})

var ListSkillsResult = Type("ListSkillsResult", func() {
	Description("A page of active project skills.")

	Attribute("skills", ArrayOf(Skill), "The active skills in this page.")
	Attribute("total_count", Int64, "The total number of active skills matching the filters.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("skills", "total_count")
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
