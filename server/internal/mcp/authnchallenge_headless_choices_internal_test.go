package mcp

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestParseRequestedAuthClients(t *testing.T) {
	t.Parallel()

	server, client := uuid.New(), uuid.New()
	got, err := parseRequestedAuthClients([]string{server.String() + ":" + client.String()})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: client, Valid: true}, got[server].clientID)

	got, err = parseRequestedAuthClients([]string{server.String() + ":none"})
	require.NoError(t, err)
	require.False(t, got[server].clientID.Valid)

	for name, values := range map[string][]string{
		"no separator": {server.String()},
		"bad server":   {"x:" + client.String()},
		"bad client":   {server.String() + ":x"},
		"server twice": {server.String() + ":none", server.String() + ":" + client.String()},
	} {
		_, err := parseRequestedAuthClients(values)
		requireOAuthCode(t, err, "invalid_request", name)
	}
}

func TestHeadlessAuthClientPicks(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_headless_choices")
	require.NoError(t, err)
	user := urn.NewUserSubject("user_1")

	// setup returns an enforcing organization whose server signs in through a
	// sign-in with one registered client, the headless caller.
	setup := func(t *testing.T) (*authClientFixture, *ResolvedMcpEndpoint, headlessCaller) {
		t.Helper()
		f := newAuthClientFixture(t, pool)
		f.enforce()
		issuerID := uuid.New()
		require.NoError(t, selectionbackfill.New(pool).SeedUserSessionIssuerFixture(t.Context(), selectionbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: validUUID(f.projectID), OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, Slug: "usi-" + issuerID.String()[:8]}))
		client, err := usersessions_repo.New(pool).CreateUserSessionClient(t.Context(), usersessions_repo.CreateUserSessionClientParams{
			ClientID: "headless-" + issuerID.String()[:8], ClientName: "Headless client", RedirectUris: []string{}, TokenEndpointAuthMethod: "none", UserSessionIssuerID: issuerID,
		})
		require.NoError(t, err)
		endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID), UserSessionIssuerID: issuerID}
		return f, endpoint, headlessCaller{clientID: validUUID(client.ID)}
	}
	shared := func(f *authClientFixture) uuid.UUID {
		id := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(id, "X-Api-Key", false, false, text("plain-value"), pgtype.Text{})
		return id
	}

	t.Run("the only ready credential is chosen", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		only := shared(f)
		_, picks, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil)
		require.NoError(t, err)
		require.Equal(t, map[uuid.UUID]uuid.UUID{f.serverID: only}, picks)
	})

	t.Run("several ready credentials need a choice", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		shared(f)
		second := shared(f)
		_, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil)
		requireOAuthCode(t, err, tokenErrorSelectionRequired, "no choice named")

		_, picks, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {clientID: validUUID(second)}})
		require.NoError(t, err)
		require.Equal(t, second, picks[f.serverID], "the request names the choice")
	})

	t.Run("an issuance profile names the choice for an unmodified client", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		shared(f)
		configured := shared(f)
		profile, err := authclientsrepo.New(pool).CreateIssuanceProfile(t.Context(), authclientsrepo.CreateIssuanceProfileParams{
			OrganizationID: f.orgID, ProjectID: validUUID(f.projectID), UserSessionIssuerID: endpoint.UserSessionIssuerID,
			UserSessionClientID: caller.clientID, Name: "Headless client",
		})
		require.NoError(t, err)
		profileID := profile.ID
		_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(t.Context(), upstreamclientsrepo.RecordAuthClientChoiceParams{
			CredentialKind: upstreamclients.CredentialKindIssuanceProfile, CredentialID: profileID, Source: upstreamclients.SelectionSourceIssuance,
			OrganizationID: f.orgID, AuthClientID: validUUID(configured), McpServerID: f.serverID,
		})
		require.NoError(t, err)
		_, picks, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil)
		require.NoError(t, err)
		require.Equal(t, configured, picks[f.serverID])
	})

	t.Run("a profile choice that no longer validates gives way to the only ready credential", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		only := shared(f)
		profile, err := authclientsrepo.New(pool).CreateIssuanceProfile(t.Context(), authclientsrepo.CreateIssuanceProfileParams{
			OrganizationID: f.orgID, ProjectID: validUUID(f.projectID), UserSessionIssuerID: endpoint.UserSessionIssuerID,
			UserSessionClientID: caller.clientID, Name: "Headless client",
		})
		require.NoError(t, err)
		_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(t.Context(), upstreamclientsrepo.RecordAuthClientChoiceParams{
			CredentialKind: upstreamclients.CredentialKindIssuanceProfile, CredentialID: profile.ID, Source: upstreamclients.SelectionSourceIssuance,
			OrganizationID: f.orgID, AuthClientID: uuid.NullUUID{}, McpServerID: f.serverID,
		})
		require.NoError(t, err)
		_, picks, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil)
		require.NoError(t, err)
		require.Equal(t, only, picks[f.serverID])
	})

	t.Run("a choice the subject may not use is refused, not replaced", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		named := shared(f)
		f.service.authClientPermitter = denyPermitter{}
		_, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {clientID: validUUID(named)}})
		requireOAuthCode(t, err, "access_denied", "denied choice")
		_, _, err = f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {clientID: validUUID(uuid.New())}})
		requireOAuthCode(t, err, "access_denied", "foreign choice")
	})

	t.Run("a server the token does not reach cannot be named", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		shared(f)
		_, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{uuid.New(): {}})
		requireOAuthCode(t, err, "invalid_request", "out of scope")
	})

	t.Run("incomplete setup is told apart from a missing choice", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: f.serverID, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
		_, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil)
		requireOAuthCode(t, err, tokenErrorSetupRequired, "required with nothing offered")
	})

	t.Run("organizations that do not enforce refuse named choices and decide nothing", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID), UserSessionIssuerID: uuid.New()}
		caller := headlessCaller{clientID: validUUID(uuid.New())}
		shared(f)
		up, picks, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil)
		require.NoError(t, err)
		require.Nil(t, up)
		require.Nil(t, picks)
		_, _, err = f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {}})
		requireOAuthCode(t, err, "invalid_request", "not enforced")
	})
}

func requireOAuthCode(t *testing.T, err error, code, msg string) {
	t.Helper()
	oauthErr, ok := errors.AsType[*oops.OAuthError](err)
	require.True(t, ok, "%s: expected an OAuth error, got %v", msg, err)
	require.Equal(t, code, oauthErr.Response.Code, msg)
}
