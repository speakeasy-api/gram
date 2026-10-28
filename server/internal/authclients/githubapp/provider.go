// Package githubapp obtains renewable credentials for customer-owned GitHub Apps.
package githubapp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"golang.org/x/sync/singleflight"
)

// Credential binds renewable material to the only origin allowed to receive it.
type Credential struct {
	remotesessions.ClientCredential
	Origin string
}

type Provider struct {
	queries *repo.Queries
	enc     *encryption.Client
	http    *guardian.HTTPClient
	tokens  *expirable.LRU[string, remotesessions.ClientCredential]
	flights singleflight.Group
}

func New(db repo.DBTX, enc *encryption.Client, policy *guardian.Policy) *Provider {
	return &Provider{queries: repo.New(db), enc: enc,
		http:   policy.PooledClient(guardian.WithCheckRedirect(func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })),
		tokens: expirable.NewLRU[string, remotesessions.ClientCredential](1024, nil, time.Hour),
	}
}

// Origin requires an explicit HTTPS origin, not an arbitrary path or userinfo.
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return "", fmt.Errorf("expected an HTTPS origin without credentials, path, query, or fragment")
	}
	return "https://" + strings.TrimSuffix(strings.ToLower(u.Host), ":443"), nil
}

// Verify proves the supplied key can mint for this installation. No token or
// private key escapes to the configuration response.
func (p *Provider) Verify(ctx context.Context, instance string, appID, installationID int64, pem string) error {
	client, err := github.NewInstanceClient(appID, []byte(pem), instance, p.http)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, _, err = client.MintInstallationToken(ctx, installationID)
	return err
}

// Credential checks live tenant, client, and destination state before every
// cache lookup. Disabling a client or rotating its key cannot reuse cached material.
// Callers must authorize mcp:auth and validate the server attachment first.
func (p *Provider) Credential(ctx context.Context, organizationID string, clientID, serverID uuid.UUID) (Credential, error) {
	var none Credential
	config, err := p.queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: clientID, OrganizationID: organizationID})
	if err != nil {
		return none, fmt.Errorf("load GitHub App credential: %w", err)
	}
	target, err := p.queries.GetGitHubAppServerURL(ctx, repo.GetGitHubAppServerURLParams{McpServerID: serverID, OrganizationID: organizationID})
	if err != nil {
		return none, fmt.Errorf("load GitHub App destination: %w", err)
	}
	u, err := url.Parse(target)
	if err != nil || u.User != nil || u.Scheme != "https" || "https://"+strings.TrimSuffix(strings.ToLower(u.Host), ":443") != config.UpstreamOrigin {
		return none, fmt.Errorf("MCP destination does not match the GitHub App credential's allowed origin")
	}
	key := organizationID + ":" + clientID.String() + ":" + config.CredentialVersion.String()
	if token, ok := p.tokens.Get(key); ok && time.Now().Before(token.ExpiresAt()) {
		return Credential{ClientCredential: token, Origin: config.UpstreamOrigin}, nil
	}
	result := p.flights.DoChan(key, func() (any, error) {
		if token, ok := p.tokens.Get(key); ok && time.Now().Before(token.ExpiresAt()) {
			return token, nil
		}
		pem, err := p.enc.Decrypt(config.PrivateKeyEncrypted)
		if err != nil {
			return none, fmt.Errorf("decrypt GitHub App key: %w", err)
		}
		client, err := github.NewInstanceClient(config.AppID, []byte(pem), config.InstanceUrl, p.http)
		if err != nil {
			return none, err
		}
		mintCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		value, expires, err := client.MintInstallationToken(mintCtx, config.InstallationID)
		if err != nil {
			return none, err
		}
		minted := time.Now()
		token := remotesessions.NewClientCredential(value, remotesessions.ClientCredentialSchemeBearer, expires.Add(-time.Minute), func(context.Context) (bool, error) {
			if time.Since(minted) < 30*time.Second {
				return false, nil
			}
			p.tokens.Remove(key)
			return true, nil
		})
		p.tokens.Add(key, token)
		return token, nil
	})
	select {
	case <-ctx.Done():
		return none, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return none, result.Err
		}
		return Credential{ClientCredential: result.Val.(remotesessions.ClientCredential), Origin: config.UpstreamOrigin}, nil
	}
}
