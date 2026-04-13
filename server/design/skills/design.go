package skills

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("skills", func() {
	Description("Capture skill artifacts and metadata from local hook producers.")

	shared.DeclareErrorResponses()

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
