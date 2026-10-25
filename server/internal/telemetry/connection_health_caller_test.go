package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// callerFixture is a gateway with a sign-in server, two of its registered
// clients, two providers the project can see, and a person in the
// organization.
type callerFixture struct {
	gateway        metamcprepo.MetaMcpServer
	client, other  string
	foreignClient  string
	providerA      string
	providerB      string
	person, second string
}

func newCallerFixture(t *testing.T, ctx context.Context, ti *testInstance) callerFixture {
	t.Helper()
	projectID := uuid.MustParse(ti.projectID)
	sessions := usersessionsrepo.New(ti.conn)
	issuer := func(slug string) uuid.UUID {
		row, err := sessions.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
			ProjectID: projectID, OrganizationID: conv.ToPGText(ti.orgID), Slug: slug + "-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive",
			SessionDuration:         pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
			AuthorizationServerMode: pgtype.Text{String: "", Valid: false}, UseAuthenticationHost: pgtype.Bool{Bool: false, Valid: false},
		})
		require.NoError(t, err)
		return row.ID
	}
	client := func(issuerID uuid.UUID, name string) string {
		row, err := sessions.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
			ClientID: "client-" + uuid.NewString(), ClientSecretHash: pgtype.Text{String: "", Valid: false}, ClientName: name,
			RedirectUris: []string{"http://127.0.0.1/callback"}, ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: "none",
			ClientJwks: nil, ClientJwksUri: pgtype.Text{String: "", Valid: false}, UserSessionIssuerID: issuerID,
		})
		require.NoError(t, err)
		return row.ID.String()
	}
	provider := func(name string) string {
		row, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
			ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(ti.orgID), Name: conv.ToPGTextEmpty(name),
			Slug: "provider-" + uuid.NewString()[:8], Issuer: "https://provider-" + uuid.NewString()[:8] + ".example.test",
			AuthorizationEndpoint: conv.ToPGText("https://provider.example.test/authorize"), TokenEndpoint: conv.ToPGText("https://provider.example.test/token"),
			ScopesSupported: []string{}, ScopeOverride: []string{}, AuthorizationGrantProfilesSupported: []string{},
			IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
			GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"},
			TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"}, CodeChallengeMethodsSupported: []string{"S256"},
		})
		require.NoError(t, err)
		return row.ID.String()
	}
	gatewayIssuer := issuer("caller-gateway")
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: ti.orgID, ProjectID: projectID, Name: "Caller Gateway", UserSessionIssuerID: uuid.NullUUID{UUID: gatewayIssuer, Valid: true}, Visibility: visibility.Private})
	require.NoError(t, err)
	personID, _ := seedConnectedOrgUser(t, ctx, ti, "Example Person")
	secondID, _ := seedConnectedOrgUser(t, ctx, ti, "Example Colleague")
	return callerFixture{
		gateway: gateway, client: client(gatewayIssuer, "Example Desktop"), other: client(gatewayIssuer, "Example Laptop"),
		foreignClient: client(issuer("caller-other"), "Other Client"),
		providerA:     provider("Example Tracker"), providerB: provider("Example Wiki"),
		person: urn.NewUserSubject(personID).String(), second: urn.NewUserSubject(secondID).String(),
	}
}

// One person and client is counted alone, across every provider: a single
// successful provider sign-in is listed though the gateway-wide ranking's
// top keys are all someone else's failures, and nothing of another person,
// client, gateway, or project, or outside [from, to), is counted.
func TestGatewayConnectionCaller_CountsOnlyThatCallerAcrossProviders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	fx := newCallerFixture(t, ctx, ti)
	gateway := fx.gateway.ID.String()
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	from, to := day.Add(10*time.Hour+10*time.Minute), day.Add(11*time.Hour+50*time.Minute)
	attempt := func(project, gatewayID, stage, outcome, subject, provider, client string, at time.Time) {
		logConnectionAttempt(t, ctx, ti, connectionAttempt{project: project, gateway: gatewayID, stage: stage, outcome: outcome, reason: "", subject: subject, provider: provider, client: client, retries: 0, at: at})
	}
	mine := func(stage, outcome, provider string, at time.Time) {
		attempt(ti.projectID, gateway, stage, outcome, fx.person, provider, fx.client, at)
	}
	callback, refresh := telemetry.ConnectionStageUpstreamCallback, telemetry.ConnectionStageUpstreamRefresh
	hiddenProvider := uuid.NewString()
	firstCode, lastCode := from.Add(5*time.Minute), from.Add(50*time.Minute)

	mine(callback, "stored", fx.providerA, from) // exactly at the start: counted
	mine(callback, "stored", fx.providerA, from.Add(4*time.Minute))
	mine(callback, "failed", fx.providerB, from.Add(30*time.Minute))
	mine(callback, "stored", hiddenProvider, from.Add(31*time.Minute))
	mine(callback, "stored", "", from.Add(32*time.Minute))
	mine(callback, "stored", fx.providerA, to) // exactly at the end: not counted
	mine(refresh, "refreshed", fx.providerA, from.Add(40*time.Minute))
	mine(telemetry.ConnectionStageInboundToken, "code_issued", "", firstCode)
	mine(telemetry.ConnectionStageInboundToken, "code_issued", "", lastCode)
	mine(telemetry.ConnectionStageInboundToken, "refreshed", "", lastCode.Add(10*time.Minute))
	mine(telemetry.ConnectionStageDiscoveryListing, "failed", "", from.Add(20*time.Minute))
	mine(telemetry.ConnectionStageDiscoveryListing, "complete", "", from.Add(60*time.Minute))
	// The same person with another client, another person with this client,
	// and this caller on another gateway or project are not this caller.
	attempt(ti.projectID, gateway, callback, "stored", fx.person, fx.providerA, fx.other, from.Add(time.Minute))
	attempt(ti.projectID, gateway, callback, "stored", fx.second, fx.providerA, fx.client, from.Add(time.Minute))
	attempt(ti.projectID, uuid.NewString(), callback, "stored", fx.person, fx.providerA, fx.client, from.Add(time.Minute))
	attempt(uuid.NewString(), gateway, callback, "stored", fx.person, fx.providerA, fx.client, from.Add(time.Minute))
	// 25 failing keys of someone else fill the gateway-wide ranking.
	for range 25 {
		noisy := uuid.NewString()
		for range 5 {
			attempt(ti.projectID, gateway, callback, "failed", fx.second, noisy, fx.other, from.Add(15*time.Minute))
		}
	}

	window := func() (*string, *string) {
		f, e := from.Format(time.RFC3339), to.Format(time.RFC3339)
		return &f, &e
	}
	caller := &gen.GatewayConnectionCaller{Subject: fx.person, Client: fx.client}
	f, e := window()
	summary, err := ti.service.GetGatewayConnectionHealth(ctx, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gateway, From: f, To: e, Key: nil, Caller: caller})
	require.NoError(t, err)
	require.Equal(t, caller, summary.Caller, "the answer says whom it counted")
	stages := map[string]*gen.GatewayConnectionStage{}
	names := []string{}
	for _, stage := range summary.Stages {
		stages[stage.Stage] = stage
		names = append(names, stage.Stage)
	}
	require.Equal(t, []string{"inbound_token", "upstream_callback", "upstream_refresh", "discovery_listing", "discovery_member"}, names, "sign-in requests are recorded before the caller is known")
	require.Equal(t, telemetry.ConnectionStageInboundAuthorize, summary.NotRecorded[0].Stage)
	require.Equal(t, int64(5), stages[callback].Attempts, "the start is counted, the end is not, and only this caller")
	require.Equal(t, map[string]int64{"stored": 4, "failed": 1}, outcomeMap(stages[callback].Outcomes))
	require.Equal(t, map[string]int64{"refreshed": 1}, outcomeMap(stages[refresh].Outcomes))
	require.Equal(t, map[string]int64{"code_issued": 2, "refreshed": 1}, outcomeMap(stages[telemetry.ConnectionStageInboundToken].Outcomes))
	require.Equal(t, lastCode.Format(time.RFC3339Nano), *stages[telemetry.ConnectionStageInboundToken].LastCodeIssuedAt, "a later refresh does not move it")
	require.Nil(t, stages[callback].LastCodeIssuedAt)
	require.Equal(t, map[string]int64{"failed": 1, "complete": 1}, outcomeMap(stages[telemetry.ConnectionStageDiscoveryListing].Outcomes))

	rank := func(callerScope *gen.GatewayConnectionCaller, limit int64) *gen.ListGatewayConnectionHotspotsResult {
		t.Helper()
		f, e := window()
		result, err := ti.service.ListGatewayConnectionHotspots(ctx, &gen.ListGatewayConnectionHotspotsPayload{MetaMcpServerID: gateway, From: f, To: e, Stage: callback, RankBy: "count", MinAttempts: 1, Limit: limit, Caller: callerScope})
		require.NoError(t, err)
		return result
	}
	global := rank(nil, 25)
	require.True(t, global.Truncated)
	require.Nil(t, global.Caller)
	for _, hotspot := range global.Hotspots {
		require.NotEqual(t, fx.person, hotspot.Subject.ID, "the gateway-wide top keys are all someone else's")
	}

	ranked := rank(caller, 25)
	require.False(t, ranked.Truncated)
	require.Equal(t, caller, ranked.Caller)
	byProvider := map[string]*gen.GatewayConnectionHotspot{}
	for _, hotspot := range ranked.Hotspots {
		require.Equal(t, fx.person, *hotspot.Subject.ID)
		require.Equal(t, fx.client, *hotspot.Client.ID)
		byProvider[hotspot.Provider.Label] = hotspot
	}
	require.Len(t, byProvider, 4)
	require.Equal(t, map[string]int64{"stored": 2}, outcomeMap(byProvider["Example Tracker"].Outcomes), "a success-only provider is listed")
	require.Equal(t, fx.providerA, *byProvider["Example Tracker"].Provider.ID)
	require.Equal(t, map[string]int64{"failed": 1}, outcomeMap(byProvider["Example Wiki"].Outcomes))
	hidden := byProvider["Unknown provider"]
	require.NotNil(t, hidden)
	require.Nil(t, hidden.Provider.ID)
	require.Nil(t, hidden.Provider.Key, "a provider the project cannot see is neither named nor selectable")
	unrecorded := byProvider["Provider not recorded"]
	require.NotNil(t, unrecorded)
	require.Equal(t, "unrecorded", unrecorded.Provider.Kind, "an empty provider on a provider stage is unrecorded, not none")
	require.Empty(t, *unrecorded.Provider.Key)

	capped := rank(caller, 2)
	require.Len(t, capped.Hotspots, 2)
	require.True(t, capped.Truncated)

	// A listing records no provider: its empty key is labeled for the whole
	// gateway, not as having no provider.
	f, e = window()
	listings, err := ti.service.ListGatewayConnectionHotspots(ctx, &gen.ListGatewayConnectionHotspotsPayload{MetaMcpServerID: gateway, From: f, To: e, Stage: telemetry.ConnectionStageDiscoveryListing, RankBy: "count", MinAttempts: 1, Limit: 25, Caller: caller})
	require.NoError(t, err)
	require.Len(t, listings.Hotspots, 1)
	require.Equal(t, "gateway_wide", listings.Hotspots[0].Provider.Kind)
	require.Empty(t, *listings.Hotspots[0].Provider.Key)
}

// A caller scope is checked before any attempt is read: a key and a caller
// together, or a caller that is not a person or agent with a registered
// client, are invalid, and one the reader may not see now is not found.
func TestGatewayConnectionCaller_RefusesBeforeReadingAttempts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	fx := newCallerFixture(t, ctx, ti)
	gateway := fx.gateway.ID.String()
	status := func(err error) oops.Code {
		shareable, ok := errors.AsType[*oops.ShareableError](err)
		require.True(t, ok, "%v", err)
		return shareable.Code
	}
	summary := func(key *gen.GatewayConnectionKey, caller *gen.GatewayConnectionCaller) error {
		_, err := ti.service.GetGatewayConnectionHealth(ctx, &gen.GetGatewayConnectionHealthPayload{MetaMcpServerID: gateway, From: nil, To: nil, Key: key, Caller: caller})
		return err //nolint:wrapcheck // the test reads the handler's own error
	}
	require.Equal(t, oops.CodeBadRequest, status(summary(&gen.GatewayConnectionKey{Stage: telemetry.ConnectionStageDiscoveryListing, Subject: fx.person, ProviderID: "", Client: fx.client}, &gen.GatewayConnectionCaller{Subject: fx.person, Client: fx.client})))
	for name, caller := range map[string]gen.GatewayConnectionCaller{
		"an API key":          {Subject: "apikey:" + uuid.NewString(), Client: fx.client},
		"not a URN":           {Subject: "someone", Client: fx.client},
		"a recorded kind":     {Subject: fx.person, Client: "credential:user"},
		"an anonymous client": {Subject: fx.person, Client: "anonymous"},
	} {
		require.Equal(t, oops.CodeBadRequest, status(summary(nil, &caller)), name)
	}

	// The reader is built with no attempt store, so a refusal that read one
	// would panic instead of answering.
	engine := authz.NewEngine(ti.logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	reader := telemetry.NewConnectionHealth(ti.conn, nil, enabled, engine)
	target := telemetry.ConnectionHealthTarget{ProjectID: uuid.MustParse(ti.projectID), OrganizationID: ti.orgID, GatewayID: fx.gateway.ID}
	window, err := telemetry.ParseConnectionHealthWindow("", "", time.Now())
	require.NoError(t, err)
	for name, caller := range map[string]telemetry.ConnectionCaller{
		"person outside the organization":  {Subject: urn.NewUserSubject("user_" + uuid.NewString()).String(), ClientKey: fx.client},
		"client of another sign-in server": {Subject: fx.person, ClientKey: fx.foreignClient},
		"client that does not exist":       {Subject: fx.person, ClientKey: uuid.NewString()},
	} {
		_, err := reader.Summary(ctx, target, window, telemetry.ConnectionHealthScope{Key: nil, Caller: &caller})
		require.ErrorIs(t, err, telemetry.ErrConnectionHealthCallerNotFound, name)
		_, err = reader.Hotspots(ctx, target, window, telemetry.ConnectionHotspotQuery{Stage: telemetry.ConnectionStageUpstreamCallback, ByRate: false, MinAttempts: 1, Limit: 25, Caller: &caller})
		require.ErrorIs(t, err, telemetry.ErrConnectionHealthCallerNotFound, name)
		require.Equal(t, oops.CodeNotFound, status(summary(nil, &gen.GatewayConnectionCaller{Subject: caller.Subject, Client: caller.ClientKey})), name)
	}
}
