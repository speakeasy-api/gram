package upstreamclients_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	oktarepo "github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type issuerFixture struct {
	conn           *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	serverID       uuid.UUID
}

func newIssuerFixture(t *testing.T, ctx context.Context) issuerFixture {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Upstream Issuers Test Org",
		Slug:        "upstream-issuers-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(organizationID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	slug := "upstream-" + uuid.NewString()[:8]
	projectID, err := testrepo.New(conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: slug, Slug: slug, OrganizationID: organizationID})
	require.NoError(t, err)
	backendID, err := oktarepo.New(conn).CreateRemoteBackendFixture(ctx, oktarepo.CreateRemoteBackendFixtureParams{ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug), Url: "https://" + slug + ".example/mcp"})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: projectID, OrganizationID: conv.ToPGText(organizationID), Slug: slug, AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		RemoteMcpServerID: conv.ToNullUUID(backendID), UserSessionIssuerID: conv.ToNullUUID(issuer.ID), Visibility: "private",
	})
	require.NoError(t, err)
	return issuerFixture{conn: conn, organizationID: organizationID, projectID: projectID, serverID: server.ID}
}

func (f issuerFixture) addProvider(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	provider := provisiontest.CreateIssuer(t, ctx, f.conn, f.organizationID, conv.ToNullUUID(f.projectID), "https://as.example/token")
	require.NoError(t, testrepo.New(f.conn).SetPreparationFixtureIssuerCapability(ctx, testrepo.SetPreparationFixtureIssuerCapabilityParams{ID: provider, ProjectID: conv.ToNullUUID(f.projectID)}))
	return provider
}

func (f issuerFixture) addClient(t *testing.T, ctx context.Context, providerID uuid.UUID) uuid.UUID {
	t.Helper()
	client, err := remotesessionsrepo.New(f.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		OrganizationID: conv.ToPGText(f.organizationID), ProjectID: conv.ToNullUUID(f.projectID), RemoteSessionIssuerID: providerID, ClientID: "client-" + uuid.NewString()[:8],
		ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now().UTC()), TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{},
	})
	require.NoError(t, err)
	return client.ID
}

func (f issuerFixture) associate(t *testing.T, ctx context.Context, clientID uuid.UUID) {
	t.Helper()
	require.NoError(t, testrepo.New(f.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID: f.projectID, OrganizationID: f.organizationID, McpServerID: f.serverID, RemoteSessionClientID: clientID,
	}))
}

func (f issuerFixture) deleteClient(t *testing.T, ctx context.Context, clientID uuid.UUID) {
	t.Helper()
	deleted, err := testrepo.New(f.conn).SoftDeleteRemoteSessionClientFixture(ctx, testrepo.SoftDeleteRemoteSessionClientFixtureParams{ID: clientID, ProjectID: conv.ToNullUUID(f.projectID), OrganizationID: conv.ToPGText(f.organizationID)})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
}

// upstreamIssuerReaders are the sources that must agree on a server's one
// provider. Each query that copies the one-provider subquery adds an entry.
var upstreamIssuerReaders = map[string]func(t *testing.T, ctx context.Context, f issuerFixture) uuid.NullUUID{
	"ServerUpstreamIssuers": func(t *testing.T, ctx context.Context, f issuerFixture) uuid.NullUUID {
		t.Helper()
		issuers, err := upstreamclients.ServerUpstreamIssuers(ctx, f.conn, f.projectID, f.serverID)
		require.NoError(t, err)
		return issuers.Sole()
	},
	"one-provider subquery": func(t *testing.T, ctx context.Context, f issuerFixture) uuid.NullUUID {
		t.Helper()
		sole, err := testrepo.New(f.conn).GetMCPServerSoleUpstreamIssuerFixture(ctx, f.serverID)
		require.NoError(t, err)
		return sole
	},
	"ListEligibleServers": func(t *testing.T, ctx context.Context, f issuerFixture) uuid.NullUUID {
		t.Helper()
		rows, err := oktarepo.New(f.conn).ListEligibleServers(ctx, f.organizationID)
		require.NoError(t, err)
		for _, row := range rows {
			if row.ID == f.serverID {
				return uuid.NullUUID{UUID: row.IssuerID, Valid: true}
			}
		}
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	},
	"GetEligibleServer": func(t *testing.T, ctx context.Context, f issuerFixture) uuid.NullUUID {
		t.Helper()
		row, err := oktarepo.New(f.conn).GetEligibleServer(ctx, oktarepo.GetEligibleServerParams{OrganizationID: f.organizationID, McpServerID: f.serverID})
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		}
		require.NoError(t, err)
		return uuid.NullUUID{UUID: row.IssuerID, Valid: true}
	},
	"ListMemberProviderIdentities": func(t *testing.T, ctx context.Context, f issuerFixture) uuid.NullUUID {
		t.Helper()
		queries := metamcprepo.New(f.conn)
		gateway, err := queries.CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
			OrganizationID: f.organizationID, ProjectID: f.projectID, Name: "gateway-" + uuid.NewString()[:8], Visibility: "private",
		})
		require.NoError(t, err)
		_, err = queries.CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{ProjectID: f.projectID, MetaMcpServerID: gateway.ID, McpServerID: f.serverID})
		require.NoError(t, err)
		identities, err := queries.ListMemberProviderIdentities(ctx, metamcprepo.ListMemberProviderIdentitiesParams{MetaMcpServerID: gateway.ID, ProjectID: f.projectID})
		require.NoError(t, err)
		if len(identities) == 0 {
			return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		}
		require.Len(t, identities, 1)
		return uuid.NullUUID{UUID: identities[0].RemoteSessionIssuerID, Valid: true}
	},
}

func TestServerUpstreamIssuers_Parity(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		// arrange shapes the server's associations and returns the provider it
		// should resolve to, uuid.Nil when none should.
		arrange     func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID
		wantSeveral bool
	}{
		"none": {
			arrange: func(*testing.T, context.Context, issuerFixture) uuid.UUID { return uuid.Nil },
		},
		"one": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				provider := f.addProvider(t, ctx)
				f.associate(t, ctx, f.addClient(t, ctx, provider))
				return provider
			},
		},
		"two clients of one provider": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				provider := f.addProvider(t, ctx)
				f.associate(t, ctx, f.addClient(t, ctx, provider))
				f.associate(t, ctx, f.addClient(t, ctx, provider))
				return provider
			},
		},
		"several": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				f.associate(t, ctx, f.addClient(t, ctx, f.addProvider(t, ctx)))
				f.associate(t, ctx, f.addClient(t, ctx, f.addProvider(t, ctx)))
				return uuid.Nil
			},
			wantSeveral: true,
		},
		"one live and one deleted provider": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				live, deleted := f.addProvider(t, ctx), f.addProvider(t, ctx)
				f.associate(t, ctx, f.addClient(t, ctx, live))
				f.associate(t, ctx, f.addClient(t, ctx, deleted))
				require.NoError(t, testrepo.New(f.conn).ForceSoftDeleteRemoteSessionIssuerFixture(ctx, deleted))
				return live
			},
		},
		"deleted provider": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				deleted := f.addProvider(t, ctx)
				f.associate(t, ctx, f.addClient(t, ctx, deleted))
				require.NoError(t, testrepo.New(f.conn).ForceSoftDeleteRemoteSessionIssuerFixture(ctx, deleted))
				return uuid.Nil
			},
		},
		"deleted client": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				client := f.addClient(t, ctx, f.addProvider(t, ctx))
				f.associate(t, ctx, client)
				f.deleteClient(t, ctx, client)
				return uuid.Nil
			},
		},
		"detached association": {
			arrange: func(t *testing.T, ctx context.Context, f issuerFixture) uuid.UUID {
				t.Helper()
				client := f.addClient(t, ctx, f.addProvider(t, ctx))
				f.associate(t, ctx, client)
				detached, err := repo.New(f.conn).DetachMCPServerRemoteSessionClient(ctx, repo.DetachMCPServerRemoteSessionClientParams{McpServerID: f.serverID, RemoteSessionClientID: client, ProjectID: f.projectID})
				require.NoError(t, err)
				require.Equal(t, int64(1), detached)
				return uuid.Nil
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			f := newIssuerFixture(t, ctx)
			want := tc.arrange(t, ctx, f)

			for reader, read := range upstreamIssuerReaders {
				require.Equal(t, uuid.NullUUID{UUID: want, Valid: want != uuid.Nil}, read(t, ctx, f), reader)
			}
			issuers, err := upstreamclients.ServerUpstreamIssuers(ctx, f.conn, f.projectID, f.serverID)
			require.NoError(t, err)
			require.Equal(t, tc.wantSeveral, issuers.Several())
		})
	}
}

func TestServerUpstreamIssuers_ScopedToProject(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newIssuerFixture(t, ctx)
	f.associate(t, ctx, f.addClient(t, ctx, f.addProvider(t, ctx)))

	issuers, err := upstreamclients.ServerUpstreamIssuers(ctx, f.conn, uuid.New(), f.serverID)
	require.NoError(t, err)
	require.False(t, issuers.Sole().Valid)
}

func TestStampMCPServerUpstreamClientFixture_WritesBothSources(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newIssuerFixture(t, ctx)
	provider := f.addProvider(t, ctx)

	stamped, err := testrepo.New(f.conn).StampMCPServerUpstreamClientFixture(ctx, testrepo.StampMCPServerUpstreamClientFixtureParams{
		ProjectID: f.projectID, OrganizationID: f.organizationID, McpServerID: f.serverID, RemoteSessionClientID: f.addClient(t, ctx, provider),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)

	server, err := mcpserversrepo.New(f.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.serverID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(provider), server.RemoteSessionIssuerID)

	issuers, err := upstreamclients.ServerUpstreamIssuers(ctx, f.conn, f.projectID, f.serverID)
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(provider), issuers.Sole())
}
