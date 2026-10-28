package mcpskills_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestResolverListAssignedByEmailPrincipal(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, versionID, pluginID := servedSkill(t, ctx, f, "git-workflow", emailPrincipal(testUserEmail))
	content := skillContent("git-workflow", "Serves git-workflow.")

	entries, err := f.resolver.List(ctx, f.scope(emailPrincipal(testUserEmail)))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, skillID, entry.SkillID)
	require.Equal(t, versionID, entry.VersionID)
	require.Equal(t, pluginID, entry.PluginID)
	require.Equal(t, "git-workflow", entry.Name)
	require.Equal(t, content, entry.Content)
	require.Equal(t, rawSHA256(content), entry.RawSHA256)
	require.False(t, entry.Pinned)
	require.Equal(t, "skill://git-workflow/SKILL.md", entry.URI())
	require.Equal(t, "sha256:"+rawSHA256(content), entry.Digest())
	require.Equal(t, len(content), entry.Size())
	require.Equal(t, map[string]any{"name": "git-workflow", "description": "Serves git-workflow."}, entry.Frontmatter)

	other, err := f.resolver.List(ctx, f.scope(emailPrincipal("bob@example.test")))
	require.NoError(t, err)
	require.Empty(t, other)
}

func TestResolverListAssignedByWildcard(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "wildcard-skill", urn.PrincipalWildcard)

	entries, err := f.resolver.List(ctx, f.scope(emailPrincipal("anyone@example.test"), urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "wildcard-skill", entries[0].Name)
}

func TestResolverListUnassignedPluginExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID := createSkill(t, ctx, f, f.projectID, "unassigned")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("unassigned", "Nobody is assigned."))
	pluginID := createPlugin(t, ctx, f, f.projectID, "unassigned-plugin")
	containToolset(t, ctx, f, pluginID, f.toolsetID)
	distribute(t, ctx, f, f.projectID, skillID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	entries, err := f.resolver.List(ctx, f.scope(emailPrincipal(testUserEmail), urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestResolverListPluginContainingOtherToolsetExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherToolsetID := createToolset(t, ctx, f, f.projectID, "other-toolset")
	skillID := createSkill(t, ctx, f, f.projectID, "elsewhere")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("elsewhere", "Carried by another toolset."))
	pluginID := createPlugin(t, ctx, f, f.projectID, "elsewhere-plugin")
	containToolset(t, ctx, f, pluginID, otherToolsetID)
	assign(t, ctx, f, pluginID, urn.PrincipalWildcard)
	distribute(t, ctx, f, f.projectID, skillID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries)

	otherScope := mcpskills.Scope{ProjectID: f.projectID, ToolsetID: otherToolsetID, MCPServerID: nil, PrincipalURNs: []string{urn.PrincipalWildcard}}
	entries, err = f.resolver.List(ctx, otherScope)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestResolverListDeletedPluginMemberExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID := createSkill(t, ctx, f, f.projectID, "detached")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("detached", "The toolset was removed from the plugin."))
	pluginID := createPlugin(t, ctx, f, f.projectID, "detached-plugin")
	memberID := containToolset(t, ctx, f, pluginID, f.toolsetID)
	assign(t, ctx, f, pluginID, urn.PrincipalWildcard)
	distribute(t, ctx, f, f.projectID, skillID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	_, err := pluginsrepo.New(f.conn).RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: memberID, PluginID: pluginID})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestResolverListMCPServerMembership(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	serverID := createFrontingServer(t, ctx, f)
	skillID := createSkill(t, ctx, f, f.projectID, "via-server")
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("via-server", "Carried through the fronting server."))
	pluginID := createPlugin(t, ctx, f, f.projectID, "server-plugin")
	containMCPServer(t, ctx, f, pluginID, serverID)
	assign(t, ctx, f, pluginID, urn.PrincipalWildcard)
	distribute(t, ctx, f, f.projectID, skillID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	withoutServer, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, withoutServer, "a plugin containing only the fronting server does not match a toolset-only scope")

	scope := f.scope(urn.PrincipalWildcard)
	scope.MCPServerID = &serverID
	withServer, err := f.resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Len(t, withServer, 1)
	require.Equal(t, "via-server", withServer[0].Name)

	unrelated := uuid.New()
	scope.MCPServerID = &unrelated
	none, err := f.resolver.List(ctx, scope)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestResolverListLatestTracksNewestValidVersion(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "tracked", urn.PrincipalWildcard)
	second := addVersion(t, ctx, f, f.projectID, skillID, skillContent("tracked", "Second version."))

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, second, entries[0].VersionID)
	require.False(t, entries[0].Pinned)
}

func TestResolverListPinnedVersionBeatsLatest(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, first, _ := servedSkill(t, ctx, f, "pinned", urn.PrincipalWildcard)
	addVersion(t, ctx, f, f.projectID, skillID, skillContent("pinned", "Second version."))
	pinningPlugin := createPlugin(t, ctx, f, f.projectID, "pinning-plugin")
	containToolset(t, ctx, f, pinningPlugin, f.toolsetID)
	assign(t, ctx, f, pinningPlugin, urn.PrincipalWildcard)
	distribute(t, ctx, f, f.projectID, skillID, pinningPlugin, conv.ToNullUUID(first))

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, first, entries[0].VersionID)
	require.Equal(t, pinningPlugin, entries[0].PluginID)
	require.True(t, entries[0].Pinned)
}

func TestResolverListConflictingPinsServeNewestAndWarn(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	var logs bytes.Buffer
	resolver := mcpskills.NewResolver(slog.New(slog.NewTextHandler(&logs, nil)), f.conn)

	skillID := createSkill(t, ctx, f, f.projectID, "contested")
	first := addVersion(t, ctx, f, f.projectID, skillID, skillContent("contested", "First version."))
	second := addVersion(t, ctx, f, f.projectID, skillID, skillContent("contested", "Second version."))
	for name, pin := range map[string]uuid.UUID{"pins-first": first, "pins-second": second} {
		pluginID := createPlugin(t, ctx, f, f.projectID, name)
		containToolset(t, ctx, f, pluginID, f.toolsetID)
		assign(t, ctx, f, pluginID, urn.PrincipalWildcard)
		distribute(t, ctx, f, f.projectID, skillID, pluginID, conv.ToNullUUID(pin))
	}

	entries, err := resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, second, entries[0].VersionID)
	require.True(t, entries[0].Pinned)
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "pinned to different versions")
	require.Contains(t, logs.String(), "gram.mcp.skill.pin_conflict_count=1")
}

func TestResolverListRevokedDistributionExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, pluginID := servedSkill(t, ctx, f, "revoked", urn.PrincipalWildcard)

	_, err := skillsrepo.New(f.conn).RevokeActiveSkillDistribution(ctx, skillsrepo.RevokeActiveSkillDistributionParams{
		ProjectID: f.projectID, SkillID: skillID, PluginID: conv.ToNullUUID(pluginID),
		AssistantID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Channel: "plugin",
	})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestResolverListArchivedSkillExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "archived", urn.PrincipalWildcard)

	_, err := skillsrepo.New(f.conn).ArchiveSkill(ctx, skillsrepo.ArchiveSkillParams{ProjectID: f.projectID, ID: skillID})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestResolverListInvalidVersionExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "half-valid", urn.PrincipalWildcard)
	invalid := "---\nname: half-valid\n---\n\nMissing description.\n"
	_, err := skillsrepo.New(f.conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: invalid, CanonicalSha256: rawSHA256(invalid), RawSha256: rawSHA256(invalid), Description: pgtype.Text{},
		Metadata: []byte("{}"), SpecValid: false, ValidationErrors: []byte(`[{"code":"required","field":"description","message":"description is required"}]`),
		CreatedByUserID: testUserID, ProjectID: f.projectID, SkillID: skillID,
	})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, skillContent("half-valid", "Serves half-valid."), entries[0].Content, "the newer invalid version is never served")
}

func TestResolverListPaddedFrontmatterNameExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, _, _ := servedSkill(t, ctx, f, "padded", urn.PrincipalWildcard)
	// Validation trims the name, so a quoted name with trailing whitespace is
	// stored valid under "padded" while its frontmatter keeps the padding.
	padded := "---\nname: \"padded \"\ndescription: Serves padded.\n---\n\n# padded\n"
	addVersion(t, ctx, f, f.projectID, skillID, padded)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries, "an entry whose frontmatter name differs from its URI is never served")

	_, err = f.resolver.Get(ctx, f.scope(urn.PrincipalWildcard), mcpskills.SkillURI("padded"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)
}

func TestResolverListDeletedPluginExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, pluginID := servedSkill(t, ctx, f, "deleted", urn.PrincipalWildcard)

	err := pluginsrepo.New(f.conn).DeletePlugin(ctx, pluginsrepo.DeletePluginParams{ID: pluginID, OrganizationID: f.organizationID, ProjectID: f.projectID})
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestResolverListOtherProjectExcluded(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	otherProjectID := createProject(t, ctx, f, "other-"+uuid.NewString()[:8])
	skillID := createSkill(t, ctx, f, otherProjectID, "foreign")
	addVersion(t, ctx, f, otherProjectID, skillID, skillContent("foreign", "Belongs to another project."))
	pluginID := createPlugin(t, ctx, f, otherProjectID, "foreign-plugin")
	containToolset(t, ctx, f, pluginID, f.toolsetID)
	assign(t, ctx, f, pluginID, urn.PrincipalWildcard)
	distribute(t, ctx, f, otherProjectID, skillID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Empty(t, entries, "a plugin in another project never serves through this project's endpoint")
}

func TestResolverListEmptyPrincipalsReturnsNothing(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "anyone", urn.PrincipalWildcard)

	entries, err := f.resolver.List(ctx, f.scope())
	require.NoError(t, err)
	require.Empty(t, entries)

	_, err = f.resolver.Get(ctx, f.scope(), mcpskills.SkillURI("anyone"))
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)
}

func TestResolverListSortsByName(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "zeta", urn.PrincipalWildcard)
	_, _, _ = servedSkill(t, ctx, f, "alpha", urn.PrincipalWildcard)
	_, _, _ = servedSkill(t, ctx, f, "mid", urn.PrincipalWildcard)

	entries, err := f.resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, []string{"alpha", "mid", "zeta"}, []string{entries[0].Name, entries[1].Name, entries[2].Name})
}

func TestResolverGet(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, versionID, _ := servedSkill(t, ctx, f, "fetched", emailPrincipal(testUserEmail))
	_, _, _ = servedSkill(t, ctx, f, "other", emailPrincipal(testUserEmail))

	entry, err := f.resolver.Get(ctx, f.scope(emailPrincipal(testUserEmail)), "skill://fetched/SKILL.md")
	require.NoError(t, err)
	require.Equal(t, skillID, entry.SkillID)
	require.Equal(t, versionID, entry.VersionID)
	require.Equal(t, "fetched", entry.Frontmatter["name"])

	_, err = f.resolver.Get(ctx, f.scope(emailPrincipal(testUserEmail)), "skill://missing/SKILL.md")
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)

	_, err = f.resolver.Get(ctx, f.scope(emailPrincipal(testUserEmail)), "skill://fetched/references/notes.md")
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound)

	_, err = f.resolver.Get(ctx, f.scope(emailPrincipal("bob@example.test")), "skill://fetched/SKILL.md")
	require.ErrorIs(t, err, mcpskills.ErrSkillNotFound, "an unassigned caller cannot fetch by URI either")
}

func TestRecordReadIdempotentPerSession(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID, versionID, _ := servedSkill(t, ctx, f, "observed", urn.PrincipalWildcard)
	entry, err := f.resolver.Get(ctx, f.scope(urn.PrincipalWildcard), mcpskills.SkillURI("observed"))
	require.NoError(t, err)
	seenAt := time.Now().UTC().Truncate(time.Microsecond)
	read := mcpskills.Read{ProjectID: f.projectID, Entry: *entry, UserID: testUserID, SessionID: "mcp-session-1", SeenAt: seenAt}

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
	require.Equal(t, "plugin", row.SourceLevel.String)
	require.Equal(t, "skill://observed/SKILL.md", row.SourcePath.String)
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

func TestRecordReadWithoutSessionCountsEveryRead(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	_, _, _ = servedSkill(t, ctx, f, "sessionless", urn.PrincipalWildcard)
	entry, err := f.resolver.Get(ctx, f.scope(urn.PrincipalWildcard), mcpskills.SkillURI("sessionless"))
	require.NoError(t, err)
	read := mcpskills.Read{ProjectID: f.projectID, Entry: *entry, UserID: "", SessionID: "", SeenAt: time.Now()}

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
	_, _, _ = servedSkill(t, ctx, f, "mismatched", urn.PrincipalWildcard)
	entry, err := f.resolver.Get(ctx, f.scope(urn.PrincipalWildcard), mcpskills.SkillURI("mismatched"))
	require.NoError(t, err)
	entry.VersionID = uuid.New()

	written, err := f.resolver.RecordRead(ctx, mcpskills.Read{ProjectID: f.projectID, Entry: *entry, UserID: "", SessionID: "s", SeenAt: time.Now()})
	require.NoError(t, err)
	require.False(t, written, "a version that is not the skill's records nothing")
}

// addMember creates a user and gives them an active membership in the
// fixture organization, which is what makes them a skill recipient.
func addMember(t *testing.T, ctx context.Context, f *fixture, userID, email string) {
	t.Helper()

	_, err := usersrepo.New(f.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: userID, Email: email, DisplayName: "Member", PhotoUrl: pgtype.Text{}, Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(f.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: f.organizationID, UserID: conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func TestPrincipalURNsForCaller(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	addMember(t, ctx, f, testUserID, "Alice@Example.test")

	principals, err := mcpskills.PrincipalURNsForCaller(ctx, f.conn, f.organizationID, testUserID)
	require.NoError(t, err)
	require.Contains(t, principals, emailPrincipal(testUserEmail), "the email principal is normalized")
	require.Contains(t, principals, urn.PrincipalWildcard)
	require.Contains(t, principals, urn.NewPrincipal(urn.PrincipalTypeUser, testUserID).String())

	unknown, err := mcpskills.PrincipalURNsForCaller(ctx, f.conn, f.organizationID, "user_unknown")
	require.NoError(t, err)
	require.Empty(t, unknown)

	anonymous, err := mcpskills.PrincipalURNsForCaller(ctx, f.conn, f.organizationID, "")
	require.NoError(t, err)
	require.Empty(t, anonymous)
}

func TestPrincipalURNsForCallerServeAssignedSkills(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	addMember(t, ctx, f, testUserID, testUserEmail)
	_, _, _ = servedSkill(t, ctx, f, "for-alice", emailPrincipal(testUserEmail))
	_, _, _ = servedSkill(t, ctx, f, "for-bob", emailPrincipal("bob@example.test"))

	principals, err := mcpskills.PrincipalURNsForCaller(ctx, f.conn, f.organizationID, testUserID)
	require.NoError(t, err)

	entries, err := f.resolver.List(ctx, f.scope(principals...))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "for-alice", entries[0].Name)
}

func TestPrincipalURNsForCallerRequiresLiveMembership(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	addMember(t, ctx, f, testUserID, testUserEmail)
	_, _, _ = servedSkill(t, ctx, f, "for-everyone", urn.PrincipalWildcard)

	resolveFor := func(userID string) []mcpskills.Entry {
		t.Helper()
		principals, err := mcpskills.PrincipalURNsForCaller(ctx, f.conn, f.organizationID, userID)
		require.NoError(t, err)
		entries, err := f.resolver.List(ctx, f.scope(principals...))
		require.NoError(t, err)
		return entries
	}
	require.Len(t, resolveFor(testUserID), 1, "an active member receives the wildcard assignment")

	// A user who exists but was never a member: a valid credential for the
	// organization does not make them a recipient of its assignments.
	_, err := usersrepo.New(f.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: "user_outsider", Email: "outsider@example.test", DisplayName: "Outsider", PhotoUrl: pgtype.Text{}, Admin: false,
	})
	require.NoError(t, err)
	principals, err := mcpskills.PrincipalURNsForCaller(ctx, f.conn, f.organizationID, "user_outsider")
	require.NoError(t, err)
	require.Empty(t, principals, "a non-member has no delivery principals, not even the wildcard")
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

	// A member removed from the organization stops receiving its assignments
	// the moment the membership ends, whatever credential they still hold.
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

func TestResolverListCapCountsSkillsNotPlugins(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	var logs bytes.Buffer
	resolver := mcpskills.NewResolver(slog.New(slog.NewTextHandler(&logs, nil)), f.conn)
	mcpskills.SetServedSkillLimit(resolver, 3)

	// "alpha" is carried by three assigned plugins; "beta" and "gamma" by one
	// each. Three skills fit the cap of three exactly, however many plugins
	// carry them, and an exact fit is not a truncation.
	alphaID, _, _ := servedSkill(t, ctx, f, "alpha", urn.PrincipalWildcard)
	for _, slug := range []string{"alpha-second", "alpha-third"} {
		pluginID := createPlugin(t, ctx, f, f.projectID, slug)
		containToolset(t, ctx, f, pluginID, f.toolsetID)
		assign(t, ctx, f, pluginID, urn.PrincipalWildcard)
		distribute(t, ctx, f, f.projectID, alphaID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	}
	_, _, _ = servedSkill(t, ctx, f, "beta", urn.PrincipalWildcard)
	_, _, _ = servedSkill(t, ctx, f, "gamma", urn.PrincipalWildcard)

	entries, err := resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta", "gamma"}, entryNames(entries))
	require.NotContains(t, logs.String(), "level=WARN", "exactly the cap's worth of skills is not a truncation")

	// A fourth skill is one past the cap: the first three by name are served
	// and the truncation is logged.
	_, _, _ = servedSkill(t, ctx, f, "delta", urn.PrincipalWildcard)
	entries, err = resolver.List(ctx, f.scope(urn.PrincipalWildcard))
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta", "delta"}, entryNames(entries))
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "exceeds its cap")

	// Get is unaffected by the cap: it names one skill.
	entry, err := resolver.Get(ctx, f.scope(urn.PrincipalWildcard), mcpskills.SkillURI("gamma"))
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
