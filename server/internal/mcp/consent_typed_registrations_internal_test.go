package mcp

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestTypedConsentRegistrationsSkipsMembersUnlessEnforced(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_typed_consent_members")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	flags := new(feature.InMemory)
	f.service.features = flags
	f.service.authEnforcement = authenforcement.NewGate(pool)
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, MetaMcpServerID: validUUID(uuid.New())}
	for _, shadow := range []bool{false, true} {
		flags.SetFlag(feature.FlagAuthClientResolver, f.orgID, shadow)
		typed, err := f.service.typedConsentRegistrations(t.Context(), endpoint, func() ([]metaMember, error) {
			t.Fatal("disabled typed resolution must not load the member snapshot")
			return nil, nil
		})
		require.NoError(t, err)
		require.Empty(t, typed)
		subject := urn.NewUserSubject("user_1")
		picks, err := f.service.consentDirectAuthClientPick(t.Context(), &ResolvedMcpEndpoint{OrganizationID: f.orgID, McpServerID: validUUID(f.serverID)}, AuthnChallengeState{Subject: &subject}, "", nil, nil)
		require.NoError(t, err)
		require.Nil(t, picks, "non-enforced approval must not need card-building dependencies")
	}
	f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	f.enforce()
	members := []metaMember{{serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied}}
	require.NoError(t, f.service.withTypedMemberRegistrations(t.Context(), f.orgID, members))
	calls := 0
	typed, err := f.service.typedConsentRegistrations(t.Context(), endpoint, func() ([]metaMember, error) {
		calls++
		return members, nil
	})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]uuid.UUID{f.registration: f.projectID}, typed)
	require.Equal(t, 1, calls)
}

func TestTypedConsentRegistrationsReuseMemberSnapshot(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_typed_consent_snapshot")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.enforce()
	registration, legacyRegistration := uuid.New(), uuid.New()
	members := []metaMember{
		{projectID: uuid.New(), associatedClients: map[uuid.UUID]bool{legacyRegistration: true}},
		{projectID: f.projectID, typedRegistrations: true, associatedClients: map[uuid.UUID]bool{registration: true}},
		{projectID: uuid.New(), typedRegistrations: true, associatedClients: map[uuid.UUID]bool{registration: true}},
	}
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, MetaMcpServerID: validUUID(uuid.New())}
	typed, err := f.service.typedConsentRegistrations(t.Context(), endpoint, func() ([]metaMember, error) {
		return members, nil
	})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]uuid.UUID{registration: f.projectID}, typed, "reuse only typed registrations; the first member owns a shared registration's project")
}

// Where typed auth client resolution is enforced, a proxied member is matched
// to the registrations it offers through typed auth clients, never to a
// client only an older association or its derived authorization server names.
func TestTypedMemberRegistrationsDecideMemberClients(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_typed_member_clients")
	require.NoError(t, err)
	for _, tc := range []struct {
		name         string
		enforced     bool
		offer        string
		usesTyped    bool
		usesOlder    bool
		serverStatus gatewayServerStatus
	}{
		{name: "not enforced keeps the association", offer: authClientTypeRemoteSession, usesOlder: true, serverStatus: gatewayServerNotSetUp},
		{name: "personal offer replaces the association", enforced: true, offer: authClientTypeRemoteSession, usesTyped: true, serverStatus: gatewayServerNotSetUp},
		{name: "identity provider offer replaces the association", enforced: true, offer: authClientTypeEMA, usesTyped: true, serverStatus: gatewayServerNotSetUp},
		{name: "shared credential offers no sign-in", enforced: true, offer: authClientTypeStaticCredential, serverStatus: gatewayServerNoSignIn},
		{name: "no offer offers no sign-in", enforced: true, serverStatus: gatewayServerNoSignIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			f.service.features = new(feature.InMemory)
			f.service.authEnforcement = authenforcement.NewGate(pool)
			if tc.enforced {
				f.enforce()
			}
			switch tc.offer {
			case authClientTypeStaticCredential:
				f.adminClient(tc.offer, uuid.NullUUID{})
			case authClientTypeRemoteSession, authClientTypeEMA:
				f.adminClient(tc.offer, validUUID(f.registration))
			}
			older := uuid.New()
			hosted := metaMember{serverID: uuid.New(), projectID: f.projectID, backend: metaMemberBackendHosted, remoteSessionIssuerID: validUUID(f.issuerID)}
			members := []metaMember{
				{serverID: f.serverID, projectID: f.projectID, slug: "proxied", backend: metaMemberBackendProxied, remoteSessionIssuerID: validUUID(f.issuerID), associatedClients: map[uuid.UUID]bool{older: true}},
				hosted,
			}
			require.NoError(t, f.service.withTypedMemberRegistrations(t.Context(), f.orgID, members))

			require.Equal(t, tc.usesTyped, memberUsesClient(members[0], f.registration, f.issuerID, validUUID(f.projectID)))
			require.Equal(t, tc.usesOlder, memberUsesClient(members[0], older, f.issuerID, validUUID(f.projectID)))
			require.Equal(t, tc.serverStatus, gatewayServersView(members[:1], nil).Servers[0].Status)
			require.Equal(t, hosted, members[1], "a hosted member keeps matching clients by its authorization server")
		})
	}
}

// A registration the endpoint offers through a typed auth client reads the
// state the auth client resolver would serve, whatever the sign-in's own
// links report for it.
func TestTypedRegistrationStatusesReplaceSignInStatuses(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_typed_registration_statuses")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})

	signInOnly := uuid.New()
	connected := remotesessions.RemoteSessionState{Status: remotesessions.RemoteSessionActive}
	statuses, err := f.service.withTypedRegistrationStatuses(t.Context(), f.orgID, urn.NewUserSubject("user_1"),
		map[uuid.UUID]remotesessions.RemoteSessionState{f.registration: connected, signInOnly: connected},
		map[uuid.UUID]uuid.UUID{f.registration: f.projectID})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]remotesessions.RemoteSessionState{signInOnly: connected}, statuses, "the resolver serves no session for the typed registration")
}

// A typed registration keeps the caller's consented session even when the
// sign-in's status snapshot has no entry for that registration.
func TestTypedRegistrationStatusesPreserveConsentedSession(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_typed_registration_session")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	ctx := t.Context()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})

	consentedUnder := uuid.New()
	require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{
		ID: consentedUnder, ProjectID: validUUID(f.projectID), Slug: "usi-" + consentedUnder.String()[:8],
	}))
	subject := urn.NewUserSubject("user_1")
	accessEnc, err := f.service.enc.Encrypt([]byte("typed-upstream-token"))
	require.NoError(t, err)
	session, err := remotesessionsrepo.New(pool).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   consentedUnder,
		RemoteSessionClientID: f.registration,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(t, err)

	signInOnly := uuid.New()
	connected := remotesessions.RemoteSessionState{Status: remotesessions.RemoteSessionActive}
	statuses, err := f.service.withTypedRegistrationStatuses(ctx, f.orgID, subject,
		map[uuid.UUID]remotesessions.RemoteSessionState{signInOnly: connected},
		map[uuid.UUID]uuid.UUID{f.registration: f.projectID})
	require.NoError(t, err)
	require.Len(t, statuses, 2)
	typedStatus, ok := statuses[f.registration]
	require.True(t, ok, "a typed registration must retain its consented session without a sign-in link")
	require.Equal(t, session.ID, typedStatus.ID)
	require.Equal(t, remotesessions.RemoteSessionActive, typedStatus.Status)
	require.Equal(t, connected, statuses[signInOnly], "unrelated sign-in statuses remain unchanged")
}
