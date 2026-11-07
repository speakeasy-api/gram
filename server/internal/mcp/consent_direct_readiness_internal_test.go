package mcp

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// These are existing consent entry points so the tests can also be run with
// the production fix reverted, against the expanded private database key.
func TestConsentSessionRouting_DirectPersistedTargetReadiness(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_direct_target_readiness")
	require.NoError(t, err)
	require.NoError(t, testrepo.New(pool).ExpandRemoteSessionResourceIndexesFixture(t.Context()))
	f := newAuthClientFixture(t, pool)
	ctx := t.Context()
	f.service.features = new(feature.InMemory)
	f.enforce()
	f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})
	signIn := uuid.New()
	require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: signIn, ProjectID: validUUID(f.projectID), Slug: "usi-" + signIn.String()[:8]}))
	subject := urn.NewUserSubject("user_target_readiness")
	endpoint := ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: signIn, McpServerID: validUUID(f.serverID)}
	typed := map[uuid.UUID]uuid.UUID{f.registration: f.projectID}
	clients := []remotesessions.Client{{ID: f.registration}}
	const resourceA = "https://target-a.example.test/mcp"
	const resourceB = "https://target-b.example.test/mcp"
	q := remotesessionsrepo.New(pool)
	token, err := f.service.enc.Encrypt([]byte("test-token"))
	require.NoError(t, err)
	put := func(resource, identity, scope string) uuid.UUID {
		t.Helper()
		row, err := q.UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
			SubjectUrn: subject, UserSessionIssuerID: signIn, RemoteSessionClientID: f.registration,
			AccessTokenEncrypted: token, AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
			Resource: pgtype.Text{String: resource, Valid: resource != ""}, Scopes: []string{scope},
			UpstreamDisplayName: pgtype.Text{String: identity, Valid: true}, IdentitySource: pgtype.Text{String: "id_token", Valid: true},
		})
		require.NoError(t, err)
		return row.ID
	}
	read := func(e ResolvedMcpEndpoint, who urn.SessionSubject) map[uuid.UUID]remotesessions.RemoteSessionState {
		t.Helper()
		statuses, _, err := f.service.consentSessionRouting(ctx, &e, AuthnChallengeState{Subject: &who}, clients, typed)
		require.NoError(t, err)
		return statuses
	}
	endpoint.UpstreamResource = resourceA
	a := put(resourceA, "Account A", "read:a")
	require.Equal(t, a, read(endpoint, subject)[f.registration].ID)
	endpoint.UpstreamResource = resourceB
	require.Empty(t, read(endpoint, subject), "A alone cannot ready B")
	b := put(resourceB, "Account B", "write:b")
	require.NotEqual(t, a, b, "both resource grants must coexist")
	for _, target := range []struct {
		resource, identity, scope string
		id                        uuid.UUID
	}{
		{resourceA, "Account A", "read:a", a}, {resourceB, "Account B", "write:b", b},
	} {
		endpoint.UpstreamResource = target.resource
		state := read(endpoint, subject)[f.registration]
		require.Equal(t, target.id, state.ID)
		require.Equal(t, remotesessions.RemoteSessionActive, state.Status)
		require.Equal(t, target.resource, state.Resource)
		require.Equal(t, []string{target.scope}, state.Scopes)
		require.Equal(t, target.identity, state.ConnectedAs)
	}
	require.Empty(t, read(endpoint, urn.NewUserSubject("user_other_readiness")), "another subject cannot lend readiness")
	foreign := endpoint
	foreign.OrganizationID = "org_other_readiness"
	require.Empty(t, read(foreign, subject), "the same registration cannot cross tenants")

	legacy := put("", "Legacy account", "legacy:scope")
	require.NotEqual(t, legacy, b)
	require.Equal(t, b, read(endpoint, subject)[f.registration].ID, "exact target outranks the legacy NULL row")
	hosted := endpoint
	hosted.ToolsetID = validUUID(uuid.New())
	require.Empty(t, read(hosted, subject), "hosted/unqualified ambiguity stays closed")
	aggregate := endpoint
	aggregate.MetaMcpServerID = validUUID(uuid.New())
	require.Empty(t, read(aggregate, subject), "aggregate reads must not borrow the direct resource")
	require.Empty(t, read(ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: signIn}, subject), "an unqualified endpoint stays closed")

	// The compatibility reader remains link-scoped; target qualification does
	// not make an unlinked typed registration a legacy sign-in registration.
	statuses, err := f.service.endpointRemoteSessionStatuses(ctx, subject, &endpoint)
	require.NoError(t, err)
	require.Empty(t, statuses)
	require.NoError(t, f.q.BindClientIssuerFixture(ctx, authclientbackfill.BindClientIssuerFixtureParams{RemoteSessionClientID: f.registration, UserSessionIssuerID: signIn}))
	statuses, err = f.service.endpointRemoteSessionStatuses(ctx, subject, &endpoint)
	require.NoError(t, err)
	require.Equal(t, b, statuses[f.registration].ID)
	unlinked := endpoint
	unlinked.UserSessionIssuerID = uuid.New()
	statuses, err = f.service.endpointRemoteSessionStatuses(ctx, subject, &unlinked)
	require.NoError(t, err)
	require.Empty(t, statuses)
}
