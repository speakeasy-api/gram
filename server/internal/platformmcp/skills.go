//nolint:exhaustruct,wrapcheck // Zero-value summaries are the documented "absent" signal, and a skills-service refusal is returned unwrapped so its typed code survives the trip to the tool result.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// maxSkillContentBytes mirrors the skills service's own manifest ceiling. It is
// checked here as well so an oversized manifest is refused before it is copied
// into a payload and shipped across the process, rather than after.
const maxSkillContentBytes = 64 << 10

// maxSkillTargetCandidates bounds the distribution targets named back to the
// caller. Naming every plugin and assistant in a large project would spend the
// caller's context on a list it did not ask for.
const maxSkillTargetCandidates = 20

// maxSkillTargetLookup bounds the candidates a named target is resolved
// against. It is deliberately far larger than the advice list: a target the
// project really has must not be refused as not_found because a display cap
// cut it off.
const maxSkillTargetLookup = 500

var (
	// ErrSkillsUnavailable is the kill switch and the missing-dependency path:
	// the capability is off for this organization, or this deployment composed
	// no skills service.
	ErrSkillsUnavailable = errors.New("platform mcp skills unavailable")
	// ErrSkillTargetNotFound is an exact target that does not exist in the
	// named project. It is never softened into the default plugin: distributing
	// a skill somewhere the caller did not name is the one outcome authoring
	// safety depends on not happening.
	ErrSkillTargetNotFound = errors.New("platform mcp skill distribution target not found")
	// ErrSkillTargetAmbiguous is a target name matching more than one plugin or
	// assistant. Picking one would be a coin flip the caller cannot see.
	ErrSkillTargetAmbiguous = errors.New("platform mcp skill distribution target ambiguous")
	// ErrSkillContentTooLarge is a manifest over the reviewed ceiling.
	ErrSkillContentTooLarge = errors.New("platform mcp skill content too large")
)

// SkillsManagement is the subset of the skills management service this surface
// calls. Going through the service rather than the repository is what keeps one
// definition of manifest validation, version immutability, canonical-content
// idempotency, and typed audit for every surface that authors a skill.
type SkillsManagement interface {
	Create(context.Context, *genskills.CreatePayload) (*genskills.RecordSkillResult, error)
	AddVersion(context.Context, *genskills.AddVersionPayload) (*genskills.RecordSkillResult, error)
	Update(context.Context, *genskills.UpdatePayload) (*types.Skill, error)
	List(context.Context, *genskills.ListPayload) (*genskills.ListSkillsResult, error)
	Get(context.Context, *genskills.GetPayload) (*genskills.GetSkillResult, error)
	ListDistributions(context.Context, *genskills.ListDistributionsPayload) (*genskills.ListSkillDistributionsResult, error)
	ListVersions(context.Context, *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error)
	Distribute(context.Context, *genskills.DistributePayload) (*types.SkillDistribution, error)
}

// SkillTargetInventory names the plugins and assistants a skill may be
// distributed to in one project. Lane D6 owns the full plugin catalogue
// surface; this is the resolution half it promises to supply to distribution,
// expressed as the narrow read it actually needs.
//
// The limit is per kind, not per result. A combined cap would let a project
// with many plugins push every assistant out of the answer, and a target that
// is missing from the answer is a target distribution refuses as not_found.
type SkillTargetInventory interface {
	SkillTargets(ctx context.Context, organizationID string, projectID uuid.UUID, limitPerKind int) ([]SkillTarget, error)
}

// GrantPreparer loads the acting user's RBAC grants onto the context.
//
// Platform MCP does not travel the session middleware that prepares grants for
// dashboard requests, so it prepares them itself. Without this the skills
// service's scope checks would find no grants and refuse every call.
type GrantPreparer interface {
	PrepareContext(ctx context.Context) (context.Context, error)
}

// SkillProjectResolver turns the project slug a caller names into the project
// every downstream call is scoped by.
type SkillProjectResolver interface {
	ResolveProject(ctx context.Context, organizationID, projectSlug string) (ResolvedProject, error)
}

// SkillTargetKind names what a distribution attaches to.
type SkillTargetKind string

const (
	SkillTargetPlugin    SkillTargetKind = "plugin"
	SkillTargetAssistant SkillTargetKind = "assistant"
)

// SkillTarget is one resolved or offered distribution target. It is echoed back
// on every distribution so the caller can see which plugin or assistant its
// name resolved to rather than inferring it.
type SkillTarget struct {
	Kind      SkillTargetKind `json:"kind"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Slug      string          `json:"slug,omitempty"`
	IsDefault bool            `json:"is_default,omitempty"`
}

// SkillSummary is the registry projection of one skill. Content is deliberately
// absent: listing a project's skills should not spend the caller's context on
// manifests it has not asked to read.
type SkillSummary struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	DisplayName     string   `json:"display_name"`
	Summary         string   `json:"summary,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	LatestVersionID string   `json:"latest_version_id,omitempty"`
	VersionCount    int64    `json:"version_count"`
	HasValidVersion bool     `json:"has_valid_version"`
	UpdatedAt       string   `json:"updated_at"`
}

// SkillVersionSummary is one immutable version. Content appears only when the
// caller opted in.
type SkillVersionSummary struct {
	ID               string   `json:"id"`
	CanonicalSHA256  string   `json:"canonical_sha256"`
	SpecValid        bool     `json:"spec_valid"`
	ValidationErrors []string `json:"validation_errors,omitempty"`
	CreatedAt        string   `json:"created_at"`
	Content          string   `json:"content,omitempty"`
	ContentTruncated bool     `json:"content_truncated,omitempty"`
}

// SkillAuthoringResult is what create and version writes return.
//
// Distributed and DistributionTargets are on it for one reason: authoring a
// skill changes no runtime behavior at all, and a result that only said
// "created" would read as activation to the exact caller most likely to stop
// there. The result states the skill is inert and names where it can be sent.
type SkillAuthoringResult struct {
	ProjectSlug         string              `json:"project_slug"`
	Skill               SkillSummary        `json:"skill"`
	Version             SkillVersionSummary `json:"version"`
	CreatedSkill        bool                `json:"created_skill"`
	CreatedVersion      bool                `json:"created_version"`
	Distributed         bool                `json:"distributed"`
	InertMessage        string              `json:"inert_message"`
	NextAction          string              `json:"next_action"`
	DistributionTargets []SkillTarget       `json:"distribution_targets,omitempty"`
}

// SkillsService is the handler-facing boundary for skill authoring and
// distribution over Platform MCP.
type SkillsService struct {
	skills   SkillsManagement
	targets  SkillTargetInventory
	projects SkillProjectResolver
	grants   GrantPreparer
	gate     CatalogRegistrationGateChecker
	budget   OperationBudget
}

func NewSkillsService(skills SkillsManagement, targets SkillTargetInventory, projects SkillProjectResolver, grants GrantPreparer, gate CatalogRegistrationGateChecker, budget OperationBudget) *SkillsService {
	return &SkillsService{
		skills:   skills,
		targets:  targets,
		projects: projects,
		grants:   grants,
		gate:     gate,
		budget:   budget,
	}
}

func (s *SkillsService) valid() bool {
	return s != nil && s.skills != nil && s.targets != nil && s.projects != nil && s.grants != nil && s.gate != nil && s.budget.valid()
}

// begin runs everything every skill call owes before it touches a skill: the
// organization kill switch, the metered allowance, the project the caller
// named, and the authorization context downstream RBAC is evaluated against.
//
// The returned context carries the acting user rather than a service identity,
// so a caller reaches exactly the projects its own grants reach and the audit
// row names the person, not the surface.
func (s *SkillsService) begin(ctx context.Context, principal Principal, projectSlug string) (context.Context, ResolvedProject, error) {
	if !s.valid() {
		return ctx, ResolvedProject{}, ErrSkillsUnavailable
	}
	if strings.TrimSpace(projectSlug) == "" {
		return ctx, ResolvedProject{}, ErrRegistrationInvalid
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return ctx, ResolvedProject{}, fmt.Errorf("check platform mcp skills gate: %w", err)
	}
	if !enabled {
		return ctx, ResolvedProject{}, ErrSkillsUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ctx, ResolvedProject{}, err
	}
	project, err := s.projects.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return ctx, ResolvedProject{}, err
	}
	projectID := project.ID
	// A session on the incoming context is carried through rather than dropped.
	// RBAC enforcement keys on it, so replacing a session-backed context with a
	// session-less one would silently turn every downstream scope check into a
	// no-op — the same hole the acting-surface carve-out closes for the
	// connection-only path.
	var sessionID *string
	if existing, ok := contextvalues.GetAuthContext(ctx); ok && existing != nil {
		sessionID = existing.SessionID
	}
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  principal.OrganizationID,
		UserID:                principal.UserID,
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             sessionID,
		ProjectID:             &projectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           &project.Slug,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})
	// Grants are loaded for the acting user, so a connection reaches exactly the
	// projects that user's own role reaches — the OAuth org-admin check at the
	// endpoint says who may connect, not what they may write.
	ctx, err = s.grants.PrepareContext(ctx)
	if err != nil {
		return ctx, ResolvedProject{}, err
	}
	return ctx, project, nil
}

// ListSkillsInput names the project whose registry to read.
type ListSkillsInput struct {
	ProjectSlug string
	Search      string
	Cursor      string
	Limit       int
}

type ListSkillsOutput struct {
	ProjectSlug string         `json:"project_slug"`
	Skills      []SkillSummary `json:"skills"`
	TotalCount  int64          `json:"total_count"`
	NextCursor  string         `json:"next_cursor,omitempty"`
}

func (s *SkillsService) ListSkills(ctx context.Context, principal Principal, input ListSkillsInput) (ListSkillsOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return ListSkillsOutput{}, err
	}
	result, err := s.skills.List(ctx, &genskills.ListPayload{
		Cursor:           optionalString(strings.TrimSpace(input.Cursor)),
		Limit:            boundedLimit(input.Limit),
		Search:           optionalString(strings.TrimSpace(input.Search)),
		SourceKinds:      nil,
		Classifications:  nil,
		Tags:             nil,
		Sort:             "updated",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return ListSkillsOutput{}, err
	}
	summaries := make([]SkillSummary, 0, len(result.Skills))
	for _, skill := range result.Skills {
		summaries = append(summaries, buildSkillSummary(skill))
	}
	return ListSkillsOutput{
		ProjectSlug: project.Slug,
		Skills:      summaries,
		TotalCount:  result.TotalCount,
		NextCursor:  stringOrEmpty(result.NextCursor),
	}, nil
}

// GetSkillInput reads one skill. Manifest content is opt-in rather than default
// so a caller that wanted a name and a version id does not pay 64 KiB for it.
type GetSkillInput struct {
	ProjectSlug    string
	SkillID        string
	IncludeContent bool
}

type GetSkillOutput struct {
	ProjectSlug   string               `json:"project_slug"`
	Skill         SkillSummary         `json:"skill"`
	LatestVersion *SkillVersionSummary `json:"latest_version,omitempty"`
	Distributed   bool                 `json:"distributed"`
}

func (s *SkillsService) GetSkill(ctx context.Context, principal Principal, input GetSkillInput) (GetSkillOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return GetSkillOutput{}, err
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return GetSkillOutput{}, ErrRegistrationInvalid
	}
	result, err := s.skills.Get(ctx, &genskills.GetPayload{
		ID:               input.SkillID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return GetSkillOutput{}, err
	}
	// Assistant attachments are counted on the skill; plugin attachments are
	// not, and a read that called a plugin-distributed skill undistributed
	// would contradict the distribute_skill call that activated it.
	distributed := result.AssistantCount > 0
	if !distributed {
		plugins, err := s.skills.ListDistributions(ctx, &genskills.ListDistributionsPayload{
			SkillID:          &input.SkillID,
			PluginID:         nil,
			Cursor:           nil,
			Limit:            1,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		if err != nil {
			return GetSkillOutput{}, err
		}
		distributed = len(plugins.Distributions) > 0
	}
	output := GetSkillOutput{
		ProjectSlug:   project.Slug,
		Skill:         buildSkillSummary(result.Skill),
		LatestVersion: nil,
		Distributed:   distributed,
	}
	if result.LatestVersion != nil {
		version := buildSkillVersionSummary(result.LatestVersion, input.IncludeContent)
		output.LatestVersion = &version
	}
	return output, nil
}

type ListSkillVersionsInput struct {
	ProjectSlug    string
	SkillID        string
	IncludeContent bool
	Cursor         string
	Limit          int
}

type ListSkillVersionsOutput struct {
	ProjectSlug string                `json:"project_slug"`
	SkillID     string                `json:"skill_id"`
	Versions    []SkillVersionSummary `json:"versions"`
	NextCursor  string                `json:"next_cursor,omitempty"`
}

func (s *SkillsService) ListSkillVersions(ctx context.Context, principal Principal, input ListSkillVersionsInput) (ListSkillVersionsOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return ListSkillVersionsOutput{}, err
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return ListSkillVersionsOutput{}, ErrRegistrationInvalid
	}
	result, err := s.skills.ListVersions(ctx, &genskills.ListVersionsPayload{
		ID:               input.SkillID,
		Cursor:           optionalString(strings.TrimSpace(input.Cursor)),
		Limit:            boundedLimit(input.Limit),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return ListSkillVersionsOutput{}, err
	}
	versions := make([]SkillVersionSummary, 0, len(result.Versions))
	for _, version := range result.Versions {
		versions = append(versions, buildSkillVersionSummary(version, input.IncludeContent))
	}
	return ListSkillVersionsOutput{
		ProjectSlug: project.Slug,
		SkillID:     input.SkillID,
		Versions:    versions,
		NextCursor:  stringOrEmpty(result.NextCursor),
	}, nil
}

type CreateSkillInput struct {
	ProjectSlug string
	Content     string
}

func (s *SkillsService) CreateSkill(ctx context.Context, principal Principal, input CreateSkillInput) (SkillAuthoringResult, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return SkillAuthoringResult{}, err
	}
	if err := checkSkillContent(input.Content); err != nil {
		return SkillAuthoringResult{}, err
	}
	result, err := s.skills.Create(ctx, &genskills.CreatePayload{
		Content:          input.Content,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return SkillAuthoringResult{}, err
	}
	return s.authoringResult(ctx, principal, project, result), nil
}

// AddSkillVersionInput carries the full replacement manifest plus the version
// the caller believes is current. Versions are immutable, so a correction is
// another version rather than an edit of one.
type AddSkillVersionInput struct {
	ProjectSlug             string
	SkillID                 string
	Content                 string
	ExpectedLatestVersionID string
}

func (s *SkillsService) AddSkillVersion(ctx context.Context, principal Principal, input AddSkillVersionInput) (SkillAuthoringResult, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return SkillAuthoringResult{}, err
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return SkillAuthoringResult{}, ErrRegistrationInvalid
	}
	if err := checkSkillContent(input.Content); err != nil {
		return SkillAuthoringResult{}, err
	}
	result, err := s.skills.AddVersion(ctx, &genskills.AddVersionPayload{
		ID:                      input.SkillID,
		Content:                 input.Content,
		DerivedFromVersionID:    optionalString(strings.TrimSpace(input.ExpectedLatestVersionID)),
		ExpectedLatestVersionID: optionalString(strings.TrimSpace(input.ExpectedLatestVersionID)),
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	if err != nil {
		return SkillAuthoringResult{}, err
	}
	return s.authoringResult(ctx, principal, project, result), nil
}

// UpdateSkillMetadataInput changes registry naming only. Instructions live in
// versions, so nothing here can alter what a skill tells an agent to do.
type UpdateSkillMetadataInput struct {
	ProjectSlug             string
	SkillID                 string
	Name                    string
	DisplayName             string
	Summary                 string
	ClearSummary            bool
	ExpectedLatestVersionID string
}

type UpdateSkillMetadataOutput struct {
	ProjectSlug string       `json:"project_slug"`
	Skill       SkillSummary `json:"skill"`
}

func (s *SkillsService) UpdateSkillMetadata(ctx context.Context, principal Principal, input UpdateSkillMetadataInput) (UpdateSkillMetadataOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return UpdateSkillMetadataOutput{}, err
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return UpdateSkillMetadataOutput{}, ErrRegistrationInvalid
	}
	// The management update replaces the whole metadata record, so unspecified
	// fields are read back and re-sent rather than blanked. Tags are carried
	// through untouched: this surface does not author them.
	current, err := s.skills.Get(ctx, &genskills.GetPayload{
		ID:               input.SkillID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return UpdateSkillMetadataOutput{}, err
	}
	name := current.Skill.Name
	if strings.TrimSpace(input.Name) != "" {
		name = input.Name
	}
	displayName := current.Skill.DisplayName
	if strings.TrimSpace(input.DisplayName) != "" {
		displayName = input.DisplayName
	}
	summary := current.Skill.Summary
	switch {
	case input.ClearSummary:
		summary = nil
	case strings.TrimSpace(input.Summary) != "":
		summary = &input.Summary
	}
	updated, err := s.skills.Update(ctx, &genskills.UpdatePayload{
		ID:                      input.SkillID,
		Name:                    name,
		DisplayName:             displayName,
		Summary:                 summary,
		Tags:                    current.Skill.Tags,
		ExpectedLatestVersionID: optionalString(strings.TrimSpace(input.ExpectedLatestVersionID)),
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	if err != nil {
		return UpdateSkillMetadataOutput{}, err
	}
	return UpdateSkillMetadataOutput{ProjectSlug: project.Slug, Skill: buildSkillSummary(updated)}, nil
}

// DistributeSkillInput names exactly one target. Plugin and assistant names are
// resolved to exactly one existing target in the named project or refused.
type DistributeSkillInput struct {
	ProjectSlug string
	SkillID     string
	Plugin      string
	Assistant   string
}

type DistributeSkillOutput struct {
	ProjectSlug       string      `json:"project_slug"`
	SkillID           string      `json:"skill_id"`
	SkillName         string      `json:"skill_name"`
	Target            SkillTarget `json:"target"`
	DistributionID    string      `json:"distribution_id"`
	ResolvedVersionID string      `json:"resolved_version_id"`
	Message           string      `json:"message"`
}

func (s *SkillsService) DistributeSkill(ctx context.Context, principal Principal, input DistributeSkillInput) (DistributeSkillOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return DistributeSkillOutput{}, err
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return DistributeSkillOutput{}, ErrRegistrationInvalid
	}
	if (strings.TrimSpace(input.Plugin) == "") == (strings.TrimSpace(input.Assistant) == "") {
		return DistributeSkillOutput{}, ErrRegistrationInvalid
	}

	target, err := s.resolveTarget(ctx, principal, project, input.Plugin, input.Assistant)
	if err != nil {
		return DistributeSkillOutput{}, err
	}
	payload := &genskills.DistributePayload{
		ID:               input.SkillID,
		PluginID:         nil,
		AssistantID:      nil,
		PinnedVersionID:  nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
	if target.Kind == SkillTargetPlugin {
		payload.PluginID = &target.ID
	} else {
		payload.AssistantID = &target.ID
	}
	// Distribution is idempotent on project, target, and skill downstream, so a
	// repeat call converges on the same attachment rather than adding a second.
	distribution, err := s.skills.Distribute(ctx, payload)
	if err != nil {
		return DistributeSkillOutput{}, err
	}
	return DistributeSkillOutput{
		ProjectSlug:       project.Slug,
		SkillID:           distribution.SkillID,
		SkillName:         distribution.SkillName,
		Target:            target,
		DistributionID:    distribution.ID,
		ResolvedVersionID: distribution.ResolvedVersionID,
		Message:           fmt.Sprintf("The skill is now carried by the %s %q and takes effect for agents using it.", target.Kind, target.Name),
	}, nil
}

// resolveTarget matches one plugin or assistant exactly. A name that matches
// nothing is not_found and a name that matches more than one is ambiguous;
// neither falls back to the default plugin.
func (s *SkillsService) resolveTarget(ctx context.Context, principal Principal, project ResolvedProject, plugin, assistant string) (SkillTarget, error) {
	// Resolution reads a far wider window than the advice list shows. Trimming
	// the candidates a name is matched against would turn a target that exists
	// into not_found, which is exactly the wrong direction for a refusal that
	// is supposed to mean "you named something that is not there".
	candidates, err := s.targets.SkillTargets(ctx, principal.OrganizationID, project.ID, maxSkillTargetLookup)
	if err != nil {
		return SkillTarget{}, err
	}
	wanted := strings.TrimSpace(plugin)
	kind := SkillTargetPlugin
	if wanted == "" {
		wanted = strings.TrimSpace(assistant)
		kind = SkillTargetAssistant
	}

	matches := make([]SkillTarget, 0, 2)
	for _, candidate := range candidates {
		if candidate.Kind == kind && matchesSkillTarget(candidate, wanted) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return SkillTarget{}, ErrSkillTargetNotFound
	case 1:
		return matches[0], nil
	default:
		return SkillTarget{}, ErrSkillTargetAmbiguous
	}
}

// matchesSkillTarget accepts the id, the slug, or the exact name. Names are
// compared case-insensitively because a caller reading a name off a dashboard
// should not have to reproduce its capitalization, but never by prefix or
// substring: a partial match is what turns "the marketing plugin" into someone
// else's plugin.
func matchesSkillTarget(candidate SkillTarget, wanted string) bool {
	return candidate.ID == wanted ||
		(candidate.Slug != "" && strings.EqualFold(candidate.Slug, wanted)) ||
		strings.EqualFold(candidate.Name, wanted)
}

// authoringResult states what an authoring write did and, deliberately, what it
// did not do. A failure to list targets degrades the advice rather than the
// write: the skill is saved either way, and refusing here would report a
// successful, already-committed write as an error.
func (s *SkillsService) authoringResult(ctx context.Context, principal Principal, project ResolvedProject, result *genskills.RecordSkillResult) SkillAuthoringResult {
	targets, err := s.targets.SkillTargets(ctx, principal.OrganizationID, project.ID, maxSkillTargetCandidates)
	if err != nil {
		targets = nil
	}
	targets = adviceTargets(targets, maxSkillTargetCandidates)
	authored := SkillAuthoringResult{
		ProjectSlug:         project.Slug,
		Skill:               buildSkillSummary(result.Skill),
		Version:             SkillVersionSummary{},
		CreatedSkill:        result.CreatedSkill,
		CreatedVersion:      result.CreatedVersion,
		Distributed:         false,
		InertMessage:        "This skill is saved but inert: nothing loads it until it is distributed to a plugin or an assistant with distribute_skill.",
		NextAction:          "distribute_skill",
		DistributionTargets: targets,
	}
	if result.Version != nil {
		authored.Version = buildSkillVersionSummary(result.Version, false)
	}
	return authored
}

func buildSkillSummary(skill *types.Skill) SkillSummary {
	if skill == nil {
		return SkillSummary{}
	}
	return SkillSummary{
		ID:              skill.ID,
		Name:            skill.Name,
		DisplayName:     skill.DisplayName,
		Summary:         stringOrEmpty(skill.Summary),
		Tags:            skill.Tags,
		LatestVersionID: stringOrEmpty(skill.LatestVersionID),
		VersionCount:    skill.VersionCount,
		HasValidVersion: skill.HasValidVersion,
		UpdatedAt:       skill.UpdatedAt,
	}
}

func buildSkillVersionSummary(version *types.SkillVersion, includeContent bool) SkillVersionSummary {
	if version == nil {
		return SkillVersionSummary{}
	}
	summary := SkillVersionSummary{
		ID:               version.ID,
		CanonicalSHA256:  version.CanonicalSha256,
		SpecValid:        version.SpecValid,
		ValidationErrors: nil,
		CreatedAt:        version.CreatedAt,
		Content:          "",
		ContentTruncated: false,
	}
	for _, validationError := range version.ValidationErrors {
		if validationError == nil {
			continue
		}
		summary.ValidationErrors = append(summary.ValidationErrors, validationError.Message)
	}
	if includeContent {
		content := version.Content
		if len(content) > maxSkillContentBytes {
			// Cut on a rune boundary. Slicing bytes can land mid-rune and hand
			// the caller a manifest whose last character is mojibake.
			content = strings.ToValidUTF8(content[:maxSkillContentBytes], "")
			summary.ContentTruncated = true
		}
		summary.Content = content
	}
	return summary
}

func checkSkillContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return ErrRegistrationInvalid
	}
	if len(content) > maxSkillContentBytes {
		return ErrSkillContentTooLarge
	}
	return nil
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// skillsRefusalCode maps a skills-service refusal onto the structured codes
// this surface returns. The service's own public message is carried through:
// it already says which manifest rule failed, and re-writing it here would
// leave the caller guessing at what to fix.
func skillsRefusalCode(err error) (string, string, bool) {
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) {
		return "", "", false
	}
	switch shareable.Code {
	case oops.CodeBadRequest, oops.CodeInvalid, oops.CodeUnsupportedMedia, oops.CodeRequestTooLarge:
		return "invalid_request", shareable.Error(), true
	case oops.CodeNotFound:
		return "not_found", shareable.Error(), true
	case oops.CodeConflict:
		return "conflict", shareable.Error(), true
	case oops.CodeUnauthorized, oops.CodeForbidden:
		return "forbidden", "This connection is not authorized to author or distribute skills in this project.", true
	case oops.CodeRateLimitExceeded:
		return "rate_limited", shareable.Error(), true
	case oops.CodeFailedPrecondition, oops.CodeInvariantViolation:
		return "conflict", shareable.Error(), true
	case oops.CodeUnavailable, oops.CodeGatewayError, oops.CodeNotImplemented:
		return unavailableCode, "The skills service is temporarily unavailable. Retry after a short delay.", true
	case oops.CodeUnexpected, oops.CodeMethodNotAllowed, oops.CodeInsufficientCredits, oops.CodeInferenceDisabled, oops.CodeCanceled:
		return "", "", false
	default:
		return "", "", false
	}
}

// PostgresSkillTargets reads the distribution targets a project actually has.
//
// It reads the tables directly rather than calling the plugins and assistants
// management services: resolution needs two names and two ids, while those
// services return whole plugin and assistant records — and the plugins service
// provisions a missing default plugin as a side effect of listing, which is not
// something naming a target should do.
type PostgresSkillTargets struct {
	db *pgxpool.Pool
}

func NewPostgresSkillTargets(db *pgxpool.Pool) *PostgresSkillTargets {
	return &PostgresSkillTargets{db: db}
}

func (s *PostgresSkillTargets) SkillTargets(ctx context.Context, organizationID string, projectID uuid.UUID, limitPerKind int) ([]SkillTarget, error) {
	if s == nil || s.db == nil || organizationID == "" || projectID == uuid.Nil {
		return nil, ErrSkillsUnavailable
	}
	limit := limitPerKind
	// Clamped to the lookup ceiling before the narrowing conversion, so a caller
	// cannot ask for a page size that does not fit the query parameter.
	bounded := int32(min(max(limit, 1), maxSkillTargetLookup)) //nolint:gosec // bounded above by maxSkillTargetLookup
	queries := platformrepo.New(s.db)
	plugins, err := queries.ListPlatformMCPProjectPlugins(ctx, platformrepo.ListPlatformMCPProjectPluginsParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		ResultLimit:    bounded,
	})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp skill plugin targets: %w", err)
	}
	assistants, err := queries.ListPlatformMCPProjectAssistants(ctx, platformrepo.ListPlatformMCPProjectAssistantsParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		ResultLimit:    bounded,
	})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp skill assistant targets: %w", err)
	}
	targets := make([]SkillTarget, 0, len(plugins)+len(assistants))
	for _, plugin := range plugins {
		targets = append(targets, SkillTarget{
			Kind:      SkillTargetPlugin,
			ID:        plugin.ID.String(),
			Name:      plugin.Name,
			Slug:      plugin.Slug,
			IsDefault: plugin.IsDefault,
		})
	}
	for _, assistant := range assistants {
		targets = append(targets, SkillTarget{
			Kind:      SkillTargetAssistant,
			ID:        assistant.ID.String(),
			Name:      assistant.Name,
			Slug:      "",
			IsDefault: false,
		})
	}
	return targets, nil
}

// adviceTargets trims the targets named back to an authoring caller while
// keeping both kinds represented. Trimming the concatenation instead would let
// a project's plugins hide every assistant from the advice, which reads as
// "there is nowhere to send this" for the target the caller most likely wants.
func adviceTargets(targets []SkillTarget, limit int) []SkillTarget {
	if limit <= 0 || len(targets) <= limit {
		return targets
	}
	perKind := max(limit/2, 1)
	kept := make([]SkillTarget, 0, limit)
	counts := map[SkillTargetKind]int{}
	for _, target := range targets {
		if counts[target.Kind] < perKind {
			kept = append(kept, target)
			counts[target.Kind]++
		}
	}
	// A project holding only one kind still fills the whole allowance.
	for _, target := range targets {
		if len(kept) >= limit {
			break
		}
		if counts[target.Kind] >= perKind && !slices.Contains(kept, target) {
			kept = append(kept, target)
		}
	}
	return kept
}
