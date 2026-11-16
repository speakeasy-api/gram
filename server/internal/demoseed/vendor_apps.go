package demoseed

import (
	"context"
	"encoding/json"
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

// vendorApp is a well-known vendor's MCP server the seed sets up in every
// organization: a real service each person signs in to with their own
// account. key builds the server's slug and its static ids ("<key>-app"), so
// keep it stable: changing it leaves the old rows to clean up.
type vendorApp struct {
	key      string
	name     string
	resource string // the MCP server URL, also its protected resource
	issuer   string // the authorization server, at the resource's origin
	scopes   []string
}

// vendorApps are Linear and Notion. Both support client ID metadata
// documents and dynamic client registration
// (<issuer>/.well-known/oauth-authorization-server).
var vendorApps = []vendorApp{
	{key: "linear", name: "Linear", resource: "https://mcp.linear.app/mcp", issuer: "https://mcp.linear.app", scopes: []string{"read", "write"}},
	{key: "notion", name: "Notion", resource: "https://mcp.notion.com/mcp", issuer: "https://mcp.notion.com", scopes: []string{"default"}},
}

// id is the static id for one of the vendor's rows, e.g. id("mcpserver").
func (v vendorApp) id(s *seeder, kind string) uuid.UUID { return s.det(kind + "-" + v.key + "-app") }

// cacheFile holds the client the local seed registered with the vendor.
func (v vendorApp) cacheFile() string { return "demoseed-" + v.key + "-client.json" }

// metadataURL is the resource's RFC 9728 metadata location.
func (v vendorApp) metadataURL() string {
	return v.issuer + "/.well-known/oauth-protected-resource" + strings.TrimPrefix(v.resource, v.issuer)
}

// vendorClient is a client the seed registered with a vendor, kept in the
// cache directory. It is a public client (no secret), registered for one
// callback URL; another callback (a worktree on other ports) registers anew.
type vendorClient struct {
	Callback string `json:"callback"`
	ClientID string `json:"client_id"`
}

// seedVendorApps sets each vendor up as an admin's Auto-Configure would, in
// every organization the seed writes: the server, the vendor's authorization
// server, a client ID metadata document client, and the server's links to
// both. Speakeasy serves the document at the deployment's own URL, and the
// vendor fetches it when someone signs in, so nothing is registered ahead of
// time: in the public demo org each visitor signs in to their own account.
// Locally, where the vendor cannot reach the document, wireLocalVendorApps
// swaps the client for one registered by DCR.
func (s *seeder) seedVendorApps(ctx context.Context) error {
	for _, v := range vendorApps {
		if err := s.seedVendorApp(ctx, v); err != nil {
			return fmt.Errorf("seed %s: %w", v.name, err)
		}
	}
	return nil
}

func (s *seeder) seedVendorApp(ctx context.Context, v vendorApp) error {
	org := s.orgID()
	proj := s.projectID()
	issuerID := v.id(s, "remote-identity-provider")
	clientRowID := v.id(s, "remote-identity-client")
	serverID := v.id(s, "mcpserver")
	authClientID := v.id(s, "auth-client")

	if err := s.q.ServersInsertVendorAppServer(ctx, repo.ServersInsertVendorAppServerParams{
		RemoteID: v.id(s, "remotemcp"), IssuerID: v.id(s, "issuer"), ServerID: serverID,
		EndpointID: v.id(s, "endpoint"), Slug: s.spec.OrgSlug + "-" + v.key,
		ProjectID: proj, OrgID: org, Name: v.name, Key: v.key, Url: v.resource,
	}); err != nil {
		return fmt.Errorf("insert server: %w", err)
	}
	if err := s.q.ServersInsertVendorAppIssuer(ctx, repo.ServersInsertVendorAppIssuerParams{
		ID: issuerID, ProjectID: proj, OrganizationID: org, Key: v.key, Issuer: v.issuer,
		AuthorizationEndpoint: v.issuer + "/authorize", TokenEndpoint: v.issuer + "/token",
		RegistrationEndpoint: v.issuer + "/register", RevocationEndpoint: v.issuer + "/token",
		TokenEndpointAuthMethods:          []string{"client_secret_basic", "client_secret_post", "none"},
		ClientIDMetadataDocumentSupported: true,
		Scopes:                            v.scopes, Name: v.name,
	}); err != nil {
		return fmt.Errorf("insert provider: %w", err)
	}
	if err := s.q.ServersInsertVendorAppClient(ctx, repo.ServersInsertVendorAppClientParams{
		ID: clientRowID, ProjectID: proj, OrganizationID: org, RemoteSessionIssuerID: issuerID,
		ClientID:        remotesessions.ClientMetadataDocumentURL(s.serverURL, clientRowID),
		CallbackBaseUrl: strings.TrimRight(s.serverURL.String(), "/"),
	}); err != nil {
		return fmt.Errorf("insert client: %w", err)
	}
	// The server's own sign-in, and the organization sign-in, which reaches
	// every server, both bind the client, as they do the Per-User OAuth
	// Server's.
	for _, issuer := range []uuid.UUID{v.id(s, "issuer"), s.det("issuer-workforce")} {
		if err := s.q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
			RemoteSessionClientID: clientRowID, UserSessionIssuerID: issuer,
		}); err != nil {
			return fmt.Errorf("bind client to a sign-in: %w", err)
		}
	}
	if err := s.q.ServersStampRemoteSessionIssuer(ctx, repo.ServersStampRemoteSessionIssuerParams{
		RemoteSessionIssuerID: issuerID, McpServerID: serverID, ProjectID: proj,
	}); err != nil {
		return fmt.Errorf("stamp provider: %w", err)
	}
	if err := s.q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
		ID: v.id(s, "server-client-association"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, RemoteSessionClientID: clientRowID,
	}); err != nil {
		return fmt.Errorf("associate client: %w", err)
	}
	if err := s.q.ServersInsertAuthClient(ctx, repo.ServersInsertAuthClientParams{
		ID: authClientID, OrgID: org, ProjectID: proj, ClientType: "remote_session", Source: "backfill",
		Name: v.name, RemoteSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
	}); err != nil {
		return fmt.Errorf("insert auth client: %w", err)
	}
	if err := s.q.ServersInsertServerAuthClient(ctx, repo.ServersInsertServerAuthClientParams{
		ID: v.id(s, "auth-client-assoc"), ProjectID: proj, OrgID: org,
		McpServerID: serverID, AuthClientID: authClientID,
	}); err != nil {
		return fmt.Errorf("offer auth client: %w", err)
	}
	if err := s.q.ServersInsertVendorAppProtectedResource(ctx, repo.ServersInsertVendorAppProtectedResourceParams{
		ID: v.id(s, "protected-resource"), ProjectID: proj, OrganizationID: org,
		Resource: v.resource, MetadataUrl: v.metadataURL(), Issuer: v.issuer, Scopes: v.scopes,
	}); err != nil {
		return fmt.Errorf("insert protected resource: %w", err)
	}
	return nil
}

// wireLocalVendorApps makes the seeded vendor servers work in the local org.
// The shared seed's metadata document clients cannot work there: a vendor
// fetches the document from Speakeasy and cannot reach a developer's
// localhost. So the seed registers a public client with each vendor by
// dynamic client registration and puts it in that client's place.
//
// A vendor cannot be reached offline, so a failed registration leaves the
// metadata document client in place (signing in then fails at the vendor),
// and the seed goes on.
func wireLocalVendorApps(ctx context.Context, logger *slog.Logger, s *seeder, opts LocalUpstreamOptions) error {
	if opts.Policy == nil {
		logger.InfoContext(ctx, "no egress policy to register vendor clients with; Linear and Notion keep their metadata document clients, which they cannot fetch from localhost")
		return nil
	}
	// Vendors redirect sign-ins, with their authorization codes, to this
	// origin: never register a plain-HTTP callback except on loopback.
	if opts.ServerURL.Scheme != "https" && !isLoopbackHost(opts.ServerURL.Hostname()) {
		logger.WarnContext(ctx, "GRAM_SERVER_URL is plain HTTP on a non-loopback host; Linear and Notion keep their metadata document clients", attr.SlogURLOriginal(opts.ServerURL.String()))
		return nil
	}
	callback := remotesessions.RemoteLoginCallbackURL(opts.ServerURL)
	for _, v := range vendorApps {
		clientID, err := vendorClientID(ctx, s, opts, v, callback)
		if err != nil {
			logger.WarnContext(ctx, "could not register a client with "+v.name+"; it keeps its metadata document client, which it cannot fetch from localhost", attr.SlogError(err))
			continue
		}
		n, err := s.q.LocalVendorAppPointRegisteredClient(ctx, repo.LocalVendorAppPointRegisteredClientParams{
			ClientID: clientID, CallbackBaseUrl: strings.TrimRight(opts.ServerURL.String(), "/"),
			ID: v.id(s, "remote-identity-client"), ProjectID: s.projectID(),
		})
		if err != nil {
			return fmt.Errorf("wire local %s: client: %w", v.name, err)
		}
		if n == 0 {
			return fmt.Errorf("wire local %s: the seeded client matched no rows", v.name)
		}
		logger.InfoContext(ctx, "registered the seeded "+v.name+" server's client", attr.SlogURLOriginal(v.resource))
	}
	return nil
}

// vendorClientID returns the client registered with v for callback: the
// cached one when there is one, otherwise a new registration, which it
// caches.
func vendorClientID(ctx context.Context, s *seeder, opts LocalUpstreamOptions, v vendorApp, callback string) (string, error) {
	path := ""
	if opts.CacheDir != "" && opts.CacheDir != "unset" {
		path = filepath.Join(opts.CacheDir, v.cacheFile())
	}
	if cached, ok := readVendorClient(path); ok && cached.Callback == callback {
		return cached.ClientID, nil
	}

	scope := strings.Join(v.scopes, " ")
	method := "none"
	registered, err := remotesessions.RegisterDynamicClient(ctx, opts.Policy, nil, opts.ServerURL, remotesessions.ProxyRegisterRequest{
		RegistrationEndpoint:    v.issuer + "/register",
		Scope:                   &scope,
		TokenEndpointAuthMethod: &method,
		TunneledMcpServerID:     nil,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("register with %s: %w", v.name, err)
	}
	// A public client only: the client row stores no secret.
	if registered.ClientSecret != "" {
		return "", fmt.Errorf("register with %s: it issued a client secret for a public client", v.name)
	}
	if path != "" {
		if err := writeVendorClient(path, vendorClient{Callback: callback, ClientID: registered.ClientID}); err != nil {
			s.logger.WarnContext(ctx, "could not cache the "+v.name+" client; the next seed registers another", attr.SlogError(err))
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

func readVendorClient(path string) (vendorClient, bool) {
	var c vendorClient
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

func writeVendorClient(path string, c vendorClient) error {
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
