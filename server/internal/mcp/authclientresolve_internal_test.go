package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func validUUID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

func TestDecideAuthClient(t *testing.T) {
	t.Parallel()

	clientID, registrationID, issuerID := uuid.New(), uuid.New(), uuid.New()
	live := func(clientType string) upstreamclientsrepo.GetAuthClientSelectionRow {
		return upstreamclientsrepo.GetAuthClientSelectionRow{
			NoAuth:               false,
			AuthClientID:         validUUID(clientID),
			ClientID:             validUUID(clientID),
			ClientType:           pgtype.Text{String: clientType, Valid: true},
			ClientEnabled:        pgtype.Bool{Bool: true, Valid: true},
			ClientDeleted:        pgtype.Bool{Bool: false, Valid: true},
			RegistrationID:       validUUID(registrationID),
			RegistrationIssuerID: validUUID(issuerID),
			RegistrationLive:     true,
			RegistrationAttached: true,
			Associated:           true,
			RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true},
			ServerHasClients:     true,
		}
	}
	noAuth := func(requires pgtype.Bool, hasClients bool) upstreamclientsrepo.GetAuthClientSelectionRow {
		row := live(authClientTypeRemoteSession)
		row.NoAuth = true
		row.AuthClientID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		row.RequiresUpstreamAuth = requires
		row.ServerHasClients = hasClients
		return row
	}
	with := func(clientType string, edit func(*upstreamclientsrepo.GetAuthClientSelectionRow)) upstreamclientsrepo.GetAuthClientSelectionRow {
		row := live(clientType)
		edit(&row)
		return row
	}
	headersOnly := func(clientType string, edit func(*upstreamclientsrepo.GetAuthClientSelectionRow)) upstreamclientsrepo.GetAuthClientSelectionRow {
		return with(clientType, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) {
			r.RegistrationID = uuid.NullUUID{}
			r.RegistrationAttached = false
			edit(r)
		})
	}
	same := func(*upstreamclientsrepo.GetAuthClientSelectionRow) {}
	undecided := pgtype.Bool{Bool: false, Valid: false}

	cases := []struct {
		name    string
		row     upstreamclientsrepo.GetAuthClientSelectionRow
		outcome authClientOutcome
		reason  string
	}{
		{"personal connection resolves to its registration", live(authClientTypeRemoteSession), authClientResolved, ""},
		{"service account resolves to its registration", live(authClientTypeServiceAccount), authClientResolved, ""},
		{"static credential resolves with no registration", headersOnly(authClientTypeStaticCredential, same), authClientResolved, ""},
		{"pass-through resolves with no registration", headersOnly(authClientTypePassThrough, same), authClientResolved, ""},
		{"EMA stays with the upstream client choice", live(authClientTypeEMA), authClientLegacy, authClientReasonEMA},
		{"a deleted client is a gap", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientDeleted.Bool = true }), authClientGap, authClientReasonClientDeleted},
		{"a disabled client is a gap", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientEnabled.Bool = false }), authClientGap, authClientReasonClientDisabled},
		{"a client detached from the server is a gap", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Associated = false }), authClientGap, authClientReasonUnassociated},
		{"a client outside the tenant reads as gone", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientID = uuid.NullUUID{} }), authClientGap, authClientReasonClientGone},
		{"a deleted registration is a gap", with(authClientTypeServiceAccount, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationLive = false }), authClientGap, authClientReasonRegistration},
		{"a registration detached from the server is a gap", with(authClientTypeServiceAccount, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationAttached = false }), authClientGap, authClientReasonDetached},
		{"an unknown type is a gap", live("smoke_signal"), authClientGap, authClientReasonUnknownType},
		{"an unmapped OAuth choice is served the old way", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.AuthClientID = uuid.NullUUID{} }), authClientLegacy, authClientReasonUnmapped},
		{"a personal connection that may chain is served the old way", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationHasEma = true }), authClientLegacy, authClientReasonLegacyMayChain},
		{"a service account never chains", with(authClientTypeServiceAccount, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationHasEma = true }), authClientResolved, ""},
		{"an OAuth client missing a server credential header is a setup gap", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.CredentialHeadersUncovered = true }), authClientGap, authClientReasonSetup},
		{"an OAuth client requiring the inbound Authorization is a setup gap", with(authClientTypeServiceAccount, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RequiresDeniedSource = true }), authClientGap, authClientReasonSetup},
		{"a static credential on a tunneled server is served the old way", headersOnly(authClientTypeStaticCredential, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }), authClientLegacy, authClientReasonTunneledHeaders},
		{"a pass-through on a tunneled server is served the old way", headersOnly(authClientTypePassThrough, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }), authClientLegacy, authClientReasonTunneledHeaders},
		{"stale header copies are served the old way", headersOnly(authClientTypeStaticCredential, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }), authClientLegacy, authClientReasonHeadersStale},
		{"a personal connection's stale header copies are served the old way", with(authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }), authClientLegacy, authClientReasonHeadersStale},
		{"a service account's stale header copies are served the old way", with(authClientTypeServiceAccount, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }), authClientLegacy, authClientReasonHeadersStale},
		{"a headers-only client missing a server credential header is a setup gap", headersOnly(authClientTypePassThrough, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.CredentialHeadersUncovered = true }), authClientGap, authClientReasonSetup},
		{"a pass-through requiring the inbound Authorization is a setup gap", headersOnly(authClientTypePassThrough, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RequiresDeniedSource = true }), authClientGap, authClientReasonSetup},
		{"no-auth on a server that explicitly needs none", noAuth(pgtype.Bool{Bool: false, Valid: true}, true), authClientNoAuth, ""},
		{"no-auth on an undecided server with no clients", noAuth(undecided, false), authClientNoAuth, ""},
		{"no-auth on an undecided server that has clients is a gap", noAuth(undecided, true), authClientGap, authClientReasonNoAuthStale},
		{"no-auth on a server that needs auth is a gap", noAuth(pgtype.Bool{Bool: true, Valid: true}, false), authClientGap, authClientReasonNoAuthForbidden},
		{"no-auth on a server with only header clients is served the old way", func() upstreamclientsrepo.GetAuthClientSelectionRow {
			row := noAuth(pgtype.Bool{Bool: true, Valid: true}, true)
			row.ServerHasHeaderClients = true
			return row
		}(), authClientLegacy, authClientReasonUnmapped},
		{"no-auth on a server with header and OAuth clients is a gap", func() upstreamclientsrepo.GetAuthClientSelectionRow {
			row := noAuth(pgtype.Bool{Bool: true, Valid: true}, true)
			row.ServerHasHeaderClients = true
			row.ServerHasOauthClients = true
			return row
		}(), authClientGap, authClientReasonNoAuthForbidden},
		{"no-auth on a server with credential headers is a setup gap", func() upstreamclientsrepo.GetAuthClientSelectionRow {
			row := noAuth(undecided, false)
			row.CredentialHeadersUncovered = true
			return row
		}(), authClientGap, authClientReasonSetup},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := decideAuthClient(tc.row)
			require.Equal(t, tc.outcome, res.outcome)
			require.Equal(t, tc.reason, res.reason)
			if res.outcome == authClientResolved && (res.clientType == authClientTypeRemoteSession || res.clientType == authClientTypeServiceAccount) {
				require.Equal(t, validUUID(registrationID), res.choice.clientID)
				require.Equal(t, validUUID(issuerID), res.choice.remoteSessionIssuerID)
			} else {
				require.True(t, res.choice.noAuth(), "only OAuth clients route by a registration")
			}
		})
	}
}

func TestWithoutDeniedSources(t *testing.T) {
	t.Parallel()

	for _, destination := range []string{"Authorization", "X-Token"} {
		t.Run("an optional header reading Authorization into "+destination+" is dropped", func(t *testing.T) {
			t.Parallel()
			kept, blocked := withoutDeniedSources([]proxy.ConfiguredHeader{
				{IsRequired: false, Name: destination, StaticValue: "", ValueFromRequestHeader: " authorization "},
				{IsRequired: true, Name: "X-Tenant", StaticValue: "", ValueFromRequestHeader: "X-Caller-Tenant"},
			})
			require.False(t, blocked)
			require.Equal(t, []proxy.ConfiguredHeader{{IsRequired: true, Name: "X-Tenant", StaticValue: "", ValueFromRequestHeader: "X-Caller-Tenant"}}, kept)
		})
		t.Run("a required header reading Authorization into "+destination+" blocks the client", func(t *testing.T) {
			t.Parallel()
			kept, blocked := withoutDeniedSources([]proxy.ConfiguredHeader{
				{IsRequired: true, Name: destination, StaticValue: "", ValueFromRequestHeader: "AUTHORIZATION"},
			})
			require.True(t, blocked)
			require.Empty(t, kept)
		})
	}
	for _, source := range []string{"Cookie", "proxy_authorization"} {
		kept, blocked := withoutDeniedSources([]proxy.ConfiguredHeader{{IsRequired: false, Name: "X-Forwarded", StaticValue: "", ValueFromRequestHeader: source}})
		require.False(t, blocked)
		require.Empty(t, kept, "%s is a denied pass-through source", source)
	}
	kept, blocked := withoutDeniedSources([]proxy.ConfiguredHeader{{IsRequired: true, Name: "Authorization", StaticValue: "Bearer stored", ValueFromRequestHeader: ""}})
	require.False(t, blocked)
	require.Len(t, kept, 1, "a stored Authorization value is the client's own credential")
}

func TestRefreshDecision(t *testing.T) {
	t.Parallel()

	resolved := func(clientType string) authClientResolution {
		res := gapResolution("")
		res.outcome = authClientResolved
		res.clientType = clientType
		return res
	}
	noAuth := gapResolution("")
	noAuth.outcome = authClientNoAuth
	legacy := gapResolution(authClientReasonUnmapped)
	legacy.outcome = authClientLegacy
	noAccess := gapResolution(authClientReasonNotPermitted)
	noAccess.outcome = authClientNoAccess

	cases := []struct {
		name               string
		res                authClientResolution
		reconnect, decided bool
	}{
		{"a gap reconnects", gapResolution(authClientReasonClientDisabled), true, true},
		{"no-auth refreshes", noAuth, false, true},
		{"a static credential refreshes", resolved(authClientTypeStaticCredential), false, true},
		{"a pass-through refreshes", resolved(authClientTypePassThrough), false, true},
		{"a personal connection is left to the upstream client choice", resolved(authClientTypeRemoteSession), false, false},
		{"a choice served the old way is left to it", legacy, false, false},
		{"a refusal is not a reconnect", noAccess, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reconnect, decided := refreshDecision(tc.res)
			require.Equal(t, tc.reconnect, reconnect)
			require.Equal(t, tc.decided, decided)
		})
	}

	reconnect, decided, err := (&Service{}).refreshNeedsAuthClientReconnect(t.Context(), &ResolvedMcpEndpoint{McpServerID: validUUID(uuid.New()), OrganizationID: "org_a"}, urn.NewUserSubject("user_a"), "jti")
	require.NoError(t, err)
	require.False(t, reconnect)
	require.False(t, decided, "without the flag the upstream client choice decides")
}

// authClientFixture is one organization with a project, an OAuth registration
// and a remote MCP server, for the resolver to read.
type authClientFixture struct {
	t            *testing.T
	q            *authclientbackfill.Queries
	service      *Service
	orgID        string
	projectID    uuid.UUID
	registration uuid.UUID
	issuerID     uuid.UUID
	serverID     uuid.UUID
	remoteID     uuid.UUID
}

func newAuthClientFixture(t *testing.T, pool *pgxpool.Pool) *authClientFixture {
	t.Helper()
	ctx := t.Context()
	q := authclientbackfill.New(pool)
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	f := &authClientFixture{t: t, q: q, service: &Service{db: pool, enc: enc, logger: testenv.NewLogger(t), authClientPermitter: connectPermitter{}}, orgID: "org_" + uuid.NewString(), projectID: uuid.New(), registration: uuid.New(), issuerID: uuid.New()}
	require.NoError(t, q.SeedOrganizationFixture(ctx, f.orgID))
	require.NoError(t, q.SeedProjectFixture(ctx, authclientbackfill.SeedProjectFixtureParams{ID: f.projectID, Slug: "p-" + f.projectID.String()[:8], OrganizationID: f.orgID}))
	require.NoError(t, q.SeedRemoteSessionIssuerFixture(ctx, authclientbackfill.SeedRemoteSessionIssuerFixtureParams{ID: f.issuerID, ProjectID: validUUID(f.projectID), Slug: "rsi-" + f.issuerID.String()[:8], Issuer: "https://auth.example.com/" + f.issuerID.String()[:8], Name: pgtype.Text{String: "Example", Valid: true}}))
	require.NoError(t, q.SeedRemoteSessionClientFixture(ctx, authclientbackfill.SeedRemoteSessionClientFixtureParams{ID: f.registration, ProjectID: validUUID(f.projectID), OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, RemoteSessionIssuerID: f.issuerID, ClientID: "cid-" + f.registration.String()[:8], CredentialOwner: "subject", TokenEndpointAuthMethod: pgtype.Text{}}))
	f.serverID, f.remoteID = f.server()
	return f
}

func (f *authClientFixture) server() (uuid.UUID, uuid.UUID) {
	f.t.Helper()
	id, remoteID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(f.t.Context(), authclientbackfill.SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: f.projectID, Url: "https://mcp.example.com/" + remoteID.String()[:8]}))
	require.NoError(f.t, f.q.SeedMCPServerFixture(f.t.Context(), authclientbackfill.SeedMCPServerFixtureParams{ID: id, ProjectID: f.projectID, Slug: pgtype.Text{String: "s-" + id.String()[:8], Valid: true}, RemoteMcpServerID: validUUID(remoteID)}))
	return id, remoteID
}

// tunneled replaces the fixture's server with one reached through a tunnel.
func (f *authClientFixture) tunneled() {
	f.t.Helper()
	sourceID, id := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedTunneledSourceFixture(f.t.Context(), authclientbackfill.SeedTunneledSourceFixtureParams{ID: sourceID, ProjectID: f.projectID, Name: "t-" + sourceID.String()[:8], KeyHash: "h-" + sourceID.String(), ResourceIdentifier: pgtype.Text{}}))
	require.NoError(f.t, f.q.SeedTunneledMCPServerFixture(f.t.Context(), authclientbackfill.SeedTunneledMCPServerFixtureParams{ID: id, ProjectID: f.projectID, Slug: pgtype.Text{String: "t-" + id.String()[:8], Valid: true}, TunneledMcpServerID: validUUID(sourceID)}))
	f.serverID, f.remoteID = id, uuid.Nil
}

// client creates an auth client; registration is set for OAuth-backed types.
func (f *authClientFixture) client(clientType string, registration uuid.NullUUID, linked bool) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedAuthClientFixture(f.t.Context(), authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: f.orgID, ProjectID: validUUID(f.projectID), ClientType: clientType, Source: "admin", RemoteSessionClientID: registration})
	require.NoError(f.t, err)
	if linked {
		require.NoError(f.t, f.q.SeedServerAuthClientLinkFixture(f.t.Context(), authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: f.serverID, AuthClientID: id}))
	}
	return id
}

// attach associates the fixture's registration with its server the legacy
// way, as admin attaches still record it.
func (f *authClientFixture) attach() {
	f.t.Helper()
	_, err := f.q.SeedAssociationFixture(f.t.Context(), authclientbackfill.SeedAssociationFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: f.serverID, RemoteSessionClientID: f.registration})
	require.NoError(f.t, err)
}

// oauthClient creates a linked client on the attached registration.
func (f *authClientFixture) oauthClient(clientType string) uuid.UUID {
	f.t.Helper()
	f.attach()
	return f.client(clientType, validUUID(f.registration), true)
}

// sourceHeader stores a header on the server's remote source.
func (f *authClientFixture) sourceHeader(name string, secret bool, value, forwarded pgtype.Text) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedRemoteHeaderFixture(f.t.Context(), authclientbackfill.SeedRemoteHeaderFixtureParams{RemoteMcpServerID: f.remoteID, Name: name, IsSecret: secret, Value: value, ValueFromRequestHeader: forwarded})
	require.NoError(f.t, err)
	return id
}

// copyHeader stores a header copy on a client.
func (f *authClientFixture) requireSourceHeader(id uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, f.q.SetRemoteHeaderRequiredFixture(f.t.Context(), authclientbackfill.SetRemoteHeaderRequiredFixtureParams{IsRequired: true, ID: id}))
}

func (f *authClientFixture) copyHeader(clientID uuid.UUID, name string, required, secret bool, value, forwarded pgtype.Text) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedAuthClientHeaderFixture(f.t.Context(), authclientbackfill.SeedAuthClientHeaderFixtureParams{AuthClientID: clientID, OrganizationID: f.orgID, Name: name, IsRequired: required, IsSecret: secret, Value: value, ValueFromRequestHeader: forwarded}))
}

// choose records a credential's choice for the fixture's server.
func (f *authClientFixture) choose(clientID uuid.NullUUID) *upstreamSelector {
	f.t.Helper()
	credentialID := uuid.New()
	require.NoError(f.t, f.q.SeedUpstreamSelectionFixture(f.t.Context(), authclientbackfill.SeedUpstreamSelectionFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, CredentialID: credentialID, McpServerID: f.serverID, NoAuth: !clientID.Valid, AuthClientID: clientID}))
	return &upstreamSelector{service: f.service, organizationID: f.orgID, userSessionIssuerID: uuid.Nil, credentialID: credentialID}
}

func (f *authClientFixture) resolve(selector *upstreamSelector) authClientResolution {
	f.t.Helper()
	res, err := selector.resolveAuthClient(f.t.Context(), f.serverID)
	require.NoError(f.t, err)
	return res
}

func (f *authClientFixture) serve(selector *upstreamSelector) authClientResolution {
	f.t.Helper()
	res, err := f.service.resolveAuthClientForServing(f.t.Context(), selector, f.serverID, f.projectID, urn.NewUserSubject("user_1"))
	require.NoError(f.t, err)
	return res
}

func text(v string) pgtype.Text { return pgtype.Text{String: v, Valid: true} }

type denyPermitter struct{}

func (denyPermitter) Permitted(context.Context, authClientPermitRequest) (bool, error) {
	return false, nil
}

// recordingPermitter records what it was asked.
type recordingPermitter struct{ asked []authClientPermitRequest }

func (p *recordingPermitter) Permitted(_ context.Context, req authClientPermitRequest) (bool, error) {
	p.asked = append(p.asked, req)
	return true, nil
}

func TestResolveAuthClientReadsTheRecordedChoice(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_resolve")
	require.NoError(t, err)
	ctx := t.Context()

	t.Run("personal connection routes by its registration", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.oauthClient(authClientTypeRemoteSession)
		res := f.resolve(f.choose(validUUID(id)))
		require.Equal(t, authClientResolved, res.outcome)
		require.Equal(t, authClientTypeRemoteSession, res.clientType)
		require.Equal(t, validUUID(f.registration), res.choice.clientID)
	})

	t.Run("a registration an admin detached is a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypeServiceAccount, validUUID(f.registration), true)
		res := f.resolve(f.choose(validUUID(id)))
		require.Equal(t, authClientGap, res.outcome)
		require.Equal(t, authClientReasonDetached, res.reason)
	})

	t.Run("a missing choice is a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		res := f.resolve(&upstreamSelector{service: f.service, organizationID: f.orgID, userSessionIssuerID: uuid.Nil, credentialID: uuid.New()})
		require.Equal(t, authClientGap, res.outcome)
		require.Equal(t, authClientReasonMissingChoice, res.reason)
	})

	t.Run("a disabled client is a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.oauthClient(authClientTypeRemoteSession)
		require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: id}))
		res := f.resolve(f.choose(validUUID(id)))
		require.Equal(t, authClientReasonClientDisabled, res.reason)
	})

	t.Run("a soft-deleted client is a gap, never no-auth", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		selector := f.choose(validUUID(id))
		require.NoError(t, f.q.SoftDeleteAuthClientFixture(ctx, id))
		res := f.resolve(selector)
		require.Equal(t, authClientGap, res.outcome)
		require.Equal(t, authClientReasonClientDeleted, res.reason)
	})

	t.Run("a client not associated with the server is a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.attach()
		id := f.client(authClientTypeRemoteSession, validUUID(f.registration), false)
		res := f.resolve(f.choose(validUUID(id)))
		require.Equal(t, authClientReasonUnassociated, res.reason)
	})

	t.Run("a deleted registration is a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.oauthClient(authClientTypeRemoteSession)
		require.NoError(t, f.q.SoftDeleteRemoteSessionClientFixture(ctx, f.registration))
		res := f.resolve(f.choose(validUUID(id)))
		require.Equal(t, authClientReasonRegistration, res.reason)
	})

	t.Run("no-auth follows requires_upstream_auth", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		require.Equal(t, authClientNoAuth, f.resolve(f.choose(uuid.NullUUID{})).outcome, "undecided with no clients")
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}, ID: f.serverID}))
		require.Equal(t, authClientReasonNoAuthForbidden, f.resolve(f.choose(uuid.NullUUID{})).reason)
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}, ID: f.serverID}))
		f.oauthClient(authClientTypeRemoteSession)
		require.Equal(t, authClientNoAuth, f.resolve(f.choose(uuid.NullUUID{})).outcome, "explicitly none even with a client")
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{}, ID: f.serverID}))
		require.Equal(t, authClientReasonNoAuthStale, f.resolve(f.choose(uuid.NullUUID{})).reason, "undecided once a client exists")
	})

	t.Run("a legacy attach alone makes a no-auth choice stale", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.attach()
		require.Equal(t, authClientReasonNoAuthStale, f.resolve(f.choose(uuid.NullUUID{})).reason)
	})

	t.Run("a disabled client still makes a no-auth choice stale", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypeServiceAccount, validUUID(f.registration), true)
		require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: id}))
		require.Equal(t, authClientReasonNoAuthStale, f.resolve(f.choose(uuid.NullUUID{})).reason)
	})

	t.Run("a no-auth choice on a server with only header clients is served the old way", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.sourceHeader("Authorization", true, text("sealed-v1"), pgtype.Text{})
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.copyHeader(id, "Authorization", true, true, text("sealed-v1"), pgtype.Text{})
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}, ID: f.serverID}))
		selector := f.choose(uuid.NullUUID{})

		res := f.serve(selector)
		require.Equal(t, authClientLegacy, res.outcome, "enforced hands it to the upstream client choice")
		require.Equal(t, authClientReasonUnmapped, res.reason)
		choice, choiceErr := selector.forServer(ctx, f.serverID)
		require.NoError(t, choiceErr)
		require.True(t, choice.noAuth(), "which serves it with the server's headers, as before")

		var buf bytes.Buffer
		f.service.logger = slog.New(slog.NewJSONHandler(&buf, nil))
		f.service.shadowAuthClient(ctx, selector, f.serverID, f.projectID, urn.NewUserSubject("user_1"), choice, choiceErr)
		require.Empty(t, buf.String(), "shadow reports no mismatch")
	})

	t.Run("a no-auth choice on a server with an OAuth client stays a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.oauthClient(authClientTypeRemoteSession)
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}, ID: f.serverID}))
		res := f.resolve(f.choose(uuid.NullUUID{}))
		require.Equal(t, authClientGap, res.outcome)
		require.Equal(t, authClientReasonNoAuthForbidden, res.reason)
	})

	t.Run("a legacy attach keeps a no-auth choice on a header-client server a gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
		f.attach()
		res := f.resolve(f.choose(uuid.NullUUID{}))
		require.Equal(t, authClientGap, res.outcome)
		require.Equal(t, authClientReasonNoAuthStale, res.reason)
	})

	t.Run("no-auth on a server with credential headers is a setup gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.sourceHeader("X-Api-Key", true, text("sealed-key"), pgtype.Text{})
		require.Equal(t, authClientReasonSetup, f.resolve(f.choose(uuid.NullUUID{})).reason)
	})

	t.Run("no-auth on a server whose only credential header is Authorization is a setup gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.sourceHeader("Authorization", false, text("Bearer plain"), pgtype.Text{})
		require.Equal(t, authClientReasonSetup, f.resolve(f.choose(uuid.NullUUID{})).reason)
	})

	t.Run("static copies matching the server's headers resolve", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.requireSourceHeader(f.sourceHeader("Authorization", true, text("sealed-v1"), pgtype.Text{}))
		f.sourceHeader("X-Api-Version", false, text("2"), pgtype.Text{})
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.copyHeader(id, "Authorization", true, true, text("sealed-v1"), pgtype.Text{})
		require.Equal(t, authClientResolved, f.resolve(f.choose(validUUID(id))).outcome)
	})

	t.Run("a server header made required leaves the copy stale", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		headerID := f.sourceHeader("X-Upstream", false, pgtype.Text{}, text("X-Caller"))
		id := f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
		f.copyHeader(id, "X-Upstream", false, false, pgtype.Text{}, text("X-Caller"))
		selector := f.choose(validUUID(id))
		require.Equal(t, authClientResolved, f.resolve(selector).outcome)
		f.requireSourceHeader(headerID)
		require.Equal(t, authClientReasonHeadersStale, f.resolve(selector).reason)
	})

	t.Run("a server header made secret leaves the copy stale", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		headerID := f.sourceHeader("X-Api-Version", false, text("2"), pgtype.Text{})
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.copyHeader(id, "X-Api-Version", false, false, text("2"), pgtype.Text{})
		selector := f.choose(validUUID(id))
		require.Equal(t, authClientResolved, f.resolve(selector).outcome)
		require.NoError(t, f.q.UpdateRemoteHeaderFixture(ctx, authclientbackfill.UpdateRemoteHeaderFixtureParams{Name: "X-Api-Version", Value: text("2"), ValueFromRequestHeader: pgtype.Text{}, IsSecret: true, ID: headerID}))
		require.Equal(t, authClientReasonHeadersStale, f.resolve(selector).reason)
	})

	t.Run("a rotated server header leaves the copy stale", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		headerID := f.sourceHeader("Authorization", true, text("sealed-v1"), pgtype.Text{})
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.copyHeader(id, "Authorization", true, true, text("sealed-v1"), pgtype.Text{})
		selector := f.choose(validUUID(id))
		require.NoError(t, f.q.UpdateRemoteHeaderFixture(ctx, authclientbackfill.UpdateRemoteHeaderFixtureParams{Name: "Authorization", Value: text("sealed-v2"), ValueFromRequestHeader: pgtype.Text{}, IsSecret: true, ID: headerID}))
		res := f.resolve(selector)
		require.Equal(t, authClientLegacy, res.outcome)
		require.Equal(t, authClientReasonHeadersStale, res.reason)
	})

	t.Run("an OAuth client never sends a stale auxiliary credential", func(t *testing.T) {
		t.Parallel()
		for _, change := range []string{"rotated", "removed", "remapped"} {
			t.Run(change, func(t *testing.T) {
				t.Parallel()
				f := newAuthClientFixture(t, pool)
				headerID := f.sourceHeader("X-Api-Key", true, text("sealed-v1"), pgtype.Text{})
				id := f.oauthClient(authClientTypeServiceAccount)
				f.copyHeader(id, "X-Api-Key", false, true, text("sealed-v1"), pgtype.Text{})
				selector := f.choose(validUUID(id))
				require.Equal(t, authClientResolved, f.resolve(selector).outcome)
				switch change {
				case "rotated":
					require.NoError(t, f.q.UpdateRemoteHeaderFixture(ctx, authclientbackfill.UpdateRemoteHeaderFixtureParams{Name: "X-Api-Key", Value: text("sealed-v2"), ValueFromRequestHeader: pgtype.Text{}, IsSecret: true, ID: headerID}))
				case "removed":
					require.NoError(t, f.q.SoftDeleteRemoteHeaderFixture(ctx, headerID))
				case "remapped":
					require.NoError(t, f.q.UpdateRemoteHeaderFixture(ctx, authclientbackfill.UpdateRemoteHeaderFixtureParams{Name: "X-Api-Key", Value: pgtype.Text{}, ValueFromRequestHeader: text("X-Caller-Key"), IsSecret: true, ID: headerID}))
				}
				res := f.resolve(selector)
				require.Equal(t, authClientLegacy, res.outcome)
				require.Equal(t, authClientReasonHeadersStale, res.reason)
				require.Empty(t, res.headers, "the stale copy is never sent")
			})
		}
	})

	t.Run("a deleted server header leaves the copy stale", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		headerID := f.sourceHeader("X-Upstream", false, pgtype.Text{}, text("X-Caller"))
		id := f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
		f.copyHeader(id, "X-Upstream", false, false, pgtype.Text{}, text("X-Caller"))
		selector := f.choose(validUUID(id))
		require.Equal(t, authClientResolved, f.resolve(selector).outcome)
		require.NoError(t, f.q.SoftDeleteRemoteHeaderFixture(ctx, headerID))
		require.Equal(t, authClientReasonHeadersStale, f.resolve(selector).reason)
	})

	t.Run("a server credential header the client does not carry is a setup gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.requireSourceHeader(f.sourceHeader("Authorization", true, text("sealed-v1"), pgtype.Text{}))
		f.sourceHeader("X-Api-Key", true, text("sealed-key"), pgtype.Text{})
		static := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.copyHeader(static, "Authorization", true, true, text("sealed-v1"), pgtype.Text{})
		require.Equal(t, authClientReasonSetup, f.resolve(f.choose(validUUID(static))).reason)

		oauth := f.oauthClient(authClientTypeRemoteSession)
		require.Equal(t, authClientReasonSetup, f.resolve(f.choose(validUUID(oauth))).reason, "the bearer replaces Authorization, not X-Api-Key")
		f.copyHeader(oauth, "X-Api-Key", false, true, text("sealed-key"), pgtype.Text{})
		require.Equal(t, authClientResolved, f.resolve(f.choose(validUUID(oauth))).outcome)
	})

	t.Run("a personal connection behind a ready EMA client may chain", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.oauthClient(authClientTypeRemoteSession)
		ema := f.client(authClientTypeEMA, validUUID(f.registration), true)
		require.NoError(t, f.q.SeedAuthClientEMAFixture(ctx, authclientbackfill.SeedAuthClientEMAFixtureParams{Resource: "https://mcp.example.com", AuthClientID: ema}))
		res := f.resolve(f.choose(validUUID(id)))
		require.Equal(t, authClientLegacy, res.outcome)
		require.Equal(t, authClientReasonLegacyMayChain, res.reason)

		require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: ema}))
		require.Equal(t, authClientResolved, f.resolve(f.choose(validUUID(id))).outcome, "a disabled EMA client chains nothing")
	})

	t.Run("headers-only clients on a tunneled server are served the old way", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.tunneled()
		for _, clientType := range []string{authClientTypeStaticCredential, authClientTypePassThrough} {
			id := f.client(clientType, uuid.NullUUID{}, true)
			res := f.resolve(f.choose(validUUID(id)))
			require.Equal(t, authClientLegacy, res.outcome, clientType)
			require.Equal(t, authClientReasonTunneledHeaders, res.reason, clientType)
		}
	})

	t.Run("an OAuth client on a tunneled server is sent no client headers", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.tunneled()
		id := f.oauthClient(authClientTypeRemoteSession)
		f.copyHeader(id, "X-Tenant", false, false, text("acme"), pgtype.Text{})
		res := f.serve(f.choose(validUUID(id)))
		require.Equal(t, authClientResolved, res.outcome)
		require.True(t, res.tunneled)
		require.Empty(t, res.headers)
	})

	t.Run("another tenant's selection is not read", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		other := newAuthClientFixture(t, pool)
		id := f.oauthClient(authClientTypeRemoteSession)
		selector := f.choose(validUUID(id))
		selector.organizationID = other.orgID
		res := f.resolve(selector)
		require.Equal(t, authClientReasonMissingChoice, res.reason)
	})

	t.Run("a refused permission is no access, not a reconnect", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.service.authClientPermitter = denyPermitter{}
		id := f.oauthClient(authClientTypeRemoteSession)
		res := f.serve(f.choose(validUUID(id)))
		require.Equal(t, authClientNoAccess, res.outcome)
		require.Equal(t, authClientReasonNotPermitted, res.reason)
		require.Empty(t, res.headers)
	})

	t.Run("the permitter is asked about the credential and the user it represents", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		permitter := &recordingPermitter{asked: nil}
		f.service.authClientPermitter = permitter
		id := f.oauthClient(authClientTypeServiceAccount)
		selector := f.choose(validUUID(id))
		f.serve(selector)
		require.Equal(t, []authClientPermitRequest{{
			OrganizationID: f.orgID,
			ServerID:       f.serverID,
			ClientID:       id,
			ClientType:     authClientTypeServiceAccount,
			RegistrationID: validUUID(f.registration),
			CredentialKind: "user_session",
			CredentialID:   selector.credentialID,
			Subject:        urn.NewUserSubject("user_1"),
		}}, permitter.asked)
	})

	t.Run("shadow never asks the permitter", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		permitter := &recordingPermitter{asked: nil}
		f.service.authClientPermitter = permitter
		id := f.oauthClient(authClientTypeRemoteSession)
		selector := f.choose(validUUID(id))
		f.service.shadowAuthClient(ctx, selector, f.serverID, f.projectID, urn.NewUserSubject("user_1"), upstreamChoice{clientID: validUUID(f.registration), remoteSessionIssuerID: validUUID(f.issuerID)}, nil)
		require.Empty(t, permitter.asked)
	})

	for _, destination := range []string{"Authorization", "X-Token"} {
		t.Run("an optional copy reading the inbound Authorization into "+destination+" is not sent", func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			f.sourceHeader(destination, false, pgtype.Text{}, text("Authorization"))
			f.sourceHeader("X-Upstream", false, pgtype.Text{}, text("X-Caller"))
			id := f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
			f.copyHeader(id, destination, false, false, pgtype.Text{}, text("Authorization"))
			f.copyHeader(id, "X-Upstream", false, false, pgtype.Text{}, text("X-Caller"))
			res := f.serve(f.choose(validUUID(id)))
			require.Equal(t, authClientResolved, res.outcome)
			require.Equal(t, []proxy.ConfiguredHeader{{IsRequired: false, Name: "X-Upstream", StaticValue: "", ValueFromRequestHeader: "X-Caller"}}, res.headers)
		})

		t.Run("a required copy reading the inbound Authorization into "+destination+" is a setup gap", func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			f.requireSourceHeader(f.sourceHeader(destination, false, pgtype.Text{}, text("authorization")))
			id := f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
			f.copyHeader(id, destination, true, false, pgtype.Text{}, text("authorization"))
			res := f.serve(f.choose(validUUID(id)))
			require.Equal(t, authClientGap, res.outcome)
			require.Equal(t, authClientReasonSetup, res.reason)
			require.Empty(t, res.headers)
		})
	}

	t.Run("a headers-only client left with no headers is a setup gap", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.sourceHeader("X-Token", false, pgtype.Text{}, text("Authorization"))
		id := f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
		f.copyHeader(id, "X-Token", false, false, pgtype.Text{}, text("Authorization"))
		res := f.serve(f.choose(validUUID(id)))
		require.Equal(t, authClientGap, res.outcome, "nothing left to send would call the upstream anonymously")
		require.Equal(t, authClientReasonSetup, res.reason)

		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}, ID: f.serverID}))
		require.Equal(t, authClientResolved, f.serve(f.choose(validUUID(id))).outcome, "a server needing no auth may be called without client headers")
	})

	t.Run("client headers are revealed for the proxy", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		sealed, err := f.service.enc.Encrypt([]byte("Bearer static-secret"))
		require.NoError(t, err)
		f.copyHeader(id, "Authorization", true, true, text(sealed), pgtype.Text{})
		f.copyHeader(id, "X-Tenant", false, false, pgtype.Text{}, text("X-Caller-Tenant"))
		headers, err := f.service.loadAuthClientHeaders(ctx, f.orgID, id)
		require.NoError(t, err)
		require.Len(t, headers, 2)
		require.Equal(t, "Authorization", headers[0].Name)
		require.Equal(t, "Bearer static-secret", headers[0].StaticValue)
		require.True(t, headers[0].IsRequired)
		require.Equal(t, "X-Caller-Tenant", headers[1].ValueFromRequestHeader)

		other, err := f.service.loadAuthClientHeaders(ctx, "org_"+uuid.NewString(), id)
		require.NoError(t, err)
		require.Empty(t, other, "headers are read only within the client's organization")
	})
}

func TestAnnotateAuthClientRecordsIDsOnly(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	ctx, span := tracer.Start(t.Context(), "request")
	clientID := uuid.New()
	res := gapResolution("")
	res.outcome = authClientResolved
	res.clientID = clientID
	res.clientType = authClientTypeStaticCredential
	res.headers = nil
	annotateAuthClient(ctx, testenv.NewLogger(t), res)
	span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	got := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	require.Equal(t, map[string]string{
		"gram.auth_client.id":      clientID.String(),
		"gram.auth_client.type":    authClientTypeStaticCredential,
		"gram.auth_client.outcome": string(authClientResolved),
	}, got)
}

// parityPoints reads the mcp.auth_client.parity counter back by attribute set.
func parityPoints(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "mcp.auth_client.parity" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}
	return points
}

func parityKey(agreed bool, outcome, legacy authClientOutcome, reason string) attribute.Set {
	return attribute.NewSet(attr.AuthClientParityAgreed(agreed), attr.AuthClientOutcome(string(outcome)), attr.AuthClientLegacyOutcome(string(legacy)), attr.AuthClientReason(reason))
}

func TestReportAuthClientParity(t *testing.T) {
	t.Parallel()

	registration, other := uuid.New(), uuid.New()
	resolved := func(clientType string, clientID uuid.UUID) authClientResolution {
		res := gapResolution("")
		res.outcome = authClientResolved
		res.clientID = uuid.New()
		res.clientType = clientType
		if clientID != uuid.Nil {
			res.choice = upstreamChoice{clientID: validUUID(clientID), remoteSessionIssuerID: validUUID(uuid.New())}
		}
		return res
	}
	legacy := func(reason string) authClientResolution {
		res := gapResolution(reason)
		res.outcome = authClientLegacy
		return res
	}
	oauthChoice := upstreamChoice{clientID: validUUID(registration), remoteSessionIssuerID: validUUID(uuid.New())}
	var noAuthChoice upstreamChoice

	cases := []struct {
		name      string
		res       authClientResolution
		choice    upstreamChoice
		choiceErr error
		warn      bool
		counted   *attribute.Set
	}{
		{"same client agrees", resolved(authClientTypeRemoteSession, registration), oauthChoice, nil, false, new(parityKey(true, authClientResolved, authClientResolved, authClientReasonNone))},
		{"different client disagrees", resolved(authClientTypeRemoteSession, other), oauthChoice, nil, true, new(parityKey(false, authClientResolved, authClientResolved, authClientReasonNone))},
		{"a headers-only client agrees with no-auth", resolved(authClientTypeStaticCredential, uuid.Nil), noAuthChoice, nil, false, new(parityKey(true, authClientResolved, authClientNoAuth, authClientReasonNone))},
		{"a headers-only client disagrees with an unusable choice", resolved(authClientTypePassThrough, uuid.Nil), noAuthChoice, errUpstreamSelectionUnusable, true, new(parityKey(false, authClientResolved, authClientGap, authClientReasonNone))},
		{"gap against a usable choice disagrees", gapResolution(authClientReasonClientDisabled), oauthChoice, nil, true, new(parityKey(false, authClientGap, authClientResolved, authClientReasonClientDisabled))},
		{"a setup gap against a usable choice disagrees", gapResolution(authClientReasonSetup), noAuthChoice, nil, true, new(parityKey(false, authClientGap, authClientNoAuth, authClientReasonSetup))},
		{"gap against an unusable choice agrees", gapResolution(authClientReasonClientDisabled), oauthChoice, errUpstreamSelectionUnusable, false, new(parityKey(true, authClientGap, authClientGap, authClientReasonClientDisabled))},
		{"an unmapped choice is counted without a warning", legacy(authClientReasonUnmapped), oauthChoice, nil, false, new(parityKey(false, authClientLegacy, authClientResolved, authClientReasonUnmapped))},
		{"stale header copies are a mismatch", legacy(authClientReasonHeadersStale), noAuthChoice, nil, true, new(parityKey(false, authClientLegacy, authClientNoAuth, authClientReasonHeadersStale))},
		{"a choice that may chain is a mismatch", legacy(authClientReasonLegacyMayChain), oauthChoice, nil, true, new(parityKey(false, authClientLegacy, authClientResolved, authClientReasonLegacyMayChain))},
		{"EMA is not compared", legacy(authClientReasonEMA), oauthChoice, nil, false, nil},
		{"tunneled headers are not compared", legacy(authClientReasonTunneledHeaders), noAuthChoice, nil, false, nil},
		{"a failed legacy read is not compared", resolved(authClientTypeRemoteSession, registration), oauthChoice, errors.New("connection reset"), false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			reader := sdkmetric.NewManualReader()
			metrics := mcpmetrics.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))
			s := &Service{logger: slog.New(slog.NewJSONHandler(&buf, nil)), metrics: metrics}
			s.reportAuthClientParity(t.Context(), "org_1", uuid.New(), tc.res, tc.choice, tc.choiceErr)
			if tc.warn {
				require.Contains(t, buf.String(), "auth client resolver parity mismatch")
			} else {
				require.Empty(t, buf.String())
			}
			points := parityPoints(t, reader)
			if tc.counted == nil {
				require.Empty(t, points)
				return
			}
			require.Equal(t, map[attribute.Set]int64{*tc.counted: 1}, points)
		})
	}
}

func TestReportAuthClientParityAnnotatesTheSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	ctx, span := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test").Start(t.Context(), "request")
	s := &Service{logger: testenv.NewLogger(t)}
	s.reportAuthClientParity(ctx, "org_1", uuid.New(), gapResolution(authClientReasonClientDisabled), upstreamChoice{}, nil)
	span.End()

	got := map[string]string{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	require.Equal(t, string(authClientNoAuth), got["gram.auth_client.legacy_outcome"])
	require.Equal(t, "false", got["gram.auth_client.parity_agreed"])
}

func TestShadowAuthClientCountsResolverFailures(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_shadow")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	reader := sdkmetric.NewManualReader()
	f.service.metrics = mcpmetrics.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))
	selector := f.choose(uuid.NullUUID{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f.service.shadowAuthClient(ctx, selector, f.serverID, f.projectID, urn.NewUserSubject("user_1"), upstreamChoice{}, nil)
	require.Equal(t, map[attribute.Set]int64{
		attribute.NewSet(attr.AuthClientParityAgreed(false), attr.AuthClientOutcome(authClientParityError), attr.AuthClientLegacyOutcome(string(authClientNoAuth)), attr.AuthClientReason(authClientReasonNone)): 1,
	}, parityPoints(t, reader))
}

func TestAuthClientModeIsOffWithoutFlags(t *testing.T) {
	t.Parallel()

	require.Equal(t, feature.AuthClientResolverOff, (&Service{}).authClientMode(t.Context(), "org_a"), "no flag provider keeps the upstream client choice")
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagAuthClientResolverShadow, "org_a", true)
	require.Equal(t, feature.AuthClientResolverShadow, (&Service{features: flags}).authClientMode(t.Context(), "org_a"))
	require.Equal(t, feature.AuthClientResolverOff, (&Service{features: flags}).authClientMode(t.Context(), "org_b"))
}

func TestUpstreamClientChoiceServesMappedHeadersOnlyClients(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_legacy")
	require.NoError(t, err)

	for _, clientType := range []string{authClientTypeStaticCredential, authClientTypePassThrough} {
		t.Run(clientType+" serves as no-auth with the flag off", func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			before, err := f.choose(uuid.NullUUID{}).forServer(t.Context(), f.serverID)
			require.NoError(t, err)
			id := f.client(clientType, uuid.NullUUID{}, true)
			after, err := f.choose(validUUID(id)).forServer(t.Context(), f.serverID)
			require.NoError(t, err)
			require.True(t, after.noAuth())
			require.Equal(t, before, after, "a mapped choice serves exactly as the unmapped no-auth choice did")
		})

		t.Run(clientType+" agrees in shadow", func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			id := f.client(clientType, uuid.NullUUID{}, true)
			selector := f.choose(validUUID(id))
			choice, choiceErr := selector.forServer(t.Context(), f.serverID)
			var buf bytes.Buffer
			f.service.logger = slog.New(slog.NewJSONHandler(&buf, nil))
			f.service.shadowAuthClient(t.Context(), selector, f.serverID, f.projectID, urn.NewUserSubject("user_1"), choice, choiceErr)
			require.Empty(t, buf.String())
		})
	}

	t.Run("a disabled headers-only client serves as before mapping", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: id}))
		choice, err := f.choose(validUUID(id)).forServer(t.Context(), f.serverID)
		require.NoError(t, err)
		require.True(t, choice.noAuth())
	})

	t.Run("a soft-deleted headers-only client serves as before mapping", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypePassThrough, uuid.NullUUID{}, true)
		selector := f.choose(validUUID(id))
		require.NoError(t, f.q.SoftDeleteAuthClientFixture(t.Context(), id))
		choice, err := selector.forServer(t.Context(), f.serverID)
		require.NoError(t, err)
		require.True(t, choice.noAuth())
	})

	t.Run("a headers-only choice on a server given an OAuth client stays unusable", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.attach()
		_, err := f.choose(validUUID(id)).forServer(t.Context(), f.serverID)
		require.ErrorIs(t, err, errUpstreamSelectionUnusable)
	})

	t.Run("an OAuth client without its registration on the choice stays unusable", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		id := f.oauthClient(authClientTypeRemoteSession)
		_, err := f.choose(validUUID(id)).forServer(t.Context(), f.serverID)
		require.ErrorIs(t, err, errUpstreamSelectionUnusable)
	})
}

func TestAnnotateAuthClientLogsAtDebug(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	res := gapResolution("")
	res.outcome = authClientResolved
	res.clientID = clientID
	res.clientType = authClientTypePassThrough
	var buf bytes.Buffer
	annotateAuthClient(t.Context(), slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), res)
	require.Contains(t, buf.String(), `"msg":"auth client resolved"`)
	require.Contains(t, buf.String(), clientID.String())
	require.Contains(t, buf.String(), authClientTypePassThrough)

	buf.Reset()
	annotateAuthClient(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)), res)
	require.Empty(t, buf.String(), "silent above debug")
}

// Under enforcement the resolver's registration serves the caller's own
// session whichever sign-in it was consented through; the upstream client
// choice still needs the sign-in link.
func TestResolveAccessTokensServesAnAuthClientWithoutTheSignInLink(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_tokens")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	ctx := t.Context()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})

	consentedUnder, serverSignIn := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{consentedUnder, serverSignIn} {
		require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: id, ProjectID: validUUID(f.projectID), Slug: "usi-" + id.String()[:8]}))
	}
	subject := urn.NewUserSubject("user_tokens")
	accessEnc, err := f.service.enc.Encrypt([]byte("upstream-token"))
	require.NoError(t, err)
	_, err = remotesessionsrepo.New(pool).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   consentedUnder,
		RemoteSessionClientID: f.registration,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(t, err)

	resolved := authClientResolution{outcome: authClientResolved, clientType: authClientTypeRemoteSession}
	authentication := &issuerGateAuthentication{
		endpoint:     &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: serverSignIn, McpServerID: validUUID(f.serverID)},
		subject:      subject,
		chosenClient: validUUID(f.registration),
		authClient:   &resolved,
	}
	tokens, err := f.service.resolveAccessTokens(ctx, authentication, false)
	require.NoError(t, err)
	require.Equal(t, "upstream-token", tokens[f.issuerID].Token)

	authentication.authClient = nil
	_, err = f.service.resolveAccessTokens(ctx, authentication, false)
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)
}

// Under enforcement the resolved registration's stored grant decides a
// refresh, whichever sign-in it was consented through; a failed check is
// undecided, and the refresh then fails open.
func TestRefreshNeedsAuthClientReconnectUnderEnforcement(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_refresh")
	require.NoError(t, err)
	redisClient, err := TestInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	ctx := t.Context()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagAuthClientResolver, f.orgID, true)
	f.service.features = flags
	f.service.sessionPolicyCache = cache.NewTypedObjectCache[sessionPolicyEntry](testenv.NewLogger(t), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone)

	consentedUnder, serverSignIn := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{consentedUnder, serverSignIn} {
		require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: id, ProjectID: validUUID(f.projectID), Slug: "usi-" + id.String()[:8]}))
	}
	selector := f.choose(validUUID(f.oauthClient(authClientTypeRemoteSession)))
	require.NoError(t, f.service.sessionPolicyCache.Store(ctx, sessionPolicyEntry{IssuerID: serverSignIn.String(), JTI: "jti", Selection: nil, Refreshable: true, CredentialID: selector.credentialID}))
	endpoint := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: serverSignIn, McpServerID: validUUID(f.serverID)}

	subject := urn.NewUserSubject("user_refresh")
	reconnect, decided, err := f.service.refreshNeedsAuthClientReconnect(ctx, endpoint, subject, "jti")
	require.NoError(t, err)
	require.True(t, decided)
	require.True(t, reconnect, "no stored grant needs a reconnect")

	accessEnc, err := f.service.enc.Encrypt([]byte("upstream-token"))
	require.NoError(t, err)
	_, err = remotesessionsrepo.New(pool).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   consentedUnder,
		RemoteSessionClientID: f.registration,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(t, err)
	reconnect, decided, err = f.service.refreshNeedsAuthClientReconnect(ctx, endpoint, subject, "jti")
	require.NoError(t, err)
	require.True(t, decided)
	require.False(t, reconnect, "a live grant from another sign-in refreshes")

	// A workload subject without workload authentication fails the check.
	workload := urn.NewWorkloadSubject(uuid.New(), "run")
	_, decided, err = f.service.refreshNeedsAuthClientReconnect(ctx, endpoint, workload, "jti")
	require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest)
	require.False(t, decided)
	require.False(t, f.service.refreshNeedsUpstreamReconnect(ctx, testenv.NewLogger(t), endpoint, workload, "jti"), "a failed check fails open")
}
