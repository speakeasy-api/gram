package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// sharedIssuerFixture is one client on a user session issuer that fronts two
// remote servers with different URLs, associated with the first only.
type sharedIssuerFixture struct {
	ti             *testInstance
	organizationID string
	clientID       uuid.UUID
	siblingID      uuid.UUID
	ownURL         string
	otherURL       string
}

func seedSharedIssuerFixture(t *testing.T, slug string) (context.Context, sharedIssuerFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fx := sharedIssuerFixture{
		ti:             ti,
		organizationID: authCtx.ActiveOrganizationID,
		clientID:       uuid.Nil,
		siblingID:      uuid.Nil,
		ownURL:         "https://own.example.com/mcp",
		otherURL:       "https://other.example.com/mcp",
	}
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+slug)
	fx.clientID, _ = seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, fx.organizationID, "rsi-"+slug)
	fx.siblingID, _ = seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, fx.organizationID, "rsi-sibling-"+slug)
	own := seedRemoteMCPServerForIssuer(t, ctx, ti, userIssuerID, slug+"-own", fx.ownURL)
	other := seedRemoteMCPServerForIssuer(t, ctx, ti, userIssuerID, slug+"-other", fx.otherURL)
	associateClient(t, ctx, ti.conn, fx.organizationID, *authCtx.ProjectID, own, fx.clientID)
	associateClient(t, ctx, ti.conn, fx.organizationID, *authCtx.ProjectID, other, fx.siblingID)
	return ctx, fx
}

func newFlaggedResolveManager(t *testing.T, fx sharedIssuerFixture, enabled bool) *remotesessions.ChallengeManager {
	t.Helper()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagUpstreamIssuerFromAssociations, fx.organizationID, enabled)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		fx.ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
		nil,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
		remotesessions.WithFeatureFlags(flags),
	)
}

func TestFallbackResourceForClient_SharedIssuerAmbiguousFromBindings(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedIssuerFixture(t, "fallback-bindings")
	mgr := newFlaggedResolveManager(t, fx, false)

	resource, err := mgr.FallbackResourceForClient(ctx, fx.organizationID, fx.clientID)
	require.NoError(t, err)
	require.Empty(t, resource, "every server on the shared issuer counts, so the client names two upstreams")
}

func TestFallbackResourceForClient_SharedIssuerFromAssociations(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedIssuerFixture(t, "fallback-associations")
	mgr := newFlaggedResolveManager(t, fx, true)

	resource, err := mgr.FallbackResourceForClient(ctx, fx.organizationID, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, fx.ownURL, resource, "only the associated server counts")
}

func TestResourceForClientAtUpstream_SharedIssuerFromAssociations(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedIssuerFixture(t, "upstream-associations")
	mgr := newFlaggedResolveManager(t, fx, true)

	resource, err := mgr.ResourceForClientAtUpstream(ctx, fx.organizationID, fx.clientID, []uuid.UUID{fx.siblingID}, fx.ownURL)
	require.NoError(t, err)
	require.Equal(t, fx.ownURL, resource)

	applies, err := mgr.ResourceAppliesToClients(ctx, fx.organizationID, []uuid.UUID{fx.clientID, fx.siblingID}, fx.ownURL)
	require.NoError(t, err)
	require.True(t, applies[fx.clientID])
	require.False(t, applies[fx.siblingID], "the sibling is associated with the other server")
}

func TestResourceForClientAtUpstream_SharedIssuerUnqualifiedFromBindings(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedIssuerFixture(t, "upstream-bindings")
	mgr := newFlaggedResolveManager(t, fx, false)

	resource, err := mgr.ResourceForClientAtUpstream(ctx, fx.organizationID, fx.clientID, []uuid.UUID{fx.siblingID}, fx.ownURL)
	require.NoError(t, err)
	require.Empty(t, resource, "the sibling also serves the upstream through the shared issuer, so no client may claim it")
}

func TestResourceOwnersAmong_SharedIssuerFromAssociations(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedIssuerFixture(t, "owners-associations")

	owners, err := remotesessions.ResourceOwnersAmong(ctx, fx.ti.conn, fx.organizationID, []uuid.UUID{fx.clientID, fx.siblingID}, fx.ownURL, true)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{fx.clientID: true, fx.siblingID: false}, owners)

	foreign, err := remotesessions.ResourceOwnersAmong(ctx, fx.ti.conn, "org-not-"+fx.organizationID, []uuid.UUID{fx.clientID, fx.siblingID}, fx.ownURL, true)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{fx.clientID: false, fx.siblingID: false}, foreign, "another organization's caller sees no associations")
}
