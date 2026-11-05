package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
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
	require.True(t, new(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RequiresUpstreamAuth = true })).staleNoAuth(), "a client was added")
	require.True(t, new(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.UpstreamAuthPending = true })).staleNoAuth(), "an association no auth client records yet")
	require.True(t, new(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.CredentialHeadersUncovered = true })).staleNoAuth(), "the message decides who can fix it")
	require.False(t, new(noAuth(func(*upstreamclientsrepo.GetAuthClientSelectionRow) {})).staleNoAuth(), "a server that still needs no auth is served")

	enforcedHeaders := withoutLegacyFallback(noAuth(func(r *upstreamclientsrepo.GetAuthClientSelectionRow) {
		r.RequiresUpstreamAuth, r.ServerHasHeaderClients = true, true
	}))
	require.True(t, enforcedHeaders.staleNoAuth(), "under enforcement a new header client also needs a new choice")

	require.False(t, new(decideAuthClient(upstreamclientsrepo.GetAuthClientSelectionRow{AuthClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}})).staleNoAuth(), "a named client that went away is a different gap")
	require.False(t, new(gapResolution(authClientReasonMissingChoice)).staleNoAuth())
}

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
		require.NotContains(t, message, "Reconnect this gateway", name)
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
		"consent can fix it": {bound: true, consent: "required", text: "tools.connect"},
		"consent cannot":     {bound: false, consent: "unknown", text: "needs your administrator"},
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
			member.authClient = new(gapWithChoice(authClientReasonNoAuthForbidden))
			gate := &metaGateContext{
				organizationID: f.orgID, projectID: f.projectID, userSessionIssuerID: issuerID,
				authentication: &issuerGateAuthentication{refreshableUserSession: true, subject: urn.NewUserSubject("user_1")},
				memberChoices:  &memberChoices{selector: &upstreamSelector{}, subject: urn.NewUserSubject("user_1"), byServer: map[uuid.UUID]*memberTokens{}},
			}
			state := (&metaCodeBackend{service: f.service}).verifyConnection(t.Context(), gate, []metaMember{member}, "db")
			require.Equal(t, tc.consent, state.Consent)
			require.Contains(t, state.Instructions, tc.text)
			require.NotContains(t, state.Instructions, "to reconnect, then verify again")
		})
	}
}

func TestStaleChoiceRemedyIsCachedPerPersonAndServer(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_stale_choice_cache")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.authEnforcement = authenforcement.NewGate(pool)
	f.service.features = new(feature.InMemory)
	f.service.staleChoiceConsent = expirable.NewLRU[string, bool](16, nil, time.Minute)
	f.oauthClient(authClientTypeRemoteSession)
	issuerID := uuid.New()
	require.NoError(t, f.q.SeedUserSessionIssuerFixture(t.Context(), authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: validUUID(f.projectID), Slug: "issuer-" + issuerID.String()[:8]}))
	member := metaMember{serverID: f.serverID, projectID: f.projectID}
	gate := func(user string) *metaGateContext {
		return &metaGateContext{organizationID: f.orgID, projectID: f.projectID, userSessionIssuerID: issuerID, memberChoices: &memberChoices{subject: urn.NewUserSubject(user)}}
	}
	first, err := f.service.consentCanReplaceChoice(t.Context(), gate("user_1"), member)
	require.NoError(t, err)
	require.False(t, first)
	require.NoError(t, f.q.BindClientIssuerFixture(t.Context(), authclientbackfill.BindClientIssuerFixtureParams{RemoteSessionClientID: f.registration, UserSessionIssuerID: issuerID}))
	cached, err := f.service.consentCanReplaceChoice(t.Context(), gate("user_1"), member)
	require.NoError(t, err)
	require.False(t, cached, "the same person and server reuse the answer")
	fresh, err := f.service.consentCanReplaceChoice(t.Context(), gate("user_2"), member)
	require.NoError(t, err)
	require.True(t, fresh, "another person gets their own")
	organizationGate := gate("user_3")
	organizationGate.projectID = uuid.Nil
	reached, err := f.service.consentCanReplaceChoice(t.Context(), organizationGate, member)
	require.NoError(t, err)
	require.True(t, reached, "an organization gateway has no project; the member's project scopes its clients")
}

// A failed lookup offers only a retry, on every surface, and is never remembered.
func TestStaleChoiceFailedLookupOffersOnlyARetry(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_stale_choice_lookup")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.staleChoiceConsent = expirable.NewLRU[string, bool](10, nil, time.Minute)
	pool.Close()
	const want = `This server's connection could not be checked. Try again in a few minutes.`
	member := metaMember{slug: "db", serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied, upstreamUnavailable: staleMemberError(authClientReasonNoAuthForbidden)}
	gate := &metaGateContext{
		organizationID: f.orgID, projectID: f.projectID, userSessionIssuerID: uuid.New(),
		authentication: &issuerGateAuthentication{refreshableUserSession: true, subject: urn.NewUserSubject("user_1")},
		memberChoices:  &memberChoices{selector: &upstreamSelector{}, subject: urn.NewUserSubject("user_1"), byServer: map[uuid.UUID]*memberTokens{}},
	}
	logger := testenv.NewLogger(t)
	require.Equal(t, want, f.service.memberFailureMessage(t.Context(), logger, gate, member, member.upstreamUnavailable))

	status, detail, outage := f.service.staleMemberStatus(t.Context(), logger, gate, member)
	require.Nil(t, outage)
	require.Equal(t, metamcp.SignInActionRequired, status)
	require.Equal(t, want, detail)
	text := metamcp.ConnectionStatusResult{ConnectURL: "https://example.com/connect", Servers: []metamcp.ConnectionStatusServer{{Slug: "db", Status: status, Detail: detail}}}.Text()
	require.NotContains(t, text, "To sign in")
	require.NotContains(t, text, "Reconnect this gateway")

	backend := &metaCodeBackend{service: f.service}
	require.Equal(t, want, backend.knownConnectionState(t.Context(), gate, member).Instructions)
	for name, refusal := range map[string]error{
		"tools.connect": backend.connectRefusal(t.Context(), gate, member),
		"helper":        backend.helperFailure(t.Context(), gate, member, member.upstreamUnavailable),
	} {
		refused, ok := errors.AsType[*codemode.MemberRefusalError](refusal)
		require.True(t, ok, name)
		require.Equal(t, want, refused.Message, name)
		require.NotErrorIs(t, refusal, codemode.ErrConsentRequired, name)
	}
	require.Zero(t, f.service.staleChoiceConsent.Len(), "a retry must look again")
}

// A choice that can't be read gets the same text.
func TestUnreadableChoiceSaysItCouldNotBeChecked(t *testing.T) {
	t.Parallel()
	member := metaMember{slug: "db"}
	(&Service{}).applyMemberAuthClient(t.Context(), &member, gapResolution(authClientReasonMissingChoice), errors.New("read failed"))
	require.Equal(t, `This server's connection could not be checked. Try again in a few minutes.`, memberErrorMessage(member.upstreamUnavailable))
}

func TestStaleChoiceCountsEveryAttemptPastLogDedupe(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	logger := testenv.NewLogger(t)
	s := &Service{staleNoAuthLog: newOnceLog(), metrics: mcpmetrics.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), logger)}
	gate := &metaGateContext{authentication: &issuerGateAuthentication{subject: urn.NewUserSubject("user_1")}}
	member := metaMember{serverID: uuid.New()}
	for range 3 {
		s.noteStaleChoiceCall(t.Context(), logger, gate, member, staleMemberError(authClientReasonNoAuthForbidden))
	}
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	total := int64(0)
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "mcp.meta.stale_choice_refusal" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				total += point.Value
			}
		}
	}
	require.Equal(t, int64(3), total)
}

func TestConnectOffersNoLinkWhenConsentCannotReplaceTheChoice(t *testing.T) {
	t.Parallel()
	backend := &metaCodeBackend{service: &Service{logger: testenv.NewLogger(t)}}
	gate := &metaGateContext{authentication: &issuerGateAuthentication{subject: urn.NewAgentSubject(uuid.New())}}
	require.NoError(t, backend.connectRefusal(t.Context(), gate, metaMember{slug: "db"}), "only a stale choice is refused")
	err := backend.connectRefusal(t.Context(), gate, metaMember{slug: "db", upstreamUnavailable: staleMemberError(authClientReasonNoAuthForbidden)})
	refused, ok := errors.AsType[*codemode.MemberRefusalError](err)
	require.True(t, ok)
	require.Contains(t, refused.Message, "Ask whoever set this up")
}

func TestCodeModeHelperFailureNonPersonGetsOwnerCopy(t *testing.T) {
	t.Parallel()
	backend := &metaCodeBackend{service: &Service{logger: testenv.NewLogger(t)}}
	gate := &metaGateContext{authentication: &issuerGateAuthentication{refreshableUserSession: true, subject: urn.NewAgentSubject(uuid.New())}, memberChoices: &memberChoices{}}
	err := backend.helperFailure(t.Context(), gate, metaMember{slug: "db"}, staleMemberError(authClientReasonNoAuthForbidden))
	refused, ok := errors.AsType[*codemode.MemberRefusalError](err)
	require.True(t, ok)
	require.Contains(t, refused.Message, "Ask whoever set this up")
	require.NotErrorIs(t, err, codemode.ErrConsentRequired)
}

func TestStaleChoiceFailureInCodeModeKeepsItsMessage(t *testing.T) {
	t.Parallel()
	invocation := &metaCodeInvocation{}
	ctx := context.WithValue(t.Context(), metaCodeInvocationKey{}, invocation)
	_, err := marshalMetaMemberFailure(ctx, testenv.NewLogger(t), mcpjsonrpc.StringID("1"), staleMemberError(authClientReasonNoAuthForbidden), "ask your administrator")
	refusal, ok := errors.AsType[*codemode.DispatchError](err)
	require.True(t, ok)
	require.Equal(t, "ask your administrator", refusal.Rejection)
	require.False(t, invocation.credential)
}
