package mcpskills_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestResolverListServesLinkedSkill(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, versionID, _ := servedSkill(t, ctx, f, "git-workflow")
	content := skillContent("git-workflow", "Serves git-workflow.")

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, skillID, entry.SkillID)
	require.Equal(t, f.projectID, entry.ProjectID)
	require.Equal(t, versionID, entry.VersionID)
	require.Equal(t, f.projectSlug, entry.ProjectSlug)
	require.Equal(t, "git-workflow", entry.Name)
	require.Equal(t, content, entry.Content)
	require.Equal(t, rawSHA256(content), entry.RawSHA256)
	require.Equal(t, "skill://"+f.projectSlug+"/git-workflow/SKILL.md", entry.URI())
	require.Equal(t, "sha256:"+rawSHA256(content), entry.Digest())
	require.Equal(t, len(content), entry.Size())
	require.Equal(t, map[string]any{"name": "git-workflow", "description": "Serves git-workflow."}, entry.Frontmatter)
}

func TestResolverListOtherServerExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherServerID := createServer(t, ctx, f, f.projectID, "other")
	skillID := createSkill(t, ctx, f, f.projectID, "elsewhere")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("elsewhere", "Linked to another server."))
	link(t, ctx, f, f.projectID, skillID, mcpskills.MCPServerTarget(otherServerID), nil)

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Empty(t, entries)

	entries, err = f.resolver.List(ctx, f.scopeFor(mcpskills.MCPServerTarget(otherServerID)))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestResolverListDeletedLinkExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, linkID := servedSkill(t, ctx, f, "unlinked")

	removed, err := mcpskills.DeleteLink(ctx, f.conn, f.organizationID, f.projectID, linkID)
	require.NoError(t, err)
	require.True(t, removed)

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Empty(t, entries)

	removed, err = mcpskills.DeleteLink(ctx, f.conn, f.organizationID, f.projectID, linkID)
	require.NoError(t, err)
	require.False(t, removed, "a deleted link is not deleted twice")

	// The live-link uniqueness frees up once the link is gone.
	link(t, ctx, f, f.projectID, skillID, mcpskills.MCPServerTarget(f.serverID), nil)
	entries, err = f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestDeleteLinkScopedToOrganization(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, other := newFixture(t)
	_, _, linkID := servedSkill(t, ctx, f, "kept")

	removed, err := mcpskills.DeleteLink(ctx, f.conn, other.organizationID, f.projectID, linkID)
	require.NoError(t, err)
	require.False(t, removed, "another organization cannot delete the link")

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestResolverListDeletedProjectExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherProjectID := createProject(t, ctx, f, "gone-"+uuid.NewString()[:8])
	skillID := createSkill(t, ctx, f, otherProjectID, "orphaned")
	addVersion(t, ctx, f, otherProjectID, skillID, skillContent("orphaned", "Linked from a project that is deleted."))
	link(t, ctx, f, otherProjectID, skillID, mcpskills.OrganizationGatewayTarget(), nil)

	entries, err := f.resolver.List(ctx, f.scopeFor(mcpskills.OrganizationGatewayTarget()))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	_, err = projectsrepo.New(f.conn).DeleteProject(ctx, otherProjectID)
	require.NoError(t, err)

	entries, err = f.resolver.List(ctx, f.scopeFor(mcpskills.OrganizationGatewayTarget()))
	require.NoError(t, err)
	require.Empty(t, entries, "a deleted project's links are not served")
}

func TestCreateLinkRejectsDuplicateLiveLink(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "twice")

	_, err := mcpskills.CreateLink(ctx, f.conn, mcpskills.NewLink{
		OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
		PinnedVersionID: nil, Target: mcpskills.MCPServerTarget(f.serverID), CreatedByUserID: testUserID,
	})
	require.Error(t, err)

	link(t, ctx, f, f.projectID, skillID, mcpskills.OrganizationGatewayTarget(), nil)
	_, err = mcpskills.CreateLink(ctx, f.conn, mcpskills.NewLink{
		OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
		PinnedVersionID: nil, Target: mcpskills.OrganizationGatewayTarget(), CreatedByUserID: testUserID,
	})
	require.Error(t, err, "a row-less target is unique per kind even though its id columns are NULL")
}

func TestCreateLinkRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID := createSkill(t, ctx, f, f.projectID, "nowhere")

	for _, target := range []mcpskills.Target{
		{Kind: mcpskills.TargetMCPServer, ID: uuid.Nil},
		{Kind: mcpskills.TargetOrganizationGateway, ID: uuid.New()},
		{Kind: "project_gateway", ID: uuid.Nil},
	} {
		_, err := mcpskills.CreateLink(ctx, f.conn, mcpskills.NewLink{
			OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
			PinnedVersionID: nil, Target: target, CreatedByUserID: testUserID,
		})
		require.ErrorIs(t, err, mcpskills.ErrInvalidTarget, "%+v", target)
	}
}

func TestCreateLinkRejectsServerInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherProjectID := createProject(t, ctx, f, "other-"+uuid.NewString()[:8])
	foreignServerID := createServer(t, ctx, f, otherProjectID, "foreign")
	skillID := createSkill(t, ctx, f, f.projectID, "crossing")

	_, err := mcpskills.CreateLink(ctx, f.conn, mcpskills.NewLink{
		OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
		PinnedVersionID: nil, Target: mcpskills.MCPServerTarget(foreignServerID), CreatedByUserID: testUserID,
	})
	require.Error(t, err, "a skill cannot be linked to a server in another project")
}

func TestResolverListMetaMCPServerTarget(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	gatewayID := createMetaServer(t, ctx, f, "gateway")
	skillID := createSkill(t, ctx, f, f.projectID, "gateway-skill")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("gateway-skill", "Linked to the gateway."))
	link(t, ctx, f, f.projectID, skillID, mcpskills.MetaMCPServerTarget(gatewayID), nil)

	entries, err := f.resolver.List(ctx, f.scopeFor(mcpskills.MetaMCPServerTarget(gatewayID)))
	require.NoError(t, err)
	require.Equal(t, []string{"gateway-skill"}, entryNames(entries))

	entries, err = f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Empty(t, entries, "a gateway link does not reach a server")

	entries, err = f.resolver.List(ctx, f.scopeFor(mcpskills.MetaMCPServerTarget(createMetaServer(t, ctx, f, "other"))))
	require.NoError(t, err)
	require.Empty(t, entries, "nor another gateway")
}

func TestResolverListOrganizationGatewaySpansProjects(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherProjectID := createProject(t, ctx, f, "other-"+uuid.NewString()[:8])
	skillID := createSkill(t, ctx, f, otherProjectID, "org-wide")
	addVersion(t, ctx, f, otherProjectID, skillID, skillContent("org-wide", "Linked to the organization gateway."))
	link(t, ctx, f, otherProjectID, skillID, mcpskills.OrganizationGatewayTarget(), nil)
	_, _, _ = servedSkill(t, ctx, f, "server-only")

	entries, err := f.resolver.List(ctx, f.scopeFor(mcpskills.OrganizationGatewayTarget()))
	require.NoError(t, err)
	require.Equal(t, []string{"org-wide"}, entryNames(entries))
	require.Equal(t, otherProjectID, entries[0].ProjectID)

	entries, err = f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Equal(t, []string{"server-only"}, entryNames(entries), "an organization gateway link does not reach a server")
}

func TestResolverListOtherOrganizationExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, other := newFixture(t)
	_, _, _ = servedSkill(t, ctx, other, "foreign")
	foreignSkillID := createSkill(t, ctx, other, other.projectID, "foreign-org-wide")
	addVersion(t, ctx, other, other.projectID, foreignSkillID, skillContent("foreign-org-wide", "Another organization's."))
	link(t, ctx, other, other.projectID, foreignSkillID, mcpskills.OrganizationGatewayTarget(), nil)

	entries, err := f.resolver.List(ctx, f.scopeFor(mcpskills.OrganizationGatewayTarget()))
	require.NoError(t, err)
	require.Empty(t, entries, "another organization's gateway links are never served")

	scope := f.scopeFor(mcpskills.OrganizationGatewayTarget())
	scope.MemberMCPServerIDs = []uuid.UUID{other.serverID}
	entries, err = f.resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Empty(t, entries, "a member server id from another organization serves nothing")
}

func TestResolverListGatewayInheritsMemberServers(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	gatewayID := createMetaServer(t, ctx, f, "gateway")
	unreachableID := createServer(t, ctx, f, f.projectID, "unreachable")
	_, _, _ = servedSkill(t, ctx, f, "member-skill")
	ownID := createSkill(t, ctx, f, f.projectID, "own-skill")
	addVersion(t, ctx, f, f.projectID, ownID, skillContent("own-skill", "Linked to the gateway itself."))
	link(t, ctx, f, f.projectID, ownID, mcpskills.MetaMCPServerTarget(gatewayID), nil)
	hiddenID := createSkill(t, ctx, f, f.projectID, "hidden-skill")
	addVersion(t, ctx, f, f.projectID, hiddenID, skillContent("hidden-skill", "Linked to a server the caller cannot reach."))
	link(t, ctx, f, f.projectID, hiddenID, mcpskills.MCPServerTarget(unreachableID), nil)

	for _, target := range []mcpskills.Target{mcpskills.MetaMCPServerTarget(gatewayID), mcpskills.OrganizationGatewayTarget()} {
		scope := f.scopeFor(target)
		scope.MemberMCPServerIDs = []uuid.UUID{f.serverID}
		entries, err := f.resolver.List(ctx, scope)
		require.NoError(t, err)

		want := []string{"member-skill"}
		if target.Kind == mcpskills.TargetMetaMCPServer {
			want = []string{"member-skill", "own-skill"}
		}
		require.Equal(t, want, entryNames(entries), "%s serves its own links plus the members it passes", target.Kind)
	}
}

func TestResolverListSameNameAcrossProjectsServesEach(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherSlug := "other-" + uuid.NewString()[:8]
	otherProjectID := createProject(t, ctx, f, otherSlug)
	firstID := createSkill(t, ctx, f, f.projectID, "shared-name")
	first := addVersion(t, ctx, f, f.projectID, firstID, skillContent("shared-name", "First project."))
	link(t, ctx, f, f.projectID, firstID, mcpskills.OrganizationGatewayTarget(), &first)
	secondID := createSkill(t, ctx, f, otherProjectID, "shared-name")
	second := addVersion(t, ctx, f, otherProjectID, secondID, skillContent("shared-name", "Second project."))
	link(t, ctx, f, otherProjectID, secondID, mcpskills.OrganizationGatewayTarget(), nil)

	scope := f.scopeFor(mcpskills.OrganizationGatewayTarget())
	entries, err := f.resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Len(t, entries, 2, "skills that share a name in different projects are both served")
	require.Equal(t, []uuid.UUID{firstID, secondID}, []uuid.UUID{entries[0].SkillID, entries[1].SkillID}, "sorted by project slug")
	require.Equal(t, first, entries[0].VersionID, "a pin on one project's skill does not decide the other's")
	require.Equal(t, second, entries[1].VersionID)
	require.Equal(t, f.uri("shared-name"), entries[0].URI())
	require.Equal(t, mcpskills.SkillURI(otherSlug, "shared-name"), entries[1].URI())

	entry, err := f.resolver.Get(ctx, scope, mcpskills.SkillURI(otherSlug, "shared-name"))
	require.NoError(t, err)
	require.Equal(t, secondID, entry.SkillID)

	_, err = f.resolver.Get(ctx, scope, "skill://shared-name/SKILL.md")
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound, "a URI without its project never resolves")
}

func TestResolverListLatestTracksNewestValidVersion(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "tracked")
	second := addVersion(t, ctx, f, f.projectID, skillID, skillContent("tracked", "Second version."))

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, second, entries[0].VersionID)
}

func TestResolverListPinnedVersionBeatsLatest(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	gatewayID := createMetaServer(t, ctx, f, "gateway")
	skillID, first, _ := servedSkill(t, ctx, f, "pinned")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("pinned", "Second version."))
	link(t, ctx, f, f.projectID, skillID, mcpskills.MetaMCPServerTarget(gatewayID), &first)

	scope := f.scopeFor(mcpskills.MetaMCPServerTarget(gatewayID))
	scope.MemberMCPServerIDs = []uuid.UUID{f.serverID}
	entries, err := f.resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, first, entries[0].VersionID, "the pin beats the newer version the server link tracks")
}

func TestResolverListConflictingPinsServeNewestAndWarn(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	var logs bytes.Buffer
	resolver := mcpskills.NewResolver(slog.New(slog.NewTextHandler(&logs, nil)), f.conn)

	gatewayID := createMetaServer(t, ctx, f, "gateway")
	skillID := createSkill(t, ctx, f, f.projectID, "contested")
	first := addVersion(t, ctx, f, f.projectID, skillID, skillContent("contested", "First version."))
	second := addVersion(t, ctx, f, f.projectID, skillID, skillContent("contested", "Second version."))
	link(t, ctx, f, f.projectID, skillID, mcpskills.MetaMCPServerTarget(gatewayID), &first)
	link(t, ctx, f, f.projectID, skillID, mcpskills.MCPServerTarget(f.serverID), &second)

	scope := f.scopeFor(mcpskills.MetaMCPServerTarget(gatewayID))
	scope.MemberMCPServerIDs = []uuid.UUID{f.serverID}
	entries, err := resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, second, entries[0].VersionID)
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "pinned to different versions")
	require.Contains(t, logs.String(), "gram.mcp.skill.pin_conflict_count=1")
	require.Contains(t, logs.String(), "gram.mcp.skill.target_kind=meta_mcp_server")
}

func TestResolverListArchivedSkillExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "archived")

	_, err := skillsrepo.New(f.conn).ArchiveSkill(ctx, skillsrepo.ArchiveSkillParams{ProjectID: f.projectID, ID: skillID})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestResolverListInvalidVersionExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "half-valid")
	invalid := "---\nname: half-valid\n---\n\nMissing description.\n"
	_, err := skillsrepo.New(f.conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: invalid, CanonicalSha256: rawSHA256(invalid), RawSha256: rawSHA256(invalid), Description: pgtype.Text{},
		Metadata: []byte("{}"), SpecValid: false, ValidationErrors: []byte(`[{"code":"required","field":"description","message":"description is required"}]`),
		CreatedByUserID: testUserID, ProjectID: f.projectID, SkillID: skillID,
	})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, skillContent("half-valid", "Serves half-valid."), entries[0].Content, "the newer invalid version is never served")
}

func TestResolverListPaddedFrontmatterNameExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "padded")
	// Validation trims the name, so a quoted name with trailing whitespace is
	// stored valid under "padded" while its frontmatter keeps the padding.
	padded := "---\nname: \"padded \"\ndescription: Serves padded.\n---\n\n# padded\n"
	addVersion(t, ctx, f, f.projectID, skillID, padded)

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Empty(t, entries, "an entry whose frontmatter name differs from its URI is never served")

	_, err = f.resolver.Get(ctx, f.scope(), f.uri("padded"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)
}

func TestResolverListSortsByName(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "zeta")
	_, _, _ = servedSkill(t, ctx, f, "alpha")
	_, _, _ = servedSkill(t, ctx, f, "mid")

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "mid", "zeta"}, entryNames(entries))
}

func TestResolverListRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, err := f.resolver.List(ctx, f.scopeFor(mcpskills.Target{Kind: mcpskills.TargetMCPServer, ID: uuid.Nil}))
	require.ErrorIs(t, err, mcpskills.ErrInvalidTarget)
}

func TestResolverGet(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, versionID, _ := servedSkill(t, ctx, f, "fetched")
	_, _, _ = servedSkill(t, ctx, f, "other")

	entry, err := f.resolver.Get(ctx, f.scope(), f.uri("fetched"))
	require.NoError(t, err)
	require.Equal(t, skillID, entry.SkillID)
	require.Equal(t, versionID, entry.VersionID)
	require.Equal(t, "fetched", entry.Frontmatter["name"])

	_, err = f.resolver.Get(ctx, f.scope(), f.uri("missing"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)

	_, err = f.resolver.Get(ctx, f.scope(), mcpskills.SkillURI("another-project", "fetched"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound, "the project segment must match the skill's project")

	_, err = f.resolver.Get(ctx, f.scope(), "skill://"+f.projectSlug+"/fetched/references/notes.md")
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)

	_, err = f.resolver.Get(ctx, f.scopeFor(mcpskills.OrganizationGatewayTarget()), f.uri("fetched"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound, "a skill not linked to the surface cannot be fetched by URI either")
}

func TestRecordReadIdempotentPerSession(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, versionID, _ := servedSkill(t, ctx, f, "observed")
	entry, err := f.resolver.Get(ctx, f.scope(), f.uri("observed"))
	require.NoError(t, err)
	seenAt := time.Now().UTC().Truncate(time.Microsecond)
	read := mcpskills.Read{Entry: *entry, UserID: testUserID, SessionID: "mcp-session-1", SeenAt: seenAt}

	written, err := f.resolver.RecordRead(ctx, read)
	require.NoError(t, err)
	require.True(t, written)

	again, err := f.resolver.RecordRead(ctx, read)
	require.NoError(t, err)
	require.False(t, again, "the same content in the same session counts once")

	observations, err := testrepo.New(f.conn).ListMCPSkillObservations(ctx, testrepo.ListMCPSkillObservationsParams{ProjectID: f.projectID, Provider: mcpskills.ObservationProvider})
	require.NoError(t, err)
	require.Len(t, observations, 1)

	row := observations[0]
	require.Equal(t, "mcp:mcp-session-1:"+entry.RawSHA256, row.IdempotencyKey.String)
	require.Equal(t, "mcp", row.Provider)
	require.Equal(t, testUserID, row.UserID.String)
	require.Equal(t, "mcp-session-1", row.SessionID.String)
	require.Equal(t, "observed", row.SkillName)
	require.Equal(t, "mcp", row.Source.String)
	require.Equal(t, "mcp", row.SourceLevel.String)
	require.Equal(t, f.uri("observed"), row.SourcePath.String)
	require.Equal(t, entry.RawSHA256, row.RawSha256.String)
	require.True(t, seenAt.Equal(row.SeenAt.Time))
	require.Equal(t, conv.ToNullUUID(skillID), row.SkillID)
	require.Equal(t, conv.ToNullUUID(versionID), row.SkillVersionID)
	require.True(t, row.ReconciledAt.Valid, "an MCP read arrives already reconciled")
	require.False(t, row.ReconcileErrorCode.Valid)
	require.True(t, row.MetricsSyncedAt.Valid, "an MCP session is nothing for the ClickHouse session sync to map, so the row is born synced")
	require.True(t, row.EfficacyEnqueuedAt.Valid, "an MCP session names no transcript to score, so the row is born enqueued")

	skill, err := skillsrepo.New(f.conn).GetSkill(ctx, skillsrepo.GetSkillParams{ProjectID: f.projectID, ID: skillID})
	require.NoError(t, err)
	require.Equal(t, int64(1), skill.SeenCount)
	require.True(t, seenAt.Equal(skill.FirstSeenAt.Time))
	require.True(t, seenAt.Equal(skill.LastSeenAt.Time))

	later := read
	later.SessionID = "mcp-session-2"
	later.SeenAt = seenAt.Add(time.Minute)
	written, err = f.resolver.RecordRead(ctx, later)
	require.NoError(t, err)
	require.True(t, written)

	skill, err = skillsrepo.New(f.conn).GetSkill(ctx, skillsrepo.GetSkillParams{ProjectID: f.projectID, ID: skillID})
	require.NoError(t, err)
	require.Equal(t, int64(2), skill.SeenCount)
	require.True(t, seenAt.Equal(skill.FirstSeenAt.Time))
	require.True(t, later.SeenAt.Equal(skill.LastSeenAt.Time))
}

func TestRecordReadLandsInSkillProject(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherSlug := "other-" + uuid.NewString()[:8]
	otherProjectID := createProject(t, ctx, f, otherSlug)
	skillID := createSkill(t, ctx, f, otherProjectID, "org-read")
	addVersion(t, ctx, f, otherProjectID, skillID, skillContent("org-read", "Read through the organization gateway."))
	link(t, ctx, f, otherProjectID, skillID, mcpskills.OrganizationGatewayTarget(), nil)

	entry, err := f.resolver.Get(ctx, f.scopeFor(mcpskills.OrganizationGatewayTarget()), mcpskills.SkillURI(otherSlug, "org-read"))
	require.NoError(t, err)
	written, err := f.resolver.RecordRead(ctx, mcpskills.Read{Entry: *entry, UserID: testUserID, SessionID: "s", SeenAt: time.Now()})
	require.NoError(t, err)
	require.True(t, written)

	observations, err := testrepo.New(f.conn).ListMCPSkillObservations(ctx, testrepo.ListMCPSkillObservationsParams{ProjectID: otherProjectID, Provider: mcpskills.ObservationProvider})
	require.NoError(t, err)
	require.Len(t, observations, 1, "a gateway read is recorded in the project that owns the skill")
}

func TestRecordReadWithoutSessionCountsEveryRead(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "sessionless")
	entry, err := f.resolver.Get(ctx, f.scope(), f.uri("sessionless"))
	require.NoError(t, err)
	read := mcpskills.Read{Entry: *entry, UserID: "", SessionID: "", SeenAt: time.Now()}

	for range 2 {
		written, err := f.resolver.RecordRead(ctx, read)
		require.NoError(t, err)
		require.True(t, written)
	}

	observations, err := testrepo.New(f.conn).ListMCPSkillObservations(ctx, testrepo.ListMCPSkillObservationsParams{ProjectID: f.projectID, Provider: mcpskills.ObservationProvider})
	require.NoError(t, err)
	require.Len(t, observations, 2)
	require.False(t, observations[0].IdempotencyKey.Valid)
	require.False(t, observations[0].UserID.Valid)
	require.False(t, observations[0].SessionID.Valid)
}

func TestRecordReadRejectsUnknownVersion(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "mismatched")
	entry, err := f.resolver.Get(ctx, f.scope(), f.uri("mismatched"))
	require.NoError(t, err)
	entry.VersionID = uuid.New()

	written, err := f.resolver.RecordRead(ctx, mcpskills.Read{Entry: *entry, UserID: "", SessionID: "s", SeenAt: time.Now()})
	require.NoError(t, err)
	require.False(t, written, "a version that is not the skill's records nothing")
}

func TestResolverRequiresLiveMembership(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "for-members")

	resolveFor := func(userID string) []mcpskills.Entry {
		t.Helper()
		scope := f.scope()
		scope.UserID = userID
		entries, err := f.resolver.List(ctx, scope)
		require.NoError(t, err)
		return entries
	}
	require.Len(t, resolveFor(testUserID), 1, "an active member receives the server's skills")
	require.Empty(t, resolveFor(""), "an anonymous caller receives nothing")
	require.Empty(t, resolveFor("user_unknown"), "an unknown caller receives nothing")

	scope := f.scope()
	scope.UserID = ""
	_, err := f.resolver.Get(ctx, scope, f.uri("for-members"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)

	// A user who exists but was never a member: a valid credential for the
	// organization does not make them a recipient of its skills.
	_, err = usersrepo.New(f.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: "user_outsider", Email: "outsider@example.test", DisplayName: "Outsider", PhotoUrl: pgtype.Text{}, Admin: false,
	})
	require.NoError(t, err)
	require.Empty(t, resolveFor("user_outsider"))

	// A member of another organization is an outsider here.
	otherOrg := "org-other-" + uuid.NewString()[:8]
	_, err = orgrepo.New(f.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: otherOrg, Name: otherOrg, Slug: otherOrg, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	_, err = orgrepo.New(f.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: otherOrg, UserID: conv.ToPGText("user_outsider"),
	})
	require.NoError(t, err)
	require.Empty(t, resolveFor("user_outsider"))

	// A member removed from the organization stops receiving its skills the
	// moment the membership ends, whatever credential they still hold.
	err = orgrepo.New(f.conn).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: f.organizationID, UserID: conv.ToPGText(testUserID),
	})
	require.NoError(t, err)
	require.Empty(t, resolveFor(testUserID), "a removed member is served nothing")

	// A deleted user is no recipient even while their membership row stands.
	addMember(t, ctx, f, "user_deleted", "deleted@example.test")
	require.Len(t, resolveFor("user_deleted"), 1)
	err = usersrepo.New(f.conn).SoftDeleteOrganizationUserFixture(ctx, usersrepo.SoftDeleteOrganizationUserFixtureParams{
		UserID: "user_deleted", OrganizationID: f.organizationID,
	})
	require.NoError(t, err)
	require.Empty(t, resolveFor("user_deleted"), "a deleted user is served nothing")
}

func TestResolverListCapCountsSkillsNotLinks(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	var logs bytes.Buffer
	resolver := mcpskills.NewResolver(slog.New(slog.NewTextHandler(&logs, nil)), f.conn)
	mcpskills.SetServedSkillLimit(resolver, 3)

	// "alpha" reaches the gateway through three links; "beta" and "gamma"
	// through one each. Three skills fit the cap of three exactly, however
	// many links carry them, and an exact fit is not a truncation.
	gatewayID := createMetaServer(t, ctx, f, "gateway")
	secondServerID := createServer(t, ctx, f, f.projectID, "second")
	alphaID, _, _ := servedSkill(t, ctx, f, "alpha")
	link(t, ctx, f, f.projectID, alphaID, mcpskills.MetaMCPServerTarget(gatewayID), nil)
	link(t, ctx, f, f.projectID, alphaID, mcpskills.MCPServerTarget(secondServerID), nil)
	_, _, _ = servedSkill(t, ctx, f, "beta")
	_, _, _ = servedSkill(t, ctx, f, "gamma")

	scope := f.scopeFor(mcpskills.MetaMCPServerTarget(gatewayID))
	scope.MemberMCPServerIDs = []uuid.UUID{f.serverID, secondServerID}
	entries, err := resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta", "gamma"}, entryNames(entries))
	require.NotContains(t, logs.String(), "level=WARN", "exactly the cap's worth of skills is not a truncation")

	// A fourth skill is one past the cap: the first three by name are served
	// and the truncation is logged.
	_, _, _ = servedSkill(t, ctx, f, "delta")
	entries, err = resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta", "delta"}, entryNames(entries))
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "exceeds its cap")

	// Get is unaffected by the cap: it names one skill.
	entry, err := resolver.Get(ctx, scope, f.uri("gamma"))
	require.NoError(t, err)
	require.Equal(t, "gamma", entry.Name)
}

func entryNames(entries []mcpskills.Entry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}
