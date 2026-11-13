package demoseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// Linear's MCP server and the authorization server that guards it, as its
// protected resource metadata names them.
const (
	linearIssuer      = "https://mcp.linear.app"
	linearResource    = linearIssuer + "/mcp"
	linearMetadataURL = linearIssuer + "/.well-known/oauth-protected-resource/mcp"
	linearScope       = "read write"
	linearCacheFile   = "demoseed-linear-client.json"
)

// linearClient is the client the seed registered with Linear, kept in the
// cache directory. It is a public client (no secret), registered for one
// callback URL; another callback (a worktree on other ports) registers anew.
type linearClient struct {
	Callback string `json:"callback"`
	ClientID string `json:"client_id"`
}

// wireLocalLinear makes the seeded Linear server work in the local org: each
// developer signs in to their own Linear account and calls Linear's tools.
//
// Linear supports client ID metadata documents, which the shared seed's
// clients use elsewhere, but Linear fetches the document from Speakeasy, and
// it cannot reach a developer's localhost. So the seed registers a public
// client by dynamic client registration, as an admin's Auto-Configure does,
// and records the provider, the client and the server's links to them the way
// Auto-Configure does.
//
// Linear cannot be reached offline, so a failed registration leaves the
// server for an admin to set up, and the seed goes on.
func wireLocalLinear(ctx context.Context, logger *slog.Logger, s *seeder, opts LocalMCPFixtureOptions) error {
	if opts.Policy == nil {
		logger.InfoContext(ctx, "no egress policy to register a Linear client with; Linear stays for an admin to set up")
		return nil
	}
	origin := strings.TrimRight(opts.ServerURL.String(), "/")
	callback := remotesessions.RemoteLoginCallbackURL(opts.ServerURL)

	clientID, err := linearClientID(ctx, s, opts, callback)
	if err != nil {
		logger.WarnContext(ctx, "could not register a client with Linear; Linear stays for an admin to set up", attr.SlogError(err))
		return nil
	}

	q := s.q
	org := s.orgID()
	proj := s.projectID()
	issuerID := s.det("remote-identity-provider-linear-app")
	clientRowID := s.det("remote-identity-client-linear-app")
	serverID := s.det("mcpserver-linear-app")
	authClientID := s.det("auth-client-linear-app")

	if err := q.LocalLinearInsertIssuer(ctx, repo.LocalLinearInsertIssuerParams{
		ID: issuerID, ProjectID: proj, OrganizationID: org, Issuer: linearIssuer,
	}); err != nil {
		return fmt.Errorf("wire local linear: provider: %w", err)
	}
	if err := q.LocalLinearInsertClient(ctx, repo.LocalLinearInsertClientParams{
		ID: clientRowID, ProjectID: proj, OrganizationID: org,
		RemoteSessionIssuerID: issuerID, ClientID: clientID, CallbackBaseUrl: origin,
	}); err != nil {
		return fmt.Errorf("wire local linear: client: %w", err)
	}
	if err := q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
		RemoteSessionClientID: clientRowID, UserSessionIssuerID: s.det("issuer-linear-app"),
	}); err != nil {
		return fmt.Errorf("wire local linear: bind the client to the server's sign-in: %w", err)
	}
	if err := q.ServersStampRemoteSessionIssuer(ctx, repo.ServersStampRemoteSessionIssuerParams{
		RemoteSessionIssuerID: issuerID, McpServerID: serverID, ProjectID: proj,
	}); err != nil {
		return fmt.Errorf("wire local linear: stamp the provider: %w", err)
	}
	if err := q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
		ID: s.det("server-client-association-linear-app"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, RemoteSessionClientID: clientRowID,
	}); err != nil {
		return fmt.Errorf("wire local linear: associate the client: %w", err)
	}
	if err := q.ServersInsertAuthClient(ctx, repo.ServersInsertAuthClientParams{
		ID: authClientID, OrgID: org, ProjectID: proj, ClientType: "remote_session", Source: "backfill",
		Name: "Linear", RemoteSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
	}); err != nil {
		return fmt.Errorf("wire local linear: auth client: %w", err)
	}
	if err := q.ServersInsertServerAuthClient(ctx, repo.ServersInsertServerAuthClientParams{
		ID: s.det("auth-client-assoc-linear-app"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, AuthClientID: authClientID,
	}); err != nil {
		return fmt.Errorf("wire local linear: offer the auth client: %w", err)
	}
	if err := q.LocalLinearInsertProtectedResource(ctx, repo.LocalLinearInsertProtectedResourceParams{
		ID: s.det("protected-resource-linear-app"), ProjectID: proj, OrganizationID: org,
		Resource: linearResource, MetadataUrl: linearMetadataURL, Issuer: linearIssuer,
	}); err != nil {
		return fmt.Errorf("wire local linear: protected resource: %w", err)
	}
	logger.InfoContext(ctx, "registered the seeded Linear server's client with Linear", attr.SlogURLOriginal(linearResource))
	return nil
}

// linearClientID returns the client registered for callback: the cached one
// when there is one, otherwise a new registration, which it caches.
func linearClientID(ctx context.Context, s *seeder, opts LocalMCPFixtureOptions, callback string) (string, error) {
	path := ""
	if opts.CacheDir != "" && opts.CacheDir != "unset" {
		path = filepath.Join(opts.CacheDir, linearCacheFile)
	}
	if cached, ok := readLinearClient(path); ok && cached.Callback == callback {
		return cached.ClientID, nil
	}

	scope := linearScope
	method := "none"
	registered, err := remotesessions.RegisterDynamicClient(ctx, opts.Policy, nil, opts.ServerURL, remotesessions.ProxyRegisterRequest{
		RegistrationEndpoint:    linearIssuer + "/register",
		Scope:                   &scope,
		TokenEndpointAuthMethod: &method,
		TunneledMcpServerID:     nil,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("register with Linear: %w", err)
	}
	// A public client only: the client row stores no secret.
	if registered.ClientSecret != "" {
		return "", errors.New("register with Linear: Linear issued a client secret for a public client")
	}
	if path != "" {
		if err := writeLinearClient(path, linearClient{Callback: callback, ClientID: registered.ClientID}); err != nil {
			s.logger.WarnContext(ctx, "could not cache the Linear client; the next seed registers another", attr.SlogError(err))
		}
	}
	return registered.ClientID, nil
}

func readLinearClient(path string) (linearClient, bool) {
	var c linearClient
	if path == "" {
		return c, false
	}
	b, err := os.ReadFile(path) //nolint:gosec // a path from the seed's own configuration
	if err != nil {
		return c, false
	}
	if err := json.Unmarshal(b, &c); err != nil || c.ClientID == "" {
		return c, false
	}
	return c, true
}

func writeLinearClient(path string, c linearClient) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode cached client: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write cached client: %w", err)
	}
	return nil
}
