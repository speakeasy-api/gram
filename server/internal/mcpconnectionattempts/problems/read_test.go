package problems

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func logsOff(context.Context, string) (bool, error) { return false, nil }

func TestRead_LogsOffReturnsTheError(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	_, err = Read(t.Context(), ch, db, logsOff, "org-"+uuid.NewString(), time.Now())
	require.ErrorIs(t, err, mcpconnectionattempts.ErrLogsOff)
}

func TestRead_RejectsAnEmptyOrganization(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	_, err = Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, "", time.Now())
	require.Error(t, err)
}

// Another organization failing on the same server id is not counted.
func TestRead_GroupsOneOrganizationsFailures(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	now := time.Now().UTC()
	orgA, orgB, server := "org-"+uuid.NewString(), "org-"+uuid.NewString(), uuid.New()
	fail := func(org string, people int) {
		project := uuid.New()
		for range people {
			p := urn.NewUserSubject("user-" + uuid.NewString())
			at := now.Add(-time.Hour)
			row, err := mcpconnectionattempts.RowAt(mcpconnectionattempts.AttemptAt(uuid.New(), at, mcpconnectionattempts.ServerScope(org, project, server, mcpconnectionattempts.AppDCR)),
				mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected, true, at, mcpconnectionattempts.Subject(&p))
			require.NoError(t, err)
			require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ch, []mcpconnectionattempts.Row{row}))
		}
	}
	fail(orgA, 2)
	fail(orgB, 2)

	got, err := Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, orgA, now)
	require.NoError(t, err)
	require.Empty(t, got, "two people in each organization make no problem")

	fail(orgA, 1)
	got, err = Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, orgA, now)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, RuleThreePeopleSameServer, got[0].Rule)
	require.Equal(t, 3, got[0].People)
	require.Equal(t, orgA, got[0].OrganizationID)
}

type upstreamFixture struct {
	organizationID string
	server         uuid.UUID
}

// seedUpstream creates an organization with an MCP server proxying url that signs in to issuer.
func seedUpstream(t *testing.T, db *pgxpool.Pool, url, issuer string) upstreamFixture {
	t.Helper()
	ctx := t.Context()
	q := testrepo.New(db)
	org := "org-" + uuid.NewString()
	require.NoError(t, q.SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: org, Slug: org}))
	project, err := q.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: "p", Slug: "p-" + uuid.NewString()[:8], OrganizationID: org})
	require.NoError(t, err)

	remote, server, issuerID := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, q.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{ID: remote, ProjectID: project, Url: url}))
	require.NoError(t, q.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: server, ProjectID: project, Slug: conv.ToPGText("srv-" + server.String()[:8]), RemoteMcpServerID: uuid.NullUUID{UUID: remote, Valid: true}, Visibility: "private",
	}))
	require.NoError(t, q.SeedDelegationLoaderIssuerFixture(ctx, testrepo.SeedDelegationLoaderIssuerFixtureParams{
		ID: issuerID, OrganizationID: conv.ToPGText(org), Slug: "issuer-" + issuerID.String()[:8], Issuer: issuer,
		AuthorizationEndpoint: pgtype.Text{}, TokenEndpoint: pgtype.Text{}, JwksUri: pgtype.Text{},
	}))
	n, err := q.SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}, ID: server, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	return upstreamFixture{organizationID: org, server: server}
}

func TestListServerUpstreams_NeverReturnsAnotherOrganizationsServer(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	mine := seedUpstream(t, db, "https://mcp.example.com/mcp", "https://auth.example.com")
	theirs := seedUpstream(t, db, "https://other.example.com/mcp", "https://auth.other.example.com")

	rows, err := repo.New(db).ListServerUpstreams(t.Context(), repo.ListServerUpstreamsParams{OrganizationID: mine.organizationID, ServerIds: []uuid.UUID{mine.server, theirs.server}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, mine.server, rows[0].ID)
	require.Equal(t, "https://mcp.example.com/mcp", rows[0].Url)
	require.Equal(t, "https://auth.example.com", rows[0].Issuer.String)
}

func TestReadServedByFamily_NeverReturnsAnotherOrganizationsRows(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	now := time.Now().UTC()
	orgA, orgB, server := "org-"+uuid.NewString(), "org-"+uuid.NewString(), uuid.NewString()
	insert := func(org, family string) {
		require.NoError(t, ch.Exec(t.Context(), `INSERT INTO `+mcpconnectionattempts.LastServedTable+`
			(organization_id, project_id, server_kind, server_id, member_id, subject_urn, app_key, client_family, remote_session_client_id, last_served_at, last_answered_at)
			VALUES (?, ?, ?, ?, '', ?, 'dcr', ?, '', ?, ?)`,
			org, uuid.New(), mcpconnectionattempts.ServerKindMCP, server, "user:"+uuid.NewString(), family, now, now))
	}
	insert(orgA, mcpconnectionattempts.FamilyCursor)
	insert(orgB, mcpconnectionattempts.FamilyClaudeCode)

	episodes := []mcpconnectionattempts.FailingPerson{{Episode: mcpconnectionattempts.Episode{ServerKind: mcpconnectionattempts.ServerKindMCP, ServerID: server}}}
	got, err := readServedByFamily(t.Context(), ch, orgA, episodes)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, mcpconnectionattempts.FamilyCursor, got[0].ClientFamily)
}
