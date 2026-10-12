//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
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

type ListSkillFeedbackToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug that owns the skill"`
	SkillID     string `json:"skill_id" jsonschema:"skill ID returned by list_skills"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skill_feedback call"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum feedback records to return; defaults to 20 and is capped at 50"`
}

type ListSkillSuggestionsToolInput struct {
	ProjectSlug            string `json:"project_slug" jsonschema:"explicit project slug whose open suggestions to review"`
	SkillID                string `json:"skill_id,omitempty" jsonschema:"optional skill ID returned by list_skills; omit to review suggestions across the project"`
	IncludeProposedContent bool   `json:"include_proposed_content,omitempty" jsonschema:"include each complete proposed SKILL.md; off by default because a manifest is up to 64 KiB"`
	OmitDiffs              bool   `json:"omit_diffs,omitempty" jsonschema:"leave each change's proposed diff out; every other field, including its ID, rationale, whether it applies cleanly, and its feedback counts, is still returned. Use to triage a large queue, then read one skill's suggestions with diffs"`
	Cursor                 string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skill_suggestions call"`
	Limit                  int    `json:"limit,omitempty" jsonschema:"maximum suggestions to return; defaults to 20 and is capped at 50"`
}

type ListSkillSuggestionFeedbackToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug that owns the suggestion"`
	ChangeID    string `json:"change_id" jsonschema:"change ID returned inside list_skill_suggestions"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum linked feedback records to return; defaults to 20 and is capped at 50"`
}

type ApproveSkillSuggestionToolInput struct {
	ProjectSlug  string   `json:"project_slug" jsonschema:"explicit project slug that owns the suggestion"`
	SuggestionID string   `json:"suggestion_id" jsonschema:"suggestion ID returned by list_skill_suggestions"`
	ChangeIDs    []string `json:"change_ids,omitempty" jsonschema:"IDs of the reviewed changes to take, from list_skill_suggestions; name every change to take the whole suggestion. Exactly one of change_ids or content is required"`
	Content      string   `json:"content,omitempty" jsonschema:"complete edited SKILL.md to record instead of the proposed changes, at most 65536 UTF-8 bytes; use when the user corrected the suggestion. Exactly one of change_ids or content is required"`
	Confirmed    bool     `json:"confirmed" jsonschema:"set true only after the user explicitly confirms recording this new version of this exact skill"`
}

type DismissSkillSuggestionToolInput struct {
	ProjectSlug  string `json:"project_slug" jsonschema:"explicit project slug that owns the suggestion"`
	SuggestionID string `json:"suggestion_id" jsonschema:"suggestion ID returned by list_skill_suggestions"`
	Confirmed    bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms discarding this suggestion"`
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

type ListSkillDistributionsToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug whose skill distributions to list"`
	SkillID     string `json:"skill_id,omitempty" jsonschema:"optional skill ID returned by list_skills; omit to list every distributed skill in the project"`
	Plugin      string `json:"plugin,omitempty" jsonschema:"optional exact existing plugin in the project, by ID, slug, or name; a name matching more than one plugin is refused as ambiguous_target"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"pagination cursor returned by a previous list_skill_distributions call"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum distributions to return; defaults to 20 and is capped at 50"`
}

type UndistributeSkillToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns both the skill and the target"`
	SkillID        string `json:"skill_id" jsonschema:"skill ID returned by list_skills or list_skill_distributions"`
	Plugin         string `json:"plugin,omitempty" jsonschema:"exact existing plugin in the project, by ID, slug, or name; exactly one of plugin or assistant is required and there is no implicit default"`
	Assistant      string `json:"assistant,omitempty" jsonschema:"exact existing assistant in the project, by ID or name; exactly one of plugin or assistant is required"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms taking this skill away from this exact plugin or assistant"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated idempotency key, at most 128 characters; reuse only to retry this exact revocation"`
}

type ListSkillInsightsToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug whose skills to rank"`
	Window      string `json:"window,omitempty" jsonschema:"how far back to look: 1h, 24h, 7d, or 30d (default and maximum)"`
	SortBy      string `json:"sort_by,omitempty" jsonschema:"order the skills by estimated_minutes_saved (default), efficacy, activations, or session_cost"`
	Limit       int    `json:"limit,omitempty" jsonschema:"how many skills to return; defaults to 10 and is capped at 20"`
}

type CompareSkillVersionsToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug that owns the skill"`
	SkillID     string `json:"skill_id" jsonschema:"the skill whose versions to compare, by ID as returned by list_skills"`
	Window      string `json:"window,omitempty" jsonschema:"how far back to look: 1h, 24h, 7d, or 30d (default and maximum)"`
}

// approveSkillSuggestionAnnotations and dismissSkillSuggestionAnnotations are
// shared by the live and unavailable registrations, so a client sees the same
// safety and retry semantics before and after skills are switched on.
// Approving adds a version rather than removing anything, and a repeat is
// refused once the suggestion is closed; dismissing discards the suggestion
// and settles on the same state when repeated.
func approveSkillSuggestionAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: false}
}

func dismissSkillSuggestionAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(true), IdempotentHint: true}
}

type skillsRefusalResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// skillInsightsToolMeta is shared by the live insight tools and their stubs, so
// a tool keeps one audience and authority whichever one a deployment serves.
// Session cost is organization spend, so like query_skill_usage these are
// administrator reads even though the skills they name are member-readable.
var skillInsightsToolMeta = ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}

// Ranking a project's skills and comparing one skill's versions are two
// questions with two answers, so they are two tools with fixed shapes rather
// than one tool whose result changes with its arguments.
func registerSkillInsightsTools(reg *Registrar, skills *SkillsService) {
	if !skills.insightsValid() {
		for _, tool := range []struct {
			name        string
			title       string
			description string
		}{
			{"list_skill_insights", "Skill Insights", "See which of a project's skills are paying off. This is not switched on for your organization yet."},
			{"compare_skill_versions", "Compare Skill Versions", "See whether a newer version of a skill is doing better than the ones before it. This is not switched on for your organization yet."},
		} {
			addTool(reg, &mcp.Tool{
				Name:        tool.name,
				Title:       tool.title,
				Description: tool.description,
				Annotations: readOnlyAnnotations(),
			}, skillInsightsToolMeta, unavailableTool("skill_insights"))
		}
		return
	}

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_insights",
		Title:       "Skill Insights",
		Description: "See which of a project's skills are paying off. For each skill: how often it was used, how many sessions used it, what those sessions cost, and, for sessions that were scored, how well it did and how much time it saved. Covers the last 30 days unless you ask for a shorter window, and orders by time saved unless you ask for another order. Two things to know when reading the numbers: a session's whole cost counts against every skill used in that session, so costs do not add up across skills; and scores come from a sample of sessions, so a skill with no scores is unmeasured rather than bad. To see how one skill's versions compare with each other, use compare_skill_versions.",
		Annotations: readOnlyAnnotations(),
	}, skillInsightsToolMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillInsightsToolInput) (*mcp.CallToolResult, ListSkillInsightsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillInsightsOutput, error) {
			return skills.ListSkillInsights(ctx, principal, ListSkillInsightsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "compare_skill_versions",
		Title:       "Compare Skill Versions",
		Description: "See whether a newer version of one skill is doing better than the versions before it. Name the skill and get the same numbers list_skill_insights gives — uses, sessions, cost, score, time saved — broken down per version, newest first, including versions nobody used. Covers the last 30 days unless you ask for a shorter window. A session's whole cost counts against every version used in that session, so costs do not add up across versions, and scores come from a sample of sessions, so a version with no scores is unmeasured rather than bad.",
		Annotations: readOnlyAnnotations(),
	}, skillInsightsToolMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input CompareSkillVersionsToolInput) (*mcp.CallToolResult, CompareSkillVersionsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (CompareSkillVersionsOutput, error) {
			return skills.CompareSkillVersions(ctx, principal, CompareSkillVersionsInput(input))
		})
	})
}

func registerSkillsTools(reg *Registrar, skills *SkillsService) {
	addTool(reg, &mcp.Tool{
		Name:        "list_skills",
		Title:       "List Skills",
		Description: "List the skills in a named project, newest change first. A skill is a written set of instructions an agent loads when it applies. Returns names and how many versions each has; the instructions themselves are read separately with get_skill.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillsToolInput) (*mcp.CallToolResult, ListSkillsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillsOutput, error) {
			return skills.ListSkills(ctx, principal, ListSkillsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_skill",
		Title:       "Get Skill",
		Description: "Read one skill in a named project. Constraints: set include_content to read the instructions themselves; it is off by default because they run to 64 KiB.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSkillToolInput) (*mcp.CallToolResult, GetSkillOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (GetSkillOutput, error) {
			return skills.GetSkill(ctx, principal, GetSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_versions",
		Title:       "List Skill Versions",
		Description: "List a skill's versions, newest first. A version is a fixed snapshot of the instructions: it is never edited in place, so a correction is recorded as a new version with add_skill_version.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillVersionsToolInput) (*mcp.CallToolResult, ListSkillVersionsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillVersionsOutput, error) {
			return skills.ListSkillVersions(ctx, principal, ListSkillVersionsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_feedback",
		Title:       "List Skill Feedback",
		Description: "Review privacy-minimized feedback and outcome trends for one skill in a named project. Returns aggregate counts, a bounded timeline, and feedback rows without user, email, or session identifiers.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillFeedbackToolInput) (*mcp.CallToolResult, ListSkillFeedbackOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillFeedbackOutput, error) {
			return skills.ListSkillFeedback(ctx, principal, ListSkillFeedbackInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_suggestions",
		Title:       "List Skill Suggestions",
		Description: "Review open proposed improvements for skills in a named project. Each suggestion includes the base version, separate reviewable changes, rationale, and whether it still applies cleanly. Complete proposed SKILL.md content is opt-in because each manifest can be 64 KiB. Each change's diff is included by default; set omit_diffs to triage a large queue first, then read one skill's suggestions with skill_id. This tool never applies a suggestion. On the Platform MCP, take one with approve_skill_suggestion or discard it with dismiss_skill_suggestion; where those tools are not offered, the user reviews it in the AI Control Plane dashboard.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillSuggestionsToolInput) (*mcp.CallToolResult, ListSkillSuggestionsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillSuggestionsOutput, error) {
			return skills.ListSkillSuggestions(ctx, principal, ListSkillSuggestionsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_suggestion_feedback",
		Title:       "List Feedback Behind a Skill Suggestion",
		Description: "Review the privacy-minimized feedback cited by one proposed skill change. Name a change returned by list_skill_suggestions. This tool returns evidence only and never applies the change.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillSuggestionFeedbackToolInput) (*mcp.CallToolResult, ListSkillSuggestionFeedbackOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillSuggestionFeedbackOutput, error) {
			return skills.ListSkillSuggestionFeedback(ctx, principal, ListSkillSuggestionFeedbackInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "approve_skill_suggestion",
		Title:       "Approve a Skill Suggestion",
		Description: "Take a proposed improvement from list_skill_suggestions: record it as the skill's new version and close the suggestion, exactly as approving it in the dashboard does. Name the reviewed changes in change_ids (every change to take the whole suggestion, or some to take only those and leave the rest proposed), or pass content to record a corrected SKILL.md instead. Tell the user which skill changes and what each taken change does, ask them to confirm out loud, then call this with confirmed: true. Constraints: a change proposed after your review is never taken, because it is not in change_ids. If the skill has moved on since the suggestion was written, nothing is recorded and the suggestion is closed as superseded. The new version reaches the plugins and assistants that already carry the skill and track its latest version; nobody new receives it.",
		Annotations: approveSkillSuggestionAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input ApproveSkillSuggestionToolInput) (*mcp.CallToolResult, ApproveSkillSuggestionOutput, error) {
		if !input.Confirmed {
			return skillsRefusal("confirmation_required", "Tell the user which skill will change and what each change you are taking does, ask them to explicitly confirm it, then call this tool again with confirmed: true."), ApproveSkillSuggestionOutput{}, nil
		}
		return skillsToolCall(ctx, func(principal Principal) (ApproveSkillSuggestionOutput, error) {
			return skills.ApproveSkillSuggestion(ctx, principal, ApproveSkillSuggestionInput{
				ProjectSlug:  input.ProjectSlug,
				SuggestionID: input.SuggestionID,
				ChangeIDs:    input.ChangeIDs,
				Content:      input.Content,
			})
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "dismiss_skill_suggestion",
		Title:       "Dismiss a Skill Suggestion",
		Description: "Discard a proposed improvement from list_skill_suggestions without changing the skill, exactly as dismissing it in the dashboard does. Tell the user which suggestion is being discarded, ask them to confirm out loud, then call this with confirmed: true. Constraints: a suggestion that is already dismissed stays dismissed, so a retry is safe; an approved or superseded suggestion is refused as a conflict.",
		Annotations: dismissSkillSuggestionAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input DismissSkillSuggestionToolInput) (*mcp.CallToolResult, DismissSkillSuggestionOutput, error) {
		if !input.Confirmed {
			return skillsRefusal("confirmation_required", "Tell the user which suggestion will be discarded, ask them to explicitly confirm it, then call this tool again with confirmed: true."), DismissSkillSuggestionOutput{}, nil
		}
		return skillsToolCall(ctx, func(principal Principal) (DismissSkillSuggestionOutput, error) {
			return skills.DismissSkillSuggestion(ctx, principal, DismissSkillSuggestionInput{
				ProjectSlug:  input.ProjectSlug,
				SuggestionID: input.SuggestionID,
			})
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "create_skill",
		Title:       "Create Skill",
		Description: "Write a new skill in a named project — a set of instructions an agent loads when it applies — from complete SKILL.md content. Writing it alone does nothing: no agent loads it until distribute_skill gives it to a plugin or an assistant. Constraints: an existing active skill with the same normalized name records a new version instead, and identical content returns the existing version unchanged.",
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateSkillToolInput) (*mcp.CallToolResult, SkillAuthoringResult, error) {
		return skillsToolCall(ctx, func(principal Principal) (SkillAuthoringResult, error) {
			return skills.CreateSkill(ctx, principal, CreateSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "add_skill_version",
		Title:       "Add Skill Version",
		Description: "Change what a skill tells an agent, by recording a new version from complete replacement SKILL.md content. Versions are fixed snapshots, so a correction is a new one rather than an edit. Constraints: pass the version you read as expected_latest_version_id — if the skill has moved on since, the write is refused rather than overwriting someone else's version. Identical content returns the existing version unchanged. Recording a version gives it to nobody new; the plugins and assistants that already carry the skill and track its latest version pick it up. To take a change proposed by list_skill_suggestions, use approve_skill_suggestion where it is offered, or have the user approve it in the AI Control Plane dashboard; either closes the suggestion, which a new version recorded here does not.",
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddSkillVersionToolInput) (*mcp.CallToolResult, SkillAuthoringResult, error) {
		return skillsToolCall(ctx, func(principal Principal) (SkillAuthoringResult, error) {
			return skills.AddSkillVersion(ctx, principal, AddSkillVersionInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "update_skill_metadata",
		Title:       "Rename a Skill",
		Description: "Rename a skill, or change how it is described in the list — its canonical name, display name, and summary. Nothing here changes what the skill tells an agent to do; that lives in its versions. Constraints: pass the version you read as expected_latest_version_id.",
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateSkillMetadataToolInput) (*mcp.CallToolResult, UpdateSkillMetadataOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (UpdateSkillMetadataOutput, error) {
			return skills.UpdateSkillMetadata(ctx, principal, UpdateSkillMetadataInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "distribute_skill",
		Title:       "Give a Skill to a Plugin or Assistant",
		Description: "Give a skill to one plugin or one assistant in the same project, so agents there start loading it. This is the only way a skill takes effect. Constraints: name the target exactly — a name matching nothing is refused as not_found and a name matching more than one target as ambiguous_target, with no fallback to the default plugin. Repeat calls settle on the same result rather than adding it twice.",
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeSkillToolInput) (*mcp.CallToolResult, DistributeSkillOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (DistributeSkillOutput, error) {
			return skills.DistributeSkill(ctx, principal, DistributeSkillInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_distributions",
		Title:       "List Where Skills Are Distributed",
		Description: "List which plugins carry which skills in a named project — the active distributions, oldest first — with the version each one resolves to and whether it follows the skill's latest version or is pinned. Narrow it to one skill, one plugin, or both. Constraints: this lists plugin distributions; whether an assistant carries a skill is reported per skill by get_skill. Name a plugin exactly — a name matching nothing is refused as not_found and one matching more than one plugin as ambiguous_target.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoverySkillRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillDistributionsToolInput) (*mcp.CallToolResult, ListSkillDistributionsOutput, error) {
		return skillsToolCall(ctx, func(principal Principal) (ListSkillDistributionsOutput, error) {
			return skills.ListSkillDistributions(ctx, principal, ListSkillDistributionsInput(input))
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "undistribute_skill",
		Title:       "Take a Skill Back from a Plugin or Assistant",
		Description: "Take a skill back from one plugin or one assistant in the same project, undoing distribute_skill, so agents there stop loading it. The skill and its versions stay; only that one distribution ends. Tell the user which plugin or assistant loses the skill, ask them to confirm out loud, then call this with confirmed: true. Constraints: name the target exactly — a name matching nothing is refused as not_found and a name matching more than one target as ambiguous_target, with no fallback to the default plugin. A distribution that is already gone is a no-op, so a retry with the same idempotency_key is safe.",
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input UndistributeSkillToolInput) (*mcp.CallToolResult, UndistributeSkillOutput, error) {
		if !input.Confirmed {
			return skillsRefusal("confirmation_required", "Tell the user which plugin or assistant will stop carrying this skill, ask them to explicitly confirm it, then call this tool again with confirmed: true."), UndistributeSkillOutput{}, nil
		}
		return skillsToolCall(ctx, func(principal Principal) (UndistributeSkillOutput, error) {
			return skills.UndistributeSkill(ctx, principal, UndistributeSkillInput{
				ProjectSlug:    input.ProjectSlug,
				SkillID:        input.SkillID,
				Plugin:         input.Plugin,
				Assistant:      input.Assistant,
				IdempotencyKey: input.IdempotencyKey,
			})
		})
	})

	registerSkillInsightsTools(reg, skills)
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
		authority   ExternalAuthorization
		audiences   []Audience
	}{
		{"list_skills", "List Skills", "List the skills in a project. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"get_skill", "Get Skill", "Read one skill in a project. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"list_skill_versions", "List Skill Versions", "List a skill's versions. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"list_skill_feedback", "List Skill Feedback", "Review privacy-minimized feedback for one skill. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"list_skill_suggestions", "List Skill Suggestions", "Review open proposed skill improvements. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"list_skill_suggestion_feedback", "List Feedback Behind a Skill Suggestion", "Review feedback cited by a proposed skill change. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"approve_skill_suggestion", "Approve a Skill Suggestion", "Take a proposed skill improvement as a new version. This is not switched on for your organization yet.", false, ExternalAuthorizationMember, externalOnly},
		{"dismiss_skill_suggestion", "Dismiss a Skill Suggestion", "Discard a proposed skill improvement. This is not switched on for your organization yet.", false, ExternalAuthorizationMember, externalOnly},
		{"create_skill", "Create Skill", "Write a new skill from complete SKILL.md content. This is not switched on for your organization yet.", false, ExternalAuthorizationMember, bothAudiences},
		{"add_skill_version", "Add Skill Version", "Change what a skill tells an agent, by recording a new version. This is not switched on for your organization yet.", false, ExternalAuthorizationMember, bothAudiences},
		{"update_skill_metadata", "Rename a Skill", "Rename a skill, or change how it is described. This is not switched on for your organization yet.", false, ExternalAuthorizationMember, bothAudiences},
		{"distribute_skill", "Give a Skill to a Plugin or Assistant", "Give a skill to one plugin or assistant. This is not switched on for your organization yet.", false, ExternalAuthorizationOrgAdmin, bothAudiences},
		{"list_skill_distributions", "List Where Skills Are Distributed", "List which plugins carry which skills in a project. This is not switched on for your organization yet.", true, ExternalAuthorizationMember, bothAudiences},
		{"undistribute_skill", "Take a Skill Back from a Plugin or Assistant", "Take a skill back from one plugin or assistant. This is not switched on for your organization yet.", false, ExternalAuthorizationOrgAdmin, bothAudiences},
	} {
		manifest := &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
		}
		switch {
		case tool.readOnly:
			manifest.Annotations = readOnlyAnnotations()
		case tool.name == "approve_skill_suggestion":
			manifest.Annotations = approveSkillSuggestionAnnotations()
		case tool.name == "dismiss_skill_suggestion":
			manifest.Annotations = dismissSkillSuggestionAnnotations()
		}
		var discoveryScopes []authz.Scope
		if tool.authority == ExternalAuthorizationMember {
			discoveryScopes = discoverySkillRead
			if tool.name == "create_skill" || tool.name == "add_skill_version" || tool.name == "update_skill_metadata" || tool.name == "approve_skill_suggestion" || tool.name == "dismiss_skill_suggestion" {
				discoveryScopes = discoverySkillWrite
			}
		}
		addTool(reg, manifest, ToolMeta{Authorization: tool.authority, Audiences: tool.audiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryScopes}, unavailableTool("skills"))
	}
	registerSkillInsightsTools(reg, nil)
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
		result = skillsRefusalResult{Code: "not_found", Message: "No plugin or assistant in this project has that exact name. Name one of the targets returned by create_skill or add_skill_version, or a plugin returned by list_skill_distributions; nothing is picked by default."}
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
	return skillsRefusal(result.Code, result.Message), true
}

// skillsRefusal is the wire shape of every skills refusal: a structured error
// result rather than a transport error, so the code and reason reach the model.
func skillsRefusal(code, message string) *mcp.CallToolResult {
	content, err := json.Marshal(skillsRefusalResult{Code: code, Message: message})
	if err != nil {
		content = []byte(`{"code":"` + code + `"}`)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}
}
