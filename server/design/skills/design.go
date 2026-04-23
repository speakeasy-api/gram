package skills

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("skills", func() {
	Description("Capture skill artifacts and metadata from local hook producers.")

	shared.DeclareErrorResponses()

	Method("get", func() {
		Description("Get a captured skill by slug.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("slug")
			Attribute("slug", shared.Slug, "The slug of the skill")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Skill)

		HTTP(func() {
			GET("/rpc/skills.get")
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "getSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "getBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Skill"}`)
	})

	Method("list", func() {
		Description("List captured skills for a project.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListSkillsResult)

		HTTP(func() {
			GET("/rpc/skills.list")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listSkills")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListSkills"}`)
	})

	Method("getSettings", func() {
		Description("Get capture settings for a project.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SkillCaptureSettings)

		HTTP(func() {
			GET("/rpc/skills.getSettings")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "getSkillsSettings")
		Meta("openapi:extension:x-speakeasy-name-override", "getSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillsSettings"}`)
	})

	Method("setSettings", func() {
		Description("Update capture settings for a project.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(SetSkillCaptureSettingsForm)
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SkillCaptureSettings)

		HTTP(func() {
			POST("/rpc/skills.setSettings")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "setSkillsSettings")
		Meta("openapi:extension:x-speakeasy-name-override", "setSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetSkillsSettings"}`)
	})

	Method("capture", func() {
		Description("Capture a skill artifact and associated metadata via producer authentication.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(CaptureSkillProducerForm)
		Result(CaptureSkillResult)

		HTTP(func() {
			POST("/rpc/skills.capture")
			security.ByKeyHeader()
			security.ProjectHeader()
			Header("name:X-Gram-Skill-Name")
			Header("scope:X-Gram-Skill-Scope")
			Header("discovery_root:X-Gram-Skill-Discovery-Root")
			Header("source_type:X-Gram-Skill-Source-Type")
			Header("content_sha256:X-Gram-Skill-Content-Sha256")
			Header("asset_format:X-Gram-Skill-Asset-Format")
			Header("resolution_status:X-Gram-Skill-Resolution-Status")
			Header("skill_id:X-Gram-Skill-Id")
			Header("skill_version_id:X-Gram-Skill-Version-Id")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "captureSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "capture")
	})

	Method("captureClaude", func() {
		Description("Capture a skill artifact and associated metadata via validated Claude session metadata.")

		Payload(func() {
			Extend(CaptureSkillClaudeForm)
			Attribute("claude_session_id", String)
			Required("claude_session_id")
		})
		Result(CaptureSkillResult)

		HTTP(func() {
			POST("/rpc/skills.captureClaude")
			Header("claude_session_id:X-Gram-Claude-Session-ID", String)
			Header("name:X-Gram-Skill-Name")
			Header("scope:X-Gram-Skill-Scope")
			Header("discovery_root:X-Gram-Skill-Discovery-Root")
			Header("source_type:X-Gram-Skill-Source-Type")
			Header("content_sha256:X-Gram-Skill-Content-Sha256")
			Header("asset_format:X-Gram-Skill-Asset-Format")
			Header("resolution_status:X-Gram-Skill-Resolution-Status")
			Header("skill_id:X-Gram-Skill-Id")
			Header("skill_version_id:X-Gram-Skill-Version-Id")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "captureClaudeSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "captureClaude")
	})

	Method("uploadManual", func() {
		Description("Upload a skill artifact manually using session authentication. Always ingests as pending review.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("request_body")
			Attribute("request_body", Bytes)
			Extend(CaptureSkillCoreForm)
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(CaptureSkillResult)

		HTTP(func() {
			POST("/rpc/skills.uploadManual")
			security.SessionHeader()
			security.ProjectHeader()
			Header("name:X-Gram-Skill-Name")
			Header("scope:X-Gram-Skill-Scope")
			Header("discovery_root:X-Gram-Skill-Discovery-Root")
			Header("source_type:X-Gram-Skill-Source-Type")
			Header("content_sha256:X-Gram-Skill-Content-Sha256")
			Header("asset_format:X-Gram-Skill-Asset-Format")
			Header("resolution_status:X-Gram-Skill-Resolution-Status")
			Header("skill_id:X-Gram-Skill-Id")
			Header("skill_version_id:X-Gram-Skill-Version-Id")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			Body("request_body")
		})

		Meta("openapi:operationId", "uploadManualSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadManual")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillsUploadManual"}`)
	})

	Method("listVersions", func() {
		Description("List captured versions for a skill.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("skill_id")
			Attribute("skill_id", String)
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListSkillVersionsResult)

		HTTP(func() {
			GET("/rpc/skills.versions")
			Param("skill_id")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listSkillVersions")
		Meta("openapi:extension:x-speakeasy-name-override", "listVersions")
	})

	Method("listPending", func() {
		Description("List skills and versions that are pending review.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListPendingSkillsResult)

		HTTP(func() {
			GET("/rpc/skills.pending")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listPendingSkills")
		Meta("openapi:extension:x-speakeasy-name-override", "listPending")
	})

	Method("approveVersion", func() {
		Description("Approve a captured skill version and mark it active for the lineage.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("version_id")
			Attribute("version_id", String)
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SkillVersion)

		HTTP(func() {
			POST("/rpc/skills.approveVersion")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "approveSkillVersion")
		Meta("openapi:extension:x-speakeasy-name-override", "approveVersion")
	})

	Method("supersedeVersion", func() {
		Description("Mark a captured skill version as superseded.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("version_id")
			Attribute("version_id", String)
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SkillVersion)

		HTTP(func() {
			POST("/rpc/skills.supersedeVersion")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "supersedeSkillVersion")
		Meta("openapi:extension:x-speakeasy-name-override", "supersedeVersion")
	})

	Method("rejectVersion", func() {
		Description("Reject a captured skill version.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("version_id")
			Attribute("version_id", String)
			Attribute("reason", String, func() {
				MaxLength(2000)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SkillVersion)

		HTTP(func() {
			POST("/rpc/skills.rejectVersion")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "rejectSkillVersion")
		Meta("openapi:extension:x-speakeasy-name-override", "rejectVersion")
	})

	Method("archive", func() {
		Description("Archive a skill lineage.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Required("skill_id")
			Attribute("skill_id", String)
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Skill)

		HTTP(func() {
			POST("/rpc/skills.archive")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "archiveSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "archive")
	})
})

var ListSkillsResult = Type("ListSkillsResult", func() {
	Required("skills")
	Attribute("skills", ArrayOf(SkillEntry))
})

var Skill = Type("Skill", func() {
	Required("id", "name", "slug", "created_at", "updated_at")

	Attribute("id", String)
	Attribute("name", String)
	Attribute("slug", String)
	Attribute("description", String)
	Attribute("skill_uuid", String)
	Attribute("active_version_id", String)
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var SkillEntry = Type("SkillEntry", func() {
	Required("id", "name", "slug", "created_at", "updated_at", "version_count")

	Attribute("id", String)
	Attribute("name", String)
	Attribute("slug", String)
	Attribute("description", String)
	Attribute("skill_uuid", String)
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("version_count", Int64)
	Attribute("active_version", SkillVersionSummary)
})

var SkillVersionSummary = Type("SkillVersionSummary", func() {
	Required("id", "content_sha256", "asset_format", "size_bytes", "created_at")

	Attribute("id", String)
	Attribute("content_sha256", String)
	Attribute("asset_format", String)
	Attribute("size_bytes", Int64)
	Attribute("author_name", String)
	Attribute("state", String, func() {
		Enum("pending_review", "active", "superseded", "rejected")
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("first_seen_at", String, func() {
		Format(FormatDateTime)
	})
})

var SkillVersion = Type("SkillVersion", func() {
	Required(
		"id",
		"skill_id",
		"content_sha256",
		"asset_format",
		"size_bytes",
		"state",
		"created_at",
		"updated_at",
	)

	Attribute("id", String)
	Attribute("skill_id", String)
	Attribute("asset_id", String)
	Attribute("content_sha256", String)
	Attribute("asset_format", String)
	Attribute("size_bytes", Int64)
	Attribute("skill_bytes", Int64)
	Attribute("state", String, func() {
		Enum("pending_review", "active", "superseded", "rejected")
	})
	Attribute("captured_by_user_id", String)
	Attribute("author_name", String)
	Attribute("rejected_by_user_id", String)
	Attribute("rejected_reason", String)
	Attribute("rejected_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("first_seen_trace_id", String)
	Attribute("first_seen_session_id", String)
	Attribute("first_seen_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListSkillVersionsResult = Type("ListSkillVersionsResult", func() {
	Required("versions")
	Attribute("versions", ArrayOf(SkillVersion))
})

var PendingSkillEntry = Type("PendingSkillEntry", func() {
	Required("skill", "versions")
	Attribute("skill", Skill)
	Attribute("versions", ArrayOf(SkillVersion))
})

var ListPendingSkillsResult = Type("ListPendingSkillsResult", func() {
	Required("skills")
	Attribute("skills", ArrayOf(PendingSkillEntry))
})

var CaptureSkillCoreForm = Type("CaptureSkillCoreForm", func() {
	Required(
		"name",
		"scope",
		"discovery_root",
		"source_type",
		"content_sha256",
		"asset_format",
		"resolution_status",
		"content_type",
		"content_length",
	)

	Attribute("name", String, func() {
		MinLength(1)
		MaxLength(100)
	})
	Attribute("scope", String, func() {
		Enum("project", "user")
	})
	Attribute("discovery_root", String, func() {
		Enum(
			"project_agents",
			"project_claude",
			"project_cursor",
			"user_agents",
			"user_claude",
			"user_cursor",
			"manual_upload",
		)
	})
	Attribute("source_type", String, func() {
		Enum("local_filesystem", "manual_upload")
	})
	Attribute("content_sha256", String, func() {
		Pattern("^[a-fA-F0-9]{64}$")
	})
	Attribute("asset_format", String, func() {
		Enum("zip")
	})
	Attribute("resolution_status", String, func() {
		Enum("resolved", "unresolved_name_only", "invalid_skill_root", "skipped_by_author")
	})
	Attribute("skill_id", String)
	Attribute("skill_version_id", String)
	Attribute("content_type", String)
	Attribute("content_length", Int64)
})

var CaptureSkillProducerForm = Type("CaptureSkillProducerForm", func() {
	Extend(CaptureSkillCoreForm)
	security.ByKeyPayload()
	security.ProjectPayload()
})

var CaptureSkillClaudeForm = Type("CaptureSkillClaudeForm", func() {
	Extend(CaptureSkillCoreForm)
})

var CaptureSkillAsset = Type("CaptureSkillAsset", func() {
	Required("id", "kind", "sha256", "content_type", "content_length", "created_at", "updated_at")

	Attribute("id", String)
	Attribute("kind", String, func() {
		Enum("skill")
	})
	Attribute("sha256", String)
	Attribute("content_type", String)
	Attribute("content_length", Int64)
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var CaptureSkillResult = Type("CaptureSkillResult", func() {
	Required("asset")
	Attribute("asset", CaptureSkillAsset)
})

var SetSkillCaptureSettingsForm = Type("SetSkillCaptureSettingsForm", func() {
	Required("enabled", "capture_project_skills", "capture_user_skills")

	Attribute("enabled", Boolean)
	Attribute("capture_project_skills", Boolean)
	Attribute("capture_user_skills", Boolean)
})

var SkillCaptureSettings = Type("SkillCaptureSettings", func() {
	Required(
		"effective_mode",
		"enabled",
		"capture_project_skills",
		"capture_user_skills",
		"inherited_from_organization",
	)

	Attribute("effective_mode", String, func() {
		Enum("disabled", "project_only", "user_only", "project_and_user")
	})
	Attribute("org_default_mode", String, func() {
		Enum("disabled", "project_only", "user_only", "project_and_user")
	})
	Attribute("project_override_mode", String, func() {
		Enum("disabled", "project_only", "user_only", "project_and_user")
	})
	Attribute("enabled", Boolean)
	Attribute("capture_project_skills", Boolean)
	Attribute("capture_user_skills", Boolean)
	Attribute("inherited_from_organization", Boolean)
})
