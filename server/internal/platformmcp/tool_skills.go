//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSkillsToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit AICP project slug whose skill registry to list"`
	Search      string `json:"search,omitempty" jsonschema:"optional case-insensitive search over skill names and summaries"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skills call"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum skills to return; defaults to 50 and is capped at 100"`
}

type GetSkillToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the skill"`
	SkillID        string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	IncludeContent bool   `json:"include_content,omitempty" jsonschema:"include the latest version's SKILL.md content; off by default because a manifest is up to 64 KiB"`
}

type ListSkillVersionsToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the skill"`
	SkillID        string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	IncludeContent bool   `json:"include_content,omitempty" jsonschema:"include each version's SKILL.md content; off by default because a manifest is up to 64 KiB"`
	Cursor         string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skill_versions call"`
	Limit          int    `json:"limit,omitempty" jsonschema:"maximum versions to return; defaults to 50 and is capped at 100"`
}

type CreateSkillToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit AICP project slug that will own the skill"`
	Content     string `json:"content" jsonschema:"the complete SKILL.md, including YAML frontmatter and instructions; at most 65536 UTF-8 bytes"`
}

type AddSkillVersionToolInput struct {
	ProjectSlug             string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the skill"`
	SkillID                 string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	Content                 string `json:"content" jsonschema:"the complete replacement SKILL.md; versions are immutable, so a correction is a new version rather than an edit"`
	ExpectedLatestVersionID string `json:"expected_latest_version_id" jsonschema:"the version the caller read before writing, from get_skill; the write is refused as a conflict if the skill has moved on"`
}

type UpdateSkillMetadataToolInput struct {
	ProjectSlug             string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the skill"`
	SkillID                 string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	Name                    string `json:"name,omitempty" jsonschema:"new canonical skill name; omitted leaves it unchanged"`
	DisplayName             string `json:"display_name,omitempty" jsonschema:"new user-facing skill name; omitted leaves it unchanged"`
	Summary                 string `json:"summary,omitempty" jsonschema:"new registry summary; omitted leaves it unchanged"`
	ClearSummary            bool   `json:"clear_summary,omitempty" jsonschema:"remove the registry summary instead of replacing it"`
	ExpectedLatestVersionID string `json:"expected_latest_version_id" jsonschema:"the version the caller read before writing, from get_skill; the write is refused as a conflict if the skill has moved on"`
}

type DistributeSkillToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit AICP project slug that owns both the skill and the target"`
	SkillID     string `json:"skill_id" jsonschema:"skill ID returned by list_skills or create_skill"`
	Plugin      string `json:"plugin,omitempty" jsonschema:"exact existing plugin in the project, by ID, slug, or name; exactly one of plugin or assistant is required and there is no implicit default"`
	Assistant   string `json:"assistant,omitempty" jsonschema:"exact existing assistant in the project, by ID or name; exactly one of plugin or assistant is required"`
}

type skillsRefusalResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerSkillsTools(reg *Registrar, skills *SkillsService) {
	addTool(reg, &mcp.Tool{
		Name:        "list_skills",
		Title:       "List Skills",
		Description: "List the skills in an explicit AICP project registry, newest change first. Returns names, version counts, and the current version ID; manifest content is read separately with get_skill.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillsToolInput) (*mcp.CallToolResult, ListSkillsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillsOutput, error) {
			return skills.ListSkills(ctx, principal, ListSkillsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_skill",
		Title:       "Get Skill",
		Description: "Read one skill in an explicit AICP project, including its current version ID. Set include_content to read the SKILL.md itself; it is off by default because a manifest is up to 64 KiB of the caller's context.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSkillToolInput) (*mcp.CallToolResult, GetSkillOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (GetSkillOutput, error) {
			return skills.GetSkill(ctx, principal, GetSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_versions",
		Title:       "List Skill Versions",
		Description: "List a skill's immutable versions, newest first. Versions are never edited in place: corrections are recorded with add_skill_version.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillVersionsToolInput) (*mcp.CallToolResult, ListSkillVersionsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillVersionsOutput, error) {
			return skills.ListSkillVersions(ctx, principal, ListSkillVersionsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "create_skill",
		Title:       "Create Skill",
		Description: "Create a skill in an explicit AICP project from complete SKILL.md content. The skill is inert: authoring alone changes nothing at runtime, and no agent loads it until distribute_skill sends it to a plugin or an assistant. An existing active skill with the same normalized name records a new version instead, and identical canonical content returns the existing version as a no-op.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateSkillToolInput) (*mcp.CallToolResult, SkillAuthoringResult, error) {
		return skillsToolCall(ctx, func(principal Principal) (SkillAuthoringResult, error) {
			return skills.CreateSkill(ctx, principal, CreateSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "add_skill_version",
		Title:       "Add Skill Version",
		Description: "Record a new immutable version of an existing skill from complete replacement SKILL.md content. Pass the version you read as expected_latest_version_id: if the skill has moved on since, the write is refused as a conflict rather than overwriting someone else's version. Identical canonical content returns the existing version as a no-op. Recording a version does not distribute it; existing distributions that track the latest valid version pick it up.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddSkillVersionToolInput) (*mcp.CallToolResult, SkillAuthoringResult, error) {
		return skillsToolCall(ctx, func(principal Principal) (SkillAuthoringResult, error) {
			return skills.AddSkillVersion(ctx, principal, AddSkillVersionInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "update_skill_metadata",
		Title:       "Update Skill Metadata",
		Description: "Change a skill's registry naming — canonical name, display name, and summary — without recording a version. Instructions live in versions, so nothing here changes what the skill tells an agent to do. Pass the version you read as expected_latest_version_id.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateSkillMetadataToolInput) (*mcp.CallToolResult, UpdateSkillMetadataOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (UpdateSkillMetadataOutput, error) {
			return skills.UpdateSkillMetadata(ctx, principal, UpdateSkillMetadataInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "distribute_skill",
		Title:       "Distribute Skill",
		Description: "Distribute a skill to one exact existing plugin or assistant in the same project. This is the only way a skill takes effect. Name the target exactly: a name that matches nothing is refused as not_found and a name that matches more than one target as ambiguous_target — there is no fallback to the default plugin. Repeat calls converge on the same attachment rather than creating a second one.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeSkillToolInput) (*mcp.CallToolResult, DistributeSkillOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (DistributeSkillOutput, error) {
			return skills.DistributeSkill(ctx, principal, DistributeSkillInput(input))
		})
	})
}

// registerUnavailableSkillsTools declares the same tools the live registration
// declares, so a rollout flip changes what a tool answers rather than whether
// the tool exists.
func registerUnavailableSkillsTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		readOnly    bool
	}{
		{"list_skills", "List Skills", "List the skills in an explicit AICP project. Skill authoring is not enabled in the current rollout.", true},
		{"get_skill", "Get Skill", "Read one skill in an explicit AICP project. Skill authoring is not enabled in the current rollout.", true},
		{"list_skill_versions", "List Skill Versions", "List a skill's immutable versions. Skill authoring is not enabled in the current rollout.", true},
		{"create_skill", "Create Skill", "Create a skill from complete SKILL.md content. Skill authoring is not enabled in the current rollout.", false},
		{"add_skill_version", "Add Skill Version", "Record a new immutable version of an existing skill. Skill authoring is not enabled in the current rollout.", false},
		{"update_skill_metadata", "Update Skill Metadata", "Change a skill's registry naming. Skill authoring is not enabled in the current rollout.", false},
		{"distribute_skill", "Distribute Skill", "Distribute a skill to one exact plugin or assistant. Skill distribution is not enabled in the current rollout.", false},
	} {
		manifest := &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
		}
		if tool.readOnly {
			manifest.Annotations = readOnlyAnnotations()
		}
		addTool(reg, manifest, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("skills"))
	}
}

// skillsToolCall runs one skill call and turns a refusal into a structured
// error result rather than a transport error, so the reason survives to the
// model that has to act on it.
func skillsToolCall[Out any](ctx context.Context, call func(principal Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err != nil {
		if refusal, ok := skillsToolResult(err); ok {
			return refusal, zero, nil
		}
		return nil, zero, err
	}
	return nil, output, nil
}

func skillsToolResult(err error) (*mcp.CallToolResult, bool) {
	var result skillsRefusalResult
	switch {
	case errors.Is(err, ErrSkillsUnavailable):
		result = skillsRefusalResult{Code: unavailableCode, Message: "Skill authoring and distribution are not enabled for this organization."}
	case errors.Is(err, ErrSkillTargetNotFound):
		result = skillsRefusalResult{Code: "not_found", Message: "No plugin or assistant in this project matches that target exactly. List the targets returned by create_skill or add_skill_version and name one of them; the skill is not distributed anywhere by default."}
	case errors.Is(err, ErrSkillTargetAmbiguous):
		result = skillsRefusalResult{Code: "ambiguous_target", Message: "More than one plugin or assistant in this project matches that name. Name the target by its ID instead."}
	case errors.Is(err, ErrSkillContentTooLarge):
		result = skillsRefusalResult{Code: "invalid_request", Message: "A SKILL.md may be at most 65536 UTF-8 bytes. Shorten the manifest rather than splitting it across versions."}
	default:
		if budgetResult, ok := operationBudgetToolResult(err); ok {
			return budgetResult, true
		}
		code, message, ok := skillsRefusalCode(err)
		if !ok {
			return nil, false
		}
		result = skillsRefusalResult{Code: code, Message: message}
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
