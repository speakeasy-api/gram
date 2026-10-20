package authclientbackfill

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

const (
	secretValue  = "enc:v1:not-a-real-secret"
	rotatedValue = "enc:v1:rotated-not-a-real-secret"
)

func (f *fixture) source() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(f.t.Context(), SeedRemoteMCPServerFixtureParams{ID: id, ProjectID: f.projectID, Url: "https://mcp.example.com/" + id.String()[:8]}))
	return id
}

func (f *fixture) serverOn(sourceID uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedMCPServerFixture(f.t.Context(), SeedMCPServerFixtureParams{
		ID:                id,
		ProjectID:         f.projectID,
		Slug:              pgtype.Text{String: "s-" + id.String()[:8], Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: sourceID, Valid: true},
	}))
	return id
}

func (f *fixture) header(sourceID uuid.UUID, name string, secret bool, value, forwarded string) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedRemoteHeaderFixture(f.t.Context(), SeedRemoteHeaderFixtureParams{
		RemoteMcpServerID:      sourceID,
		Name:                   name,
		IsSecret:               secret,
		Value:                  pgtype.Text{String: value, Valid: value != ""},
		ValueFromRequestHeader: pgtype.Text{String: forwarded, Valid: forwarded != ""},
	})
	require.NoError(f.t, err)
	return id
}

func (f *fixture) staticHeader(sourceID uuid.UUID, name string) uuid.UUID {
	return f.header(sourceID, name, true, secretValue, "")
}

func (f *fixture) forwardedHeader(sourceID uuid.UUID) uuid.UUID {
	return f.header(sourceID, "Authorization", false, "", "X-Upstream-Authorization")
}

func (f *fixture) summaries() []ListAuthClientSummariesFixtureRow {
	f.t.Helper()
	rows, err := f.q.ListAuthClientSummariesFixture(f.t.Context(), f.orgID)
	require.NoError(f.t, err)
	return rows
}

func (f *fixture) live(clientType string) []ListAuthClientSummariesFixtureRow {
	f.t.Helper()
	var out []ListAuthClientSummariesFixtureRow
	for _, c := range f.summaries() {
		if c.ClientType == clientType && !c.Deleted {
			out = append(out, c)
		}
	}
	return out
}

func (f *fixture) headers(clientID uuid.UUID) []ListAuthClientHeadersFixtureRow {
	f.t.Helper()
	rows, err := f.q.ListAuthClientHeadersFixture(f.t.Context(), clientID)
	require.NoError(f.t, err)
	return rows
}

func (f *fixture) userSessionIssuer() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedUserSessionIssuerFixture(f.t.Context(), SeedUserSessionIssuerFixtureParams{
		ID:        id,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "usi-" + id.String()[:8],
	}))
	return id
}

// emaClient seeds a confidential client and its issuer for an EMA upstream.
func (f *fixture) emaClient() (clientID, issuerID uuid.UUID) {
	f.t.Helper()
	ctx := f.t.Context()
	issuerID, clientID = uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://auth.vendor.example.com/" + issuerID.String()[:8],
		Name:      pgtype.Text{String: "Vendor", Valid: true},
	}))
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                      clientID,
		ProjectID:               uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:          pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                "cid-" + clientID.String()[:8],
		CredentialOwner:         "self",
		TokenEndpointAuthMethod: pgtype.Text{String: "client_secret_basic", Valid: true},
	}))
	return clientID, issuerID
}

func (f *fixture) serverAt(url string, usi uuid.UUID) uuid.UUID {
	f.t.Helper()
	sourceID := uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(f.t.Context(), SeedRemoteMCPServerFixtureParams{ID: sourceID, ProjectID: f.projectID, Url: url}))
	serverID := f.serverOn(sourceID)
	require.NoError(f.t, f.q.SetServerUserSessionIssuerFixture(f.t.Context(), SetServerUserSessionIssuerFixtureParams{UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}, ID: serverID}))
	return serverID
}

func (f *fixture) binding(usi, issuerID, clientID uuid.UUID, resource string, scopes []string) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedEMABindingFixture(f.t.Context(), SeedEMABindingFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
		UserSessionIssuerID:   usi,
		RemoteSessionIssuerID: issuerID,
		Resource:              resource,
		RemoteSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true},
		RequestedScopes:       scopes,
	})
	require.NoError(f.t, err)
	return id
}

func results(report Report, kind Kind, outcome Outcome) []Result {
	var out []Result
	for _, r := range report.Results {
		if r.Kind == kind && r.Outcome == outcome {
			out = append(out, r)
		}
	}
	return out
}

func TestRun_StaticHeaderIsOneClientOfferedByEveryServerOnTheSource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	first, second := f.serverOn(source), f.serverOn(source)
	f.staticHeader(source, "Authorization")

	dry := f.run(false)
	require.Empty(t, f.summaries(), "dry run writes nothing")
	require.Equal(t, 1, dry.PendingClients)
	require.Equal(t, 2, dry.PendingLinks)
	require.Equal(t, 1, dry.PendingHeaders)

	applied := f.run(true)
	require.Equal(t, dry.PendingClients, applied.PendingClients, "dry run matches apply")
	require.Equal(t, dry.PendingLinks, applied.PendingLinks, "dry run matches apply")
	require.Equal(t, dry.Outcomes, applied.Outcomes, "dry run matches apply")
	require.Equal(t, 1, applied.ByKind[KindHeader].PendingClients)

	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	require.Equal(t, uuid.NullUUID{UUID: f.projectID, Valid: true}, clients[0].ProjectID)
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(first))
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(second))
	headers := f.headers(clients[0].ID)
	require.Len(t, headers, 1)
	require.Equal(t, "Authorization", headers[0].Name)
	require.Equal(t, secretValue, headers[0].Value.String, "the stored value is copied as is, never decrypted")

	again := f.run(true)
	require.Zero(t, again.ClientsWritten)
	require.Zero(t, again.LinksWritten)
	require.Zero(t, again.HeadersWritten)
	require.Equal(t, clients[0].ID, f.live(TypeStaticCredential)[0].ID, "rerun finds the same client")
}

func TestRun_ForwardedHeaderIsAPassThroughClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.forwardedHeader(source)

	f.run(true)
	clients := f.live(TypePassThrough)
	require.Len(t, clients, 1)
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))
	headers := f.headers(clients[0].ID)
	require.Len(t, headers, 1)
	require.False(t, headers[0].Value.Valid)
	require.Equal(t, "X-Upstream-Authorization", headers[0].ValueFromRequestHeader.String)
}

func TestRun_StaticHeaderBesideAnOAuthClientIsLinkedToo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.associate(server, f.client("subject"))
	f.staticHeader(source, "Authorization")

	report := f.run(true)
	require.Empty(t, results(report, KindHeader, OutcomeAmbiguous))
	staticID := f.live(TypeStaticCredential)[0].ID
	oauthID := f.live(TypeRemoteSession)[0].ID
	require.ElementsMatch(t, []uuid.UUID{staticID, oauthID}, f.links(server), "both identities are offered")
}

func TestRun_TwoAuthorizationHeadersAreAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.staticHeader(source, "authorization")

	report := f.run(true)
	ambiguous := results(report, KindHeader, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonMultipleAuthorizationHeaders, ambiguous[0].Reason)
	require.Equal(t, source, ambiguous[0].SubjectID)
	require.Empty(t, f.summaries())
}

func TestRun_HeaderClientsStayInTheirOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	source := other.source()
	other.serverOn(source)
	other.staticHeader(source, "Authorization")

	report := f.run(true)
	require.Zero(t, report.Associations)
	require.Empty(t, f.summaries())
	require.Empty(t, other.summaries())
}

func TestRun_RetiredHeaderClientIsRevivedWhileTheHeaderExists(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.run(true)
	clients := f.summaries()
	require.Len(t, clients, 1)
	require.NoError(t, f.q.SoftDeleteAuthClientFixture(t.Context(), clients[0].ID))

	report := f.run(true)
	require.Equal(t, 1, report.ClientsWritten)
	require.Equal(t, clients[0].ID, f.live(TypeStaticCredential)[0].ID, "the stable id is reused")
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))
}

func TestRun_RemovedHeaderRetiresItsClientLinksAndCopies(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	header := f.staticHeader(source, "Authorization")
	f.run(true)
	clientID := f.live(TypeStaticCredential)[0].ID
	require.NoError(t, f.q.SoftDeleteRemoteHeaderFixture(t.Context(), header))

	dry := f.run(false)
	require.Equal(t, 1, dry.PendingRetiredClients)
	require.Equal(t, 1, dry.PendingRetiredLinks)
	require.Equal(t, 2, dry.ByKind[KindHeader].PendingRetirements)
	require.Len(t, f.live(TypeStaticCredential), 1, "dry run retires nothing")

	applied := f.run(true)
	require.Equal(t, 1, applied.ClientsRetired)
	require.Equal(t, 1, applied.LinksRetired)
	require.Empty(t, f.live(TypeStaticCredential))
	require.Empty(t, f.links(server))
	require.Empty(t, f.headers(clientID), "header copies retire with their client")

	again := f.run(true)
	require.Zero(t, again.PendingRetiredClients)
	require.Zero(t, again.PendingRetiredLinks)
}

func TestRun_SwitchingStoredAndForwardedReplacesTheClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	header := f.staticHeader(source, "Authorization")
	f.run(true)
	staticID := f.live(TypeStaticCredential)[0].ID
	require.NoError(t, f.q.UpdateRemoteHeaderFixture(t.Context(), UpdateRemoteHeaderFixtureParams{
		Name:                   "Authorization",
		Value:                  pgtype.Text{String: "", Valid: false},
		ValueFromRequestHeader: pgtype.Text{String: "X-Upstream-Authorization", Valid: true},
		IsSecret:               false,
		ID:                     header,
	}))

	report := f.run(true)
	require.Equal(t, 1, report.ClientsWritten)
	require.Equal(t, 1, report.ClientsRetired)
	require.Empty(t, f.live(TypeStaticCredential))
	passID := f.live(TypePassThrough)[0].ID
	require.NotEqual(t, staticID, passID)
	require.Equal(t, []uuid.UUID{passID}, f.links(server))
}

func TestRun_HeaderCopiesFollowTheirSource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	header := f.forwardedHeader(source)
	f.run(true)
	clientID := f.live(TypePassThrough)[0].ID

	update := func(name, forwarded string) {
		require.NoError(t, f.q.UpdateRemoteHeaderFixture(t.Context(), UpdateRemoteHeaderFixtureParams{
			Name:                   name,
			Value:                  pgtype.Text{String: "", Valid: false},
			ValueFromRequestHeader: pgtype.Text{String: forwarded, Valid: true},
			IsSecret:               false,
			ID:                     header,
		}))
	}

	// Mapping change.
	update("Authorization", "X-Other-Authorization")
	dry := f.run(false)
	require.Equal(t, 1, dry.PendingHeaders, "dry run reports the pending update")
	applied := f.run(true)
	require.Equal(t, 1, applied.HeadersWritten)
	require.Len(t, results(applied, KindHeader, OutcomeUpdated), 1)
	require.Equal(t, "X-Other-Authorization", f.headers(clientID)[0].ValueFromRequestHeader.String)

	// Letter-case rename keeps the same copy.
	update("authorization", "X-Other-Authorization")
	f.run(true)
	headers := f.headers(clientID)
	require.Len(t, headers, 1)
	require.Equal(t, "authorization", headers[0].Name)
	require.Equal(t, clientID, f.live(TypePassThrough)[0].ID)
}

func TestRun_RotatedStoredValueUpdatesTheCopy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	header := f.staticHeader(source, "Authorization")
	f.run(true)
	clientID := f.live(TypeStaticCredential)[0].ID
	require.NoError(t, f.q.UpdateRemoteHeaderFixture(t.Context(), UpdateRemoteHeaderFixtureParams{
		Name:                   "Authorization",
		Value:                  pgtype.Text{String: rotatedValue, Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
		IsSecret:               true,
		ID:                     header,
	}))

	applied := f.run(true)
	require.Equal(t, 1, applied.HeadersWritten)
	require.Equal(t, rotatedValue, f.headers(clientID)[0].Value.String)
	require.Zero(t, f.run(true).HeadersWritten, "in sync after the update")
}

func TestRun_SecretHeadersWithoutAuthorizationFormAClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.header(source, "X-Api-Key", true, secretValue, "")
	f.header(source, "X-Api-Version", false, "2024-01-01", "")

	f.run(true)
	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))
	headers := f.headers(clients[0].ID)
	require.Len(t, headers, 1, "a non-secret static header stays a server header")
	require.Equal(t, "X-Api-Key", headers[0].Name)
}

func TestRun_NonSecretStaticHeadersStayServerHeaders(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.header(source, "X-Api-Version", false, "2024-01-01", "")

	report := f.run(true)
	require.Zero(t, report.Associations)
	require.Empty(t, f.summaries())
}

func TestRun_CredentialHeadersOnAnOAuthSourceAreCopiedOntoTheOAuthClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.associate(server, f.client("subject"))
	key := f.header(source, "X-Api-Key", true, secretValue, "")

	dry := f.run(false)
	require.Equal(t, 1, dry.ByKind[KindOAuth].PendingHeaders, "dry run counts the copy")

	f.run(true)
	require.Empty(t, f.live(TypeStaticCredential), "no separate client for a server with an OAuth client")
	oauthID := f.live(TypeRemoteSession)[0].ID
	headers := f.headers(oauthID)
	require.Len(t, headers, 1)
	require.Equal(t, "X-Api-Key", headers[0].Name)

	require.NoError(t, f.q.SoftDeleteRemoteHeaderFixture(t.Context(), key))
	f.run(true)
	require.Empty(t, f.headers(oauthID), "a copy no source names is retired")
}

func TestRun_MixedCredentialHeadersAreAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.header(source, "X-Api-Key", true, secretValue, "")
	f.header(source, "X-Token", false, "", "X-Caller-Token")

	report := f.run(true)
	ambiguous := results(report, KindHeader, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonMixedCredentialHeaders, ambiguous[0].Reason)
	require.Equal(t, 1, report.ByKind[KindHeader].Unresolved)
}

func TestRun_HeaderPagesDecideEachSourceOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for range 3 {
		source := f.source()
		f.serverOn(source)
		f.staticHeader(source, "Authorization")
	}
	report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 1})
	require.NoError(t, err)
	seen := map[uuid.UUID]int{}
	for _, r := range report.Results {
		if r.Kind == KindHeader && r.MCPServerID == uuid.Nil {
			seen[r.SubjectID]++
		}
	}
	require.Len(t, seen, 3)
	for _, n := range seen {
		require.Equal(t, 1, n)
	}
	require.Len(t, f.live(TypeStaticCredential), 3)
}

func TestRun_ReportCarriesNoHeaderValues(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.staticHeader(source, "Authorization")
	report := f.run(true)
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(raw), secretValue)
}

func TestRun_EMABindingsAcrossServersAreOneClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi, otherUSI := f.userSessionIssuer(), f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	first := f.serverAt(resource, usi)
	second := f.serverAt(resource+"/", otherUSI)
	f.binding(usi, issuerID, clientID, resource, []string{"read", "write"})
	f.binding(otherUSI, issuerID, clientID, resource, []string{"write", "read"})

	dry := f.run(false)
	applied := f.run(true)
	require.Equal(t, dry.Outcomes, applied.Outcomes, "dry run matches apply")
	created := results(applied, KindEMA, OutcomeCreated)
	require.Len(t, created, 1)
	require.Len(t, created[0].BindingIDs, 2)

	ema := f.live(TypeEMA)
	require.Len(t, ema, 1, "bindings for one upstream become one client")
	details, err := f.q.GetEMAFixture(t.Context(), ema[0].ID)
	require.NoError(t, err)
	require.Equal(t, issuerID, details.RemoteSessionIssuerID)
	require.Equal(t, clientID, details.RemoteSessionClientID)
	require.Equal(t, resource, details.Resource)
	require.Equal(t, []string{"read", "write"}, details.RequestedScopes)
	require.Contains(t, f.links(first), ema[0].ID)
	require.Contains(t, f.links(second), ema[0].ID)
	for _, link := range results(applied, KindEMA, OutcomeAssociated) {
		require.Equal(t, f.projectID, link.ProjectID, "a link names its server's project")
	}

	again := f.run(true)
	require.Zero(t, again.ClientsWritten)
	require.Zero(t, again.LinksWritten)
}

func TestRun_EMABindingsThatDisagreeAreAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"

	usi, otherUSI := f.userSessionIssuer(), f.userSessionIssuer()
	f.serverAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{"read"})
	f.binding(otherUSI, issuerID, clientID, resource, []string{"read", "write"})
	report := f.run(true)
	ambiguous := results(report, KindEMA, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonConflictingScopes, ambiguous[0].Reason)
	require.Len(t, ambiguous[0].BindingIDs, 2, "every binding in the group is listed")
	require.Empty(t, f.live(TypeEMA))
}

func TestRun_EMAResourceVariantsAreAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	usi, otherUSI := f.userSessionIssuer(), f.userSessionIssuer()
	f.serverAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{})
	f.binding(otherUSI, issuerID, clientID, resource+"/", []string{})

	report := f.run(true)
	ambiguous := results(report, KindEMA, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonResourceVariants, ambiguous[0].Reason)
}

func TestRun_EMABindingReachingNoServerIsNotMatched(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	f.binding(usi, issuerID, clientID, "https://unreached.example.com/mcp", []string{})

	report := f.run(true)
	ambiguous := results(report, KindEMA, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonResourceNotMatched, ambiguous[0].Reason)
	require.Empty(t, f.live(TypeEMA))
}

func TestRun_EMAMatchesAPersistedProtectedResource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	server := f.serverAt("https://mcp.vendor.example.com/sse", usi)
	resource := "https://mcp.vendor.example.com/"
	f.validProtectedResource(resource, "https://mcp.vendor.example.com/.well-known/oauth-protected-resource/sse", nil)
	f.binding(usi, issuerID, clientID, resource, []string{})

	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server))
}

func TestRun_EMAMatchesATunneledServerUnderItsOwnIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://tunnel.vendor.example.com/mcp"
	sourceID, serverID := uuid.New(), uuid.New()
	require.NoError(t, f.q.SeedTunneledSourceFixture(t.Context(), SeedTunneledSourceFixtureParams{
		ID: sourceID, ProjectID: f.projectID, Name: "t-" + sourceID.String()[:8], KeyHash: "h-" + sourceID.String(),
		ResourceIdentifier: pgtype.Text{String: resource, Valid: true},
	}))
	require.NoError(t, f.q.SeedTunneledMCPServerFixture(t.Context(), SeedTunneledMCPServerFixtureParams{
		ID: serverID, ProjectID: f.projectID, Slug: pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: sourceID, Valid: true},
	}))
	require.NoError(t, f.q.SetServerUserSessionIssuerFixture(t.Context(), SetServerUserSessionIssuerFixtureParams{UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}, ID: serverID}))
	f.binding(usi, issuerID, clientID, resource, []string{})

	// Without its own derived issuer the tunneled server is not reached.
	report := f.run(false)
	require.Len(t, results(report, KindEMA, OutcomeAmbiguous), 1)

	require.NoError(t, f.q.SetServerRemoteSessionIssuerFixture(t.Context(), SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}, ID: serverID}))
	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(serverID))
}

func TestRun_EMAScopesFollowTheBindings(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	f.serverAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{"read"})
	f.run(true)
	emaID := f.live(TypeEMA)[0].ID
	require.NoError(t, f.q.SetEMAStoredScopesFixture(t.Context(), SetEMAStoredScopesFixtureParams{RequestedScopes: []string{"stale"}, AuthClientID: emaID}))

	dry := f.run(false)
	require.Equal(t, 1, dry.ByKind[KindEMA].PendingHeaders)
	applied := f.run(true)
	require.Len(t, results(applied, KindEMA, OutcomeUpdated), 1)
	details, err := f.q.GetEMAFixture(t.Context(), emaID)
	require.NoError(t, err)
	require.Equal(t, []string{"read"}, details.RequestedScopes)
}

func TestRun_EMAClientsArePerOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	resource := "https://mcp.vendor.example.com/mcp"
	for _, fx := range []*fixture{f, other} {
		usi := fx.userSessionIssuer()
		clientID, issuerID := fx.emaClient()
		fx.serverAt(resource, usi)
		fx.binding(usi, issuerID, clientID, resource, []string{})
	}
	f.run(true)
	other.run(true)
	require.Len(t, f.live(TypeEMA), 1)
	require.Len(t, other.live(TypeEMA), 1)
}

func TestRun_RemovedBindingRetiresTheEMAClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	server := f.serverAt(resource, usi)
	binding := f.binding(usi, issuerID, clientID, resource, []string{})
	f.run(true)
	emaID := f.live(TypeEMA)[0].ID
	require.NoError(t, f.q.DeleteEMABindingFixture(t.Context(), binding))

	report := f.run(true)
	require.Equal(t, 1, report.ClientsRetired)
	require.Equal(t, 1, report.LinksRetired)
	require.Empty(t, f.links(server))
	deleted, err := f.q.GetEMADeletedFixture(t.Context(), emaID)
	require.NoError(t, err)
	require.True(t, deleted, "the EMA details retire with their client")

	// The upstream comes back: a new client is recorded.
	f.binding(usi, issuerID, clientID, resource, []string{})
	require.Equal(t, 1, f.run(true).ClientsWritten)
}

func TestRun_HoldsTheOrganizationLock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	tx, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; the lock uses a generated SQLc method.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	require.NoError(t, New(tx).LockOrganizationBackfill(t.Context(), f.orgID))

	for _, apply := range []bool{false, true} {
		start := time.Now()
		_, err = Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: apply, PageSize: 0})
		require.ErrorIs(t, err, ErrRunInProgress, "a run never waits for a concurrent one")
		require.Less(t, time.Since(start), 5*time.Second)
	}

	require.NoError(t, tx.Rollback(t.Context()))
	_, err = Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
}

// TestRun_AFailedPhaseLeavesTheOthers is not parallel: it swaps beforePhase.
func TestRun_AFailedPhaseLeavesTheOthers(t *testing.T) { //nolint:paralleltest // swaps a package seam
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.associate(server, f.client("subject"))
	f.staticHeader(source, "Authorization")
	f.run(true)
	staticID := f.live(TypeStaticCredential)[0].ID

	original := beforePhase
	beforePhase = func(name string) error {
		if name == PhaseHeaders {
			return errors.New("injected")
		}
		return nil
	}
	t.Cleanup(func() { beforePhase = original })

	report := f.run(true)
	require.Contains(t, report.PhaseErrors, PhaseHeaders)
	require.NotContains(t, report.PhaseErrors, PhaseOAuth)
	require.Equal(t, staticID, f.live(TypeStaticCredential)[0].ID, "a failed phase's records are not retired")
	require.Contains(t, f.links(server), staticID)
}

func TestRun_MarksHeaderAndEMAServersAsNeedingUpstreamAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	headerServer := f.serverOn(source)
	decidedNone := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}, ID: decidedNone}))

	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	emaServer := f.serverAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{})

	dry := f.run(false)
	require.Equal(t, 2, dry.PendingMarkedServers, "the explicitly undecided header and EMA servers")
	applied := f.run(true)
	require.Equal(t, 2, applied.ServersMarked)

	for _, server := range []uuid.UUID{headerServer, emaServer} {
		got, err := f.q.GetRequiresUpstreamAuthFixture(t.Context(), server)
		require.NoError(t, err)
		require.True(t, got.Valid && got.Bool, "a server offering a backfilled client needs upstream auth")
	}
	got, err := f.q.GetRequiresUpstreamAuthFixture(t.Context(), decidedNone)
	require.NoError(t, err)
	require.True(t, got.Valid && !got.Bool, "an explicit decision is never overwritten")
	require.Zero(t, f.run(true).ServersMarked)
}

func TestRun_NeverCopiesCredentialHeadersOntoAnOAuthClientSharedAcrossSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	oauthClient := f.client("subject")
	plain := f.source()
	s1, s3 := f.serverOn(plain), f.serverOn(f.source())
	keyed := f.source()
	s2 := f.serverOn(keyed)
	for _, server := range []uuid.UUID{s1, s2, s3} {
		f.associate(server, oauthClient)
	}
	f.header(keyed, "X-Tenant-Key", true, secretValue, "")

	report := f.run(true)
	oauthID := f.live(TypeRemoteSession)[0].ID
	require.Empty(t, f.headers(oauthID), "one source's key never reaches the other sources' servers")
	ambiguous := results(report, KindOAuth, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonOAuthClientSharedAcrossSources, ambiguous[0].Reason)
	require.Equal(t, oauthClient, ambiguous[0].SubjectID)
	require.Len(t, ambiguous[0].SourceIDs, 3)
	require.ElementsMatch(t, []uuid.UUID{s1, s2, s3}, ambiguous[0].ServerIDs)
}

func TestRun_EMASameOriginServerItCannotConfirmIsAWarning(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	exact := f.serverAt(resource, usi)
	child := f.serverAt("https://mcp.vendor.example.com/mcp-s23", usi)
	f.serverAt("https://other.example.com/mcp", usi)
	f.binding(usi, issuerID, clientID, resource, []string{})

	report := f.run(true)
	emaID := f.live(TypeEMA)[0].ID
	require.Equal(t, []uuid.UUID{emaID}, f.links(exact))
	require.Empty(t, f.links(child), "never linked on an unconfirmed parent resource")
	require.Empty(t, results(report, KindEMA, OutcomeAmbiguous))
	warnings := results(report, KindEMA, OutcomeWarning)
	require.Len(t, warnings, 1, "only the same-origin server is reported")
	require.Equal(t, ReasonServerNotMatched, warnings[0].Reason)
	require.Equal(t, child, warnings[0].MCPServerID)
	require.Len(t, warnings[0].BindingIDs, 1)
	require.Zero(t, report.Gaps, "a warning never holds the run back")
	require.Equal(t, 1, report.ByKind[KindEMA].Warnings)
	require.Zero(t, report.ByKind[KindEMA].Unresolved)
	dry := f.run(false)
	require.Zero(t, dry.PendingClients+dry.PendingLinks+dry.PendingHeaders+dry.PendingRetiredClients+dry.PendingRetiredLinks+dry.PendingMarkedServers)
}

func TestRun_RetiresCopiesOnceAnOAuthClientIsSharedAcrossSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	oauthClient := f.client("subject")
	keyed := f.source()
	f.associate(f.serverOn(keyed), oauthClient)
	f.header(keyed, "X-Tenant-Key", true, secretValue, "")
	f.run(true)
	oauthID := f.live(TypeRemoteSession)[0].ID
	require.Len(t, f.headers(oauthID), 1, "copied while one source uses the client")

	f.associate(f.serverOn(f.source()), oauthClient)
	f.run(true)
	require.Empty(t, f.headers(oauthID), "retired once another source's server uses the client")
}

// sourceAt seeds a remote source at url with one server signed in through usi.
func (f *fixture) sourceAt(url string, usi uuid.UUID) (sourceID, serverID uuid.UUID) {
	f.t.Helper()
	sourceID = uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(f.t.Context(), SeedRemoteMCPServerFixtureParams{ID: sourceID, ProjectID: f.projectID, Url: url}))
	serverID = f.serverOn(sourceID)
	require.NoError(f.t, f.q.SetServerUserSessionIssuerFixture(f.t.Context(), SetServerUserSessionIssuerFixtureParams{UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}, ID: serverID}))
	return sourceID, serverID
}

// hostedServer seeds a server with no remote or tunneled source.
func (f *fixture) hostedServer() uuid.UUID {
	f.t.Helper()
	toolsetID, id := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedToolsetFixture(f.t.Context(), SeedToolsetFixtureParams{ID: toolsetID, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "Toolset", Slug: "t-" + toolsetID.String()[:8]}))
	require.NoError(f.t, f.q.SeedHostedMCPServerFixture(f.t.Context(), SeedHostedMCPServerFixtureParams{
		ID:        id,
		ProjectID: f.projectID,
		Slug:      pgtype.Text{String: "h-" + id.String()[:8], Valid: true},
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
	}))
	return id
}

func (f *fixture) validProtectedResource(resource, metadataURL string, authorizationServers []string) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedValidProtectedResourceFixture(f.t.Context(), SeedValidProtectedResourceFixtureParams{
		ProjectID:            f.projectID,
		OrganizationID:       f.orgID,
		ResourceIdentifier:   resource,
		MetadataUrl:          pgtype.Text{String: metadataURL, Valid: true},
		AuthorizationServers: authorizationServers,
	}))
}

func issuerURL(issuerID uuid.UUID) string {
	return "https://auth.vendor.example.com/" + issuerID.String()[:8]
}

func TestRun_NeverCopiesCredentialHeadersOntoAnOAuthClientAHostedServerShares(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	oauthClient := f.client("subject")
	keyed := f.source()
	keyedServer, hosted := f.serverOn(keyed), f.hostedServer()
	f.associate(keyedServer, oauthClient)
	f.associate(hosted, oauthClient)
	f.header(keyed, "X-Tenant-Key", true, secretValue, "")

	dry := f.run(false)
	require.Zero(t, dry.ByKind[KindOAuth].PendingHeaders, "nothing to copy onto a shared client")
	report := f.run(true)
	oauthID := f.live(TypeRemoteSession)[0].ID
	require.Empty(t, f.headers(oauthID), "the key never reaches the hosted server")
	ambiguous := results(report, KindOAuth, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonOAuthClientSharedAcrossSources, ambiguous[0].Reason)
	require.ElementsMatch(t, []uuid.UUID{keyed, hosted}, ambiguous[0].SourceIDs, "a sourceless server is a source of its own")
	require.ElementsMatch(t, []uuid.UUID{keyedServer, hosted}, ambiguous[0].ServerIDs)
}

func TestHeaderCopyWritesRecheckTheClientStaysOnOneSource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	oauthClient := f.client("subject")
	keyed := f.source()
	f.associate(f.serverOn(keyed), oauthClient)
	key := f.header(keyed, "X-Tenant-Key", true, secretValue, "")
	f.run(true)
	oauthID := f.live(TypeRemoteSession)[0].ID
	require.Len(t, f.headers(oauthID), 1)
	plan, err := f.q.PlanClientHeaders(ctx, PlanClientHeadersParams{OrganizationID: f.orgID, RemoteHeaderIds: []uuid.UUID{key}, TunneledHeaderIds: []uuid.UUID{}, AuthClientID: oauthID})
	require.NoError(t, err)
	require.Len(t, plan, 1)
	copyID := plan[0].CopyID

	// Another server starts using the client after the copy was decided.
	f.associate(f.hostedServer(), oauthClient)
	required := uuid.NullUUID{UUID: oauthClient, Valid: true}
	n, err := f.q.UpdateHeaderCopy(ctx, UpdateHeaderCopyParams{SrcKind: "remote", SrcID: key, CopyID: copyID, OrganizationID: f.orgID, RequiredRemoteSessionClientID: required})
	require.NoError(t, err)
	require.Zero(t, n, "an update races once the client spans sources")
	_, err = f.q.RetireHeaderCopy(ctx, RetireHeaderCopyParams{CopyID: copyID, OrganizationID: f.orgID})
	require.NoError(t, err)
	n, err = f.q.InsertHeaderCopy(ctx, InsertHeaderCopyParams{SrcKind: "remote", SrcID: key, OrganizationID: f.orgID, AuthClientID: oauthID, RequiredRemoteSessionClientID: required})
	require.NoError(t, err)
	require.Zero(t, n, "an insert races once the client spans sources")
	n, err = f.q.InsertHeaderCopy(ctx, InsertHeaderCopyParams{SrcKind: "remote", SrcID: key, OrganizationID: f.orgID, AuthClientID: oauthID, RequiredRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "the source recheck alone still copies")
}

func TestCreateSourceAuthClientRechecksTheHeadersShareAType(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.header(source, "X-Api-Key", true, secretValue, "")
	params := CreateSourceAuthClientParams{
		ID:             SourceClientID(f.orgID, "remote", source, TypeStaticCredential),
		OrganizationID: f.orgID,
		ClientType:     TypeStaticCredential,
		Name:           staticCredentialName,
		SourceKind:     "remote",
		SourceID:       source,
		AnchorHeaderID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}

	// A forwarded header arrives after the source was decided all static.
	f.header(source, "X-Forwarded-Key", false, "", "X-Upstream-Key")
	n, err := f.q.CreateSourceAuthClient(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, n, "mixed headers race")
	require.Empty(t, f.summaries())
}

func TestReconcileDerived_KeepsARacedClientsLinks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.run(true)
	clientID := f.live(TypeStaticCredential)[0].ID

	want := newExpected()
	want.race(clientID)
	report := newReport(true)
	require.NoError(t, reconcileDerived(t.Context(), f.q, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0}, &report, want, map[Kind]bool{}))
	require.Zero(t, report.ClientsRetired+report.LinksRetired+report.PendingRetiredLinks)
	require.Equal(t, []uuid.UUID{clientID}, f.links(server), "a race never retires the client's links")

	report = newReport(true)
	require.NoError(t, reconcileDerived(t.Context(), f.q, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0}, &report, newExpected(), map[Kind]bool{}))
	require.Equal(t, 1, report.LinksRetired, "without the race the link is retired")
}

func TestRun_CaseVariantHeadersAreNeverCopiedOntoAnOAuthClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.associate(f.serverOn(source), f.client("subject"))
	f.header(source, "X-Api-Key", true, secretValue, "")
	f.run(true)
	oauthID := f.live(TypeRemoteSession)[0].ID
	require.Len(t, f.headers(oauthID), 1)

	f.header(source, "x-api-key", true, rotatedValue, "")
	dry := f.run(false)
	require.Equal(t, 1, dry.ByKind[KindOAuth].PendingHeaders, "the existing copy is to retire")
	report := f.run(true)
	require.Empty(t, f.headers(oauthID), "an existing copy is retired")
	ambiguous := results(report, KindOAuth, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonDuplicateHeaderNames, ambiguous[0].Reason)
	require.Equal(t, []uuid.UUID{source}, ambiguous[0].SourceIDs)
}

func TestRun_EMAServerNotMatchedSkipsServersCoveredOtherwise(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	resource := "https://mcp.vendor.example.com/mcp"
	f.serverAt(resource, usi)
	keyedSource, keyed := f.sourceAt("https://mcp.vendor.example.com/keyed", usi)
	f.staticHeader(keyedSource, "Authorization")
	_, elsewhere := f.sourceAt("https://mcp.vendor.example.com/elsewhere", usi)
	f.validProtectedResource("https://mcp.vendor.example.com/elsewhere", "https://mcp.vendor.example.com/.well-known/oauth-protected-resource/elsewhere", nil)
	_, unconfirmed := f.sourceAt("https://mcp.vendor.example.com/unconfirmed", usi)
	// Two upstreams at the origin still warn once per server.
	for range 2 {
		clientID, issuerID := f.emaClient()
		f.binding(usi, issuerID, clientID, resource, []string{})
	}

	report := f.run(true)
	warnings := results(report, KindEMA, OutcomeWarning)
	require.Len(t, warnings, 1)
	require.Equal(t, unconfirmed, warnings[0].MCPServerID)
	for _, w := range warnings {
		require.NotEqual(t, keyed, w.MCPServerID, "a server with a header client is covered")
		require.NotEqual(t, elsewhere, w.MCPServerID, "a server whose record names another resource is not a candidate")
	}
}

func TestRun_EMAHonorsThePersistedProtectedResource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()

	// The record lists other authorization servers only.
	clientID, issuerID := f.emaClient()
	refused := f.serverAt("https://mcp.vendor.example.com/sse", usi)
	f.validProtectedResource("https://mcp.vendor.example.com/sse", "https://mcp.vendor.example.com/.well-known/oauth-protected-resource/sse", []string{"https://other-auth.example.com"})
	f.binding(usi, issuerID, clientID, "https://mcp.vendor.example.com/sse", []string{})

	// The record names another resource than the server's URL.
	otherClient, otherIssuer := f.emaClient()
	renamed := f.serverAt("https://api.vendor.example.com/mcp", usi)
	f.validProtectedResource("https://api.vendor.example.com/", "https://api.vendor.example.com/.well-known/oauth-protected-resource/mcp", []string{issuerURL(otherIssuer)})
	f.binding(usi, otherIssuer, otherClient, "https://api.vendor.example.com/mcp", []string{})

	report := f.run(true)
	require.Empty(t, f.live(TypeEMA))
	require.Empty(t, f.links(refused))
	require.Empty(t, f.links(renamed))
	reasons := map[Reason]uuid.UUID{}
	for _, r := range results(report, KindEMA, OutcomeAmbiguous) {
		reasons[r.Reason] = r.MCPServerID
	}
	require.Equal(t, refused, reasons[ReasonIssuerNotAllowed])
	require.Contains(t, reasons, ReasonResourceNotMatched, "the URL fallback never overrides a valid record")

	// Listing the issuer lets the first upstream through.
	allowedClient, allowedIssuer := f.emaClient()
	allowed := f.serverAt("https://mcp2.vendor.example.com/sse", usi)
	f.validProtectedResource("https://mcp2.vendor.example.com/sse", "https://mcp2.vendor.example.com/.well-known/oauth-protected-resource/sse", []string{issuerURL(allowedIssuer) + "/"})
	f.binding(usi, allowedIssuer, allowedClient, "https://mcp2.vendor.example.com/sse", []string{})
	f.run(true)
	require.Len(t, f.live(TypeEMA), 1)
	require.Len(t, f.links(allowed), 1)
}

// TestRun_AFailedPhaseKeepsNoCounts is not parallel: it swaps afterPhase.
func TestRun_AFailedPhaseKeepsNoCounts(t *testing.T) { //nolint:paralleltest // swaps a package seam
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.staticHeader(source, "Authorization")

	original := afterPhase
	afterPhase = func(name string) error {
		if name == PhaseHeaders {
			return errors.New("injected")
		}
		return nil
	}
	t.Cleanup(func() { afterPhase = original })

	report := f.run(true)
	require.Contains(t, report.PhaseErrors, PhaseHeaders)
	require.Zero(t, report.ClientsWritten+report.LinksWritten+report.HeadersWritten, "a rolled-back phase reports no writes")
	require.Zero(t, report.ByKind[KindHeader].Written+report.ByKind[KindHeader].PendingClients+report.ByKind[KindHeader].Decided)
	require.Empty(t, results(report, KindHeader, OutcomeCreated))
	require.Empty(t, f.summaries(), "the phase's writes rolled back")
}

func TestRun_DryRunSkipsCopiesForAnUnrecordableOAuthClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.associate(f.serverOn(source), f.client("someone"))
	f.header(source, "X-Api-Key", true, secretValue, "")

	dry := f.run(false)
	require.Zero(t, dry.ByKind[KindOAuth].PendingHeaders, "no copies for a client the OAuth phase never records")
	require.Empty(t, results(dry, KindOAuth, OutcomeUpdated))
	applied := f.run(true)
	require.Zero(t, applied.ByKind[KindOAuth].PendingHeaders)
}

func TestRun_DryRunModelsReviving(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.header(source, "X-Api-Key", true, secretValue, "")
	f.run(true)
	clientID := f.live(TypeStaticCredential)[0].ID
	require.Len(t, f.headers(clientID), 2)
	// Only the client row is retired; its copies and link survive.
	require.NoError(t, f.q.SoftDeleteAuthClientFixture(t.Context(), clientID))

	dry := f.run(false)
	require.Equal(t, 1, dry.ByKind[KindHeader].PendingClients, "the client is revived")
	require.Zero(t, dry.ByKind[KindHeader].PendingLinks, "the surviving link is kept")
	require.Zero(t, dry.ByKind[KindHeader].PendingHeaders, "the surviving copies are kept")
	require.Zero(t, dry.ByKind[KindHeader].PendingRetirements)

	applied := f.run(true)
	require.Equal(t, dry.ByKind[KindHeader].PendingClients, applied.ByKind[KindHeader].PendingClients)
	require.Zero(t, applied.LinksRetired)
	require.Equal(t, []uuid.UUID{clientID}, f.links(server))
}

func TestRun_MarksOnlyServersWhoseHeaderOrEMAEvidenceHolds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	source := f.source()
	headerServer := f.serverOn(source)
	auth := f.staticHeader(source, "Authorization")
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	emaServer := f.serverAt(resource, usi)
	binding := f.binding(usi, issuerID, clientID, resource, []string{})
	f.run(true)

	servers := []uuid.UUID{headerServer, emaServer}
	undecided := pgtype.Bool{Bool: false, Valid: false}
	for _, server := range servers {
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: undecided, ID: server}))
	}
	listed, err := f.q.ListUndecidedUpstreamAuthServers(ctx, ListUndecidedUpstreamAuthServersParams{OrganizationID: f.orgID, McpServerIds: servers})
	require.NoError(t, err)
	require.ElementsMatch(t, servers, listed, "both still hold their evidence")

	require.NoError(t, f.q.SoftDeleteRemoteHeaderFixture(ctx, auth))
	require.NoError(t, f.q.DeleteEMABindingFixture(ctx, binding))
	listed, err = f.q.ListUndecidedUpstreamAuthServers(ctx, ListUndecidedUpstreamAuthServersParams{OrganizationID: f.orgID, McpServerIds: servers})
	require.NoError(t, err)
	require.Empty(t, listed)
	marked, err := f.q.MarkServersRequiringUpstreamAuth(ctx, MarkServersRequiringUpstreamAuthParams{OrganizationID: f.orgID, McpServerIds: servers})
	require.NoError(t, err)
	require.Empty(t, marked, "a link whose evidence is gone never marks its server")
}

func TestRun_AnAssociationOutOfReachLeavesCredentialHeadersTheirOwnClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	oauthClient := f.client("subject")
	f.associate(server, oauthClient)
	f.header(source, "X-Api-Key", true, secretValue, "")
	require.NoError(t, f.q.SoftDeleteIssuerOfClientFixture(t.Context(), oauthClient))

	f.run(true)
	static := f.live(TypeStaticCredential)
	require.Len(t, static, 1, "the OAuth phase never records a client whose issuer is gone")
	require.Equal(t, []uuid.UUID{static[0].ID}, f.links(server))
}
