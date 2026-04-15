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
		Description("Capture a skill artifact and associated metadata.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(CaptureSkillForm)
		Result(CaptureSkillResult)

		HTTP(func() {
			POST("/rpc/skills.capture")
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
			security.ByKeyHeader()
			security.ProjectHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "captureSkill")
		Meta("openapi:extension:x-speakeasy-name-override", "capture")
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
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("first_seen_at", String, func() {
		Format(FormatDateTime)
	})
})

var CaptureSkillForm = Type("CaptureSkillForm", func() {
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

	security.ByKeyPayload()
	security.ProjectPayload()

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
		)
	})
	Attribute("source_type", String, func() {
		Enum("local_filesystem")
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
