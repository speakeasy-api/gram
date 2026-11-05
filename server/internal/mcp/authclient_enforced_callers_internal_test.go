package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// enforcedCallerFixture is an enforcing organization whose server is reached
// through a sign-in that links no upstream client: only typed auth clients
// say how the server authenticates.
type enforcedCallerFixture struct {
	*authClientFixture
	// signIn is the server's user session issuer.
	signIn uuid.UUID
	// consentedUnder is another sign-in of the project, which stored
	// connections were consented through.
	consentedUnder uuid.UUID
}

func newEnforcedCallerFixture(t *testing.T, pool *pgxpool.Pool) *enforcedCallerFixture {
	t.Helper()
	f := newAuthClientFixture(t, pool)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})
	f.service.metrics = mcpmetrics.NewMetrics(testenv.NewMeterProvider(t).Meter("test"), testenv.NewLogger(t))
	f.enforce()
	fixture := &enforcedCallerFixture{authClientFixture: f, signIn: uuid.New(), consentedUnder: uuid.New()}
	for _, id := range []uuid.UUID{fixture.signIn, fixture.consentedUnder} {
		require.NoError(t, f.q.SeedUserSessionIssuerFixture(t.Context(), authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: id, ProjectID: validUUID(f.projectID), Slug: "usi-" + id.String()[:8]}))
	}
	return fixture
}

// connect stores subject's live connection for the fixture's registration.
func (f *enforcedCallerFixture) connect(subject urn.SessionSubject, token string) {
	f.t.Helper()
	accessEnc, err := f.service.enc.Encrypt([]byte(token))
	require.NoError(f.t, err)
	_, err = remotesessionsrepo.New(f.service.db).UpsertRemoteSession(f.t.Context(), remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   f.consentedUnder,
		RemoteSessionClientID: f.registration,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(f.t, err)
}

func (f *enforcedCallerFixture) authentication(subject urn.SessionSubject) *issuerGateAuthentication {
	return &issuerGateAuthentication{
		endpoint: &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn, McpServerID: validUUID(f.serverID)},
		subject:  subject,
	}
}

// chainable offers the fixture's registration as a personal client and as an
// identity-provider client, so the personal client may chain.
func (f *enforcedCallerFixture) chainable() uuid.UUID {
	f.t.Helper()
	personal := f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	ema := f.adminClient(authClientTypeEMA, validUUID(f.registration))
	require.NoError(f.t, f.q.SeedAuthClientEMAFixture(f.t.Context(), authclientbackfill.SeedAuthClientEMAFixtureParams{Resource: "https://api.example.com", AuthClientID: ema}))
	return personal
}

// staleStatic offers a static credential copied from the server's headers
// whose copy no longer matches them.
func (f *enforcedCallerFixture) staleStatic() uuid.UUID {
	f.t.Helper()
	headerID := f.sourceHeader("X-Upstream", false, pgtype.Text{}, text("X-Caller"))
	client := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
	f.copyHeader(client, "X-Upstream", false, false, pgtype.Text{}, text("X-Caller"))
	f.requireSourceHeader(headerID)
	return client
}

// A session's choice of a client on a compatibility path is served from what
// the resolver read: its registration, without a sign-in link or an
// association, or the server's own headers. The upstream client choice, which
// needs both, would refuse each of them.
func TestEnforcedSessionCompatibilityPathNeedsNoSignInLink(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_enforced_compatibility_direct")
	require.NoError(t, err)
	subject := urn.NewUserSubject("user_1")

	t.Run("a personal connection that may chain", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		selector := f.choose(validUUID(f.chainable()))
		selector.userSessionIssuerID = f.signIn
		f.connect(subject, "upstream-token")
		_, err := selector.forServer(t.Context(), f.serverID)
		require.ErrorIs(t, err, errUpstreamSelectionUnusable, "the sign-in links no client")

		authentication := f.authentication(subject)
		choice, err := f.service.enforceAuthClient(t.Context(), httptest.NewRecorder(), authentication, selector)
		require.NoError(t, err)
		require.Equal(t, upstreamChoice{clientID: validUUID(f.registration), remoteSessionIssuerID: validUUID(f.issuerID)}, choice)
		require.Nil(t, authentication.authClient, "identity chaining may still serve the registration")
		require.NotNil(t, authentication.chainingAuthClient)
		require.Equal(t, authClientReasonLegacyMayChain, authentication.chainingAuthClient.reason)

		tokens, err := f.service.resolveAccessTokens(t.Context(), authentication, false)
		require.NoError(t, err)
		require.Equal(t, "upstream-token", tokens[f.issuerID].Token)
	})

	t.Run("an identity-provider client", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		ema := f.adminClient(authClientTypeEMA, validUUID(f.registration))
		require.NoError(t, f.q.SeedAuthClientEMAFixture(t.Context(), authclientbackfill.SeedAuthClientEMAFixtureParams{Resource: "https://api.example.com", AuthClientID: ema}))
		selector := f.choose(validUUID(ema))
		selector.userSessionIssuerID = f.signIn

		authentication := f.authentication(subject)
		choice, err := f.service.enforceAuthClient(t.Context(), httptest.NewRecorder(), authentication, selector)
		require.NoError(t, err)
		require.Equal(t, validUUID(f.registration), choice.clientID, "chaining is asked for the client's own registration")
		require.Equal(t, choice.clientID, authentication.chosenClient)
		require.Nil(t, authentication.authClient)
	})

	t.Run("an identity-provider client whose registration is gone", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		ema := f.adminClient(authClientTypeEMA, validUUID(f.registration))
		require.NoError(t, f.q.SeedAuthClientEMAFixture(t.Context(), authclientbackfill.SeedAuthClientEMAFixtureParams{Resource: "https://api.example.com", AuthClientID: ema}))
		selector := f.choose(validUUID(ema))
		selector.userSessionIssuerID = f.signIn
		deleted, err := remotesessionsrepo.New(pool).SoftDeleteRemoteSessionClientFixture(t.Context(), f.registration)
		require.NoError(t, err)
		require.EqualValues(t, 1, deleted)

		authentication := f.authentication(subject)
		w := httptest.NewRecorder()
		_, err = f.service.enforceAuthClient(t.Context(), w, authentication, selector)
		require.Error(t, err)
		require.NotEmpty(t, w.Header().Get("WWW-Authenticate"), "nothing routes it, so the caller reconnects")
		require.False(t, authentication.chosenNoAuth, "it is never dialed anonymously")
	})

	t.Run("stale copies of a static credential beside an association", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		selector := f.choose(validUUID(f.staleStatic()))
		selector.userSessionIssuerID = f.signIn
		// An association the server never configured as an auth client.
		f.attach()
		_, err := selector.forServer(t.Context(), f.serverID)
		require.ErrorIs(t, err, errUpstreamSelectionUnusable, "the association makes the choice read as stale")

		authentication := f.authentication(subject)
		choice, err := f.service.enforceAuthClient(t.Context(), httptest.NewRecorder(), authentication, selector)
		require.NoError(t, err)
		require.True(t, choice.noAuth())
		require.True(t, authentication.chosenNoAuth, "the proxy sends the server's current headers")
		require.Nil(t, authentication.authClient, "the client's stale copies are not sent")
		tokens, err := f.service.resolveAccessTokens(t.Context(), authentication, false)
		require.NoError(t, err)
		require.Empty(t, tokens)
	})
}

// A gateway member's choice of a client on a compatibility path is served
// from what the resolver read, without a sign-in link or an association.
func TestEnforcedMemberCompatibilityPathNeedsNoSignInLink(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_enforced_compatibility_member")
	require.NoError(t, err)
	subject := urn.NewUserSubject("user_1")

	apply := func(t *testing.T, f *enforcedCallerFixture, selector *upstreamSelector) (*memberChoices, metaMember) {
		t.Helper()
		endpoint := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn}
		choices := &memberChoices{selector: selector, enforced: true, subject: subject, byServer: map[uuid.UUID]*memberTokens{}}
		choices.resolve = func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			return f.service.resolveChosenMemberTokens(ctx, endpoint, subject, true, member)
		}
		members := []metaMember{{serverID: f.serverID, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied}}
		choices.apply(t.Context(), testenv.NewLogger(t), members, true)
		return choices, members[0]
	}

	t.Run("a personal connection that may chain", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		selector := f.choose(validUUID(f.chainable()))
		selector.userSessionIssuerID = f.signIn
		f.connect(subject, "upstream-token")

		choices, member := apply(t, f, selector)
		require.NoError(t, member.upstreamUnavailable)
		require.Equal(t, authClientLegacy, member.authClient.outcome)
		require.Equal(t, validUUID(f.registration), member.chosenClientID)
		require.Equal(t, validUUID(f.issuerID), member.remoteSessionIssuerID)
		tokens, err := choices.tokens(t.Context(), member)
		require.NoError(t, err)
		require.Equal(t, "upstream-token", tokens[f.issuerID].Token)
	})

	t.Run("stale copies of a static credential beside an association", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		selector := f.choose(validUUID(f.staleStatic()))
		selector.userSessionIssuerID = f.signIn
		f.attach()

		_, member := apply(t, f, selector)
		require.NoError(t, member.upstreamUnavailable)
		require.True(t, member.upstreamNoAuth, "the proxy sends the server's current headers")
		require.False(t, member.chosenClientID.Valid)
	})

	t.Run("an identity-provider client whose registration is gone", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		ema := f.adminClient(authClientTypeEMA, validUUID(f.registration))
		require.NoError(t, f.q.SeedAuthClientEMAFixture(t.Context(), authclientbackfill.SeedAuthClientEMAFixtureParams{Resource: "https://api.example.com", AuthClientID: ema}))
		selector := f.choose(validUUID(ema))
		selector.userSessionIssuerID = f.signIn
		_, err := remotesessionsrepo.New(pool).SoftDeleteRemoteSessionClientFixture(t.Context(), f.registration)
		require.NoError(t, err)

		_, member := apply(t, f, selector)
		require.Error(t, member.upstreamUnavailable)
		require.False(t, member.upstreamNoAuth, "it is never dialed anonymously")
	})
}

// A refresh of an enforcing organization's session is decided by the
// resolver for every choice, including one on a compatibility path whose
// registration the sign-in does not link.
func TestEnforcedRefreshIsDecidedByTheResolver(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_enforced_refresh")
	require.NoError(t, err)
	redisClient, err := TestInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	subject := urn.NewUserSubject("user_refresh")

	setup := func(t *testing.T, choice func(f *enforcedCallerFixture) uuid.NullUUID) (*enforcedCallerFixture, *ResolvedMcpEndpoint) {
		t.Helper()
		f := newEnforcedCallerFixture(t, pool)
		f.service.sessionPolicyCache = cache.NewTypedObjectCache[sessionPolicyEntry](testenv.NewLogger(t), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone)
		selector := f.choose(choice(f))
		jti := "jti-" + uuid.NewString()
		require.NoError(t, f.service.sessionPolicyCache.Store(t.Context(), sessionPolicyEntry{IssuerID: f.signIn.String(), JTI: jti, Selection: nil, Refreshable: true, CredentialID: selector.credentialID}))
		return f, &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn, McpServerID: validUUID(f.serverID), Slug: jti, UpstreamResource: "https://mcp.example.com"}
	}

	t.Run("a registration that may chain refreshes while its grant is live", func(t *testing.T) {
		t.Parallel()
		f, endpoint := setup(t, func(f *enforcedCallerFixture) uuid.NullUUID { return validUUID(f.chainable()) })

		reconnect, chain, decided, err := f.service.refreshNeedsAuthClientReconnect(t.Context(), endpoint, subject, endpoint.Slug)
		require.NoError(t, err)
		require.True(t, decided)
		require.True(t, reconnect, "no stored grant")
		require.Equal(t, validUUID(f.registration), chain.clientID, "identity chaining is asked before the refresh is refused")
		require.True(t, f.service.refreshNeedsUpstreamReconnect(t.Context().reconnect(), testenv.NewLogger(t), endpoint, subject, endpoint.Slug), "without identity chaining the reconnect stands")

		f.connect(subject, "upstream-token")
		reconnect, chain, decided, err = f.service.refreshNeedsAuthClientReconnect(t.Context(), endpoint, subject, endpoint.Slug)
		require.NoError(t, err)
		require.True(t, decided)
		require.False(t, reconnect)
		require.False(t, chain.clientID.Valid)
		require.False(t, f.service.refreshNeedsUpstreamReconnect(t.Context().reconnect(), testenv.NewLogger(t), endpoint, subject, endpoint.Slug), "a live grant refreshes, though the sign-in links no client")
	})

	t.Run("stale copies of a static credential refresh", func(t *testing.T) {
		t.Parallel()
		f, endpoint := setup(t, func(f *enforcedCallerFixture) uuid.NullUUID {
			client := f.staleStatic()
			f.attach()
			return validUUID(client)
		})
		require.False(t, f.service.refreshNeedsUpstreamReconnect(t.Context().reconnect(), testenv.NewLogger(t), endpoint, subject, endpoint.Slug), "it holds no upstream session")
	})

	t.Run("a static credential refreshes", func(t *testing.T) {
		t.Parallel()
		f, endpoint := setup(t, func(f *enforcedCallerFixture) uuid.NullUUID {
			return validUUID(f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{}))
		})
		reconnect, _, decided, err := f.service.refreshNeedsAuthClientReconnect(t.Context(), endpoint, subject, endpoint.Slug)
		require.NoError(t, err)
		require.True(t, decided)
		require.False(t, reconnect)
	})

	t.Run("a disabled client reconnects", func(t *testing.T) {
		t.Parallel()
		f, endpoint := setup(t, func(f *enforcedCallerFixture) uuid.NullUUID {
			client := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
			require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: client}))
			return validUUID(client)
		})
		require.True(t, f.service.refreshNeedsUpstreamReconnect(t.Context().reconnect(), testenv.NewLogger(t), endpoint, subject, endpoint.Slug))
	})

	t.Run("a no-auth choice refreshes", func(t *testing.T) {
		t.Parallel()
		f, endpoint := setup(t, func(*enforcedCallerFixture) uuid.NullUUID { return uuid.NullUUID{} })
		require.False(t, f.service.refreshNeedsUpstreamReconnect(t.Context().reconnect(), testenv.NewLogger(t), endpoint, subject, endpoint.Slug))
	})

	t.Run("a hosted server records no choice to decide by", func(t *testing.T) {
		t.Parallel()
		f, endpoint := setup(t, func(*enforcedCallerFixture) uuid.NullUUID { return uuid.NullUUID{} })
		endpoint.McpServerID = validUUID(uuid.New())
		endpoint.ToolsetID = validUUID(uuid.New())
		_, _, decided, err := f.service.refreshNeedsAuthClientReconnect(t.Context(), endpoint, subject, endpoint.Slug)
		require.NoError(t, err)
		require.False(t, decided, "a missing choice is not a reason to refuse a hosted server's refresh")
	})
}

// A credential with no choice of its own on a direct server is decided by
// what the server offers, never by the clients its sign-in links.
func TestEnforcedCallerWithoutAChoiceIsDecidedByWhatTheServerOffers(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_enforced_unchosen_direct")
	require.NoError(t, err)
	subject := urn.NewUserSubject("user_1")
	// An assistant-runtime token: an authenticated caller with no session row,
	// agent key or principal credential.
	assistant := func(t *testing.T, f *enforcedCallerFixture) context.Context {
		t.Helper()
		return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, UserID: subject.ID, ProjectID: &f.projectID})
	}
	serve := func(t *testing.T, f *enforcedCallerFixture) (*issuerGateAuthentication, upstreamChoice, bool, http.Header, error) {
		t.Helper()
		authentication := f.authentication(subject)
		w := httptest.NewRecorder()
		choice, selected, err := f.service.directUpstreamChoice(assistant(t, f), w, authentication)
		return authentication, choice, selected, w.Header(), err
	}

	t.Run("a server offering no client calls anonymously", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		authentication, choice, selected, _, err := serve(t, f)
		require.NoError(t, err)
		require.True(t, selected, "the request never reads the sign-in's clients")
		require.True(t, choice.noAuth())
		require.True(t, authentication.chosenNoAuth)
	})

	t.Run("the one shared credential serves with its own headers", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		client := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(client, "X-Api-Key", true, false, text("configured"), pgtype.Text{})
		authentication, _, selected, _, err := serve(t, f)
		require.NoError(t, err)
		require.True(t, selected)
		require.NotNil(t, authentication.authClient)
		require.Equal(t, client, authentication.authClient.clientID)
		require.Len(t, authentication.authClient.headers, 1)
		require.True(t, authentication.chosenNoAuth, "a headers-only client acquires no bearer")
	})

	t.Run("the one personal client serves the user's live connection", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
		f.connect(subject, "upstream-token")
		authentication, choice, selected, _, err := serve(t, f)
		require.NoError(t, err)
		require.True(t, selected)
		require.Equal(t, validUUID(f.registration), choice.clientID)
		tokens, err := f.service.resolveAccessTokens(assistant(t, f), authentication, false)
		require.NoError(t, err)
		require.Equal(t, "upstream-token", tokens[f.issuerID].Token, "the sign-in links no client, and the registration still serves")
	})

	t.Run("a personal client with no connection is refused", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
		authentication, _, selected, header, err := serve(t, f)
		require.Error(t, err)
		require.True(t, selected)
		require.NotEmpty(t, header.Get("WWW-Authenticate"))
		require.False(t, authentication.chosenNoAuth)
	})

	t.Run("several usable clients are refused, since nobody chooses", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		for range 2 {
			client := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
			f.copyHeader(client, "X-Api-Key", true, false, text("configured"), pgtype.Text{})
		}
		authentication, _, selected, header, err := serve(t, f)
		require.Error(t, err)
		require.True(t, selected)
		require.NotEmpty(t, header.Get("WWW-Authenticate"))
		require.Nil(t, authentication.authClient)
	})

	t.Run("a client the caller may not use is refused", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		client := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(client, "X-Api-Key", true, false, text("configured"), pgtype.Text{})
		f.service.authClientPermitter = denyPermitter{}
		authentication, _, selected, _, err := serve(t, f)
		require.Error(t, err)
		require.True(t, selected)
		require.Nil(t, authentication.authClient)
		require.False(t, authentication.chosenNoAuth, "a refused client is never replaced by an anonymous call")
	})
}

// A gateway reached by a caller with no credential choice decides each member
// by what the member offers.
func TestEnforcedGatewayWithoutChoicesDecidesEachMemberByWhatItOffers(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_enforced_unchosen_gateway")
	require.NoError(t, err)
	subject := urn.NewUserSubject("user_1")

	f := newEnforcedCallerFixture(t, pool)
	personal := f.serverID
	f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	f.connect(subject, "upstream-token")
	open, _ := f.server()
	shared, _ := f.server()
	f.serverID = shared
	static := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	f.copyHeader(static, "X-Api-Key", true, false, text("configured"), pgtype.Text{})
	ambiguous, _ := f.server()
	f.serverID = ambiguous
	for range 2 {
		client := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(client, "X-Api-Key", true, false, text("configured"), pgtype.Text{})
	}

	endpoint := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn}
	choices := f.service.unchosenMemberChoices(endpoint, subject)
	members := make([]metaMember, 0, 4)
	for _, id := range []uuid.UUID{personal, open, shared, ambiguous} {
		members = append(members, metaMember{serverID: id, projectID: f.projectID, slug: "member-" + id.String()[:8], backend: metaMemberBackendProxied})
	}
	choices.apply(t.Context(), testenv.NewLogger(t), members, false)

	require.NoError(t, members[0].upstreamUnavailable)
	require.Equal(t, validUUID(f.registration), members[0].chosenClientID)
	tokens, err := choices.tokens(t.Context(), members[0])
	require.NoError(t, err)
	require.Equal(t, "upstream-token", tokens[f.issuerID].Token, "the gateway's sign-in links no client, and the registration still serves")

	require.NoError(t, members[1].upstreamUnavailable)
	require.True(t, members[1].upstreamNoAuth, "a member offering no client needs no upstream authentication")
	require.Equal(t, authClientNoAuth, members[1].authClient.outcome)

	require.NoError(t, members[2].upstreamUnavailable)
	require.Equal(t, static, members[2].authClient.clientID)
	require.Len(t, members[2].authClient.headers, 1, "the member's one shared credential sends its own headers")

	require.Error(t, members[3].upstreamUnavailable, "nobody chooses between two usable clients")
}

// An agent key with no choice for a server is served by the registrations the
// server's own auth clients offer, whether or not its sign-in links them.
func TestEnforcedAgentKeyWithoutAChoiceReadsTheServersAuthClients(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_enforced_agent_key_clients")
	require.NoError(t, err)

	t.Run("an offered registration the sign-in does not link serves", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		f.adminClient(authClientTypeServiceAccount, validUUID(f.registration))
		target := agentServerTarget{organizationID: f.orgID, agentID: uuid.New(), serverID: f.serverID, projectID: f.projectID, slug: "server"}

		clients, err := f.service.agentServerClients(t.Context(), target, f.signIn)
		require.NoError(t, err)
		require.Len(t, clients, 1)
		require.Equal(t, f.registration, clients[0].ID)

		source := &fakeUpstreamTokens{issuerID: f.issuerID, bound: uuid.New()}
		f.service.upstreamTokens = source
		authentication := f.authentication(urn.NewAgentSubject(target.agentID))
		choice, err := f.service.authorizeDirectAgentKey(t.Context(), authentication, target.agentID)
		require.NoError(t, err)
		require.Equal(t, upstreamChoice{clientID: validUUID(f.registration), remoteSessionIssuerID: validUUID(f.issuerID)}, choice, "the sole registration routes the request")
		require.Len(t, authentication.agentKeyClients, 1)
		tokens, err := f.service.resolveAccessTokens(t.Context(), authentication, false)
		require.NoError(t, err)
		require.Contains(t, tokens, f.issuerID)
	})

	t.Run("a registration the sign-in links but no auth client offers does not", func(t *testing.T) {
		t.Parallel()
		f := newEnforcedCallerFixture(t, pool)
		f.attach()
		require.NoError(t, f.q.BindClientIssuerFixture(t.Context(), authclientbackfill.BindClientIssuerFixtureParams{RemoteSessionClientID: f.registration, UserSessionIssuerID: f.signIn}))
		target := agentServerTarget{organizationID: f.orgID, agentID: uuid.New(), serverID: f.serverID, projectID: f.projectID, slug: "server"}

		clients, err := f.service.agentServerClients(t.Context(), target, f.signIn)
		require.NoError(t, err)
		require.Empty(t, clients)
	})
}
