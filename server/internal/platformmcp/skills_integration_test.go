//nolint:wrapcheck // Integration assertions intentionally return test setup errors directly.
package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	skillsservice "github.com/speakeasy-api/gram/server/internal/skills"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The skills lane is only as good as its slowest-moving joint: a Platform MCP
// tool call has to travel the OAuth endpoint, the tool schema, the acting
// principal, the skills management service, RBAC, and Postgres, and come back
// as something a model can act on. The unit tests hold each joint still; this
// one drives the whole run with a real MCP client, a real skills service, and a
// real database.
func TestPlatformMCPSkillsToolsAuthorAndDistributeEndToEnd(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_vertical", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: true})
	session := fixture.session

	// Authoring. The result has to say the skill is inert, because a model that
	// reads "created" and stops leaves a skill nothing will ever load.
	created := callSkillsTool[SkillAuthoringResult](t, ctx, session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("catalog-add", "Adds a reviewed MCP from the catalogue.", "Ask which project first."),
	})
	require.True(t, created.CreatedSkill)
	require.True(t, created.CreatedVersion)
	require.False(t, created.Distributed)
	require.Contains(t, created.InertMessage, "no agent loads it yet")
	require.Equal(t, "distribute_skill", created.NextAction)
	require.Contains(t, skillTargetNames(created.DistributionTargets), "Marketing")

	stored, err := skillsrepo.New(fixture.conn).GetSkillState(ctx, skillsrepo.GetSkillStateParams{
		ProjectID: fixture.project.ID,
		SkillID:   uuid.MustParse(created.Skill.ID),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, stored.VersionCount)
	require.Equal(t, created.Version.ID, stored.LatestVersionID.String())

	// Reads withhold manifest content unless the caller asks for it.
	read := callSkillsTool[GetSkillOutput](t, ctx, session, "get_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
	})
	require.NotNil(t, read.LatestVersion)
	require.Empty(t, read.LatestVersion.Content)

	withContent := callSkillsTool[GetSkillOutput](t, ctx, session, "get_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"include_content": true,
	})
	require.Contains(t, withContent.LatestVersion.Content, "Ask which project first.")

	// A correction is a new immutable version, guarded by the version the
	// caller read.
	revised := callSkillsTool[SkillAuthoringResult](t, ctx, session, "add_skill_version", map[string]any{
		"project_slug":               fixture.project.Slug,
		"skill_id":                   created.Skill.ID,
		"content":                    skillsFixtureManifest("catalog-add", "Adds a reviewed MCP from the catalogue.", "Ask which project first, then which catalogue entry."),
		"expected_latest_version_id": created.Version.ID,
	})
	require.True(t, revised.CreatedVersion)
	require.NotEqual(t, created.Version.ID, revised.Version.ID)

	// The same token again is stale, and a stale write is refused rather than
	// applied on top of the version it did not see.
	conflict := callSkillsRefusal(t, ctx, session, "add_skill_version", map[string]any{
		"project_slug":               fixture.project.Slug,
		"skill_id":                   created.Skill.ID,
		"content":                    skillsFixtureManifest("catalog-add", "Adds a reviewed MCP from the catalogue.", "A third opinion."),
		"expected_latest_version_id": created.Version.ID,
	})
	require.Equal(t, "conflict", conflict.Code)

	versions, err := skillsrepo.New(fixture.conn).GetSkillState(ctx, skillsrepo.GetSkillStateParams{
		ProjectID: fixture.project.ID,
		SkillID:   uuid.MustParse(created.Skill.ID),
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, versions.VersionCount, "the refused write recorded nothing")

	// Naming a target that does not exist distributes nothing — least of all to
	// the default plugin, which is exactly the plugin an implicit fallback would
	// have picked here.
	missing := callSkillsRefusal(t, ctx, session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketng",
	})
	require.Equal(t, "not_found", missing.Code)

	require.Empty(t, listSkillDistributions(t, ctx, fixture.conn, fixture.project.ID, created.Skill.ID))

	// Distribution is the activation step, and it echoes back the target the
	// caller's name resolved to.
	distributed := callSkillsTool[DistributeSkillOutput](t, ctx, session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketing",
	})
	require.Equal(t, SkillTargetPlugin, distributed.Target.Kind)
	require.Equal(t, fixture.marketingPluginID.String(), distributed.Target.ID)
	require.Equal(t, revised.Version.ID, distributed.ResolvedVersionID, "a distribution that pins nothing tracks the latest valid version")

	// Idempotent on project, target, and skill: a repeat converges rather than
	// attaching the skill twice.
	repeat := callSkillsTool[DistributeSkillOutput](t, ctx, session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketing",
	})
	require.Equal(t, distributed.DistributionID, repeat.DistributionID)
	require.Len(t, listSkillDistributions(t, ctx, fixture.conn, fixture.project.ID, created.Skill.ID), 1)

	// The listing names the plugin the distribution resolved to and says the
	// distribution follows the skill's latest version, since nothing was pinned.
	listed := callSkillsTool[ListSkillDistributionsOutput](t, ctx, session, "list_skill_distributions", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "Marketing",
	})
	require.Len(t, listed.Distributions, 1)
	require.Equal(t, distributed.DistributionID, listed.Distributions[0].ID)
	require.Equal(t, SkillTarget{Kind: SkillTargetPlugin, ID: fixture.marketingPluginID.String(), Name: "Marketing"}, listed.Distributions[0].Target)
	require.True(t, listed.Distributions[0].FollowsLatest)
	require.Empty(t, listed.Distributions[0].PinnedVersionID)
	require.Equal(t, revised.Version.ID, listed.Distributions[0].ResolvedVersionID)
	require.Empty(t, listed.NextCursor)

	// Taking the skill back is gated on the user's explicit confirmation, and
	// an unconfirmed call revokes nothing.
	unconfirmed := callSkillsRefusal(t, ctx, session, "undistribute_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"plugin":          "marketing",
		"confirmed":       false,
		"idempotency_key": "revoke-marketing",
	})
	require.Equal(t, "confirmation_required", unconfirmed.Code)
	require.Len(t, listSkillDistributions(t, ctx, fixture.conn, fixture.project.ID, created.Skill.ID), 1)

	revoked := callSkillsTool[UndistributeSkillOutput](t, ctx, session, "undistribute_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"plugin":          "marketing",
		"confirmed":       true,
		"idempotency_key": "revoke-marketing",
	})
	require.Equal(t, distributed.Target, revoked.Target)
	require.Equal(t, created.Skill.ID, revoked.SkillID)
	require.Empty(t, listSkillDistributions(t, ctx, fixture.conn, fixture.project.ID, created.Skill.ID))

	// A repeat finds nothing left to revoke and reports the same end state.
	again := callSkillsTool[UndistributeSkillOutput](t, ctx, session, "undistribute_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"plugin":          "marketing",
		"confirmed":       true,
		"idempotency_key": "revoke-marketing",
	})
	require.Equal(t, revoked, again)
	require.Empty(t, callSkillsTool[ListSkillDistributionsOutput](t, ctx, session, "list_skill_distributions", map[string]any{
		"project_slug": fixture.project.Slug,
	}).Distributions)
}

// A skill lives in one project, and naming another project's plugin must not
// reach it even when the same connection is authorized for both.
func TestPlatformMCPDistributeSkillRefusesATargetInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_cross_project", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: true})

	otherSlug := "other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(fixture.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: fixture.principal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.principal.OrganizationID,
		ProjectID:      otherProject.ID,
		Name:           "Elsewhere",
		Slug:           "elsewhere",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	created := callSkillsTool[SkillAuthoringResult](t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("scoped", "Scoped to one project.", "Body."),
	})

	refusal := callSkillsRefusal(t, ctx, fixture.session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "elsewhere",
	})

	require.Equal(t, "not_found", refusal.Code)
}

// A capability that is off must still answer. The endpoint keeps serving, the
// tool keeps existing, and the caller gets a reason rather than a tool that
// silently disappeared from the manifest.
func TestPlatformMCPSkillsToolsRefuseReadablyWhenTheCapabilityIsOff(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_capability_off", skillsVerticalOptions{capabilityEnabled: false, grantAdmin: true})

	tools, err := fixture.session.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Contains(t, toolNames(tools.Tools), "create_skill")

	refusal := callSkillsRefusal(t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("gated", "Gated by the rollout.", "Body."),
	})

	require.Equal(t, unavailableCode, refusal.Code)
}

// Authorization is the acting user's, not the surface's. A connection whose
// user only holds skill:read can inspect existing skills but cannot author one.
func TestPlatformMCPSkillReadsAllowSkillReaderButWritesRequireSkillWrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_reader", skillsVerticalOptions{capabilityEnabled: true, grantSkillRead: true})
	manifest := skillsFixtureManifest("reader-visible", "Visible to a permitted reader.", "Read-only body.")
	queries := skillsrepo.New(fixture.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID:   fixture.project.ID,
		Name:        "reader-visible",
		DisplayName: "Reader visible",
		Summary:     pgtype.Text{String: "Visible to a permitted reader.", Valid: true},
	})
	require.NoError(t, err)
	version, err := queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          manifest,
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "Visible to a permitted reader.", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  fixture.principal.UserID,
		ProjectID:        fixture.project.ID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)

	result := callSkillsTool[ListSkillsOutput](t, ctx, fixture.session, "list_skills", map[string]any{
		"project_slug": fixture.project.Slug,
	})
	require.Len(t, result.Skills, 1)
	require.Equal(t, skill.ID.String(), result.Skills[0].ID)
	require.Equal(t, version.ID.String(), result.Skills[0].LatestVersionID)

	read := callSkillsTool[GetSkillOutput](t, ctx, fixture.session, "get_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        skill.ID.String(),
		"include_content": true,
	})
	require.Equal(t, skill.ID.String(), read.Skill.ID)
	require.Equal(t, manifest, read.LatestVersion.Content)

	feedbackRow, err := queries.CreateSkillFeedback(ctx, skillsrepo.CreateSkillFeedbackParams{
		ID: uuid.NullUUID{}, ProjectID: fixture.project.ID,
		SkillID: uuid.NullUUID{UUID: skill.ID, Valid: true}, SkillVersionID: uuid.NullUUID{UUID: version.ID, Valid: true},
		SkillName: skill.Name, Source: string(skillsservice.FeedbackSourceDev), Outcome: string(skillsservice.FeedbackOutcomeDidNotHelp),
		Note: pgtype.Text{String: "Add an escalation step.", Valid: true}, SessionID: pgtype.Text{String: "private-session", Valid: true},
		UserID: pgtype.Text{String: "private-user", Valid: true}, UserEmail: pgtype.Text{String: "private@example.test", Valid: true},
	})
	require.NoError(t, err)
	proposedContent := skillsFixtureManifest("reader-visible", "Visible to a permitted reader.", "Read-only body with an escalation step.")
	proposedDiff, err := skilldiff.Unified(manifest, proposedContent)
	require.NoError(t, err)
	suggestion, err := queries.CreateSkillEditSuggestion(ctx, skillsrepo.CreateSkillEditSuggestionParams{
		Rationale: "Agents need an escalation step.", ScoredSessionCount: 1,
		BaseVersionID: version.ID, ProjectID: fixture.project.ID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	change, err := queries.CreateSkillEditSuggestionChange(ctx, skillsrepo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: proposedDiff, Rationale: suggestion.Rationale, Position: 0,
		ProjectID: fixture.project.ID, SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)
	linked, err := queries.LinkSkillEditSuggestionFeedback(ctx, skillsrepo.LinkSkillEditSuggestionFeedbackParams{
		ChangeID: change.ID, ProjectID: fixture.project.ID, FeedbackIds: []uuid.UUID{feedbackRow.ID},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, linked)

	feedback := callSkillsTool[ListSkillFeedbackOutput](t, ctx, fixture.session, "list_skill_feedback", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     skill.ID.String(),
		"limit":        1,
	})
	require.Equal(t, skill.ID.String(), feedback.SkillID)
	require.EqualValues(t, 1, feedback.Counts.Total)
	require.Len(t, feedback.Feedback, 1)
	require.Equal(t, "Add an escalation step.", feedback.Feedback[0].Note)
	feedbackJSON, err := json.Marshal(feedback)
	require.NoError(t, err)
	require.NotContains(t, string(feedbackJSON), "private-session")
	require.NotContains(t, string(feedbackJSON), "private-user")
	require.NotContains(t, string(feedbackJSON), "private@example.test")

	suggestions := callSkillsTool[ListSkillSuggestionsOutput](t, ctx, fixture.session, "list_skill_suggestions", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     skill.ID.String(),
	})
	require.EqualValues(t, 1, suggestions.TotalOpenCount)
	require.Len(t, suggestions.Suggestions, 1)
	require.Empty(t, suggestions.Suggestions[0].ProposedContent)
	require.EqualValues(t, 1, suggestions.Suggestions[0].FeedbackCount)
	require.Len(t, suggestions.Suggestions[0].Changes, 1)
	require.Equal(t, change.ID.String(), suggestions.Suggestions[0].Changes[0].ID)

	withProposedContent := callSkillsTool[ListSkillSuggestionsOutput](t, ctx, fixture.session, "list_skill_suggestions", map[string]any{
		"project_slug":             fixture.project.Slug,
		"skill_id":                 skill.ID.String(),
		"include_proposed_content": true,
	})
	require.Len(t, withProposedContent.Suggestions, 1)
	require.Equal(t, proposedContent, withProposedContent.Suggestions[0].ProposedContent)

	suggestionFeedback := callSkillsTool[ListSkillSuggestionFeedbackOutput](t, ctx, fixture.session, "list_skill_suggestion_feedback", map[string]any{
		"project_slug": fixture.project.Slug,
		"change_id":    change.ID.String(),
		"limit":        1,
	})
	require.Equal(t, change.ID.String(), suggestionFeedback.ChangeID)
	require.Len(t, suggestionFeedback.Feedback, 1)
	suggestionFeedbackJSON, err := json.Marshal(suggestionFeedback)
	require.NoError(t, err)
	require.NotContains(t, string(suggestionFeedbackJSON), "private-session")
	require.NotContains(t, string(suggestionFeedbackJSON), "private-user")
	require.NotContains(t, string(suggestionFeedbackJSON), "private@example.test")

	refusal := callSkillsRefusal(t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("reader-write", "Requires skill write access.", "Body."),
	})
	require.Equal(t, "forbidden", refusal.Code)
}

func TestPlatformMCPSkillWriterCanAuthorButCannotDistribute(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_writer", skillsVerticalOptions{capabilityEnabled: true, grantSkillRead: true, grantSkillWrite: true})
	created := callSkillsTool[SkillAuthoringResult](t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("delegated-writer", "Authored with skill write.", "Body."),
	})
	require.True(t, created.CreatedSkill)

	current := callSkillsTool[GetSkillOutput](t, ctx, fixture.session, "get_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
	})
	require.Equal(t, created.Version.ID, current.LatestVersion.ID)

	revised := callSkillsTool[SkillAuthoringResult](t, ctx, fixture.session, "add_skill_version", map[string]any{
		"project_slug":               fixture.project.Slug,
		"skill_id":                   created.Skill.ID,
		"content":                    skillsFixtureManifest("delegated-writer", "Authored with delegated skill write.", "Revised body."),
		"expected_latest_version_id": current.LatestVersion.ID,
	})
	require.True(t, revised.CreatedVersion)

	updated := callSkillsTool[UpdateSkillMetadataOutput](t, ctx, fixture.session, "update_skill_metadata", map[string]any{
		"project_slug":               fixture.project.Slug,
		"skill_id":                   created.Skill.ID,
		"display_name":               "Delegated writer updated",
		"summary":                    "Updated with delegated skill write.",
		"expected_latest_version_id": revised.Version.ID,
	})
	require.Equal(t, "Delegated writer updated", updated.Skill.DisplayName)
	require.Equal(t, "Updated with delegated skill write.", updated.Skill.Summary)

	refusal := callSkillsRefusal(t, ctx, fixture.session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketing",
	})
	require.Equal(t, "permission_denied", refusal.Code)

	listed := callSkillsTool[ListSkillDistributionsOutput](t, ctx, fixture.session, "list_skill_distributions", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
	})
	require.Empty(t, listed.Distributions)

	revocation := callSkillsRefusal(t, ctx, fixture.session, "undistribute_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"plugin":          "marketing",
		"confirmed":       true,
		"idempotency_key": "revoke-as-writer",
	})
	require.Equal(t, "permission_denied", revocation.Code)
}

func TestPlatformMCPSkillsToolsRefuseAUserWithoutGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_ungranted", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: false})

	refusal := callSkillsRefusal(t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("ungranted", "Written without grants.", "Body."),
	})
	require.Equal(t, "forbidden", refusal.Code)

	for _, call := range []struct {
		name      string
		arguments map[string]any
	}{
		{name: "list_skill_feedback", arguments: map[string]any{"project_slug": fixture.project.Slug, "skill_id": uuid.NewString()}},
		{name: "list_skill_suggestions", arguments: map[string]any{"project_slug": fixture.project.Slug}},
		{name: "list_skill_suggestion_feedback", arguments: map[string]any{"project_slug": fixture.project.Slug, "change_id": uuid.NewString()}},
		{name: "list_skill_distributions", arguments: map[string]any{"project_slug": fixture.project.Slug}},
		// An existing plugin and a missing one refuse identically, so the
		// refusal cannot be used to learn which plugins the project has.
		{name: "list_skill_distributions", arguments: map[string]any{"project_slug": fixture.project.Slug, "plugin": "marketing"}},
		{name: "list_skill_distributions", arguments: map[string]any{"project_slug": fixture.project.Slug, "plugin": "nowhere"}},
	} {
		refusal = callSkillsRefusal(t, ctx, fixture.session, call.name, call.arguments)
		require.Equal(t, "forbidden", refusal.Code, call.name)
	}

	skills, err := skillsrepo.New(fixture.conn).ListSkills(ctx, skillsrepo.ListSkillsParams{ProjectID: fixture.project.ID, PageLimit: 10})
	require.NoError(t, err)
	require.Empty(t, skills)
}

type skillsVerticalFixture struct {
	conn              *pgxpool.Pool
	principal         Principal
	project           ResolvedProject
	session           *mcp.ClientSession
	marketingPluginID uuid.UUID

	// insights stands in for ClickHouse behind the insight tools. The two
	// counters are the connection and organization buckets of the diagnostics
	// budget it is metered on: an OperationBudget charges both buckets on every
	// permitted call from a connection-bearing principal, so they are counted
	// apart rather than through one shared limiter that would read as two
	// charges per call.
	insights                 *stubSkillInsightsReader
	insightsConnectionLane   *countingLimiter
	insightsOrganizationLane *countingLimiter
}

// countingLimiter always allows and counts what it was charged. It is safe to
// read from the test while the fixture's server charges it on its own
// goroutine.
type countingLimiter struct {
	mu    sync.Mutex
	calls int
}

func (l *countingLimiter) Allow(context.Context, string) (ratelimit.Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return ratelimit.Result{Allowed: true}, nil
}

func (l *countingLimiter) AllowN(context.Context, string, int) (ratelimit.Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return ratelimit.Result{Allowed: true}, nil
}

func (l *countingLimiter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

// newSkillsVerticalFixture composes the production wiring against a real
// database: the real skills management service, the Postgres target inventory,
// the real registration store as project resolver, and the runtime's own HTTP
// handler behind an MCP client. Only the OAuth token exchange is stood in for,
// because minting one would test the OAuth lane rather than this one.
type skillsVerticalOptions struct {
	// capabilityEnabled is the Platform MCP kill switch as the skills tools see
	// it. The runtime's own gate stays on so the endpoint keeps serving; this
	// isolates what a caller gets from a tool whose capability is off.
	capabilityEnabled bool
	// grantAdmin gives the acting user real organization-admin grants. Off, the
	// call travels the whole path and is refused by RBAC at the end of it.
	grantAdmin      bool
	grantSkillRead  bool
	grantSkillWrite bool
}

func newSkillsVerticalFixture(t *testing.T, ctx context.Context, name string, options skillsVerticalOptions) *skillsVerticalFixture {
	t.Helper()

	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	// The endpoint refuses a principal missing any half of its OAuth identity,
	// so the fixture presents the complete one a real connection carries.
	principal.ClientID = "client-" + uuid.NewString()
	principal.Surface = SurfacePlatformMCP

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	siteURL, err := url.Parse("https://app.getgram.test")
	require.NoError(t, err)
	skills := skillsservice.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, features, audit.NewLogger(), nil, nil, siteURL)

	_, err = featurerepo.New(conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: principal.OrganizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	features.UpdateFeatureCache(ctx, principal.OrganizationID, productfeatures.FeatureSkills, true)

	// The acting user holds real grants rather than context overrides, so the
	// test exercises the authorization the OAuth surface actually relies on.
	require.NoError(t, testrepo.New(conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID:          principal.UserID,
		Email:       principal.UserID + "@example.test",
		DisplayName: "Platform MCP skills test user",
	}))
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: principal.OrganizationID,
		UserID:         pgtype.Text{String: principal.UserID, Valid: true},
	}))
	if options.grantAdmin {
		require.NoError(t, authz.NewProvisioner(conn).ProvisionOrganizationAdmin(ctx, principal.OrganizationID, authz.InitialOrganizationAdmin{
			UserID:             principal.UserID,
			WorkOSUserID:       principal.UserID,
			WorkOSMembershipID: "membership-" + uuid.NewString(),
		}))
	} else {
		// A member of the organization holding no role. Seeding the roles without
		// assigning one is what makes this "authenticated but unauthorized"
		// rather than an organization that predates RBAC.
		require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, principal.OrganizationID))
		grantedScopes := make([]authz.Scope, 0, 2)
		if options.grantSkillRead {
			grantedScopes = append(grantedScopes, authz.ScopeSkillRead)
		}
		if options.grantSkillWrite {
			grantedScopes = append(grantedScopes, authz.ScopeSkillWrite)
		}
		for _, grantedScope := range grantedScopes {
			selector, marshalErr := authz.NewSelector(grantedScope, project.ID.String()).MarshalJSON()
			require.NoError(t, marshalErr)
			_, grantErr := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
				OrganizationID: principal.OrganizationID,
				PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
				Scope:          string(grantedScope),
				Selectors:      selector,
			})
			require.NoError(t, grantErr)
		}
	}

	plugins := pluginsrepo.New(conn)
	_, err = plugins.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	marketing, err := plugins.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		Name:           "Marketing",
		Slug:           "marketing",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       project.ID,
		OrganizationID:  principal.OrganizationID,
		CreatedByUserID: pgtype.Text{String: principal.UserID, Valid: true},
		Name:            "Support",
		Model:           "claude-sonnet-5",
		Instructions:    "Help customers.",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)

	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
	allow := func() Limiter { return &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}} }
	insights := &stubSkillInsightsReader{}
	insightsConnectionLane := &countingLimiter{}
	insightsOrganizationLane := &countingLimiter{}
	skillsSurface := NewSkillsService(
		skills,
		NewPostgresSkillTargets(conn),
		store,
		authzEngine,
		NewCatalogRegistrationGate(testGate{enabled: options.capabilityEnabled}),
		OperationBudget{Connection: allow(), Organization: allow()},
	).WithInsights(insights, OperationBudget{Connection: insightsConnectionLane, Organization: insightsOrganizationLane})

	runtimeAuthorizer := Authorizer(&testAuthorizer{})
	if options.grantAdmin || options.grantSkillRead || options.grantSkillWrite {
		runtimeAuthorizer = NewLiveOrgAdminAuthorizer(conn, authzEngine)
	}
	runtime := NewRuntimeWithLifecycle(
		logger, &testAuthenticator{principal: principal}, testGate{enabled: true}, runtimeAuthorizer,
		"", "test-cursor-key", nil, nil, nil, nil, nil, nil, nil, nil, skillsSurface, nil, nil, nil, CatalogDescriptor{},
	)
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	// The endpoint authenticates every request, so the client presents a bearer
	// token on each one exactly as a real MCP client would.
	httpClient := server.Client()
	httpClient.Transport = bearerTokenTransport{base: httpClient.Transport}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   server.URL,
		HTTPClient: httpClient,
		MaxRetries: 0,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "skills-vertical-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return &skillsVerticalFixture{
		conn:                     conn,
		principal:                principal,
		project:                  project,
		session:                  session,
		marketingPluginID:        marketing.ID,
		insights:                 insights,
		insightsConnectionLane:   insightsConnectionLane,
		insightsOrganizationLane: insightsOrganizationLane,
	}
}

// insightsLaneCharges reports how often each bucket of the diagnostics budget
// was charged, connection then organization.
func (f *skillsVerticalFixture) insightsLaneCharges() (int, int) {
	return f.insightsConnectionLane.count(), f.insightsOrganizationLane.count()
}

// createInsightsSkillVersion records one immutable version so the registry has
// something real for the insight tools to resolve.
func createInsightsSkillVersion(t *testing.T, ctx context.Context, conn *pgxpool.Pool, fixture *skillsVerticalFixture, skillID uuid.UUID, body string) skillsrepo.SkillVersion {
	t.Helper()

	version, err := skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          skillsFixtureManifest("measured", "Measured by the judge.", body),
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "Measured by the judge.", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  fixture.principal.UserID,
		ProjectID:        fixture.project.ID,
		SkillID:          skillID,
	})
	require.NoError(t, err)
	return version
}

// The insight tools resolve which skills to report through the registry under
// the caller's real grants and then read ClickHouse for exactly those IDs. This
// drives both tools through the endpoint as an organization admin against a
// real skill and checks the ClickHouse read was scoped to what the registry
// returned.
func TestPlatformMCPSkillInsightsRankAndCompareUnderRealGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skill_insights_admin", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: true})
	skill, err := skillsrepo.New(fixture.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID:   fixture.project.ID,
		Name:        "measured",
		DisplayName: "Measured",
		Summary:     pgtype.Text{String: "Measured by the judge.", Valid: true},
	})
	require.NoError(t, err)
	older := createInsightsSkillVersion(t, ctx, fixture.conn, fixture, skill.ID, "First body.")
	newer := createInsightsSkillVersion(t, ctx, fixture.conn, fixture, skill.ID, "Second body.")
	fixture.insights.SetRows([]telemetryrepo.SkillInsightBucket{{
		SkillID: skill.ID.String(), SkillVersionID: newer.ID.String(),
		ActivationCount: 3, ActivatedSessions: 2, TotalSessionCost: 1.5,
		ScoredSessions: 1, ScoreSum: 0.8, EstimatedMinutesSavedSum: 12, EstimatedMinutesSamples: 1,
	}})

	ranked := callSkillsTool[ListSkillInsightsOutput](t, ctx, fixture.session, "list_skill_insights", map[string]any{
		"project_slug": fixture.project.Slug,
	})
	require.Len(t, ranked.Skills, 1)
	require.Equal(t, skill.ID.String(), ranked.Skills[0].ID)
	require.Equal(t, "measured", ranked.Skills[0].Name)
	require.EqualValues(t, 3, ranked.Skills[0].Metrics.Activations)
	require.NotNil(t, ranked.Skills[0].Metrics.Efficacy)
	require.EqualValues(t, 1, ranked.Skills[0].Metrics.Efficacy.EstimatedMinutesSavedSamples)
	params := fixture.insights.LastParams()
	require.NotNil(t, params)
	require.Equal(t, fixture.principal.OrganizationID, params.OrganizationID)
	require.Equal(t, fixture.project.ID.String(), params.ProjectID)
	require.Equal(t, []string{skill.ID.String()}, params.SkillIDs)

	compared := callSkillsTool[CompareSkillVersionsOutput](t, ctx, fixture.session, "compare_skill_versions", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     skill.ID.String(),
	})
	require.Equal(t, skill.ID.String(), compared.SkillID)
	require.Equal(t, "measured", compared.SkillName)
	// The skill's own figures are the same whichever tool reported them.
	require.Equal(t, ranked.Skills[0].Metrics.Activations, compared.Metrics.Activations)
	require.Len(t, compared.Versions, 2, "comparison lists every registry version, used or not")
	byID := map[string]SkillVersionInsight{}
	for _, version := range compared.Versions {
		byID[version.ID] = version
	}
	require.EqualValues(t, 3, byID[newer.ID.String()].Metrics.Activations)
	require.Zero(t, byID[older.ID.String()].Metrics.Activations)
	require.NotEmpty(t, byID[older.ID.String()].CreatedAt, "creation time comes from the registry")
	connectionCharges, organizationCharges := fixture.insightsLaneCharges()
	require.Equal(t, 2, connectionCharges, "each read charges the diagnostics lane's connection bucket once")
	require.Equal(t, 2, organizationCharges, "each read charges the diagnostics lane's organization bucket once")
}

// A caller's grants decide what the insight tools may read. A member holding
// no role reaches the handler and is refused by the skills service's own RBAC,
// so ClickHouse is never read; a member holding only skill:read is turned back
// at the organization-admin gate before the handler spends anything at all.
func TestPlatformMCPSkillInsightsRefuseCallersWithoutTheRightGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ungranted := newSkillsVerticalFixture(t, ctx, "platform_mcp_skill_insights_ungranted", skillsVerticalOptions{capabilityEnabled: true})
	refusal := callSkillsRefusal(t, ctx, ungranted.session, "list_skill_insights", map[string]any{
		"project_slug": ungranted.project.Slug,
	})
	require.Equal(t, "forbidden", refusal.Code)
	require.Nil(t, ungranted.insights.LastParams(), "ClickHouse is never read for a caller the registry refuses")
	// The lane is charged before the registry refuses, as every skills call
	// charges its allowance before RBAC: an unauthorized caller cannot probe
	// for free, and the refusal reveals nothing about what it would have read.
	connectionCharges, organizationCharges := ungranted.insightsLaneCharges()
	require.Equal(t, 1, connectionCharges, "the refused call charged the connection bucket once")
	require.Equal(t, 1, organizationCharges, "the refused call charged the organization bucket once")

	readerOnly := newSkillsVerticalFixture(t, ctx, "platform_mcp_skill_insights_reader", skillsVerticalOptions{capabilityEnabled: true, grantSkillRead: true})
	refusal = callSkillsRefusal(t, ctx, readerOnly.session, "compare_skill_versions", map[string]any{
		"project_slug": readerOnly.project.Slug,
		"skill_id":     uuid.NewString(),
	})
	require.Equal(t, "permission_denied", refusal.Code)
	require.Nil(t, readerOnly.insights.LastParams())
	connectionCharges, organizationCharges = readerOnly.insightsLaneCharges()
	require.Zero(t, connectionCharges, "the admin gate refuses before the handler charges the connection bucket")
	require.Zero(t, organizationCharges, "the admin gate refuses before the handler charges the organization bucket")
}

// callSkillsTool calls one tool and decodes its structured result, failing the
// test if the call came back as a refusal.
func callSkillsTool[Out any](t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) Out {
	t.Helper()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	require.Falsef(t, result.IsError, "tool %q refused: %s", name, skillsToolText(t, result))

	var out Out
	require.NoError(t, json.Unmarshal([]byte(skillsToolText(t, result)), &out))
	return out
}

// callSkillsRefusal calls one tool that is expected to refuse, and returns the
// structured refusal. A refusal must arrive as a readable result rather than a
// transport error, or the model loses the reason.
func callSkillsRefusal(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) skillsRefusalResult {
	t.Helper()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	require.Truef(t, result.IsError, "tool %q was expected to refuse", name)

	var refusal skillsRefusalResult
	require.NoError(t, json.Unmarshal([]byte(skillsToolText(t, result)), &refusal))
	require.NotEmpty(t, refusal.Message)
	return refusal
}

func skillsToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func skillTargetNames(targets []SkillTarget) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

func skillsFixtureManifest(name, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, body)
}

type bearerTokenTransport struct{ base http.RoundTripper }

func (t bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer platform-mcp-test-token")
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

func listSkillDistributions(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, skillID string) []skillsrepo.ListActiveSkillDistributionsRow {
	t.Helper()

	rows, err := skillsrepo.New(conn).ListActiveSkillDistributions(ctx, skillsrepo.ListActiveSkillDistributionsParams{
		ProjectID:       projectID,
		SkillID:         uuid.NullUUID{UUID: uuid.MustParse(skillID), Valid: true},
		PluginID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       50,
	})
	require.NoError(t, err)
	return rows
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
