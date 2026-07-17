package skills

import (
	"context"
	"fmt"
	"io"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type List struct{ skills SkillsService }

type listInput struct {
	Cursor *string `json:"cursor,omitempty" jsonschema:"Cursor for the next page of skills."`
	Limit  int     `json:"limit,omitempty" jsonschema:"Number of skills to return (1-200)."`
}

func NewListTool(svc SkillsService) *List { return &List{skills: svc} }

func (t *List) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "list_skills",
		Name:        "platform_list_skills",
		Description: "List active skills in the current project, including names, summaries, classifications, latest version IDs, and version counts.",
		InputSchema: core.BuildInputSchema[listInput](core.WithPropertyNumberRange("limit", 1, 200)),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *List) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := listInput{Cursor: nil, Limit: 50}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 200 {
		return fmt.Errorf("limit must be between 1 and 200")
	}

	result, err := t.skills.List(ctx, &genskills.ListPayload{
		Cursor:           input.Cursor,
		Limit:            input.Limit,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list skills: %w", err)
	}

	return core.EncodeResult(wr, result)
}

type Get struct{ skills SkillsService }

type getInput struct {
	ID string `json:"id" jsonschema:"The skill ID."`
}

func NewGetTool(svc SkillsService) *Get { return &Get{skills: svc} }

func (t *Get) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "get_skill",
		Name:        "platform_get_skill",
		Description: "Get an active skill and its latest immutable version, including the exact SKILL.md content, metadata, and validation status.",
		InputSchema: core.BuildInputSchema[getInput](core.WithPropertyFormat("id", "uuid")),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *Get) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := getInput{ID: ""}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}

	result, err := t.skills.Get(ctx, &genskills.GetPayload{
		ID:               input.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("get skill: %w", err)
	}

	return core.EncodeResult(wr, result)
}

type ListVersions struct{ skills SkillsService }

type listVersionsInput struct {
	ID     string  `json:"id" jsonschema:"The skill ID."`
	Cursor *string `json:"cursor,omitempty" jsonschema:"Cursor for the next page of versions."`
	Limit  int     `json:"limit,omitempty" jsonschema:"Number of versions to return (1-50)."`
}

func NewListVersionsTool(svc SkillsService) *ListVersions { return &ListVersions{skills: svc} }

func (t *ListVersions) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "list_skill_versions",
		Name:        "platform_list_skill_versions",
		Description: "List immutable versions of an active skill, newest first, including exact SKILL.md content and validation status.",
		InputSchema: core.BuildInputSchema[listVersionsInput](
			core.WithPropertyFormat("id", "uuid"),
			core.WithPropertyNumberRange("limit", 1, 50),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListVersions) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := listVersionsInput{ID: "", Cursor: nil, Limit: 20}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Limit < 1 || input.Limit > 50 {
		return fmt.Errorf("limit must be between 1 and 50")
	}

	result, err := t.skills.ListVersions(ctx, &genskills.ListVersionsPayload{
		ID:               input.ID,
		Cursor:           input.Cursor,
		Limit:            input.Limit,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list skill versions: %w", err)
	}

	return core.EncodeResult(wr, result)
}

type ListDistributions struct{ skills SkillsService }

type listDistributionsInput struct {
	SkillID  *string `json:"skill_id,omitempty" jsonschema:"Only return distributions of this skill ID."`
	PluginID *string `json:"plugin_id,omitempty" jsonschema:"Only return distributions carried by this plugin ID."`
	Cursor   *string `json:"cursor,omitempty" jsonschema:"Cursor for the next page of distributions."`
	Limit    int     `json:"limit,omitempty" jsonschema:"Number of distributions to return (1-50)."`
}

func NewListDistributionsTool(svc SkillsService) *ListDistributions {
	return &ListDistributions{skills: svc}
}

func (t *ListDistributions) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "list_skill_distributions",
		Name:        "platform_list_skill_distributions",
		Description: "List active distributions of project skills to plugins, optionally filtered by skill or plugin.",
		InputSchema: core.BuildInputSchema[listDistributionsInput](
			core.WithPropertyFormat("skill_id", "uuid"),
			core.WithPropertyFormat("plugin_id", "uuid"),
			core.WithPropertyNumberRange("limit", 1, 50),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListDistributions) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := listDistributionsInput{SkillID: nil, PluginID: nil, Cursor: nil, Limit: 20}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Limit < 1 || input.Limit > 50 {
		return fmt.Errorf("limit must be between 1 and 50")
	}

	result, err := t.skills.ListDistributions(ctx, &genskills.ListDistributionsPayload{
		SkillID:          input.SkillID,
		PluginID:         input.PluginID,
		Cursor:           input.Cursor,
		Limit:            input.Limit,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list skill distributions: %w", err)
	}

	return core.EncodeResult(wr, result)
}
