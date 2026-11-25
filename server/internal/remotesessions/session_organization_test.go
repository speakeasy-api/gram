package remotesessions_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestUpsertRemoteSessionTakesTheProjectClientsOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createRemoteIssuer(t, ctx, ti, "session-org-project-provider", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "session-org-project-config")
	client := createRemoteClient(t, ctx, ti, issuer, userIssuer.String(), "session-org-project-client")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), userIssuer.String(), client)

	require.Equal(t, conv.ToPGText(auth.ActiveOrganizationID), session.OrganizationID)
}

func TestUpsertRemoteSessionTakesTheClientsOrganizationNotTheSignIns(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clientOrg := createOrganization(t, ctx, ti.conn, "session-org-client-owner")
	issuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, clientOrg, "session-org-client-provider")
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, clientOrg, issuer, "session-org-client")
	signIn := createUserSessionIssuer(t, ctx, ti.conn, "session-org-client-config")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), signIn.String(), client.String())

	require.NotEqual(t, clientOrg, auth.ActiveOrganizationID)
	require.Equal(t, conv.ToPGText(clientOrg), session.OrganizationID)
}

func TestUpsertRemoteSessionPrefersTheClientsProjectOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectOrg := createOrganization(t, ctx, ti.conn, "session-org-project-owner")
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "session-org-mixed", Slug: "session-org-mixed", OrganizationID: projectOrg})
	require.NoError(t, err)
	issuer := seedGlobalRemoteIssuer(t, ctx, ti.conn, "session-org-mixed-provider")
	// The schema allows a client with both a project and an organization_id
	// that disagree; attachment_scope puts the project first, and so does the
	// session's organization.
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(project.ID),
		OrganizationID:        conv.ToPGText(auth.ActiveOrganizationID),
		RemoteSessionIssuerID: issuer,
		ClientID:              "session-org-mixed-client",
	})
	require.NoError(t, err)
	signIn := createUserSessionIssuer(t, ctx, ti.conn, "session-org-mixed-config")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), signIn.String(), client.ID.String())

	require.Equal(t, conv.ToPGText(projectOrg), session.OrganizationID)
}

func TestUpsertRemoteSessionLeavesGlobalClientSessionsWithoutOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedGlobalRemoteIssuer(t, ctx, ti.conn, "session-org-global-provider")
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: issuer,
		ClientID:              "session-org-global-client",
	})
	require.NoError(t, err)
	signIn := createUserSessionIssuer(t, ctx, ti.conn, "session-org-global-config")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), signIn.String(), client.ID.String())

	require.False(t, session.OrganizationID.Valid, "a global client has no organization, whatever the sign-in's")
}

func TestUpsertRemoteSessionSignsInWhenTheClientsOrganizationHasNoMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	// projects.organization_id has no foreign key, but
	// remote_sessions.organization_id does.
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "session-org-orphan", Slug: "session-org-orphan", OrganizationID: "org_" + uuid.NewString()})
	require.NoError(t, err)
	issuer := seedGlobalRemoteIssuer(t, ctx, ti.conn, "session-org-orphan-provider")
	client := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, project.ID, issuer, "session-org-orphan-client")
	signIn := createUserSessionIssuer(t, ctx, ti.conn, "session-org-orphan-config")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), signIn.String(), client.String())

	require.False(t, session.OrganizationID.Valid)
}

func TestUpsertRemoteSessionFillsOrganizationOnReconsent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createRemoteIssuer(t, ctx, ti, "session-org-reconsent-provider", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "session-org-reconsent-config")
	client := createRemoteClient(t, ctx, ti, issuer, userIssuer.String(), "session-org-reconsent-client")
	subject := urn.NewUserSubject(auth.UserID)
	first := insertRemoteSession(t, ctx, ti.conn, subject, userIssuer.String(), client)
	require.NoError(t, testrepo.New(ti.conn).SetRemoteSessionOrganizationFixture(ctx, testrepo.SetRemoteSessionOrganizationFixtureParams{OrganizationID: pgtype.Text{}, ID: first.ID}))

	second := insertRemoteSession(t, ctx, ti.conn, subject, userIssuer.String(), client)

	require.Equal(t, first.ID, second.ID, "re-consent updates the live row in place")
	require.Equal(t, conv.ToPGText(auth.ActiveOrganizationID), second.OrganizationID)
}

var (
	// insertRemoteSessionsStatement matches every statement that inserts into
	// the remote_sessions table and captures its column list. A statement
	// without a column list leaves the group empty and is reported.
	insertRemoteSessionsStatement = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+remote_sessions\b(\s*\(([^)]*)\))?`)

	// organizationIDColumn matches the column itself, not a longer name that
	// ends in it.
	organizationIDColumn = regexp.MustCompile(`(?i)\borganization_id\b`)

	// statementEnd ends a statement in a .sql file (;) or in a Go string
	// literal (a backtick).
	statementEnd = regexp.MustCompile("[;`]")

	whitespace = regexp.MustCompile(`\s+`)
)

// clientOrganizationLookup is the expression every writer derives
// organization_id from: the client's project's organization, otherwise the
// client's own organization.
const clientOrganizationLookup = "coalesce(project.organization_id, client.organization_id)"

// TestEveryRemoteSessionsInsertSetsOrganization keeps new writers, fixtures
// included, from leaving organization_id NULL or taking it from anywhere but
// the session's client. The column must stay nullable for global clients, so
// the schema cannot enforce it. This is a text check: it requires the column
// and the client lookup in each statement, not that the lookup feeds the
// column, and it does not see writes through pgx.CopyFrom.
func TestEveryRemoteSessionsInsertSetsOrganization(t *testing.T) {
	t.Parallel()

	serverRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	var inserts int
	err = filepath.WalkDir(serverRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "migrations", "gen", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".sql" && filepath.Ext(path) != ".go" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		relative, _ := filepath.Rel(serverRoot, path)
		for _, match := range insertRemoteSessionsStatement.FindAllSubmatchIndex(content, -1) {
			inserts++
			columns := ""
			if match[4] >= 0 {
				columns = string(content[match[4]:match[5]])
			}
			require.Regexp(t, organizationIDColumn, columns, "%s: every insert into the remote_sessions table must list organization_id", relative)

			statement := content[match[0]:]
			if end := statementEnd.FindIndex(statement); end != nil {
				statement = statement[:end[0]]
			}
			normalized := strings.ToLower(whitespace.ReplaceAllString(string(statement), " "))
			require.Contains(t, normalized, clientOrganizationLookup, "%s: organization_id must come from the session's client, never the sign-in", relative)
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, inserts, "the scan found no inserts into the remote_sessions table; check the server root")
}
