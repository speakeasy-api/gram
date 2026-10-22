package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// credential seeds a live user session credential for subject. refreshHash
// selects its class: empty is a workload session, which stores none.
func (f *fixture) credential(subject urn.SessionSubject, refreshHash string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	hash := pgtype.Text{String: refreshHash, Valid: refreshHash != ""}
	require.NoError(f.t, f.q.SeedUserSessionFixture(f.t.Context(), SeedUserSessionFixtureParams{
		ProjectID:           uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:      pgtype.Text{String: f.orgID, Valid: true},
		UserSessionIssuerID: f.userSessionIssuer(),
		SubjectUrn:          subject,
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    hash,
		CredentialID:        uuid.NullUUID{UUID: id, Valid: true},
	}))
	return id
}

func (f *fixture) interactiveCredential() uuid.UUID {
	f.t.Helper()
	return f.credential(urn.NewUserSubject("user_"+uuid.NewString()[:8]), "refresh-"+uuid.NewString())
}

func (f *fixture) oauthSelection(credentialID, serverID, clientID uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedOAuthSelectionFixture(f.t.Context(), SeedOAuthSelectionFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
		CredentialID:          credentialID,
		McpServerID:           serverID,
		RemoteSessionClientID: clientID,
	}))
}

func (f *fixture) noAuthSelection(credentialID, serverID uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedUpstreamSelectionFixture(f.t.Context(), SeedUpstreamSelectionFixtureParams{
		ProjectID:      f.projectID,
		OrganizationID: f.orgID,
		CredentialID:   credentialID,
		McpServerID:    serverID,
		NoAuth:         true,
		AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}))
}

func (f *fixture) selection(credentialID, serverID uuid.UUID) GetSelectionFixtureRow {
	f.t.Helper()
	row, err := f.q.GetSelectionFixture(f.t.Context(), GetSelectionFixtureParams{CredentialID: credentialID, McpServerID: serverID})
	require.NoError(f.t, err)
	return row
}

func selectionGaps(report Report, outcome SelectionOutcome) []SelectionGap {
	var out []SelectionGap
	for _, gap := range report.SelectionGaps {
		if gap.Outcome == outcome {
			out = append(out, gap)
		}
	}
	return out
}

func TestRun_OAuthChoiceNamesItsRegistrationsClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	f.run(true)
	credential := f.interactiveCredential()
	f.oauthSelection(credential, server, registration)

	dry := f.run(false)
	require.Equal(t, 1, dry.PendingSelectionMappings)
	require.False(t, f.selection(credential, server).AuthClientID.Valid, "dry run writes nothing")

	applied := f.run(true)
	require.Equal(t, 1, applied.SelectionsMapped)
	require.Equal(t, 1, applied.Selections[ClassInteractive].Written)
	require.Empty(t, applied.SelectionGaps)

	row := f.selection(credential, server)
	require.Equal(t, uuid.NullUUID{UUID: f.live(TypeRemoteSession)[0].ID, Valid: true}, row.AuthClientID)
	require.Equal(t, uuid.NullUUID{UUID: registration, Valid: true}, row.RemoteSessionClientID, "the upstream client choice is kept")

	again := f.run(true)
	require.Zero(t, again.SelectionsMapped, "rerun is idempotent")
	require.Equal(t, 1, again.Selections[ClassInteractive].Outcomes[SelectionMapped])
}

func TestRun_NoAuthChoiceNamesTheServersHeaderClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	report := f.run(true)
	require.Equal(t, 1, report.SelectionsMapped)
	row := f.selection(credential, server)
	require.Equal(t, uuid.NullUUID{UUID: f.live(TypeStaticCredential)[0].ID, Valid: true}, row.AuthClientID)
	require.False(t, row.NoAuth, "a choice naming a client is never no-auth")
}

func TestRun_NoAuthChoiceWithoutClientsStaysNoAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	report := f.run(true)
	require.Zero(t, report.SelectionsMapped)
	require.Equal(t, 1, report.Selections[ClassInteractive].Outcomes[SelectionNoAuth])
	require.Empty(t, report.SelectionGaps)
	require.True(t, f.selection(credential, server).NoAuth)
}

func TestRun_NoAuthChoiceBesideAnOAuthClientIsStale(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	report := f.run(true)
	require.Len(t, selectionGaps(report, SelectionStaleNoAuth), 1)
	require.False(t, f.selection(credential, server).AuthClientID.Valid)
}

func TestRun_ChoiceSeveralClientsCouldServeIsNeverGuessed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)
	f.run(true)
	// A second header client an administrator linked to the same server.
	second, err := f.q.SeedAuthClientFixture(t.Context(), SeedAuthClientFixtureParams{
		OrganizationID:        f.orgID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		ClientType:            TypeStaticCredential,
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.NoError(t, f.q.SeedServerAuthClientLinkFixture(t.Context(), SeedServerAuthClientLinkFixtureParams{
		ProjectID:      f.projectID,
		OrganizationID: f.orgID,
		McpServerID:    server,
		AuthClientID:   second,
	}))
	other := f.interactiveCredential()
	f.noAuthSelection(other, server)

	report := f.run(true)
	gaps := selectionGaps(report, SelectionAmbiguous)
	require.Len(t, gaps, 1)
	require.Equal(t, other, gaps[0].CredentialID)
	require.Equal(t, ClassInteractive, gaps[0].CredentialClass)
	require.False(t, f.selection(other, server).AuthClientID.Valid)
}

func TestRun_MappedChoiceWhoseClientIsDisabledIsAGap(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	credential := f.interactiveCredential()
	f.oauthSelection(credential, server, registration)
	f.run(true)
	clientID := f.live(TypeRemoteSession)[0].ID
	require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), SetAuthClientEnabledFixtureParams{Enabled: false, ID: clientID}))

	report := f.run(true)
	require.Len(t, selectionGaps(report, SelectionUnusable), 1)
	require.Equal(t, uuid.NullUUID{UUID: clientID, Valid: true}, f.selection(credential, server).AuthClientID, "a named client is never replaced")
}

func TestRun_SelectionCoverageIsReportedByCredentialClass(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	for _, credential := range []uuid.UUID{
		f.interactiveCredential(),
		f.credential(urn.NewUserSubject("user_jag"), "id-jag:"+uuid.NewString()),
		f.credential(urn.NewUserSubject("user_mint"), "dashboard-mint:"+uuid.NewString()),
		f.credential(urn.NewAgentSubject(uuid.New()), "refresh-"+uuid.NewString()),
		f.credential(urn.NewUserSubject("user_workload"), ""),
	} {
		f.oauthSelection(credential, server, registration)
	}
	// A choice of a credential that can no longer refresh is never served
	// again, so it is not counted.
	f.oauthSelection(uuid.New(), server, registration)

	report := f.run(true)
	for _, class := range []CredentialClass{ClassInteractive, ClassIDJAG, ClassDashboardMint, ClassAgent, ClassWorkload} {
		require.Equal(t, 1, report.Selections[class].Selections, class)
		require.Equal(t, 1, report.Selections[class].Written, class)
	}
	require.Equal(t, 5, report.SelectionsMapped)
}
