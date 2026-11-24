package mcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
		_, picks, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil, true)
		require.NoError(t, err)
		require.Equal(t, map[uuid.UUID]uuid.UUID{f.serverID: only}, picks)
	})

	t.Run("several ready credentials need a choice", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		shared(f)
		second := shared(f)
		_, _, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil, true)
		requireOAuthCode(t, err, tokenErrorSelectionRequired, "no choice named")

		_, picks, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {clientID: validUUID(second)}}, true)
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
		_, picks, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil, true)
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
		_, picks, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil, true)
		require.NoError(t, err)
		require.Equal(t, only, picks[f.serverID])
	})

	t.Run("a choice the subject may not use is refused, not replaced", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		named := shared(f)
		f.service.authClientPermitter = denyPermitter{}
		_, _, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {clientID: validUUID(named)}}, true)
		requireOAuthCode(t, err, "access_denied", "denied choice")
		_, _, _, err = f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {clientID: validUUID(uuid.New())}}, true)
		requireOAuthCode(t, err, "access_denied", "foreign choice")
	})

	t.Run("a server the token does not reach cannot be named", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		shared(f)
		_, _, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{uuid.New(): {}}, true)
		requireOAuthCode(t, err, "invalid_request", "out of scope")
	})

	t.Run("incomplete setup is told apart from a missing choice", func(t *testing.T) {
		t.Parallel()
		f, endpoint, caller := setup(t)
		// An upstream OAuth client no auth client records yet.
		f.attach()
		_, _, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil, true)
		requireOAuthCode(t, err, tokenErrorSetupRequired, "upstream credentials with nothing offered")
	})

	t.Run("a workload subject has no personal credential to connect", func(t *testing.T) {
		t.Parallel()
		f, endpoint, _ := setup(t)
		f.oauthClient(authClientTypeRemoteSession)
		workload := headlessCaller{workloadIssuerID: validUUID(uuid.New()), workloadSubject: "repo:example/app:ref:refs/heads/main"}
		_, _, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, urn.NewWorkloadSubject(workload.workloadIssuerID.UUID, workload.workloadSubject), workload, nil, true)
		requireOAuthCode(t, err, tokenErrorSetupRequired, "personal credentials are not offered to a workload")
		code, _ := workloadGrantHappened(err)
		require.Equal(t, mcpconnectionattempts.CodeUpstreamSignInNotSetUp, code)

		w := httptest.NewRecorder()
		oops.OAuthErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/token", nil))
		require.JSONEq(t, `{"error":"setup_required","error_description":"a server's upstream sign-in is not set up"}`, w.Body.String())
	})

	t.Run("organizations that do not enforce refuse named choices and decide nothing", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID), UserSessionIssuerID: uuid.New()}
		caller := headlessCaller{clientID: validUUID(uuid.New())}
		shared(f)
		up, picks, _, err := f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, nil, false)
		require.NoError(t, err)
		require.Nil(t, up)
		require.Nil(t, picks)
		_, _, _, err = f.service.headlessAuthClientPicks(t.Context(), endpoint, user, caller, map[uuid.UUID]headlessChoice{f.serverID: {}}, false)
		requireOAuthCode(t, err, "invalid_request", "not enforced")
	})
}

func requireOAuthCode(t *testing.T, err error, code, msg string) {
	t.Helper()
	oauthErr, ok := errors.AsType[*oops.OAuthError](err)
	require.True(t, ok, "%s: expected an OAuth error, got %v", msg, err)
	require.Equal(t, code, oauthErr.Response.Code, msg)
}

// Only a credential the person can connect is theirs to fix.
func TestDecideHeadlessChoiceNamesWhoClearsAnUnreadyServer(t *testing.T) {
	t.Parallel()

	connect := consentAuthClientOption{ID: uuid.New(), Name: "Personal", Type: authz.AuthClientTypeRemoteSession, State: consentClientConnect, ConnectClientID: "", Selected: false, registrationID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	setup := consentAuthClientOption{ID: uuid.New(), Name: "Shared", Type: authz.AuthClientTypeServiceAccount, State: consentClientSetup, ConnectClientID: "", Selected: false, registrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	reconnect := connect
	reconnect.ID, reconnect.State = uuid.New(), consentClientReconnect
	none := headlessChoice{clientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	for name, tc := range map[string]struct {
		options []consentAuthClientOption
		choice  headlessChoice
		named   bool
		want    headlessChoiceError
	}{
		"one to connect":           {options: []consentAuthClientOption{setup, connect}, choice: none, named: false, want: errChoiceNoneConnected},
		"one to reconnect":         {options: []consentAuthClientOption{setup, reconnect}, choice: none, named: false, want: errChoiceNoneConnected},
		"none to connect":          {options: []consentAuthClientOption{setup}, choice: none, named: false, want: errChoiceNotSetUp},
		"named one to connect":     {options: []consentAuthClientOption{setup, connect}, choice: headlessChoice{clientID: uuid.NullUUID{UUID: connect.ID, Valid: true}}, named: true, want: errChoiceNotConnected},
		"named one not set up yet": {options: []consentAuthClientOption{setup, connect}, choice: headlessChoice{clientID: uuid.NullUUID{UUID: setup.ID, Valid: true}}, named: true, want: errChoiceChosenNotSetUp},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			auth := &consentServerAuth{ServerID: uuid.New(), Status: consentAuthChoose, Options: tc.options}
			_, err := decideHeadlessChoice(consentAuthServer{id: auth.ServerID, projectID: uuid.New()}, auth, tc.choice, tc.named)
			require.ErrorIs(t, err, tc.want)
			require.True(t, awaitsConnection(err, tc.named), "a gateway still defers the server")
		})
	}
}

// A not-connected refusal from the shared decision serializes without a connect link and records the person's missing connection.
func TestHeadlessNotConnectedRefusalSerializesWithoutLink(t *testing.T) {
	t.Parallel()

	connect := consentAuthClientOption{ID: uuid.New(), Name: "Personal", Type: authz.AuthClientTypeRemoteSession, State: consentClientConnect, ConnectClientID: "", Selected: false, registrationID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	auth := &consentServerAuth{ServerID: uuid.New(), Status: consentAuthChoose, Options: []consentAuthClientOption{connect}}
	_, refusal := decideHeadlessChoice(consentAuthServer{id: auth.ServerID, projectID: uuid.New()}, auth, headlessChoice{clientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, false)
	require.Error(t, refusal)

	rec := mcpconnectionattemptstest.New(t)
	refuseWorkloadGrantRow(t.Context(), rec.Recorder.Track(directScope(), mcpconnectionattempts.StepInboundToken), refusal)
	require.Equal(t, "no_credential_ready", rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeInvalidRemoteSession).SubReason)

	w := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return refusal }).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/token", nil))
	require.JSONEq(t, `{"error":"setup_required","error_description":"Your account for this server isn't connected, or its connection has expired. Connect it, then try again."}`, w.Body.String())
}

// Server and credential names are quoted only when they read as plain labels; anything else falls back.
func TestWithConnectPageQuotesOnlyPlainLabels(t *testing.T) {
	t.Parallel()

	const page = "https://mcp.example.test/mcp/docs/connect/first-party"
	longest := strings.Repeat("a", maxDescribedNameLength)
	for name, tc := range map[string]struct {
		label string
		plain bool
	}{
		"plain":         {label: "Acme Inc. Docs, R&D", plain: true},
		"80 characters": {label: longest, plain: true},
		"81 characters": {label: longest + "a", plain: false},
		"markup":        {label: "<b>x</b>", plain: false},
		"autolink":      {label: "<help@example.test>", plain: false},
		"bare link":     {label: "example.test/path", plain: false},
		"host-like":     {label: "docs.example", plain: false},
		"colon":         {label: "Docs: internal", plain: false},
		"non-ASCII":     {label: "Caf\u00e9", plain: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			endpoint := &ResolvedMcpEndpoint{McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, serverDisplayName: tc.label, serverSlug: "docs"}
			unnamed := withConnectPage(endpoint, "https://mcp.example.test/mcp/docs", setupRequired("no", errChoiceNoneConnected))
			server := "docs"
			if tc.plain {
				server = tc.label
			}
			requireConnectDescription(t, unnamed, "Your account for "+server+" isn't connected, or its connection has expired. Open "+page+" to connect it, then try again.")

			endpoint.serverDisplayName = "Docs"
			named := withConnectPage(endpoint, "https://mcp.example.test/mcp/docs", oops.NewOAuthError(http.StatusBadRequest, tokenErrorSetupRequired, "no", namedCredentialError{cause: errChoiceNotConnected, name: tc.label}))
			credential := "The credential chosen for Docs"
			if tc.plain {
				credential = tc.label + " for Docs"
			}
			requireConnectDescription(t, named, credential+" isn't connected, or its connection has expired. Open "+page+" and connect it, then try again.")
		})
	}
}

func requireConnectDescription(t *testing.T, err error, first string) {
	t.Helper()
	refusal, ok := errors.AsType[*oops.OAuthError](err)
	require.True(t, ok)
	require.Equal(t, "https://mcp.example.test/mcp/docs/connect/first-party", refusal.Response.URI)
	require.Equal(t, first+" If this server is in your Speakeasy Device Agent, you can connect it there too.", refusal.Response.Description)
}
