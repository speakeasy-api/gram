package skills

import (
	"context"
	"fmt"
	"io"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type Create struct{ skills SkillsService }

type createInput struct {
	Content string `json:"content" jsonschema:"The complete SKILL.md content, including YAML frontmatter and instructions."`
}

func NewCreateTool(svc SkillsService) *Create { return &Create{skills: svc} }

func (t *Create) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "create_skill",
		Name:        "platform_create_skill",
		Description: "Create a project skill from complete SKILL.md content. If an active skill with the same normalized name exists, this records a new immutable version unless the canonical content already exists, in which case the existing version is returned as a no-op. Returns the saved skill, version, validation status, and whether either was newly created.",
		InputSchema: core.BuildInputSchema[createInput](),
		Variables:   nil,
		Annotations: skillMutationAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *Create) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := createInput{Content: ""}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Content == "" {
		return fmt.Errorf("content is required")
	}

	result, err := t.skills.Create(ctx, &genskills.CreatePayload{
		Content:          input.Content,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("create skill: %w", err)
	}

	return core.EncodeResult(wr, result)
}

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
		Search:           nil,
		SourceKinds:      nil,
		Classifications:  nil,
		Tags:             nil,
		Sort:             "name",
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

type Distribute struct{ skills SkillsService }

type distributeInput struct {
	ID              string  `json:"id" jsonschema:"The skill ID to distribute."`
	PluginID        *string `json:"plugin_id,omitempty" jsonschema:"The plugin that should carry the skill. Exactly one of plugin_id or assistant_id is required."`
	AssistantID     *string `json:"assistant_id,omitempty" jsonschema:"The assistant that should carry the skill. Exactly one of plugin_id or assistant_id is required."`
	PinnedVersionID *string `json:"pinned_version_id,omitempty" jsonschema:"A valid version of the skill to pin. Omit to track the latest valid version."`
}

func NewDistributeTool(svc SkillsService) *Distribute { return &Distribute{skills: svc} }

func (t *Distribute) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "distribute_skill",
		Name:        "platform_distribute_skill",
		Description: "Distribute a project skill to exactly one plugin or assistant so machines that install it receive the skill. Repeating the request for the same target updates the version pin or is a no-op. Omit pinned_version_id to track the latest valid version; the skill must already have a valid version. Use platform_list_plugins to find a plugin ID and platform_list_skill_distributions to see existing distributions.",
		InputSchema: core.BuildInputSchema[distributeInput](
			core.WithPropertyFormat("id", "uuid"),
			core.WithPropertyFormat("plugin_id", "uuid"),
			core.WithPropertyFormat("assistant_id", "uuid"),
			core.WithPropertyFormat("pinned_version_id", "uuid"),
		),
		Variables:   nil,
		Annotations: skillMutationAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *Distribute) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := distributeInput{ID: "", PluginID: nil, AssistantID: nil, PinnedVersionID: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}
	if (input.PluginID == nil) == (input.AssistantID == nil) {
		return fmt.Errorf("exactly one of plugin_id or assistant_id is required")
	}

	result, err := t.skills.Distribute(ctx, &genskills.DistributePayload{
		ID:               input.ID,
		PluginID:         input.PluginID,
		AssistantID:      input.AssistantID,
		PinnedVersionID:  input.PinnedVersionID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("distribute skill: %w", err)
	}

	return core.EncodeResult(wr, result)
}

type Undistribute struct{ skills SkillsService }

type undistributeInput struct {
	ID          string  `json:"id" jsonschema:"The skill ID to revoke."`
	PluginID    *string `json:"plugin_id,omitempty" jsonschema:"The plugin the skill was distributed to. Exactly one of plugin_id or assistant_id is required."`
	AssistantID *string `json:"assistant_id,omitempty" jsonschema:"The assistant the skill was distributed to. Exactly one of plugin_id or assistant_id is required."`
}

// undistributeResult gives the model something to read back, since the
// management API returns no body on success. Field names stay PascalCase to
// match the Goa result types the sibling skill tools return.
type undistributeResult struct {
	SkillID     string
	PluginID    *string
	AssistantID *string
	Revoked     bool
}

func NewUndistributeTool(svc SkillsService) *Undistribute { return &Undistribute{skills: svc} }

func (t *Undistribute) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "undistribute_skill",
		Name:        "platform_undistribute_skill",
		Description: "Revoke a project skill's active distribution to exactly one plugin or assistant, so machines stop receiving it on their next install. Repeated requests are a no-op. The skill and its versions are left untouched.",
		InputSchema: core.BuildInputSchema[undistributeInput](
			core.WithPropertyFormat("id", "uuid"),
			core.WithPropertyFormat("plugin_id", "uuid"),
			core.WithPropertyFormat("assistant_id", "uuid"),
		),
		Variables:   nil,
		Annotations: skillRevocationAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *Undistribute) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil {
		return fmt.Errorf("skills service not configured")
	}

	input := undistributeInput{ID: "", PluginID: nil, AssistantID: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}
	if (input.PluginID == nil) == (input.AssistantID == nil) {
		return fmt.Errorf("exactly one of plugin_id or assistant_id is required")
	}

	if err := t.skills.Undistribute(ctx, &genskills.UndistributePayload{
		ID:               input.ID,
		PluginID:         input.PluginID,
		AssistantID:      input.AssistantID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}); err != nil {
		return fmt.Errorf("undistribute skill: %w", err)
	}

	return core.EncodeResult(wr, undistributeResult{
		SkillID:     input.ID,
		PluginID:    input.PluginID,
		AssistantID: input.AssistantID,
		Revoked:     true,
	})
}

func skillMutationAnnotations() *types.ToolAnnotations {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

// skillRevocationAnnotations marks a tool that takes something away. Revoking
// a distribution is idempotent but destructive: machines lose the skill on
// their next install, and re-distributing needs a separate call.
func skillRevocationAnnotations() *types.ToolAnnotations {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
