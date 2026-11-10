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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
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

// wireLocalEMA makes the Enterprise Sign-In (ID-JAG) Server work end to end
// in the local org:
//
//   - at the dev-idp, Speakeasy's client is an EMA app the developer is
//     assigned to, for a resource whose authorization server trusts the
//     dev-idp's ID-JAGs and guards the fixture's /mcp/ema endpoint;
//   - the server's company identity provider (Acme Identity in the shared
//     seed) becomes the dev-idp, and its sign-in client the dev-idp's static
//     login client, which accepts Speakeasy's per-client federated callback;
//   - the server's URL, authorization server, client, protected resource,
//     binding and typed auth client move from the shared seed's placeholder
//     hosts to the fixture and the dev-idp.
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
// A dev-idp that cannot be reached leaves the server display-only.
func wireLocalEMA(ctx context.Context, logger *slog.Logger, tx pgx.Tx, opts LocalMCPFixtureOptions, base string) error {
	if opts.DevIDPURL == "" || opts.DevIDPURL == "unset" || opts.DevIDPClientID == "" || opts.DevIDPClientID == "unset" {
		logger.InfoContext(ctx, "the dev-idp URL or login client id is not set; the Enterprise Sign-In (ID-JAG) Server stays display-only")
		return nil
	}
	if opts.HTTPClient == nil {
		return errors.New("wire local ema: an HTTP client is required to reach the dev-idp")
	}
	devIDP := strings.TrimRight(opts.DevIDPURL, "/")
	identityProvider := devIDP + devIDPOAuth21Prefix
	authServer := fixtureauth.EMAAuthorizationServer(devIDP)
	resource := fixtureauth.Resource(base, fixtureauth.PathEMAMCP)
	scopes := []string{fixtureauth.EMAScope}
	clientID := opts.DevIDPClientID

	idp := devIDPControl{base: devIDP, client: opts.HTTPClient}
	if err := idp.provisionEMA(ctx, clientID, resource, identityProvider); err != nil {
		logger.WarnContext(ctx, "could not set up the dev-idp for the Enterprise Sign-In (ID-JAG) Server; it stays display-only", attr.SlogError(err))
		return nil
	}

	spec := LocalSpec()
	s := &seeder{spec: spec, q: repo.New(tx), logger: logger, enc: opts.Encryption, now: time.Now(), signalSequences: nil}
	q := s.q
	org := spec.OrgID
	proj := s.projectID()
	serverURL := strings.TrimRight(opts.ServerURL.String(), "/")
	secret, err := opts.Encryption.Encrypt([]byte(localEMAClientSecret))
	if err != nil {
		return fmt.Errorf("wire local ema: encrypt client secret: %w", err)
	}

	steps := []struct {
		what string
		run  func() (int64, error)
	}{
		{"point the identity provider", func() (int64, error) {
			return q.LocalEMAPointIdentityProvider(ctx, repo.LocalEMAPointIdentityProviderParams{Issuer: identityProvider, Name: localEMAProviderName, ID: s.det("ema-idp"), OrganizationID: org})
		}},
		{"point the sign-in client", func() (int64, error) {
			return q.LocalEMAPointIdentityProviderClient(ctx, repo.LocalEMAPointIdentityProviderClientParams{ClientID: clientID, ClientSecretEncrypted: secret, CallbackBaseUrl: serverURL, ID: s.det("ema-idp-client"), OrganizationID: org})
		}},
		{"point the remote server", func() (int64, error) {
			return q.LocalFixturePointRemoteServer(ctx, repo.LocalFixturePointRemoteServerParams{Url: resource, ID: s.det("remotemcp-enterprise-sign-in"), ProjectID: proj})
		}},
		{"point the authorization server", func() (int64, error) {
			return q.LocalEMAPointVendorIssuer(ctx, repo.LocalEMAPointVendorIssuerParams{Issuer: authServer, Scopes: scopes, ID: s.det("ema-vendor-enterprise-sign-in"), ProjectID: proj})
		}},
		{"point the authorization server client", func() (int64, error) {
			return q.LocalEMAPointVendorClient(ctx, repo.LocalEMAPointVendorClientParams{ClientID: clientID, ClientSecretEncrypted: secret, Scopes: scopes, ID: s.det("ema-client-enterprise-sign-in"), ProjectID: proj})
		}},
		{"point the protected resource", func() (int64, error) {
			return q.LocalEMAPointProtectedResource(ctx, repo.LocalEMAPointProtectedResourceParams{
				Resource: resource, MetadataUrl: base + "/.well-known/oauth-protected-resource" + fixtureauth.PathEMAMCP,
				Issuer: authServer, Scopes: scopes, ID: s.det("protected-resource-enterprise-sign-in"), ProjectID: proj,
			})
		}},
		{"point the binding", func() (int64, error) {
			return q.LocalEMAPointBinding(ctx, repo.LocalEMAPointBindingParams{Resource: resource, ID: s.det("ema-binding-enterprise-sign-in"), ProjectID: proj})
		}},
		{"point the typed auth client", func() (int64, error) {
			return q.LocalEMAPointAuthClient(ctx, repo.LocalEMAPointAuthClientParams{Resource: resource, RemoteSessionClientID: s.det("ema-client-enterprise-sign-in"), OrganizationID: org})
		}},
	}
	for _, step := range steps {
		n, err := step.run()
		if err != nil {
			return fmt.Errorf("wire local ema: %s: %w", step.what, err)
		}
		if n == 0 {
			return fmt.Errorf("wire local ema: %s matched no rows", step.what)
		}
	}

	logger.InfoContext(ctx, "pointed the Enterprise Sign-In (ID-JAG) Server at the dev-idp and the local MCP fixture", attr.SlogURLOriginal(resource))
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

type devIDPEMATrustRule struct {
	ID string `json:"id"`
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
	var rule devIDPEMATrustRule
	if err := c.call(ctx, "emaTrustRules.create", map[string]any{
		"resource_id": res.ID, "trusted_issuer": identityProvider,
		"allowed_client_ids": string(allowed), "allowed_scopes": fixtureauth.EMAScope, "enabled": true,
	}, &rule); err != nil {
		return err
	}
	if err := c.call(ctx, "emaTrustRules.update", map[string]any{
		"id": rule.ID, "allowed_client_ids": string(allowed), "allowed_scopes": fixtureauth.EMAScope, "enabled": true,
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
