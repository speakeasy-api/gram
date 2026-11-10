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

	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// LocalMCPFixtureOptions points seeded servers at the throwaway MCP fixture
// server (mcp-fixture/), so they work end to end right after seeding.
type LocalMCPFixtureOptions struct {
	// BaseURL is the fixture's URL (GRAM_MCP_FIXTURE_URL). Empty skips the
	// fixture wiring.
	BaseURL string
	// ServerURL is Speakeasy's own URL (GRAM_SERVER_URL). The fixture fetches
	// client ID metadata documents from it and redirects sign-ins back to it.
	ServerURL *url.URL
	// AccessTokenTTL is the lifetime of the access tokens the seed mints. Keep
	// it equal to the fixture's GRAM_MCP_FIXTURE_ACCESS_TOKEN_TTL.
	AccessTokenTTL time.Duration
	// Encryption encrypts the minted tokens the way Speakeasy stores them.
	Encryption *encryption.Client
	// DevIDPURL is the dev-idp's external URL (GRAM_DEVIDP_EXTERNAL_URL). With
	// DevIDPClientID it makes the Enterprise Sign-In (ID-JAG) Server work
	// (local_ema.go); empty leaves that server display-only.
	DevIDPURL string
	// DevIDPClientID is the dev-idp's static login client (GRAM_IDP_CLIENT_ID).
	DevIDPClientID string
	// HTTPClient reaches the dev-idp's management API.
	HTTPClient *guardian.HTTPClient
}

// Fixed values for the servers wired to the fixture.
const (
	fixtureProviderName       = "MCP Fixture OAuth"
	fixtureEnvironmentFixedID = "0000-4000-a000-000000007008"
	fixtureAPIEnvVar          = "ACME_SERVER_URL"
	demoOAuthResource         = "https://mcp-fixture.example.com/mcp/oauth"
)

// wireLocalMCPFixture rewires, in the local org only:
//
//   - the generated toolset servers (Acme Support Tools, Acme Ops, Acme
//     Partner Gateway, Acme OAuth Discovery) to call the fixture's stub of
//     the demo API, through a system environment;
//   - the Per-User OAuth Server: its URL, MCP Fixture OAuth provider,
//     protected resource and OAuth client move from the shared seed's
//     placeholder host (mcp-fixture.example.com) to the running fixture, and
//     every live seeded sign-in holds real fixture tokens;
//   - the Shared or Own Token Server: its URL points at the fixture's
//     shared-token endpoint and its shared credential carries the checked-in
//     shared token;
//   - the Enterprise Sign-In (ID-JAG) Server, through the dev-idp
//     (local_ema.go).
//
// The shared seed already gives these servers their names. The public demo
// org keeps the placeholder host: prod cannot reach a developer's localhost.
func wireLocalMCPFixture(ctx context.Context, logger *slog.Logger, tx pgx.Tx, opts LocalMCPFixtureOptions) error {
	if opts.BaseURL == "" || opts.BaseURL == "unset" {
		logger.InfoContext(ctx, "GRAM_MCP_FIXTURE_URL is not set; seeded servers stay display-only")
		return nil
	}
	// Speakeasy discovers OAuth servers over HTTPS only, and the seeded
	// sessions carry bearer tokens: never wire a plain-HTTP fixture.
	if !strings.HasPrefix(opts.BaseURL, "https://") {
		logger.WarnContext(ctx, "GRAM_MCP_FIXTURE_URL is not HTTPS (enable local TLS); seeded servers stay display-only", attr.SlogURLOriginal(opts.BaseURL))
		return nil
	}
	if opts.ServerURL == nil {
		return errors.New("wire local mcp fixture: the server URL is required")
	}
	if opts.Encryption == nil {
		return errors.New("wire local mcp fixture: an encryption client is required to store fixture tokens")
	}
	if opts.AccessTokenTTL <= 0 {
		opts.AccessTokenTTL = time.Hour
	}

	spec := LocalSpec()
	s := &seeder{spec: spec, q: repo.New(tx), logger: logger, now: time.Now(), signalSequences: nil}
	q := s.q
	org := spec.OrgID
	proj := s.projectID()
	base := strings.TrimRight(opts.BaseURL, "/")
	serverURL := strings.TrimRight(opts.ServerURL.String(), "/")
	resource := fixtureauth.OAuthResource(base)

	expect := func(what string, n int64, err error) error {
		if err != nil {
			return fmt.Errorf("wire local mcp fixture: %s: %w", what, err)
		}
		if n == 0 {
			return fmt.Errorf("wire local mcp fixture: %s matched no rows", what)
		}
		return nil
	}

	// 1. The demo API stub, as the generated servers' system environment.
	envID := uuid.MustParse(spec.FixedUUID(fixtureEnvironmentFixedID))
	if err := q.LocalFixtureUpsertEnvironment(ctx, repo.LocalFixtureUpsertEnvironmentParams{ID: envID, OrganizationID: org, ProjectID: proj}); err != nil {
		return fmt.Errorf("wire local mcp fixture: environment: %w", err)
	}
	n, err := q.LocalFixtureUpsertEnvironmentEntry(ctx, repo.LocalFixtureUpsertEnvironmentEntryParams{EnvironmentID: envID, ProjectID: proj, Name: fixtureAPIEnvVar, Value: base + fixtureauth.PathAPI})
	if err := expect("environment entry", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixtureLinkToolsetEnvironments(ctx, repo.LocalFixtureLinkToolsetEnvironmentsParams{
		EnvironmentID: envID, ProjectID: proj,
		ToolsetIds: []uuid.UUID{s.toolset1(), s.toolset2(), s.toolset3(), s.toolset4()},
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
		return fmt.Errorf("wire local mcp fixture: external oauth metadata: %w", err)
	}
	n, err = q.LocalFixturePointExternalOAuth(ctx, repo.LocalFixturePointExternalOAuthParams{Metadata: metadata, ID: s.det("external-oauth-discovery"), ProjectID: proj})
	if err := expect("point the external oauth server", n, err); err != nil {
		return err
	}

	// 2. The Per-User OAuth Server.
	n, err = q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: resource, ID: s.det("remotemcp-linear"), ProjectID: proj})
	if err := expect("point the OAuth remote server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixturePointIssuer(ctx, repo.LocalFixturePointIssuerParams{Issuer: base, Scopes: fixtureauth.DefaultScopes, Name: fixtureProviderName, ID: s.det("remote-identity-provider-linear"), ProjectID: proj})
	if err := expect("point the OAuth provider", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixturePointProtectedResource(ctx, repo.LocalFixturePointProtectedResourceParams{
		Resource: resource, MetadataUrl: base + "/.well-known/oauth-protected-resource" + fixtureauth.PathOAuthMCP,
		Issuer: base, Scopes: fixtureauth.DefaultScopes, ProjectID: proj, OrganizationID: org, OldResource: demoOAuthResource,
	})
	if err := expect("point the protected resource", n, err); err != nil {
		return err
	}

	// The main client publishes a client ID metadata document, which
	// Speakeasy serves and the fixture fetches.
	mainClient := s.det("remote-identity-client-linear")
	n, err = q.LocalFixturePointMetadataDocumentClient(ctx, repo.LocalFixturePointMetadataDocumentClientParams{
		ClientID: remotesessions.ClientMetadataDocumentURL(opts.ServerURL, mainClient), CallbackBaseUrl: serverURL, ID: mainClient, ProjectID: proj,
	})
	if err := expect("point the sign-in client", n, err); err != nil {
		return err
	}

	// Every live seeded sign-in on that client, including the agents'
	// attached connection, gets real fixture tokens. Expired ones stay
	// expired, so Needs refresh still shows; reconnecting them signs in
	// against the fixture.
	clients := []uuid.UUID{mainClient}
	sessions, err := q.LocalFixtureListLiveSessions(ctx, repo.LocalFixtureListLiveSessionsParams{ClientIds: clients, ProjectID: proj})
	if err != nil {
		return fmt.Errorf("wire local mcp fixture: list sessions: %w", err)
	}
	for _, sess := range sessions {
		email := sess.Email
		if email == "" {
			email = "someone@fixture.test"
		}
		clientID := remotesessions.ClientMetadataDocumentURL(opts.ServerURL, mainClient)
		pair, err := fixtureauth.IssueTokens(base, resource, clientID, email, fixtureauth.DefaultScopes, opts.AccessTokenTTL, time.Now())
		if err != nil {
			return fmt.Errorf("wire local mcp fixture: mint tokens: %w", err)
		}
		access, err := opts.Encryption.Encrypt([]byte(pair.AccessToken))
		if err != nil {
			return fmt.Errorf("wire local mcp fixture: encrypt access token: %w", err)
		}
		refresh, err := opts.Encryption.Encrypt([]byte(pair.RefreshToken))
		if err != nil {
			return fmt.Errorf("wire local mcp fixture: encrypt refresh token: %w", err)
		}
		n, err = q.LocalFixtureSetSessionTokens(ctx, repo.LocalFixtureSetSessionTokensParams{
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
	n, err = q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: base + fixtureauth.PathSharedMCP, ID: s.det("remotemcp-slack"), ProjectID: proj})
	if err := expect("point the service account server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixtureSetRemoteHeaderValue(ctx, repo.LocalFixtureSetRemoteHeaderValueParams{Value: "Bearer " + fixtureauth.SharedToken, ID: s.det("agent-identity-header-slack"), ProjectID: proj})
	if err := expect("set the service account token", n, err); err != nil {
		return err
	}

	// 4. The Pass-Through Token Server: each caller brings their own upstream
	// token in a request header Speakeasy forwards unchanged (Manual identity:
	// users manage their own headers). The fixture's own-token endpoint lets
	// anyone connect but runs tools only for a caller with a token.
	n, err = q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: base + fixtureauth.PathOwnTokenMCP, ID: s.det("remotemcp-github"), ProjectID: proj})
	if err := expect("point the pass-through server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixtureUpsertForwardedHeader(ctx, repo.LocalFixtureUpsertForwardedHeaderParams{
		ID: s.det("remote-header-pass-through"), RemoteMcpServerID: s.det("remotemcp-github"), ProjectID: proj,
		Name: fixtureauth.OwnTokenHeader, FromRequestHeader: fixtureauth.OwnTokenHeader,
	})
	if err := expect("forward the caller's token", n, err); err != nil {
		return err
	}
	// The typed shared credential (used once upstream auth is enforced)
	// carries the same checked-in token.
	n, err = q.LocalFixtureSetHeaderValue(ctx, repo.LocalFixtureSetHeaderValueParams{Value: "Bearer " + fixtureauth.SharedToken, ID: s.det("auth-client-github-token-header"), OrganizationID: org})
	if err := expect("set the typed shared token", n, err); err != nil {
		return err
	}

	// 5. The Auto-Configured OAuth Server: a second OAuth-protected fixture
	// endpoint with no client yet. An admin's "Set up sign-in" discovers its
	// authorization server and registers a client, as with a real vendor.
	n, err = q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: base + fixtureauth.PathAutoMCP, ID: s.det("remotemcp-notion"), ProjectID: proj})
	if err := expect("point the auto-configured oauth server", n, err); err != nil {
		return err
	}

	// 6. The Tunnelled Private Server: the fixture daemon's tunnel agent
	// connects with the checked-in key, so its hash goes on the seeded tunnel.
	n, err = q.LocalFixtureSetTunnelKey(ctx, repo.LocalFixtureSetTunnelKeyParams{
		KeyHash: wire.HashKey(fixtureauth.TunnelKey), KeyPrefix: fixtureauth.TunnelKey[:len(wire.KeyPrefix)+6],
		ID: s.tunnelSourceID(), ProjectID: proj,
	})
	if err := expect("set the tunnel key", n, err); err != nil {
		return err
	}

	// 8. The Enterprise Sign-In (ID-JAG) Server: signed in through the
	// dev-idp, it reaches the fixture's EMA endpoint with a token Speakeasy
	// obtains by exchanging that identity for an ID-JAG.
	if err := wireLocalEMA(ctx, logger, tx, opts, base); err != nil {
		return err
	}

	logger.InfoContext(ctx, "pointed seeded servers at the local MCP fixture",
		attr.SlogURLOriginal(base), attr.SlogValueInt(len(sessions)))
	return nil
}
