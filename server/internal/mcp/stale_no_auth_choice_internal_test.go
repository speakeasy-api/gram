package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestStaleNoAuthClassification(t *testing.T) {
	t.Parallel()
	noAuth := func(edit func(*upstreamclientsrepo.GetAuthClientSelectionRow)) authClientResolution {
		row := upstreamclientsrepo.GetAuthClientSelectionRow{NoAuth: true}
		edit(&row)
		return decideAuthClient(row)
	}
	require.True(t, ptr(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RequiresUpstreamAuth = true })).staleNoAuth(), "a client was added")
	require.True(t, ptr(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.UpstreamAuthPending = true })).staleNoAuth(), "an association no auth client records yet")
	require.True(t, ptr(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.CredentialHeadersUncovered = true })).staleNoAuth(), "the message decides who can fix it")
	require.False(t, ptr(noAuth(func(*upstreamclientsrepo.GetAuthClientSelectionRow) {})).staleNoAuth(), "a server that still needs no auth is served")

	enforcedHeaders := withoutLegacyFallback(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) {
		r.RequiresUpstreamAuth, r.ServerHasHeaderClients = true, true
	}))
	require.True(t, enforcedHeaders.staleNoAuth(), "under enforcement a new header client also needs a new choice")

	require.False(t, ptr(decideAuthClient(upstreamclientsrepo.GetAuthClientSelectionRow{AuthClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}})).staleNoAuth(), "a named client that went away is a different gap")
	require.False(t, ptr(gapResolution(authClientReasonMissingChoice)).staleNoAuth())
}

func ptr[T any](v T) *T { return &v }

func TestStaleChoiceErrorChain(t *testing.T) {
	t.Parallel()
	s := &Service{}
	member := metaMember{slug: "db"}
	s.applyMemberAuthClient(t.Context(), &member, gapWithChoice(authClientReasonNoAuthForbidden), nil)
	stale, ok := errors.AsType[memberStaleChoiceError](member.upstreamUnavailable)
	require.True(t, ok)
	require.Equal(t, authClientReasonNoAuthForbidden, stale.reason)
	_, credential := errors.AsType[memberCredentialError](member.upstreamUnavailable)
	require.True(t, credential, "consent status still reads it as a credential failure")
	_, ok = errors.AsType[*metaMemberError](member.upstreamUnavailable)
	require.True(t, ok)
	require.True(t, needsNewConsent(member.upstreamUnavailable))

	other := metaMember{slug: "db"}
	s.applyMemberAuthClient(t.Context(), &other, gapResolution(authClientReasonClientDisabled), nil)
	_, ok = errors.AsType[memberStaleChoiceError](other.upstreamUnavailable)
	require.False(t, ok)
	require.False(t, needsNewConsent(other.upstreamUnavailable))

	legacy := metaMember{slug: "db"}
	s.applyMemberAuthClient(t.Context(), &legacy, gapResolution(authClientReasonMissingChoice), errUpstreamSelectionStaleNoAuth)
	_, ok = errors.AsType[memberStaleChoiceError](legacy.upstreamUnavailable)
	require.True(t, ok, "the upstream client choice path reaches the same error")
	require.ErrorIs(t, errUpstreamSelectionStaleNoAuth, errUpstreamSelectionUnusable, "direct servers keep their reconnect challenge")
}

func gapWithChoice(reason string) authClientResolution {
	res := gapResolution(reason)
	res.noAuthChoice = true
	return res
}

func staleMemberError(reason string) memberStaleChoiceError {
	return memberStaleChoiceError{memberCredentialError{&metaMemberError{message: "stale"}}, reason}
}

func TestStaleChoiceWarnsAtTheCallWithTheSubject(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := &Service{staleNoAuthLog: newOnceLog()}
	member := metaMember{serverID: uuid.New(), slug: "db"}
	gate := func(subject urn.SessionSubject) *metaGateContext {
		return &metaGateContext{organizationID: "org", authentication: &issuerGateAuthentication{subject: subject}}
	}
	stale := staleMemberError(authClientReasonNoAuthForbidden)
	agent := urn.NewAgentSubject(uuid.New())
	s.noteStaleChoiceCall(t.Context(), logger, gate(agent), member, stale)
	s.noteStaleChoiceCall(t.Context(), logger, gate(agent), member, stale)
	s.noteStaleChoiceCall(t.Context(), logger, gate(urn.NewUserSubject("user_2")), member, stale)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	require.Contains(t, lines[0], `"level":"WARN"`)
	require.Contains(t, lines[0], authClientReasonNoAuthForbidden)
	require.Contains(t, lines[0], member.serverID.String())
	require.Contains(t, lines[0], agent.String())
}

func TestStaleChoiceEveryNonPersonGetsOwnerCopy(t *testing.T) {
	t.Parallel()
	s := &Service{logger: testenv.NewLogger(t)}
	member := metaMember{slug: "db", upstreamUnavailable: staleMemberError(authClientReasonNoAuthForbidden)}
	gates := map[string]*metaGateContext{"no sign-in": {memberChoices: &memberChoices{}}}
	for name, tc := range map[string]struct {
		refreshable bool
		subject     urn.SessionSubject
	}{
		"agent":          {true, urn.SessionSubject{Kind: urn.SessionSubjectKindAgent, ID: "agent"}},
		"workload":       {true, urn.SessionSubject{Kind: urn.SessionSubjectKindWorkload, ID: "workload"}},
		"anonymous":      {true, urn.SessionSubject{Kind: urn.SessionSubjectKindAnonymous, ID: "anon"}},
		"api key":        {true, urn.SessionSubject{Kind: urn.SessionSubjectKindAPIKey, ID: "key"}},
		"assistant turn": {false, urn.NewUserSubject("user_1")},
	} {
		gates[name] = &metaGateContext{authentication: &issuerGateAuthentication{refreshableUserSession: tc.refreshable, subject: tc.subject}, memberChoices: &memberChoices{}}
	}
	for name, gate := range gates {
		message, reconnect := s.staleChoiceMessage(t.Context(), s.logger, gate, member)
		require.False(t, reconnect, name)
		require.Contains(t, message, "Ask whoever set this up", name)
		require.NotContains(t, message, "needs a sign-in", name)
	}
}

func TestAgentKeyStaleChoiceGetsOwnerCopy(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_agent_key_stale_choice")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.enforce()
	keyID := uuid.New()
	_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(t.Context(), upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance, OrganizationID: f.orgID, AuthClientID: uuid.NullUUID{}, McpServerID: f.serverID,
	})
	require.NoError(t, err)
	f.oauthClient(authClientTypeServiceAccount)
	var buf bytes.Buffer
	f.service.logger = slog.New(slog.NewJSONHandler(&buf, nil))
	f.service.staleNoAuthLog = newOnceLog()
	agent := urn.NewAgentSubject(uuid.New())
	choices := &memberChoices{selector: &upstreamSelector{service: f.service, organizationID: f.orgID, credentialKind: upstreamclients.CredentialKindAPIKey, credentialID: keyID}, enforced: true, profile: true, subject: agent, byServer: map[uuid.UUID]*memberTokens{}}
	members := []metaMember{{slug: "db", serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied}}
	choices.apply(t.Context(), f.service.logger, members, true)
	require.Empty(t, buf.String(), "nothing is logged until the member is called")
	gate := &metaGateContext{organizationID: f.orgID, memberChoices: choices, authentication: &issuerGateAuthentication{subject: agent}}
	message := f.service.memberFailureMessage(t.Context(), f.service.logger, gate, members[0], members[0].upstreamUnavailable)
	require.Contains(t, message, "Ask whoever set this up")
	require.Contains(t, buf.String(), agent.String())
}

func TestCodeModeStaleChoiceKeepsConsentInstruction(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_code_stale_choice")
	require.NoError(t, err)
	for name, tc := range map[string]struct {
		bound   bool
		consent string
		text    string
	}{
		"consent can fix it": {bound: true, consent: "required", text: "reconnect this gateway"},
		"consent cannot":     {bound: false, consent: "unknown", text: "Ask your administrator"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			f.service.authEnforcement = authenforcement.NewGate(pool)
			f.service.features = new(feature.InMemory)
			f.oauthClient(authClientTypeRemoteSession)
			issuerID := uuid.New()
			require.NoError(t, f.q.SeedUserSessionIssuerFixture(t.Context(), authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: validUUID(f.projectID), Slug: "issuer-" + issuerID.String()[:8]}))
			if tc.bound {
				require.NoError(t, f.q.BindClientIssuerFixture(t.Context(), authclientbackfill.BindClientIssuerFixtureParams{RemoteSessionClientID: f.registration, UserSessionIssuerID: issuerID}))
			}
			member := metaMember{slug: "db", serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied, upstreamUnavailable: staleMemberError(authClientReasonNoAuthForbidden)}
			member.authClient = ptr(gapWithChoice(authClientReasonNoAuthForbidden))
			gate := &metaGateContext{
				organizationID: f.orgID, userSessionIssuerID: issuerID,
				authentication: &issuerGateAuthentication{refreshableUserSession: true, subject: urn.NewUserSubject("user_1")},
				memberChoices:  &memberChoices{subject: urn.NewUserSubject("user_1"), byServer: map[uuid.UUID]*memberTokens{}},
			}
			state := (&metaCodeBackend{service: f.service}).verifyConnection(t.Context(), gate, []metaMember{member}, "db")
			require.Equal(t, tc.consent, state.Consent)
			require.Contains(t, state.Instructions, tc.text)
			require.NotContains(t, state.Instructions, "to reconnect, then verify again")
		})
	}
}

// A direct server keeps its 401 reconnect challenge.
func TestDirectStaleNoAuthKeepsReconnectChallenge(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_direct_stale_no_auth")
	require.NoError(t, err)
	for _, enforced := range []bool{false, true} {
		f := newAuthClientFixture(t, pool)
		if enforced {
			f.enforce()
		}
		selector := f.choose(uuid.NullUUID{})
		f.oauthClient(authClientTypeRemoteSession)
		issuerID := uuid.New()
		subject := urn.NewUserSubject("user_1")
		proof := validatedSessionProof(t, subject)
		ctx := mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), proof)
		ctx = context.WithValue(ctx, liveSessionPolicyKey{}, true)
		ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user_1", ActiveOrganizationID: f.orgID})
		require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: validUUID(f.projectID), Slug: "issuer"}))
		require.NoError(t, f.q.SeedUserSessionFixture(ctx, authclientbackfill.SeedUserSessionFixtureParams{
			ProjectID: validUUID(f.projectID), OrganizationID: text(f.orgID), UserSessionIssuerID: issuerID,
			SubjectUrn: subject, Jti: proof.JTI(), CredentialID: validUUID(selector.credentialID),
		}))
		w := httptest.NewRecorder()
		_, handled, err := f.service.directUpstreamChoice(ctx, w, &issuerGateAuthentication{
			endpoint:               &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: issuerID, McpServerID: validUUID(f.serverID)},
			subject:                subject,
			refreshableUserSession: true,
			protectedResourceURL:   "https://example.com/.well-known/oauth-protected-resource/mcp/x",
		})
		require.True(t, handled)
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeUnauthorized, shareable.Code)
		require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
		_, choiceErr := selector.forServer(ctx, f.serverID)
		require.ErrorIs(t, choiceErr, errUpstreamSelectionUnusable, "refresh refuses this choice too")
	}
}
