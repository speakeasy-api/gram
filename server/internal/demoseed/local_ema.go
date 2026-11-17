package demoseed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
)

// localEMAClientSecret is the client secret Speakeasy presents at the dev-idp
// for the Enterprise Sign-In (ID-JAG) Server: the dev-idp EMA app holds the
// same value. It is checked in on purpose, like the fixture's credentials, and
// only ever exists in a developer's local dev-idp and database.
const localEMAClientSecret = "speakeasy-local-ema-client-secret-not-a-secret"

// devIDPOAuth21Prefix is the path the dev-idp serves its OAuth 2.1 identity
// provider under; with the external URL it is the provider's issuer.
const devIDPOAuth21Prefix = "/oauth2-1"

// localEMAProviderName names the company identity provider locally, where the
// dev-idp plays it.
const localEMAProviderName = "Acme Identity (local dev-idp)"

// provisionLocalEMA sets up the dev-idp side of the Enterprise Sign-In
// (ID-JAG) Server: the EMA app, its resource and the trust rule. It runs
// before the local fixtures' transaction opens, so a slow dev-idp never holds
// its locks, and reports whether wireLocalEMA should add the server.
func provisionLocalEMA(ctx context.Context, logger *slog.Logger, opts LocalUpstreamOptions) bool {
	if opts.ServerURL == nil || opts.DevIDPURL == "" || opts.DevIDPURL == "unset" || opts.DevIDPClientID == "" || opts.DevIDPClientID == "unset" {
		logger.InfoContext(ctx, "the server URL, dev-idp URL or login client id is not set; skipping the Enterprise Sign-In (ID-JAG) Server")
		return false
	}
	if opts.HTTPClient == nil {
		logger.WarnContext(ctx, "no HTTP client to reach the dev-idp; skipping the Enterprise Sign-In (ID-JAG) Server")
		return false
	}
	devIDP := strings.TrimRight(opts.DevIDPURL, "/")
	resource := fixtureauth.Resource(fixtureauth.BaseURL(opts.ServerURL), fixtureauth.PathEMAMCP)
	idp := devIDPControl{base: devIDP, client: opts.HTTPClient}
	if err := idp.provisionEMA(ctx, opts.DevIDPClientID, resource, devIDP+devIDPOAuth21Prefix); err != nil {
		logger.WarnContext(ctx, "could not set up the dev-idp for the Enterprise Sign-In (ID-JAG) Server; skipping it", attr.SlogError(err))
		return false
	}
	return true
}

// wireLocalEMA adds the Enterprise Sign-In (ID-JAG) Server to the local org
// and makes it work end to end. The server exists only locally: the demo org
// has no dev-idp or MCP fixture for it to reach.
//
//   - at the dev-idp, Speakeasy's client is an EMA app the developer is
//     assigned to, for a resource whose authorization server trusts the
//     dev-idp's ID-JAGs and guards the fixture's /mcp/ema endpoint;
//   - the company identity provider (Acme Identity in the shared seed)
//     becomes the dev-idp, and its sign-in client the dev-idp's static login
//     client, which accepts Speakeasy's per-client federated callback;
//   - the server gets an organization sign-in of its own that federates
//     through that provider, and its URL, authorization server, client,
//     protected resource and binding point at the fixture and the dev-idp.
//
// A caller signs in at the company provider, and Speakeasy exchanges that
// identity for an ID-JAG and redeems it at the server's authorization server
// for the upstream token.
//
// One client_id serves all three legs: the dev-idp names the requesting app
// as the ID-JAG's client_id, and its resource authorization server requires
// the redeeming client to present that same id. A client ID metadata
// document does not fit: the dev-idp checks the authorize leg's redirect_uri
// against the document, which lists Speakeasy's remote login callback, not
// the per-client federated callback. Dynamic registration would mint a new
// client every run. The login client is exempt from the redirect check and
// is fixed.
//
// A dev-idp that cannot be reached leaves the server out.
//
// wireLocalEMA runs inside the local fixtures' transaction, after
// provisionLocalEMA has made the dev-idp calls. It reports whether it added
// the server, so the caller records its typed upstream auth.
func wireLocalEMA(ctx context.Context, logger *slog.Logger, tx pgx.Tx, opts LocalUpstreamOptions, base string, provisioned bool) error {
	if !provisioned {
		return nil
	}
	devIDP := strings.TrimRight(opts.DevIDPURL, "/")
	identityProvider := devIDP + devIDPOAuth21Prefix
	authServer := fixtureauth.EMAAuthorizationServer(devIDP)
	resource := fixtureauth.Resource(base, fixtureauth.PathEMAMCP)
	scopes := []string{fixtureauth.EMAScope}
	clientID := opts.DevIDPClientID

	spec := LocalSpec()
	s := &seeder{spec: spec, q: repo.New(tx), logger: logger, enc: opts.Encryption, assistantIssuer: "", serverURL: opts.ServerURL, now: time.Now(), signalSequences: nil}
	q := s.q
	org := spec.OrgID
	proj := s.projectID()
	serverURL := strings.TrimRight(opts.ServerURL.String(), "/")
	secret, err := opts.Encryption.Encrypt([]byte(localEMAClientSecret))
	if err != nil {
		return fmt.Errorf("wire local ema: encrypt client secret: %w", err)
	}

	points := []struct {
		what string
		run  func() (int64, error)
	}{
		{"point the identity provider", func() (int64, error) {
			return q.LocalEMAPointIdentityProvider(ctx, repo.LocalEMAPointIdentityProviderParams{Issuer: identityProvider, Name: localEMAProviderName, ID: s.det("ema-idp"), OrganizationID: org})
		}},
		{"point the sign-in client", func() (int64, error) {
			return q.LocalEMAPointIdentityProviderClient(ctx, repo.LocalEMAPointIdentityProviderClientParams{ClientID: clientID, ClientSecretEncrypted: secret, CallbackBaseUrl: serverURL, ID: s.det("ema-idp-client"), OrganizationID: org})
		}},
	}
	for _, step := range points {
		n, err := step.run()
		if err != nil {
			return fmt.Errorf("wire local ema: %s: %w", step.what, err)
		}
		if n == 0 {
			return fmt.Errorf("wire local ema: %s matched no rows", step.what)
		}
	}

	issuer := s.det("issuer-ema-tracker")
	server := s.det("mcpserver-ema-tracker")
	vendor := s.det("ema-vendor-tracker")
	client := s.det("ema-client-tracker")
	inserts := []struct {
		what string
		run  func() error
	}{
		{"insert the organization sign-in", func() error {
			return q.LocalEMAInsertUserSessionIssuer(ctx, repo.LocalEMAInsertUserSessionIssuerParams{ID: issuer, OrgID: org, TrustedIssuerID: s.det("ema-idp"), TrustedClientID: s.det("ema-idp-client")})
		}},
		{"insert the remote server", func() error {
			return q.LocalEMAInsertRemoteMCPServer(ctx, repo.LocalEMAInsertRemoteMCPServerParams{ID: s.det("remotemcp-ema-tracker"), ProjectID: proj, Url: resource})
		}},
		{"insert the server", func() error {
			return q.LocalEMAInsertMCPServer(ctx, repo.LocalEMAInsertMCPServerParams{ID: server, ProjectID: proj, RemoteID: s.det("remotemcp-ema-tracker"), IssuerID: issuer})
		}},
		{"insert the endpoint", func() error {
			return q.LocalEMAInsertEndpoint(ctx, repo.LocalEMAInsertEndpointParams{ID: s.det("endpoint-ema-tracker"), ProjectID: proj, McpServerID: server, Slug: spec.OrgSlug + "-enterprise-sign-in"})
		}},
		{"insert the authorization server", func() error {
			return q.LocalEMAInsertVendorIssuer(ctx, repo.LocalEMAInsertVendorIssuerParams{ID: vendor, ProjectID: proj, OrgID: org, Issuer: authServer, Scopes: scopes})
		}},
		{"insert the authorization server client", func() error {
			return q.LocalEMAInsertVendorClient(ctx, repo.LocalEMAInsertVendorClientParams{ID: client, ProjectID: proj, OrgID: org, IssuerID: vendor, ClientID: clientID, ClientSecretEncrypted: secret, Scopes: scopes})
		}},
		// The client is bound to both organization sign-ins that reach the
		// server: its own federated sign-in, and the workforce sign-in. The
		// server is associated with it: the one client of the authorization
		// server its protected resource names.
		{"bind the client to the sign-in", func() error {
			return q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{RemoteSessionClientID: client, UserSessionIssuerID: issuer})
		}},
		{"bind the client to the workforce sign-in", func() error {
			return q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{RemoteSessionClientID: client, UserSessionIssuerID: s.det("issuer-workforce")})
		}},
		{"associate the server with its client", func() error {
			return q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{ID: s.det("server-client-association-ema-tracker"), ProjectID: proj, OrgID: org, McpServerID: server, RemoteSessionClientID: client})
		}},
		{"stamp the authorization server", func() error {
			return q.ServersStampRemoteSessionIssuer(ctx, repo.ServersStampRemoteSessionIssuerParams{RemoteSessionIssuerID: vendor, McpServerID: server, ProjectID: proj})
		}},
		{"insert the protected resource", func() error {
			return q.LocalEMAInsertProtectedResource(ctx, repo.LocalEMAInsertProtectedResourceParams{
				ID: s.det("protected-resource-ema-tracker"), ProjectID: proj, OrgID: org, Resource: resource,
				MetadataUrl: base + "/.well-known/oauth-protected-resource" + fixtureauth.PathEMAMCP, Issuer: authServer, Scopes: scopes,
			})
		}},
		{"insert the binding", func() error {
			return q.LocalEMAInsertBinding(ctx, repo.LocalEMAInsertBindingParams{ID: s.det("ema-binding-tracker"), ProjectID: proj, OrgID: org, UserSessionIssuerID: issuer, IssuerID: vendor, Resource: resource, ClientID: client})
		}},
	}
	for _, step := range inserts {
		if err := step.run(); err != nil {
			return fmt.Errorf("wire local ema: %s: %w", step.what, err)
		}
	}
	// An organization sign-in reaches every server in the organization, so
	// each seeded one, the expired history row included, chose the server's
	// client, as the shared seed's do for the other servers.
	for i := 1; i <= 3; i++ {
		if err := q.ServersInsertUpstreamClientChoice(ctx, repo.ServersInsertUpstreamClientChoiceParams{
			ID: s.det("org-gateway-choice-" + strconv.Itoa(i) + "-ema-tracker"), ProjectID: proj, OrgID: org,
			CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: s.det("org-gateway-session-" + strconv.Itoa(i)),
			McpServerID: server, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, Source: upstreamclients.SelectionSourceIssuance,
		}); err != nil {
			return fmt.Errorf("wire local ema: record organization sign-in %d's client choice: %w", i, err)
		}
	}

	logger.InfoContext(ctx, "added the Enterprise Sign-In (ID-JAG) Server, signing in at the dev-idp and reaching the local MCP fixture", attr.SlogURLOriginal(resource))
	return nil
}

// devIDPControl calls the dev-idp's unauthenticated management RPCs.
type devIDPControl struct {
	base   string
	client *guardian.HTTPClient
}

type devIDPUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type devIDPEMAApp struct {
	ID           string `json:"id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	JWKS         string `json:"jwks"`
	Enabled      bool   `json:"enabled"`
}

type devIDPEMAResource struct {
	ID                 string `json:"id"`
	Slug               string `json:"slug"`
	ResourceIdentifier string `json:"resource_identifier"`
}

// provisionEMA registers, idempotently, what the dev-idp needs to mint and
// redeem ID-JAGs for the fixture's /mcp/ema endpoint on behalf of the
// developer (the dev-idp's current OAuth 2.1 user).
func (c devIDPControl) provisionEMA(ctx context.Context, clientID, resource, identityProvider string) error {
	var current struct {
		User *devIDPUser `json:"user"`
	}
	if err := c.call(ctx, "devIdp.getCurrentUser", map[string]any{"mode": "oauth2-1"}, &current); err != nil {
		return err
	}
	if current.User == nil || current.User.ID == "" {
		return errors.New("the dev-idp has no current OAuth 2.1 user")
	}

	app, err := c.ensureApp(ctx, clientID)
	if err != nil {
		return err
	}
	res, err := c.ensureResource(ctx, resource)
	if err != nil {
		return err
	}

	// The resource authorization server accepts the dev-idp's own ID-JAGs for
	// this client, narrowed to the fixture's one scope.
	allowed, err := json.Marshal([]string{clientID})
	if err != nil {
		return fmt.Errorf("encode allowed client ids: %w", err)
	}
	// Create upserts on (resource, trusted issuer), so a reseed rewrites the
	// rule's clients, scopes and enabled flag in place.
	if err := c.call(ctx, "emaTrustRules.create", map[string]any{
		"resource_id": res.ID, "trusted_issuer": identityProvider,
		"allowed_client_ids": string(allowed), "allowed_scopes": fixtureauth.EMAScope, "enabled": true,
	}, nil); err != nil {
		return err
	}

	// The assignment is what lets the app act for the developer there.
	return c.call(ctx, "emaAppAssignments.create", map[string]any{
		"app_id": app.ID, "user_id": current.User.ID, "resource_id": res.ID, "granted_scopes": fixtureauth.EMAScope,
	}, nil)
}

func (c devIDPControl) ensureApp(ctx context.Context, clientID string) (devIDPEMAApp, error) {
	var apps struct {
		Items []devIDPEMAApp `json:"items"`
	}
	if err := c.call(ctx, "emaApps.list", map[string]any{"limit": 100}, &apps); err != nil {
		return devIDPEMAApp{}, err
	}
	for _, app := range apps.Items {
		if app.ClientID != clientID {
			continue
		}
		if app.ClientSecret == localEMAClientSecret && app.JWKS == "" && app.Enabled {
			return app, nil
		}
		var updated devIDPEMAApp
		err := c.call(ctx, "emaApps.update", map[string]any{"id": app.ID, "client_secret": localEMAClientSecret, "jwks": "", "enabled": true}, &updated)
		return updated, err
	}
	var created devIDPEMAApp
	err := c.call(ctx, "emaApps.create", map[string]any{
		"client_id": clientID, "client_secret": localEMAClientSecret, "name": "Speakeasy (local)", "enabled": true,
	}, &created)
	return created, err
}

func (c devIDPControl) ensureResource(ctx context.Context, resource string) (devIDPEMAResource, error) {
	var resources struct {
		Items []devIDPEMAResource `json:"items"`
	}
	if err := c.call(ctx, "emaResources.list", map[string]any{"limit": 100}, &resources); err != nil {
		return devIDPEMAResource{}, err
	}
	for _, res := range resources.Items {
		if res.Slug != fixtureauth.EMAResourceSlug {
			continue
		}
		if res.ResourceIdentifier == resource {
			return res, nil
		}
		var updated devIDPEMAResource
		err := c.call(ctx, "emaResources.update", map[string]any{"id": res.ID, "resource_identifier": resource}, &updated)
		return updated, err
	}
	var created devIDPEMAResource
	err := c.call(ctx, "emaResources.create", map[string]any{
		"slug": fixtureauth.EMAResourceSlug, "name": "Speakeasy MCP fixture", "resource_identifier": resource,
	}, &created)
	return created, err
}

// call posts payload to the dev-idp RPC method and decodes the result into
// out, when out is not nil.
func (c devIDPControl) call(ctx context.Context, method string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dev-idp %s: encode payload: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/rpc/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dev-idp %s: build request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("dev-idp %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("dev-idp %s: read response: %w", method, err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dev-idp %s: %s: %s", method, resp.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("dev-idp %s: decode response: %w", method, err)
	}
	return nil
}
