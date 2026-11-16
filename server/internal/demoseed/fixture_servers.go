package demoseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// LocalUpstreamOptions configures the local-only upstream wiring: clients
// registered with Linear and Notion (vendor_apps.go). The fixture-backed
// servers need none of it: the shared seed points them at the fixture the
// Speakeasy server hosts (seedFixtureServers).
type LocalUpstreamOptions struct {
	// ServerURL is Speakeasy's own URL (GRAM_SERVER_URL). Nil skips the
	// local upstream wiring.
	ServerURL *url.URL
	// Policy is the egress policy the seed registers its vendor clients
	// through (vendor_apps.go). Nil leaves the vendors' metadata document
	// clients, which they cannot fetch from localhost.
	Policy *guardian.Policy
	// CacheDir keeps the vendor clients the seed registered, so a reseed
	// reuses them rather than registering more. Empty registers every run.
	CacheDir string
}

// Fixed values for the servers the fixture backs.
const (
	fixtureProviderName       = "MCP Fixture OAuth"
	fixtureDCRProviderName    = "MCP Fixture OAuth (DCR)"
	fixtureEnvironmentFixedID = "0000-4000-a000-000000007008"
	fixtureAPIEnvVar          = "ACME_SERVER_URL"
	fixtureAccessTokenTTL     = time.Hour
	demoOAuthResource         = "https://mcp-fixture.example.com/mcp/oauth"
)

// seedFixtureServers points the seeded servers the MCP fixture backs at the
// fixture the Speakeasy server hosts (fixtureauth.BaseURL of the server URL),
// in every organization the seed writes, so they work right after seeding:
//
//   - the generated toolset servers (OpenAPI Server (API Key),
//     Server-Specific Sign-In, External OAuth Server) call the fixture's
//     stub of the demo API, through a system environment, and the External
//     OAuth Server's authorization server is the fixture's;
//   - the Per-User OAuth Server: its URL, MCP Fixture OAuth provider,
//     protected resource and client ID metadata document client move from
//     the example.com host the servers phase writes to the fixture, and
//     every live seeded sign-in holds real fixture tokens;
//   - the Service Account Server: the fixture's shared-token endpoint, with
//     the checked-in shared token as its static Authorization header;
//   - the Pass-Through Token Server: the fixture's own-token endpoint, with
//     the caller's X-Upstream-Token header forwarded;
//
// The Auto-Configured OAuth Server is set up with the other servers
// (seedFixtureAutoConfigured), since sign-ins choose its client. This runs
// after every phase that writes sessions, so it sees them all.
func (s *seeder) seedFixtureServers(ctx context.Context) error {
	q := s.q
	org := s.orgID()
	proj := s.projectID()
	base := fixtureauth.BaseURL(s.serverURL)
	serverURL := strings.TrimRight(s.serverURL.String(), "/")
	resource := fixtureauth.OAuthResource(base)

	expect := func(what string, n int64, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if n == 0 {
			return fmt.Errorf("%s matched no rows", what)
		}
		return nil
	}

	// 1. The demo API stub, as the generated servers' system environment.
	envID := uuid.MustParse(s.spec.FixedUUID(fixtureEnvironmentFixedID))
	if err := q.FixtureUpsertEnvironment(ctx, repo.FixtureUpsertEnvironmentParams{ID: envID, OrganizationID: org, ProjectID: proj}); err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	n, err := q.FixtureUpsertEnvironmentEntry(ctx, repo.FixtureUpsertEnvironmentEntryParams{EnvironmentID: envID, ProjectID: proj, Name: fixtureAPIEnvVar, Value: base + fixtureauth.PathAPI})
	if err := expect("environment entry", n, err); err != nil {
		return err
	}
	n, err = q.FixtureLinkToolsetEnvironments(ctx, repo.FixtureLinkToolsetEnvironmentsParams{
		EnvironmentID: envID, ProjectID: proj,
		ToolsetIds: []uuid.UUID{s.toolset1(), s.toolset3(), s.toolset4()},
	})
	if err := expect("link toolset environments", n, err); err != nil {
		return err
	}

	// The External OAuth Server sends callers to sign in at the fixture's
	// authorization server, as if it were the customer's own.
	metadata, err := json.Marshal(map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"scopes_supported":                      fixtureauth.DefaultScopes,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
	if err != nil {
		return fmt.Errorf("external oauth metadata: %w", err)
	}
	n, err = q.FixturePointExternalOAuth(ctx, repo.FixturePointExternalOAuthParams{Metadata: metadata, ID: s.det("external-oauth-discovery"), ProjectID: proj})
	if err := expect("point the external oauth server", n, err); err != nil {
		return err
	}

	// 2. The Per-User OAuth Server.
	n, err = q.FixturePointRemoteServer(ctx, repo.FixturePointRemoteServerParams{Url: resource, ID: s.det("remotemcp-linear"), ProjectID: proj})
	if err := expect("point the OAuth remote server", n, err); err != nil {
		return err
	}
	n, err = q.FixturePointIssuer(ctx, repo.FixturePointIssuerParams{Issuer: base, Scopes: fixtureauth.DefaultScopes, Name: fixtureProviderName, ID: s.det("remote-identity-provider-linear"), ProjectID: proj})
	if err := expect("point the OAuth provider", n, err); err != nil {
		return err
	}
	n, err = q.FixturePointProtectedResource(ctx, repo.FixturePointProtectedResourceParams{
		Resource: resource, MetadataUrl: base + "/.well-known/oauth-protected-resource" + fixtureauth.PathOAuthMCP,
		Issuer: base, Scopes: fixtureauth.DefaultScopes, ProjectID: proj, OrganizationID: org, OldResource: demoOAuthResource,
	})
	if err := expect("point the protected resource", n, err); err != nil {
		return err
	}

	// The main client publishes a client ID metadata document, which
	// Speakeasy serves and the fixture fetches.
	mainClient := s.det("remote-identity-client-linear")
	mainClientID := remotesessions.ClientMetadataDocumentURL(s.serverURL, mainClient)
	n, err = q.FixturePointMetadataDocumentClient(ctx, repo.FixturePointMetadataDocumentClientParams{
		ClientID: mainClientID, CallbackBaseUrl: serverURL, ID: mainClient, ProjectID: proj,
	})
	if err := expect("point the sign-in client", n, err); err != nil {
		return err
	}

	// Every live seeded sign-in on that client, including the agents'
	// attached connection, gets real fixture tokens. Expired ones stay
	// expired, so Needs refresh still shows; reconnecting them signs in
	// against the fixture.
	clients := []uuid.UUID{mainClient}
	sessions, err := q.FixtureListLiveSessions(ctx, repo.FixtureListLiveSessionsParams{ClientIds: clients, ProjectID: proj})
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	for _, sess := range sessions {
		email := sess.Email
		if email == "" {
			email = "someone@fixture.test"
		}
		pair, err := fixtureauth.IssueTokens(base, resource, mainClientID, email, fixtureauth.DefaultScopes, fixtureAccessTokenTTL, s.now)
		if err != nil {
			return fmt.Errorf("mint tokens: %w", err)
		}
		access, err := s.enc.Encrypt([]byte(pair.AccessToken))
		if err != nil {
			return fmt.Errorf("encrypt access token: %w", err)
		}
		refresh, err := s.enc.Encrypt([]byte(pair.RefreshToken))
		if err != nil {
			return fmt.Errorf("encrypt refresh token: %w", err)
		}
		n, err = q.FixtureSetSessionTokens(ctx, repo.FixtureSetSessionTokensParams{
			AccessTokenEncrypted:  access,
			AccessExpiresAt:       pgtype.Timestamptz{Time: pair.AccessExpiresAt, InfinityModifier: pgtype.Finite, Valid: true},
			RefreshTokenEncrypted: refresh,
			RefreshExpiresAt:      pgtype.Timestamptz{Time: pair.RefreshExpires, InfinityModifier: pgtype.Finite, Valid: true},
			Scopes:                fixtureauth.DefaultScopes,
			Email:                 email,
			Resource:              resource,
			ID:                    sess.ID,
			ClientIds:             clients,
			ProjectID:             proj,
		})
		if err := expect("store session tokens", n, err); err != nil {
			return err
		}
	}

	// 3. The Service Account Server: every caller shares the checked-in
	// token, sent as a static Authorization header (Settings shows "Shared
	// credential").
	n, err = q.FixturePointRemoteServer(ctx, repo.FixturePointRemoteServerParams{Url: base + fixtureauth.PathSharedMCP, ID: s.det("remotemcp-slack"), ProjectID: proj})
	if err := expect("point the service account server", n, err); err != nil {
		return err
	}
	n, err = q.FixtureSetRemoteHeaderValue(ctx, repo.FixtureSetRemoteHeaderValueParams{Value: "Bearer " + fixtureauth.SharedToken, ID: s.det("agent-identity-header-slack"), ProjectID: proj})
	if err := expect("set the service account token", n, err); err != nil {
		return err
	}

	// 4. The Pass-Through Token Server: each caller brings their own upstream
	// token in a request header Speakeasy forwards unchanged (Manual identity:
	// users manage their own headers). The header is required, so Speakeasy
	// turns away a caller without one. The fixture's own-token endpoint answers
	// the handshake without a token only so Speakeasy's setup probe, which
	// calls the upstream directly with no headers, sees a working server; it
	// runs tools only for a caller with a token, read from this header or from
	// Authorization when the typed pass-through client forwards it there.
	n, err = q.FixturePointRemoteServer(ctx, repo.FixturePointRemoteServerParams{Url: base + fixtureauth.PathOwnTokenMCP, ID: s.det("remotemcp-github"), ProjectID: proj})
	if err := expect("point the pass-through server", n, err); err != nil {
		return err
	}
	n, err = q.FixtureUpsertForwardedHeader(ctx, repo.FixtureUpsertForwardedHeaderParams{
		ID: s.det("remote-header-pass-through"), RemoteMcpServerID: s.det("remotemcp-github"), ProjectID: proj,
		Name: fixtureauth.OwnTokenHeader, FromRequestHeader: fixtureauth.OwnTokenHeader,
	})
	if err := expect("forward the caller's token", n, err); err != nil {
		return err
	}
	// The typed shared credential (used once upstream auth is enforced)
	// carries the same checked-in token.
	n, err = q.FixtureSetHeaderValue(ctx, repo.FixtureSetHeaderValueParams{Value: "Bearer " + fixtureauth.SharedToken, ID: s.det("auth-client-github-token-header"), OrganizationID: org})
	if err := expect("set the typed shared token", n, err); err != nil {
		return err
	}

	return nil
}

// seedFixtureAutoConfigured sets the Auto-Configured OAuth Server up as an
// admin's Auto-Configure would against the fixture's DCR-only authorization
// server: the provider, a client registered by dynamic registration, and the
// server's links to both. The fixture's client ids are signed tokens, so the
// seed registers the client exactly as the fixture's registration endpoint
// would, without calling it. Its static ids keep the "notion" names the
// server had before it was the fixture's. It runs in the servers phase, as
// the seeded sign-ins choose its client.
func (s *seeder) seedFixtureAutoConfigured(ctx context.Context) error {
	q := s.q
	org := s.orgID()
	proj := s.projectID()
	base := fixtureauth.BaseURL(s.serverURL)
	serverURL := strings.TrimRight(s.serverURL.String(), "/")
	issuer := fixtureauth.DCRIssuer(base)
	resource := fixtureauth.AutoResource(base)
	issuerID := s.det("remote-identity-provider-notion")
	clientRowID := s.det("remote-identity-client-notion")
	serverID := s.det("mcpserver-notion")
	authClientID := s.det("auth-client-notion")

	n, err := q.FixturePointRemoteServer(ctx, repo.FixturePointRemoteServerParams{Url: resource, ID: s.det("remotemcp-notion"), ProjectID: proj})
	if err != nil {
		return fmt.Errorf("point the auto-configured oauth server: %w", err)
	}
	if n == 0 {
		return errors.New("point the auto-configured oauth server matched no rows")
	}
	if err := q.ServersInsertVendorAppIssuer(ctx, repo.ServersInsertVendorAppIssuerParams{
		ID: issuerID, ProjectID: proj, OrganizationID: org, Key: "mcp-fixture-dcr", Issuer: issuer,
		AuthorizationEndpoint: issuer + "/oauth/authorize", TokenEndpoint: issuer + "/oauth/token",
		RegistrationEndpoint: issuer + "/oauth/register", RevocationEndpoint: issuer + "/oauth/revoke",
		TokenEndpointAuthMethods:          []string{"none"},
		ClientIDMetadataDocumentSupported: false,
		Scopes:                            fixtureauth.DefaultScopes, Name: fixtureDCRProviderName,
	}); err != nil {
		return fmt.Errorf("insert the auto-configured provider: %w", err)
	}
	clientID, err := fixtureauth.RegisterClient(issuer, "Speakeasy", []string{remotesessions.RemoteLoginCallbackURL(s.serverURL)}, s.now)
	if err != nil {
		return fmt.Errorf("register the auto-configured client: %w", err)
	}
	if err := q.FixtureInsertRegisteredClient(ctx, repo.FixtureInsertRegisteredClientParams{
		ID: clientRowID, ProjectID: proj, OrganizationID: org, RemoteSessionIssuerID: issuerID,
		ClientID: clientID, CallbackBaseUrl: serverURL,
	}); err != nil {
		return fmt.Errorf("insert the auto-configured client: %w", err)
	}
	// The server's own sign-in, and the organization sign-in, which reaches
	// every server, both bind the client.
	for _, sessionIssuer := range []uuid.UUID{s.det("issuer-notion"), s.det("issuer-workforce")} {
		if err := q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
			RemoteSessionClientID: clientRowID, UserSessionIssuerID: sessionIssuer,
		}); err != nil {
			return fmt.Errorf("bind the auto-configured client to a sign-in: %w", err)
		}
	}
	if err := q.ServersStampRemoteSessionIssuer(ctx, repo.ServersStampRemoteSessionIssuerParams{
		RemoteSessionIssuerID: issuerID, McpServerID: serverID, ProjectID: proj,
	}); err != nil {
		return fmt.Errorf("stamp the auto-configured provider: %w", err)
	}
	if err := q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
		ID: s.det("server-client-association-notion"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, RemoteSessionClientID: clientRowID,
	}); err != nil {
		return fmt.Errorf("associate the auto-configured client: %w", err)
	}
	if err := q.ServersInsertAuthClient(ctx, repo.ServersInsertAuthClientParams{
		ID: authClientID, OrgID: org, ProjectID: proj, ClientType: "remote_session", Source: "backfill",
		Name: fixtureDCRProviderName, RemoteSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
	}); err != nil {
		return fmt.Errorf("insert the auto-configured auth client: %w", err)
	}
	if err := q.ServersInsertServerAuthClient(ctx, repo.ServersInsertServerAuthClientParams{
		ID: s.det("auth-client-assoc-notion"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, AuthClientID: authClientID,
	}); err != nil {
		return fmt.Errorf("offer the auto-configured auth client: %w", err)
	}
	if err := q.ServersInsertVendorAppProtectedResource(ctx, repo.ServersInsertVendorAppProtectedResourceParams{
		ID: s.det("protected-resource-notion"), ProjectID: proj, OrganizationID: org,
		Resource: resource, MetadataUrl: issuer + "/.well-known/oauth-protected-resource" + fixtureauth.PathAutoMCP,
		Issuer: issuer, Scopes: fixtureauth.DefaultScopes,
	}); err != nil {
		return fmt.Errorf("insert the auto-configured protected resource: %w", err)
	}
	return nil
}

// wireLocalUpstreams wires, in the local org only, the servers that need the
// developer's machine: Linear and Notion, through clients registered there.
// The public demo org keeps them as the shared seed writes them.
func wireLocalUpstreams(ctx context.Context, logger *slog.Logger, tx pgx.Tx, opts LocalUpstreamOptions) error {
	if opts.ServerURL == nil {
		logger.InfoContext(ctx, "GRAM_SERVER_URL is not set; Linear and Notion keep the shared seed's metadata document clients")
		return nil
	}
	s := &seeder{spec: LocalSpec(), q: repo.New(tx), logger: logger, enc: nil, assistantIssuer: "", serverURL: opts.ServerURL, now: time.Now(), signalSequences: nil}
	return wireLocalVendorApps(ctx, logger, s, opts)
}
