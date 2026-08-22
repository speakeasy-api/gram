package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	testSkillID          = "6f2f4c0e-2f6a-4a0e-9c1a-1a2b3c4d5e6f"
	testSkillVersionID   = "5a1b2c3d-4e5f-4a6b-8c9d-0e1f2a3b4c5d"
	testDefaultPluginID  = "11111111-1111-4111-8111-111111111111"
	testMarketingPlugin  = "22222222-2222-4222-8222-222222222222"
	testAssistantID      = "33333333-3333-4333-8333-333333333333"
	testSkillProjectSlug = "acme"
)

type recordingSkillsManagement struct {
	created             *genskills.CreatePayload
	addedVersion        *genskills.AddVersionPayload
	updated             *genskills.UpdatePayload
	distributed         *genskills.DistributePayload
	skill               *types.Skill
	latestVersion       *types.SkillVersion
	recordResult        *genskills.RecordSkillResult
	distribution        *types.SkillDistribution
	err                 error
	assistantCount      int64
	listVersionsOut     *genskills.ListSkillVersionsResult
	pluginDistributions []*types.PluginSkillDistribution
}

func (s *recordingSkillsManagement) Create(_ context.Context, payload *genskills.CreatePayload) (*genskills.RecordSkillResult, error) {
	s.created = payload
	if s.err != nil {
		return nil, s.err
	}
	return s.recordResult, nil
}

func (s *recordingSkillsManagement) AddVersion(_ context.Context, payload *genskills.AddVersionPayload) (*genskills.RecordSkillResult, error) {
	s.addedVersion = payload
	if s.err != nil {
		return nil, s.err
	}
	return s.recordResult, nil
}

func (s *recordingSkillsManagement) Update(_ context.Context, payload *genskills.UpdatePayload) (*types.Skill, error) {
	s.updated = payload
	if s.err != nil {
		return nil, s.err
	}
	return s.skill, nil
}

func (s *recordingSkillsManagement) List(_ context.Context, _ *genskills.ListPayload) (*genskills.ListSkillsResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &genskills.ListSkillsResult{Skills: []*types.Skill{s.skill}, TotalCount: 1, NextCursor: nil}, nil
}

func (s *recordingSkillsManagement) Get(_ context.Context, _ *genskills.GetPayload) (*genskills.GetSkillResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &genskills.GetSkillResult{
		Skill:                   s.skill,
		LatestVersion:           s.latestVersion,
		Adoption:                nil,
		SightingTimeline:        nil,
		Drift:                   nil,
		AssistantCount:          s.assistantCount,
		PromptInjectionFindings: nil,
	}, nil
}

func (s *recordingSkillsManagement) ListVersions(_ context.Context, _ *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.listVersionsOut, nil
}

func (s *recordingSkillsManagement) ListDistributions(_ context.Context, _ *genskills.ListDistributionsPayload) (*genskills.ListSkillDistributionsResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &genskills.ListSkillDistributionsResult{Distributions: s.pluginDistributions, NextCursor: nil}, nil
}

func (s *recordingSkillsManagement) Distribute(_ context.Context, payload *genskills.DistributePayload) (*types.SkillDistribution, error) {
	s.distributed = payload
	if s.err != nil {
		return nil, s.err
	}
	return s.distribution, nil
}

type stubSkillTargets struct{ targets []SkillTarget }

func (s stubSkillTargets) SkillTargets(_ context.Context, _ string, _ uuid.UUID, limit int) ([]SkillTarget, error) {
	if len(s.targets) > limit {
		return s.targets[:limit], nil
	}
	return s.targets, nil
}

type stubSkillProjects struct{ err error }

func (s stubSkillProjects) ResolveProject(_ context.Context, _, projectSlug string) (ResolvedProject, error) {
	if s.err != nil {
		return ResolvedProject{}, s.err
	}
	return ResolvedProject{ID: uuid.MustParse("44444444-4444-4444-8444-444444444444"), Name: "Acme", Slug: projectSlug}, nil
}

// passthroughGrants stands in for the RBAC engine. Grant loading is exercised
// against a real database in the vertical slice test; here it only has to not
// be the thing under test.
type passthroughGrants struct{}

func (passthroughGrants) PrepareContext(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

type stubSkillsGate struct {
	enabled bool
	err     error
}

func (s stubSkillsGate) Enabled(_ context.Context, _, _ string) (bool, error) {
	return s.enabled, s.err
}

func testTargets() []SkillTarget {
	return []SkillTarget{
		{Kind: SkillTargetPlugin, ID: testDefaultPluginID, Name: "Default", Slug: "default", IsDefault: true},
		{Kind: SkillTargetPlugin, ID: testMarketingPlugin, Name: "Marketing", Slug: "marketing", IsDefault: false},
		{Kind: SkillTargetAssistant, ID: testAssistantID, Name: "Support", Slug: "", IsDefault: false},
	}
}

func testSkillsService(t *testing.T, skills *recordingSkillsManagement) *SkillsService {
	t.Helper()
	return NewSkillsService(
		skills,
		stubSkillTargets{targets: testTargets()},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)
}

func testSkill() *types.Skill {
	summary := "Adds an MCP from the catalogue"
	latest := testSkillVersionID
	return &types.Skill{
		ID:              testSkillID,
		Name:            "add-mcp",
		DisplayName:     "Add MCP",
		Summary:         &summary,
		Tags:            []string{"catalog"},
		LatestVersionID: &latest,
		VersionCount:    2,
		HasValidVersion: true,
		UpdatedAt:       "2026-08-20T00:00:00Z",
	}
}

func testSkillVersion(content string) *types.SkillVersion {
	return &types.SkillVersion{
		ID:              testSkillVersionID,
		SkillID:         testSkillID,
		Content:         content,
		CanonicalSha256: "abc123",
		SpecValid:       true,
		CreatedAt:       "2026-08-20T00:00:00Z",
	}
}

func TestCreateSkillReportsTheSkillIsInertUntilDistributed(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill:         testSkill(),
		latestVersion: testSkillVersion("---\nname: add-mcp\n---\n"),
		recordResult: &genskills.RecordSkillResult{
			Skill:          testSkill(),
			Version:        testSkillVersion("---\nname: add-mcp\n---\n"),
			CreatedSkill:   true,
			CreatedVersion: true,
		},
	}
	service := testSkillsService(t, skills)

	result, err := service.CreateSkill(t.Context(), testPrincipal(), CreateSkillInput{
		ProjectSlug: testSkillProjectSlug,
		Content:     "---\nname: add-mcp\n---\n",
	})

	require.NoError(t, err)
	require.True(t, result.CreatedSkill)
	require.False(t, result.Distributed)
	require.Equal(t, "distribute_skill", result.NextAction)
	require.Contains(t, result.InertMessage, "inert")
	// The result names where the skill can be sent, so a caller does not have
	// to guess a target — or assume authoring already activated it.
	require.NotEmpty(t, result.DistributionTargets)
	require.Empty(t, result.Version.Content, "authoring echoes back no manifest content")
}

func TestCreateSkillRefusesContentOverTheManifestCeiling(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{}
	service := testSkillsService(t, skills)

	_, err := service.CreateSkill(t.Context(), testPrincipal(), CreateSkillInput{
		ProjectSlug: testSkillProjectSlug,
		Content:     strings.Repeat("a", maxSkillContentBytes+1),
	})

	require.ErrorIs(t, err, ErrSkillContentTooLarge)
	require.Nil(t, skills.created, "an oversized manifest never reaches the skills service")
}

func TestAddSkillVersionForwardsTheExpectedVersionToken(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill:         testSkill(),
		latestVersion: testSkillVersion("---\nname: add-mcp\n---\n"),
		recordResult: &genskills.RecordSkillResult{
			Skill:          testSkill(),
			Version:        testSkillVersion("---\nname: add-mcp\n---\n"),
			CreatedSkill:   false,
			CreatedVersion: true,
		},
	}
	service := testSkillsService(t, skills)

	_, err := service.AddSkillVersion(t.Context(), testPrincipal(), AddSkillVersionInput{
		ProjectSlug:             testSkillProjectSlug,
		SkillID:                 testSkillID,
		Content:                 "---\nname: add-mcp\n---\n",
		ExpectedLatestVersionID: testSkillVersionID,
	})

	require.NoError(t, err)
	require.NotNil(t, skills.addedVersion)
	require.NotNil(t, skills.addedVersion.ExpectedLatestVersionID)
	require.Equal(t, testSkillVersionID, *skills.addedVersion.ExpectedLatestVersionID)
	require.NotNil(t, skills.addedVersion.DerivedFromVersionID)
	require.Equal(t, testSkillVersionID, *skills.addedVersion.DerivedFromVersionID)
}

func TestUpdateSkillMetadataLeavesUnspecifiedFieldsAndTagsUntouched(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill(), latestVersion: testSkillVersion("")}
	service := testSkillsService(t, skills)

	_, err := service.UpdateSkillMetadata(t.Context(), testPrincipal(), UpdateSkillMetadataInput{
		ProjectSlug:             testSkillProjectSlug,
		SkillID:                 testSkillID,
		DisplayName:             "Add MCP from Catalogue",
		ExpectedLatestVersionID: testSkillVersionID,
	})

	require.NoError(t, err)
	require.NotNil(t, skills.updated)
	require.Equal(t, "Add MCP from Catalogue", skills.updated.DisplayName)
	require.Equal(t, "add-mcp", skills.updated.Name, "an omitted name is carried through, not blanked")
	require.NotNil(t, skills.updated.Summary)
	require.Equal(t, []string{"catalog"}, skills.updated.Tags, "this surface does not author tags")
}

func TestUpdateSkillMetadataClearsTheSummaryOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill(), latestVersion: testSkillVersion("")}
	service := testSkillsService(t, skills)

	_, err := service.UpdateSkillMetadata(t.Context(), testPrincipal(), UpdateSkillMetadataInput{
		ProjectSlug:             testSkillProjectSlug,
		SkillID:                 testSkillID,
		ClearSummary:            true,
		ExpectedLatestVersionID: testSkillVersionID,
	})

	require.NoError(t, err)
	require.Nil(t, skills.updated.Summary)
}

func TestGetSkillWithholdsManifestContentUntilAskedFor(t *testing.T) {
	t.Parallel()

	content := "---\nname: add-mcp\n---\nInstructions"
	skills := &recordingSkillsManagement{skill: testSkill(), latestVersion: testSkillVersion(content)}
	service := testSkillsService(t, skills)

	withheld, err := service.GetSkill(t.Context(), testPrincipal(), GetSkillInput{
		ProjectSlug: testSkillProjectSlug,
		SkillID:     testSkillID,
	})
	require.NoError(t, err)
	require.NotNil(t, withheld.LatestVersion)
	require.Empty(t, withheld.LatestVersion.Content)

	included, err := service.GetSkill(t.Context(), testPrincipal(), GetSkillInput{
		ProjectSlug:    testSkillProjectSlug,
		SkillID:        testSkillID,
		IncludeContent: true,
	})
	require.NoError(t, err)
	require.Equal(t, content, included.LatestVersion.Content)
}

func TestGetSkillTruncatesContentAtTheReadCeiling(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill(), latestVersion: testSkillVersion(strings.Repeat("a", maxSkillContentBytes+512))}
	service := testSkillsService(t, skills)

	result, err := service.GetSkill(t.Context(), testPrincipal(), GetSkillInput{
		ProjectSlug:    testSkillProjectSlug,
		SkillID:        testSkillID,
		IncludeContent: true,
	})

	require.NoError(t, err)
	require.Len(t, result.LatestVersion.Content, maxSkillContentBytes)
	require.True(t, result.LatestVersion.ContentTruncated)
}

func TestDistributeSkillResolvesAnExactTargetAndEchoesIt(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		input  DistributeSkillInput
		wantID string
		kind   SkillTargetKind
	}{
		{name: "plugin by slug", input: DistributeSkillInput{Plugin: "marketing"}, wantID: testMarketingPlugin, kind: SkillTargetPlugin},
		{name: "plugin by name, case-insensitively", input: DistributeSkillInput{Plugin: "marketing"}, wantID: testMarketingPlugin, kind: SkillTargetPlugin},
		{name: "default plugin named exactly", input: DistributeSkillInput{Plugin: "default"}, wantID: testDefaultPluginID, kind: SkillTargetPlugin},
		{name: "plugin by id", input: DistributeSkillInput{Plugin: testMarketingPlugin}, wantID: testMarketingPlugin, kind: SkillTargetPlugin},
		{name: "assistant by name", input: DistributeSkillInput{Assistant: "Support"}, wantID: testAssistantID, kind: SkillTargetAssistant},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			skills := &recordingSkillsManagement{
				skill: testSkill(),
				distribution: &types.SkillDistribution{
					ID:                "77777777-7777-4777-8777-777777777777",
					SkillID:           testSkillID,
					SkillName:         "add-mcp",
					ResolvedVersionID: testSkillVersionID,
					Channel:           string(test.kind),
				},
			}
			service := testSkillsService(t, skills)
			input := test.input
			input.ProjectSlug = testSkillProjectSlug
			input.SkillID = testSkillID

			result, err := service.DistributeSkill(t.Context(), testPrincipal(), input)

			require.NoError(t, err)
			require.Equal(t, test.wantID, result.Target.ID)
			require.Equal(t, test.kind, result.Target.Kind)
			if test.kind == SkillTargetPlugin {
				require.NotNil(t, skills.distributed.PluginID)
				require.Equal(t, test.wantID, *skills.distributed.PluginID)
				require.Nil(t, skills.distributed.AssistantID)
			} else {
				require.NotNil(t, skills.distributed.AssistantID)
				require.Equal(t, test.wantID, *skills.distributed.AssistantID)
				require.Nil(t, skills.distributed.PluginID)
			}
		})
	}
}

func TestDistributeSkillRefusesAnUnmatchedTargetRatherThanFallingBackToDefault(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill()}
	service := testSkillsService(t, skills)

	_, err := service.DistributeSkill(t.Context(), testPrincipal(), DistributeSkillInput{
		ProjectSlug: testSkillProjectSlug,
		SkillID:     testSkillID,
		Plugin:      "markting",
	})

	require.ErrorIs(t, err, ErrSkillTargetNotFound)
	require.Nil(t, skills.distributed, "a target that does not exist distributes nothing at all")
}

func TestDistributeSkillRefusesAnAmbiguousTarget(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill()}
	service := NewSkillsService(
		skills,
		stubSkillTargets{targets: []SkillTarget{
			{Kind: SkillTargetPlugin, ID: testDefaultPluginID, Name: "Marketing", Slug: "marketing-one", IsDefault: false},
			{Kind: SkillTargetPlugin, ID: testMarketingPlugin, Name: "marketing", Slug: "marketing-two", IsDefault: false},
		}},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)

	_, err := service.DistributeSkill(t.Context(), testPrincipal(), DistributeSkillInput{
		ProjectSlug: testSkillProjectSlug,
		SkillID:     testSkillID,
		Plugin:      "Marketing",
	})

	require.ErrorIs(t, err, ErrSkillTargetAmbiguous)
	require.Nil(t, skills.distributed)
}

func TestDistributeSkillRequiresExactlyOneTargetKind(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill()}
	service := testSkillsService(t, skills)

	for _, input := range []DistributeSkillInput{
		{ProjectSlug: testSkillProjectSlug, SkillID: testSkillID},
		{ProjectSlug: testSkillProjectSlug, SkillID: testSkillID, Plugin: "default", Assistant: "Support"},
	} {
		_, err := service.DistributeSkill(t.Context(), testPrincipal(), input)
		require.ErrorIs(t, err, ErrRegistrationInvalid)
	}
	require.Nil(t, skills.distributed)
}

func TestSkillCallsRefuseWhenTheCapabilityIsOff(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{skill: testSkill()}
	service := NewSkillsService(
		skills,
		stubSkillTargets{targets: testTargets()},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: false},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)

	_, err := service.ListSkills(t.Context(), testPrincipal(), ListSkillsInput{ProjectSlug: testSkillProjectSlug})

	require.ErrorIs(t, err, ErrSkillsUnavailable)
}

func TestSkillCallsAreMeteredBeforeTheyResolveAProject(t *testing.T) {
	t.Parallel()

	projects := &countingSkillProjects{}
	service := NewSkillsService(
		&recordingSkillsManagement{skill: testSkill()},
		stubSkillTargets{targets: testTargets()},
		projects,
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)

	_, err := service.CreateSkill(t.Context(), testPrincipal(), CreateSkillInput{ProjectSlug: testSkillProjectSlug, Content: "---\nname: a\n---\n"})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Zero(t, projects.calls)
}

type countingSkillProjects struct{ calls int }

func (s *countingSkillProjects) ResolveProject(_ context.Context, _, projectSlug string) (ResolvedProject, error) {
	s.calls++
	return ResolvedProject{ID: uuid.Nil, Name: "Acme", Slug: projectSlug}, nil
}

func TestSkillCallsActUnderTheCallingUserRatherThanTheSurface(t *testing.T) {
	t.Parallel()

	skills := &authContextCapturingSkills{}
	service := NewSkillsService(
		skills,
		stubSkillTargets{targets: testTargets()},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)

	principal := testPrincipal()
	_, err := service.ListSkills(t.Context(), principal, ListSkillsInput{ProjectSlug: testSkillProjectSlug})

	require.NoError(t, err)
	require.NotNil(t, skills.captured)
	require.Equal(t, principal.UserID, skills.captured.UserID)
	require.Equal(t, principal.OrganizationID, skills.captured.ActiveOrganizationID)
	require.NotNil(t, skills.captured.ProjectID)
}

type authContextCapturingSkills struct {
	recordingSkillsManagement
	captured *contextvalues.AuthContext
}

func (s *authContextCapturingSkills) List(ctx context.Context, _ *genskills.ListPayload) (*genskills.ListSkillsResult, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	s.captured = authCtx
	return &genskills.ListSkillsResult{Skills: nil, TotalCount: 0, NextCursor: nil}, nil
}

func TestSkillsToolResultMapsRefusalsToStructuredCodes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		code string
	}{
		{name: "capability off", err: ErrSkillsUnavailable, code: unavailableCode},
		{name: "unmatched target", err: ErrSkillTargetNotFound, code: "not_found"},
		{name: "ambiguous target", err: ErrSkillTargetAmbiguous, code: "ambiguous_target"},
		{name: "oversized manifest", err: ErrSkillContentTooLarge, code: "invalid_request"},
		{name: "rate limited", err: ErrOperationRateLimited, code: "rate_limited"},
		{name: "stale version token", err: oops.E(oops.CodeConflict, nil, "the skill has a newer version than the expected one"), code: "conflict"},
		{name: "invalid manifest", err: oops.E(oops.CodeBadRequest, nil, "skill manifest frontmatter is too deeply nested"), code: "invalid_request"},
		{name: "missing skill", err: oops.E(oops.CodeNotFound, nil, "skill not found"), code: "not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, ok := skillsToolResult(test.err)

			require.True(t, ok)
			require.True(t, result.IsError)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var body skillsRefusalResult
			require.NoError(t, json.Unmarshal([]byte(text.Text), &body))
			require.Equal(t, test.code, body.Code)
			require.NotEmpty(t, body.Message)
		})
	}
}

func TestSkillsToolResultLeavesUnexpectedFailuresAsErrors(t *testing.T) {
	t.Parallel()

	_, ok := skillsToolResult(oops.E(oops.CodeUnexpected, nil, "load skill state"))

	require.False(t, ok, "an internal failure is a transport error, not a refusal the model should act on")
}

// The kill switch must change what a skills tool answers, not whether it
// exists: a tool that vanishes with the rollout looks to a client exactly like
// one that was never built.
func TestSkillsToolsAreDeclaredWithAndWithoutTheirDependencies(t *testing.T) {
	t.Parallel()

	wanted := []string{"list_skills", "get_skill", "list_skill_versions", "create_skill", "add_skill_version", "update_skill_metadata", "distribute_skill"}

	for _, test := range []struct {
		name  string
		build func() *SkillsService
	}{
		{name: "composed", build: func() *SkillsService { return testSkillsService(t, &recordingSkillsManagement{skill: testSkill()}) }},
		{name: "absent", build: func() *SkillsService { return nil }},
		{name: "incomplete", build: func() *SkillsService {
			return NewSkillsService(nil, nil, nil, nil, nil, OperationBudget{Connection: nil, Organization: nil})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, test.build(), CatalogDescriptor{}, nil, nil)

			registered := make(map[string]Descriptor, len(wanted))
			for _, descriptor := range registrar.Descriptors() {
				registered[descriptor.Name] = descriptor
			}
			for _, name := range wanted {
				descriptor, ok := registered[name]
				require.True(t, ok, "tool %q is not registered", name)
				require.Equal(t, bothAudiences, descriptor.Meta.Audiences, "skills tools serve the assistant as well; the adapter supplies the project it acts in")
				require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
			}
		})
	}
}

// Truncation cuts bytes, so a manifest whose 64 KiB boundary lands mid-rune
// would otherwise end in mojibake.
func TestGetSkillTruncatesContentOnARuneBoundary(t *testing.T) {
	t.Parallel()

	// One multi-byte rune straddling the ceiling.
	content := strings.Repeat("a", maxSkillContentBytes-1) + "€" + strings.Repeat("b", 16)
	skills := &recordingSkillsManagement{skill: testSkill(), latestVersion: testSkillVersion(content)}
	service := testSkillsService(t, skills)

	result, err := service.GetSkill(t.Context(), testPrincipal(), GetSkillInput{
		ProjectSlug:    testSkillProjectSlug,
		SkillID:        testSkillID,
		IncludeContent: true,
	})

	require.NoError(t, err)
	require.True(t, result.LatestVersion.ContentTruncated)
	require.True(t, utf8.ValidString(result.LatestVersion.Content))
}

// A skill carried only by a plugin is distributed. Reporting otherwise would
// contradict the distribute_skill call that put it there.
func TestGetSkillReportsAPluginOnlyDistributionAsDistributed(t *testing.T) {
	t.Parallel()

	skills := &recordingSkillsManagement{
		skill:               testSkill(),
		latestVersion:       testSkillVersion(""),
		assistantCount:      0,
		pluginDistributions: []*types.PluginSkillDistribution{{SkillID: testSkillID}},
	}
	service := testSkillsService(t, skills)

	result, err := service.GetSkill(t.Context(), testPrincipal(), GetSkillInput{
		ProjectSlug: testSkillProjectSlug,
		SkillID:     testSkillID,
	})

	require.NoError(t, err)
	require.True(t, result.Distributed)
}

// The registry read has to ask for an ordering the skills service actually
// declares, or it silently falls back to name order.
func TestListSkillsRequestsAnOrderingTheServiceSupports(t *testing.T) {
	t.Parallel()

	skills := &sortCapturingSkills{}
	service := NewSkillsService(
		skills,
		stubSkillTargets{targets: testTargets()},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)

	_, err := service.ListSkills(t.Context(), testPrincipal(), ListSkillsInput{ProjectSlug: testSkillProjectSlug})

	require.NoError(t, err)
	require.Contains(t, []string{"name", "updated"}, skills.sort, "sort must be one of the values the skills design declares")
	require.Equal(t, "updated", skills.sort)
}

type sortCapturingSkills struct {
	recordingSkillsManagement
	sort string
}

func (s *sortCapturingSkills) List(_ context.Context, payload *genskills.ListPayload) (*genskills.ListSkillsResult, error) {
	s.sort = payload.Sort
	return &genskills.ListSkillsResult{Skills: nil, TotalCount: 0, NextCursor: nil}, nil
}

// Resolution reads every candidate. Capping the combined list would let a
// project's plugins hide its assistants, turning an existing target into
// not_found — the one refusal that must mean "you named something absent".
func TestDistributeSkillResolvesAnAssistantBehindManyPlugins(t *testing.T) {
	t.Parallel()

	targets := make([]SkillTarget, 0, maxSkillTargetCandidates+2)
	for i := range maxSkillTargetCandidates + 1 {
		targets = append(targets, SkillTarget{Kind: SkillTargetPlugin, ID: uuid.NewString(), Name: fmt.Sprintf("plugin-%d", i), Slug: fmt.Sprintf("plugin-%d", i)})
	}
	targets = append(targets, SkillTarget{Kind: SkillTargetAssistant, ID: testAssistantID, Name: "Support"})

	skills := &recordingSkillsManagement{
		skill:        testSkill(),
		distribution: &types.SkillDistribution{ID: "d", SkillID: testSkillID, SkillName: "add-mcp", ResolvedVersionID: testSkillVersionID, Channel: "assistant"},
	}
	service := NewSkillsService(
		skills,
		stubSkillTargets{targets: targets},
		stubSkillProjects{},
		passthroughGrants{},
		stubSkillsGate{enabled: true},
		OperationBudget{
			Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
			Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		},
	)

	result, err := service.DistributeSkill(t.Context(), testPrincipal(), DistributeSkillInput{
		ProjectSlug: testSkillProjectSlug,
		SkillID:     testSkillID,
		Assistant:   "Support",
	})

	require.NoError(t, err)
	require.Equal(t, testAssistantID, result.Target.ID)
}

// The advice list is capped, but a caller that can only see plugins would read
// "there is nowhere else to send this" when an assistant target exists.
func TestAuthoringAdviceKeepsBothTargetKindsWithinTheCap(t *testing.T) {
	t.Parallel()

	targets := make([]SkillTarget, 0, maxSkillTargetCandidates+2)
	for i := range maxSkillTargetCandidates + 1 {
		targets = append(targets, SkillTarget{Kind: SkillTargetPlugin, ID: uuid.NewString(), Name: fmt.Sprintf("plugin-%d", i)})
	}
	targets = append(targets, SkillTarget{Kind: SkillTargetAssistant, ID: testAssistantID, Name: "Support"})

	kept := adviceTargets(targets, maxSkillTargetCandidates)

	require.Len(t, kept, maxSkillTargetCandidates)
	require.Contains(t, skillTargetKinds(kept), SkillTargetAssistant)
	require.Contains(t, skillTargetKinds(kept), SkillTargetPlugin)
}

func skillTargetKinds(targets []SkillTarget) []SkillTargetKind {
	kinds := make([]SkillTargetKind, 0, len(targets))
	for _, target := range targets {
		kinds = append(kinds, target.Kind)
	}
	return kinds
}

// The advice cap is a cap for any limit, not only the one the caller happens
// to pass today.
func TestAuthoringAdviceNeverExceedsTheCap(t *testing.T) {
	t.Parallel()

	targets := []SkillTarget{
		{Kind: SkillTargetPlugin, ID: testDefaultPluginID, Name: "Default"},
		{Kind: SkillTargetAssistant, ID: testAssistantID, Name: "Support"},
	}

	for _, limit := range []int{1, 2, 3} {
		require.LessOrEqual(t, len(adviceTargets(targets, limit)), limit)
	}
}
