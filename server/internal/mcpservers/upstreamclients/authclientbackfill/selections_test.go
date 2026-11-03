package authclientbackfill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// credential seeds a live user session credential for subject, issued by the
// fixture's user session issuer. refreshHash selects the class of a user's
// session; empty stores none.
func (f *fixture) credential(subject urn.SessionSubject, refreshHash string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	hash := pgtype.Text{String: refreshHash, Valid: refreshHash != ""}
	require.NoError(f.t, f.q.SeedUserSessionFixture(f.t.Context(), SeedUserSessionFixtureParams{
		ProjectID:           uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:      pgtype.Text{String: f.orgID, Valid: true},
		UserSessionIssuerID: f.sessionIssuer(),
		SubjectUrn:          subject,
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    hash,
		CredentialID:        uuid.NullUUID{UUID: id, Valid: true},
	}))
	return id
}

// sessionIssuer is the user session issuer the fixture's credentials are
// issued by.
func (f *fixture) sessionIssuer() uuid.UUID {
	f.t.Helper()
	if f.sessionIssuerID == uuid.Nil {
		f.sessionIssuerID = f.userSessionIssuer()
	}
	return f.sessionIssuerID
}

func (f *fixture) interactiveCredential() uuid.UUID {
	f.t.Helper()
	return f.credential(urn.NewUserSubject("user_"+uuid.NewString()[:8]), "refresh-"+uuid.NewString())
}

// oauthSelection seeds a credential's choice of clientID, bound to the
// credential's user session issuer as issuance requires.
func (f *fixture) oauthSelection(credentialID, serverID, clientID uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, f.q.BindClientIssuerFixture(f.t.Context(), BindClientIssuerFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: f.sessionIssuer()}))
	f.unboundOAuthSelection(credentialID, serverID, clientID)
}

func (f *fixture) unboundOAuthSelection(credentialID, serverID, clientID uuid.UUID) {
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

// adminHeaderClient links to server a static_credential client an
// administrator recorded, carrying the Authorization header staticHeader
// seeds.
func (f *fixture) adminHeaderClient(server uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	id, err := f.q.SeedAuthClientFixture(ctx, SeedAuthClientFixtureParams{
		OrganizationID:        f.orgID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		ClientType:            TypeStaticCredential,
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(f.t, err)
	require.NoError(f.t, f.q.SeedAuthClientHeaderFixture(ctx, SeedAuthClientHeaderFixtureParams{
		AuthClientID:           id,
		OrganizationID:         f.orgID,
		Name:                   "Authorization",
		IsRequired:             false,
		IsSecret:               true,
		Value:                  pgtype.Text{String: secretValue, Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	}))
	require.NoError(f.t, f.q.SeedServerAuthClientLinkFixture(ctx, SeedServerAuthClientLinkFixtureParams{
		ProjectID:      f.projectID,
		OrganizationID: f.orgID,
		McpServerID:    server,
		AuthClientID:   id,
		Source:         "admin",
	}))
	return id
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
	f.adminHeaderClient(server)
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
		f.credential(urn.NewWorkloadSubject(uuid.New(), "ci"), ""),
		// A user's session without a refresh token is still interactive.
		f.credential(urn.NewUserSubject("user_norefresh"), ""),
	} {
		f.oauthSelection(credential, server, registration)
	}
	// A choice whose credential does not exist is never served again, so it
	// is not counted.
	f.oauthSelection(uuid.New(), server, registration)

	report := f.run(true)
	require.Equal(t, 2, report.Selections[ClassInteractive].Selections)
	for _, class := range []CredentialClass{ClassIDJAG, ClassDashboardMint, ClassAgent, ClassWorkload} {
		require.Equal(t, 1, report.Selections[class].Selections, class)
		require.Equal(t, 1, report.Selections[class].Written, class)
	}
	require.Equal(t, 6, report.SelectionsMapped)
}

func TestRun_HeaderClientWithoutTheServersHeadersIsNotACandidate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.run(true)
	stale := f.adminHeaderClient(server)
	require.NoError(t, f.q.SeedAuthClientHeaderFixture(t.Context(), SeedAuthClientHeaderFixtureParams{
		AuthClientID:           stale,
		OrganizationID:         f.orgID,
		Name:                   "X-Extra",
		IsRequired:             false,
		IsSecret:               true,
		Value:                  pgtype.Text{String: rotatedValue, Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	}))
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	report := f.run(true)
	require.Equal(t, 1, report.SelectionsMapped)
	named := f.selection(credential, server).AuthClientID
	require.True(t, named.Valid)
	require.NotEqual(t, stale, named.UUID, "only the client carrying the server's current headers is the server's headers")
}

func TestRun_OAuthChoiceTheIssuerIsNotBoundToIsAGap(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	credential := f.interactiveCredential()
	f.unboundOAuthSelection(credential, server, registration)

	report := f.run(true)
	require.Zero(t, report.SelectionsMapped)
	require.Len(t, selectionGaps(report, SelectionUnusable), 1, "the upstream client choice already rejects it")
	require.False(t, f.selection(credential, server).AuthClientID.Valid)
}

// A choice for a deleted server is never served again: it stays recorded but
// is not counted as live coverage, while a live server's unusable choice still
// is.
func TestRun_ChoiceForADeletedServerIsNotLiveCoverage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	live, deleted, registration := f.server(), f.server(), f.client("subject")
	f.associate(live, registration)
	credential := f.interactiveCredential()
	f.unboundOAuthSelection(credential, live, registration)
	f.unboundOAuthSelection(credential, deleted, registration)
	require.NoError(t, f.q.SoftDeleteMCPServerFixture(t.Context(), deleted))

	report := f.run(true)
	gaps := selectionGaps(report, SelectionUnusable)
	require.Len(t, gaps, 1)
	require.Equal(t, live, gaps[0].MCPServerID)
	require.Equal(t, 1, report.Selections[ClassInteractive].Selections)
	require.False(t, f.selection(credential, deleted).AuthClientID.Valid, "the deleted server's choice is kept as recorded")
}

// A choice already naming its registration's auth client no longer depends on
// the legacy issuer binding: the typed resolver serves it on the client
// alone, so removing the binding leaves it covered.
func TestRun_MappedOAuthChoiceKeepsCoverageWhenTheIssuerBindingIsRemoved(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	f.run(true)
	credential := f.interactiveCredential()
	f.oauthSelection(credential, server, registration)
	require.Equal(t, 1, f.run(true).SelectionsMapped)
	require.NoError(t, f.q.UnbindClientIssuerFixture(t.Context(), UnbindClientIssuerFixtureParams{RemoteSessionClientID: registration, UserSessionIssuerID: f.sessionIssuer()}))

	report := f.run(true)
	require.Equal(t, 1, report.Selections[ClassInteractive].Outcomes[SelectionMapped])
	require.Empty(t, report.SelectionGaps)
	require.True(t, f.selection(credential, server).AuthClientID.Valid)
}

func TestRun_MappedHeaderChoiceBesideANewOAuthAssociationIsStale(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)
	require.Equal(t, 1, f.run(true).SelectionsMapped)
	f.associate(server, f.client("subject"))

	report := f.run(true)
	gaps := selectionGaps(report, SelectionStaleNoAuth)
	require.Len(t, gaps, 1)
	require.Equal(t, credential, gaps[0].CredentialID)
	require.Zero(t, report.Selections[ClassInteractive].Outcomes[SelectionMapped])
}

// TestRun_ChoiceIsDecidedAgainUnderLock enables a second candidate while the
// mapping waits on it: the choice is decided again and reported, never
// written from the stale decision.
func TestRun_ChoiceIsDecidedAgainUnderLock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.run(true)
	second := f.adminHeaderClient(server)
	require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), SetAuthClientEnabledFixtureParams{Enabled: false, ID: second}))
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	tx, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; the write uses a generated SQLc method.
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	require.NoError(t, New(tx).SetAuthClientEnabledFixture(t.Context(), SetAuthClientEnabledFixtureParams{Enabled: true, ID: second}))

	done := make(chan Report, 1)
	go func() {
		report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
		assert.NoError(t, err)
		done <- report
	}()
	require.Eventually(t, func() bool {
		_, err := f.q.ProbeServerLockFixture(t.Context(), server)
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.LockNotAvailable
	}, 10*time.Second, 20*time.Millisecond, "the mapping holds the server and waits on the enabling write")
	require.NoError(t, tx.Commit(t.Context()))

	report := <-done
	require.Equal(t, 1, report.PendingSelectionMappings, "the first read saw one candidate")
	require.Zero(t, report.SelectionsMapped)
	require.Equal(t, 1, report.Selections[ClassInteractive].Outcomes[SelectionAmbiguous])
	require.Equal(t, 1, report.Selections[ClassInteractive].Gaps())
	require.Len(t, selectionGaps(report, SelectionAmbiguous), 1)
	require.False(t, f.selection(credential, server).AuthClientID.Valid)
}

// TestRun_OAuthChoiceIsNotMappedAfterItsIssuerBindingIsRemoved removes the
// binding an unmapped OAuth choice is decided from while the mapping waits on
// it: the choice is decided again and left unmapped.
func TestRun_OAuthChoiceIsNotMappedAfterItsIssuerBindingIsRemoved(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	f.run(true)
	credential := f.interactiveCredential()
	f.oauthSelection(credential, server, registration)

	tx, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; the write uses a generated SQLc method.
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	require.NoError(t, New(tx).UnbindClientIssuerFixture(t.Context(), UnbindClientIssuerFixtureParams{RemoteSessionClientID: registration, UserSessionIssuerID: f.sessionIssuer()}))

	done := make(chan Report, 1)
	go func() {
		report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
		assert.NoError(t, err)
		done <- report
	}()
	require.Eventually(t, func() bool {
		_, err := f.q.ProbeServerLockFixture(t.Context(), server)
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.LockNotAvailable
	}, 10*time.Second, 20*time.Millisecond, "the mapping holds the server and waits on the binding")
	require.NoError(t, tx.Commit(t.Context()))

	report := <-done
	require.Equal(t, 1, report.PendingSelectionMappings, "the first read saw the binding")
	require.Zero(t, report.SelectionsMapped)
	require.Len(t, selectionGaps(report, SelectionUnusable), 1)
	require.False(t, f.selection(credential, server).AuthClientID.Valid)
}

// TestRun_ChoicesAreNotMappedAfterAFailedPhase is not parallel: it swaps
// beforePhase.
func TestRun_ChoicesAreNotMappedAfterAFailedPhase(t *testing.T) { //nolint:paralleltest // swaps a package seam
	f := newFixture(t)
	server, registration := f.server(), f.client("subject")
	f.associate(server, registration)
	f.run(true)
	credential := f.interactiveCredential()
	f.oauthSelection(credential, server, registration)
	original := beforePhase
	beforePhase = func(name string) error {
		if name == PhaseReconcile {
			return errors.New("injected failure")
		}
		return nil
	}
	t.Cleanup(func() { beforePhase = original })

	report := f.run(true)
	require.Contains(t, report.PhaseErrors, PhaseReconcile)
	require.Equal(t, 1, report.PendingSelectionMappings)
	require.Zero(t, report.SelectionsMapped)
	require.False(t, f.selection(credential, server).AuthClientID.Valid)
}
