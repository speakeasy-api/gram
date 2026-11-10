package demoseed

import (
	"context"
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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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
}

// Names the local org shows for the servers wired to the fixture. They say
// what each server demonstrates, not which vendor it once imitated.
const (
	fixtureOAuthServerName    = "Per-User OAuth Server"
	fixtureSharedServerName   = "Shared or Own Token Server"
	fixtureProviderName       = "MCP Fixture OAuth"
	fixtureSignInClientName   = "Fixture OAuth sign-in"
	fixtureAttachedClientName = "Fixture OAuth agent connections"
	fixtureEnvironmentFixedID = "0000-4000-a000-000000007008"
	fixtureAPIEnvVar          = "ACME_SERVER_URL"
	linearOldResource         = "https://mcp.linear.app/mcp"
)

// wireLocalMCPFixture rewires, in the local org only:
//
//   - the generated toolset servers (Acme Support Tools, Acme Ops, Acme
//     Partner Gateway, Acme OAuth Discovery) to call the fixture's stub of
//     the demo API, through a system environment;
//   - Linear into the Per-User OAuth Server: its URL, OAuth provider,
//     protected resource and both OAuth clients point at the fixture, and
//     every live seeded sign-in holds real fixture tokens;
//   - GitHub into the Shared or Own Token Server: its URL points at the
//     fixture's shared-token endpoint and its shared credential carries the
//     checked-in shared token.
//
// The public demo org keeps its display-only values: prod cannot reach a
// developer's localhost.
func wireLocalMCPFixture(ctx context.Context, logger *slog.Logger, tx pgx.Tx, opts LocalMCPFixtureOptions) error {
	if opts.BaseURL == "" || opts.BaseURL == "unset" {
		logger.InfoContext(ctx, "GRAM_MCP_FIXTURE_URL is not set; seeded servers stay display-only")
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
	if err := q.LocalFixtureUpsertEnvironmentEntry(ctx, repo.LocalFixtureUpsertEnvironmentEntryParams{EnvironmentID: envID, Name: fixtureAPIEnvVar, Value: base + fixtureauth.PathAPI}); err != nil {
		return fmt.Errorf("wire local mcp fixture: environment entry: %w", err)
	}
	n, err := q.LocalFixtureLinkToolsetEnvironments(ctx, repo.LocalFixtureLinkToolsetEnvironmentsParams{
		EnvironmentID: envID, ProjectID: proj,
		ToolsetIds: []uuid.UUID{s.toolset1(), s.toolset2(), s.toolset3(), s.toolset4()},
	})
	if err := expect("link toolset environments", n, err); err != nil {
		return err
	}

	// 2. Linear becomes the Per-User OAuth Server.
	n, err = q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: resource, Name: fixtureOAuthServerName, ID: s.det("remotemcp-linear"), ProjectID: proj})
	if err := expect("point the OAuth remote server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixtureRenameServer(ctx, repo.LocalFixtureRenameServerParams{Name: fixtureOAuthServerName, ID: s.det("mcpserver-linear"), ProjectID: proj})
	if err := expect("rename the OAuth server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixturePointIssuer(ctx, repo.LocalFixturePointIssuerParams{Issuer: base, Scopes: fixtureauth.DefaultScopes, Name: fixtureProviderName, ID: s.det("remote-identity-provider-linear"), ProjectID: proj})
	if err := expect("point the OAuth provider", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixturePointProtectedResource(ctx, repo.LocalFixturePointProtectedResourceParams{
		Resource: resource, MetadataUrl: base + "/.well-known/oauth-protected-resource" + fixtureauth.PathOAuthMCP,
		Issuer: base, Scopes: fixtureauth.DefaultScopes, ProjectID: proj, OrganizationID: org, OldResource: linearOldResource,
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
	// The attachment client is a legacy-callback client, so it cannot use a
	// metadata document; the seed registers it with the fixture up front.
	attachedClient := s.det("attachment-client")
	registered, err := fixtureauth.RegisterClient(base, fixtureAttachedClientName, []string{
		serverURL + "/mcp/remote_login_callback",
		serverURL + "/oauth/callback",
	}, s.now)
	if err != nil {
		return fmt.Errorf("wire local mcp fixture: register the attachment client: %w", err)
	}
	n, err = q.LocalFixturePointRegisteredClient(ctx, repo.LocalFixturePointRegisteredClientParams{ClientID: registered, CallbackBaseUrl: serverURL, ID: attachedClient, ProjectID: proj})
	if err := expect("point the attachment client", n, err); err != nil {
		return err
	}
	for id, name := range map[string]string{"auth-client-linear": fixtureSignInClientName, "auth-client-linear-attachment": fixtureAttachedClientName} {
		n, err = q.LocalFixtureRenameAuthClient(ctx, repo.LocalFixtureRenameAuthClientParams{Name: name, ID: s.det(id), OrganizationID: org})
		if err := expect("rename auth client "+id, n, err); err != nil {
			return err
		}
	}

	// Every live seeded sign-in on those clients gets real fixture tokens.
	// Expired ones stay expired, so Needs refresh still shows; reconnecting
	// them signs in against the fixture.
	clients := []uuid.UUID{mainClient, attachedClient}
	sessions, err := q.LocalFixtureListLiveSessions(ctx, clients)
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
			ID:                    sess.ID,
			ClientIds:             clients,
		})
		if err := expect("store session tokens", n, err); err != nil {
			return err
		}
	}

	// 3. GitHub becomes the Shared or Own Token Server.
	n, err = q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: base + fixtureauth.PathSharedMCP, Name: fixtureSharedServerName, ID: s.det("remotemcp-github"), ProjectID: proj})
	if err := expect("point the shared-token remote server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixtureRenameServer(ctx, repo.LocalFixtureRenameServerParams{Name: fixtureSharedServerName, ID: s.det("mcpserver-github"), ProjectID: proj})
	if err := expect("rename the shared-token server", n, err); err != nil {
		return err
	}
	n, err = q.LocalFixtureSetHeaderValue(ctx, repo.LocalFixtureSetHeaderValueParams{Value: "Bearer " + fixtureauth.SharedToken, ID: s.det("auth-client-github-token-header"), OrganizationID: org})
	if err := expect("set the shared token", n, err); err != nil {
		return err
	}

	logger.InfoContext(ctx, "pointed seeded servers at the local MCP fixture",
		attr.SlogURLOriginal(base), attr.SlogValueInt(len(sessions)))
	return nil
}
