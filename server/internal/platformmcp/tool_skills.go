//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSkillsToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug whose skill registry to list"`
	Search      string `json:"search,omitempty" jsonschema:"optional case-insensitive search over skill names and summaries"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skills call"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum skills to return; defaults to 50 and is capped at 100"`
}

type GetSkillToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the skill"`
	SkillID        string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	IncludeContent bool   `json:"include_content,omitempty" jsonschema:"include the latest version's SKILL.md content; off by default because a manifest is up to 64 KiB"`
}

type ListSkillVersionsToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the skill"`
	SkillID        string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	IncludeContent bool   `json:"include_content,omitempty" jsonschema:"include each version's SKILL.md content; off by default because a manifest is up to 64 KiB"`
	Cursor         string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skill_versions call"`
	Limit          int    `json:"limit,omitempty" jsonschema:"maximum versions to return; defaults to 50 and is capped at 100"`
}

type CreateSkillToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug that will own the skill"`
	Content     string `json:"content" jsonschema:"the complete SKILL.md, including YAML frontmatter and instructions; at most 65536 UTF-8 bytes"`
}

type AddSkillVersionToolInput struct {
	ProjectSlug             string `json:"project_slug" jsonschema:"explicit project slug that owns the skill"`
	SkillID                 string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	Content                 string `json:"content" jsonschema:"the complete replacement SKILL.md; versions are immutable, so a correction is a new version rather than an edit"`
	ExpectedLatestVersionID string `json:"expected_latest_version_id" jsonschema:"the version the caller read before writing, from get_skill; the write is refused as a conflict if the skill has moved on"`
}

type UpdateSkillMetadataToolInput struct {
	ProjectSlug             string `json:"project_slug" jsonschema:"explicit project slug that owns the skill"`
	SkillID                 string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	Name                    string `json:"name,omitempty" jsonschema:"new canonical skill name; omitted leaves it unchanged"`
	DisplayName             string `json:"display_name,omitempty" jsonschema:"new user-facing skill name; omitted leaves it unchanged"`
	Summary                 string `json:"summary,omitempty" jsonschema:"new registry summary; omitted leaves it unchanged"`
	ClearSummary            bool   `json:"clear_summary,omitempty" jsonschema:"remove the registry summary instead of replacing it"`
	ExpectedLatestVersionID string `json:"expected_latest_version_id" jsonschema:"the version the caller read before writing, from get_skill; the write is refused as a conflict if the skill has moved on"`
}

type DistributeSkillToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug that owns both the skill and the target"`
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
		Description: "List the skills in a named project, newest change first. A skill is a written set of instructions an agent loads when it applies. Returns names and how many versions each has; the instructions themselves are read separately with get_skill.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillsToolInput) (*mcp.CallToolResult, ListSkillsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillsOutput, error) {
			return skills.ListSkills(ctx, principal, ListSkillsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_skill",
		Title:       "Get Skill",
		Description: "Read one skill in a named project. Constraints: set include_content to read the instructions themselves; it is off by default because they run to 64 KiB.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSkillToolInput) (*mcp.CallToolResult, GetSkillOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (GetSkillOutput, error) {
			return skills.GetSkill(ctx, principal, GetSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_versions",
		Title:       "List Skill Versions",
		Description: "List a skill's versions, newest first. A version is a fixed snapshot of the instructions: it is never edited in place, so a correction is recorded as a new version with add_skill_version.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillVersionsToolInput) (*mcp.CallToolResult, ListSkillVersionsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillVersionsOutput, error) {
			return skills.ListSkillVersions(ctx, principal, ListSkillVersionsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "create_skill",
		Title:       "Create Skill",
		Description: "Write a new skill in a named project — a set of instructions an agent loads when it applies — from complete SKILL.md content. Writing it alone does nothing: no agent loads it until distribute_skill gives it to a plugin or an assistant. Constraints: an existing active skill with the same normalized name records a new version instead, and identical content returns the existing version unchanged.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateSkillToolInput) (*mcp.CallToolResult, SkillAuthoringResult, error) {
		return skillsToolCall(ctx, func(principal Principal) (SkillAuthoringResult, error) {
			return skills.CreateSkill(ctx, principal, CreateSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "add_skill_version",
		Title:       "Add Skill Version",
		Description: "Change what a skill tells an agent, by recording a new version from complete replacement SKILL.md content. Versions are fixed snapshots, so a correction is a new one rather than an edit. Constraints: pass the version you read as expected_latest_version_id — if the skill has moved on since, the write is refused rather than overwriting someone else's version. Identical content returns the existing version unchanged. Recording a version gives it to nobody new; the plugins and assistants that already carry the skill and track its latest version pick it up.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddSkillVersionToolInput) (*mcp.CallToolResult, SkillAuthoringResult, error) {
		return skillsToolCall(ctx, func(principal Principal) (SkillAuthoringResult, error) {
			return skills.AddSkillVersion(ctx, principal, AddSkillVersionInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "update_skill_metadata",
		Title:       "Rename a Skill",
		Description: "Rename a skill, or change how it is described in the list — its canonical name, display name, and summary. Nothing here changes what the skill tells an agent to do; that lives in its versions. Constraints: pass the version you read as expected_latest_version_id.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateSkillMetadataToolInput) (*mcp.CallToolResult, UpdateSkillMetadataOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (UpdateSkillMetadataOutput, error) {
			return skills.UpdateSkillMetadata(ctx, principal, UpdateSkillMetadataInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "distribute_skill",
		Title:       "Give a Skill to a Plugin or Assistant",
		Description: "Give a skill to one plugin or one assistant in the same project, so agents there start loading it. This is the only way a skill takes effect. Constraints: name the target exactly — a name matching nothing is refused as not_found and a name matching more than one target as ambiguous_target, with no fallback to the default plugin. Repeat calls settle on the same result rather than adding it twice.",
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
		{"list_skills", "List Skills", "List the skills in a project. This is not switched on for your organization yet.", true},
		{"get_skill", "Get Skill", "Read one skill in a project. This is not switched on for your organization yet.", true},
		{"list_skill_versions", "List Skill Versions", "List a skill's versions. This is not switched on for your organization yet.", true},
		{"create_skill", "Create Skill", "Write a new skill from complete SKILL.md content. This is not switched on for your organization yet.", false},
		{"add_skill_version", "Add Skill Version", "Change what a skill tells an agent, by recording a new version. This is not switched on for your organization yet.", false},
		{"update_skill_metadata", "Rename a Skill", "Rename a skill, or change how it is described. This is not switched on for your organization yet.", false},
		{"distribute_skill", "Give a Skill to a Plugin or Assistant", "Give a skill to one plugin or assistant. This is not switched on for your organization yet.", false},
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
		result = skillsRefusalResult{Code: unavailableCode, Message: "Skills are not switched on for your organization yet."}
	case errors.Is(err, ErrSkillTargetNotFound):
		result = skillsRefusalResult{Code: "not_found", Message: "No plugin or assistant in this project has that exact name. Name one of the targets returned by create_skill or add_skill_version; nothing is picked by default."}
	case errors.Is(err, ErrSkillTargetAmbiguous):
		result = skillsRefusalResult{Code: "ambiguous_target", Message: "More than one plugin or assistant in this project has that name. Name it by its ID instead."}
	case errors.Is(err, ErrSkillContentTooLarge):
		result = skillsRefusalResult{Code: "invalid_request", Message: "A SKILL.md may be at most 65536 UTF-8 bytes. Shorten the instructions rather than splitting them across versions."}
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
