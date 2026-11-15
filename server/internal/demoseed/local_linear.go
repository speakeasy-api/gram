package demoseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// linearResource is Linear's MCP server, and linearScope what the seeded
// client asks Linear for.
const (
	linearResource  = "https://mcp.linear.app/mcp"
	linearIssuer    = "https://mcp.linear.app"
	linearScope     = "read write"
	linearCacheFile = "demoseed-linear-client.json"
)

// linearClient is the client the seed registered with Linear, kept in the
// cache directory. It is a public client (no secret), registered for one
// callback URL; another callback (a worktree on other ports) registers anew.
type linearClient struct {
	Callback string `json:"callback"`
	ClientID string `json:"client_id"`
}

// seedLinearApp sets Linear up as an admin's Auto-Configure would, in every
// organization the seed writes: Linear's authorization server, a client ID
// metadata document client, and the server's links to both. Speakeasy serves
// the document at the deployment's own URL, and Linear fetches it when someone
// signs in, so nothing is registered ahead of time: in the public demo org each
// visitor signs in to their own Linear account. Locally, where Linear cannot
// reach the document, wireLocalLinear swaps the client for one registered by
// DCR.
func (s *seeder) seedLinearApp(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	issuerID := s.det("remote-identity-provider-linear-app")
	clientRowID := s.det("remote-identity-client-linear-app")
	serverID := s.det("mcpserver-linear-app")
	authClientID := s.det("auth-client-linear-app")

	if err := s.q.ServersInsertLinearAppIssuer(ctx, repo.ServersInsertLinearAppIssuerParams{
		ID: issuerID, ProjectID: proj, OrganizationID: org,
	}); err != nil {
		return fmt.Errorf("insert demo linear provider: %w", err)
	}
	if err := s.q.ServersInsertLinearAppClient(ctx, repo.ServersInsertLinearAppClientParams{
		ID: clientRowID, ProjectID: proj, OrganizationID: org, RemoteSessionIssuerID: issuerID,
		ClientID:        remotesessions.ClientMetadataDocumentURL(s.serverURL, clientRowID),
		CallbackBaseUrl: strings.TrimRight(s.serverURL.String(), "/"),
	}); err != nil {
		return fmt.Errorf("insert demo linear client: %w", err)
	}
	if err := s.q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
		RemoteSessionClientID: clientRowID, UserSessionIssuerID: s.det("issuer-linear-app"),
	}); err != nil {
		return fmt.Errorf("bind demo linear client to the server's sign-in: %w", err)
	}
	// An organization sign-in reaches every server, Linear included, so the
	// workforce issuer binds Linear's client too, as it does the Per-User
	// OAuth Server's.
	if err := s.q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
		RemoteSessionClientID: clientRowID, UserSessionIssuerID: s.det("issuer-workforce"),
	}); err != nil {
		return fmt.Errorf("bind demo linear client to the workforce sign-in: %w", err)
	}
	if err := s.q.ServersStampRemoteSessionIssuer(ctx, repo.ServersStampRemoteSessionIssuerParams{
		RemoteSessionIssuerID: issuerID, McpServerID: serverID, ProjectID: proj,
	}); err != nil {
		return fmt.Errorf("stamp demo linear provider: %w", err)
	}
	if err := s.q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
		ID: s.det("server-client-association-linear-app"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, RemoteSessionClientID: clientRowID,
	}); err != nil {
		return fmt.Errorf("associate demo linear client: %w", err)
	}
	if err := s.q.ServersInsertAuthClient(ctx, repo.ServersInsertAuthClientParams{
		ID: authClientID, OrgID: org, ProjectID: proj, ClientType: "remote_session", Source: "backfill",
		Name: "Linear", RemoteSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
	}); err != nil {
		return fmt.Errorf("insert demo linear auth client: %w", err)
	}
	if err := s.q.ServersInsertServerAuthClient(ctx, repo.ServersInsertServerAuthClientParams{
		ID: s.det("auth-client-assoc-linear-app"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, AuthClientID: authClientID,
	}); err != nil {
		return fmt.Errorf("offer demo linear auth client: %w", err)
	}
	if err := s.q.ServersInsertLinearAppProtectedResource(ctx, repo.ServersInsertLinearAppProtectedResourceParams{
		ID: s.det("protected-resource-linear-app"), ProjectID: proj, OrganizationID: org,
	}); err != nil {
		return fmt.Errorf("insert demo linear protected resource: %w", err)
	}
	return nil
}

// wireLocalLinear makes the seeded Linear server work in the local org. The
// shared seed's metadata document client cannot work there: Linear fetches the
// document from Speakeasy and cannot reach a developer's localhost. So the seed
// registers a public client with Linear by dynamic client registration and puts
// it in that client's place.
//
// Linear cannot be reached offline, so a failed registration leaves the
// metadata document client in place (signing in then fails at Linear), and
// the seed goes on.
func wireLocalLinear(ctx context.Context, logger *slog.Logger, s *seeder, opts LocalMCPFixtureOptions) error {
	if opts.Policy == nil {
		logger.InfoContext(ctx, "no egress policy to register a Linear client with; Linear keeps its metadata document client, which Linear cannot fetch from localhost")
		return nil
	}
	// Linear redirects sign-ins, with their authorization codes, to this
	// origin: never register a plain-HTTP callback except on loopback.
	if opts.ServerURL.Scheme != "https" && !isLoopbackHost(opts.ServerURL.Hostname()) {
		logger.WarnContext(ctx, "GRAM_SERVER_URL is plain HTTP on a non-loopback host; Linear keeps its metadata document client", attr.SlogURLOriginal(opts.ServerURL.String()))
		return nil
	}
	callback := remotesessions.RemoteLoginCallbackURL(opts.ServerURL)
	clientID, err := linearClientID(ctx, s, opts, callback)
	if err != nil {
		logger.WarnContext(ctx, "could not register a client with Linear; Linear keeps its metadata document client, which Linear cannot fetch from localhost", attr.SlogError(err))
		return nil
	}
	n, err := s.q.LocalLinearPointRegisteredClient(ctx, repo.LocalLinearPointRegisteredClientParams{
		ClientID: clientID, CallbackBaseUrl: strings.TrimRight(opts.ServerURL.String(), "/"),
		ID: s.det("remote-identity-client-linear-app"), ProjectID: s.projectID(),
	})
	if err != nil {
		return fmt.Errorf("wire local linear: client: %w", err)
	}
	if n == 0 {
		return errors.New("wire local linear: the seeded Linear client matched no rows")
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

// isLoopbackHost reports whether host is localhost or a loopback address.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
