package mcpskills_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false, Temporal: false, Presidio: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// fixture is one organization with one project, one member caller, and one
// MCP server fronting a hosted toolset: the surface every resolution in a
// test is scoped to unless it names another.
type fixture struct {
	conn           *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	projectSlug    string
	serverID       uuid.UUID
	resolver       *mcpskills.Resolver
}

const (
	testUserID    = "user_mcpskills"
	testUserEmail = "alice@example.test"
)

func newFixture(t *testing.T) (context.Context, *fixture) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	organizationID := "org-mcpskills-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: organizationID, Slug: organizationID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	f := &fixture{
		conn:           conn,
		organizationID: organizationID,
		projectID:      uuid.Nil,
		projectSlug:    "mcpskills-" + uuid.NewString()[:8],
		serverID:       uuid.Nil,
		resolver:       mcpskills.NewResolver(testenv.NewLogger(t), conn),
	}
	f.projectID = createProject(t, ctx, f, f.projectSlug)
	f.serverID = createServer(t, ctx, f, f.projectID, "served")
	addMember(t, ctx, f, testUserID, testUserEmail)
	return ctx, f
}

// uri is the URI the fixture's project serves the named skill at.
func (f *fixture) uri(name string) string {
	return mcpskills.SkillURI(f.projectSlug, name)
}

func createProject(t *testing.T, ctx context.Context, f *fixture, slug string) uuid.UUID {
	t.Helper()

	project, err := projectsrepo.New(f.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: slug, Slug: slug, OrganizationID: f.organizationID,
	})
	require.NoError(t, err)
	return project.ID
}

func createToolset(t *testing.T, ctx context.Context, f *fixture, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: f.organizationID, ProjectID: projectID, Name: slug, Slug: slug,
		Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: conv.ToPGText(slug), McpEnabled: true,
	})
	require.NoError(t, err)
	return toolset.ID
}

// createServer creates an MCP server fronting a new hosted toolset in the
// project, the stored row an mcp_server link names.
func createServer(t *testing.T, ctx context.Context, f *fixture, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	toolsetID := createToolset(t, ctx, f, projectID, slug+"-toolset")
	server, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		ToolsetID: conv.ToNullUUID(toolsetID), Visibility: visibility.Private,
	})
	require.NoError(t, err)
	return server.ID
}

func createMetaServer(t *testing.T, ctx context.Context, f *fixture, name string) uuid.UUID {
	t.Helper()

	server, err := metamcprepo.New(f.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: f.organizationID, ProjectID: f.projectID, Name: name,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Visibility: "private", NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
	return server.ID
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

func skillContent(name, description string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name)
}

func rawSHA256(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func createSkill(t *testing.T, ctx context.Context, f *fixture, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()

	skill, err := skillsrepo.New(f.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: projectID, Name: name, DisplayName: name, Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	return skill.ID
}

// addVersion records one spec-valid version of a skill. Versions added in
// sequence are strictly newer, which is the order the resolver relies on.
func addVersion(t *testing.T, ctx context.Context, f *fixture, projectID, skillID uuid.UUID, content string) uuid.UUID {
	t.Helper()

	version, err := skillsrepo.New(f.conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: content, CanonicalSha256: rawSHA256(content), RawSha256: rawSHA256(content), Description: pgtype.Text{},
		Metadata: []byte("{}"), SpecValid: true, ValidationErrors: []byte("[]"), CreatedByUserID: testUserID,
		ProjectID: projectID, SkillID: skillID,
	})
	require.NoError(t, err)
	return version.ID
}

// link links a skill in projectID to target, pinned when pinnedVersionID is
// non-nil, and returns the link's id.
func link(t *testing.T, ctx context.Context, f *fixture, projectID, skillID uuid.UUID, target mcpskills.Target, pinnedVersionID *uuid.UUID) uuid.UUID {
	t.Helper()

	linkID, err := mcpskills.CreateLink(ctx, f.conn, mcpskills.NewLink{
		OrganizationID: f.organizationID, ProjectID: projectID, SkillID: skillID,
		PinnedVersionID: pinnedVersionID, Target: target, CreatedByUserID: testUserID,
	})
	require.NoError(t, err)
	return linkID
}

// servedSkill stands up the common case: one skill with one version, linked
// to the fixture server and tracking its latest version.
func servedSkill(t *testing.T, ctx context.Context, f *fixture, name string) (skillID, versionID, linkID uuid.UUID) {
	t.Helper()

	skillID = createSkill(t, ctx, f, f.projectID, name)
	versionID = addVersion(t, ctx, f, f.projectID, skillID, skillContent(name, "Serves "+name+"."))
	linkID = link(t, ctx, f, f.projectID, skillID, mcpskills.MCPServerTarget(f.serverID), nil)
	return skillID, versionID, linkID
}

// scope resolves the fixture server for the fixture's member caller.
func (f *fixture) scope() mcpskills.Scope {
	return f.scopeFor(mcpskills.MCPServerTarget(f.serverID))
}

// scopeFor resolves target for the fixture's member caller.
func (f *fixture) scopeFor(target mcpskills.Target) mcpskills.Scope {
	return mcpskills.Scope{OrganizationID: f.organizationID, Target: target, MemberMCPServerIDs: nil, UserID: testUserID}
}
