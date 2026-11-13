// Package githubapp obtains renewable credentials for customer-owned GitHub Apps.
package githubapp

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/httpsorigin"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

// Credential binds renewable material to the only origin allowed to receive it.
type Credential struct {
	ClientCredential
	Origin string
}

type Provider struct {
	mu             sync.Mutex
	now            func() time.Time
	tracerProvider trace.TracerProvider
	queries        *repo.Queries
	enc            *encryption.Client
	http           *guardian.HTTPClient
	tokens         *expirable.LRU[string, ClientCredential]
	flights        singleflight.Group
}

func New(db repo.DBTX, enc *encryption.Client, policy *guardian.Policy, tracerProvider trace.TracerProvider) *Provider {
	return &Provider{mu: sync.Mutex{}, now: time.Now, tracerProvider: tracerProvider, flights: singleflight.Group{}, queries: repo.New(db), enc: enc,
		http:   policy.PooledClient(guardian.WithCheckRedirect(func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })),
		tokens: expirable.NewLRU[string, ClientCredential](1024, nil, time.Hour),
	}
}

// Verify proves the supplied key can mint for this installation. No token or
// private key escapes to the configuration response.
func (p *Provider) Verify(ctx context.Context, instance string, appID, installationID int64, pem string) error {
	client, err := github.NewInstanceClient(appID, []byte(pem), instance, p.http, p.tracerProvider)
	if err != nil {
		return fmt.Errorf("initialize GitHub App verifier: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, _, err = client.MintInstallationToken(ctx, installationID)
	if err != nil {
		return fmt.Errorf("verify GitHub App installation: %w", err)
	}
	return nil
}

// Credential checks live tenant, client, and destination state before every
// cache lookup. Disabling a client or rotating its key cannot reuse cached material.
// The live server attachment is checked with the credential. Callers must
// authorize mcp:auth before requesting or renewing a credential.
func (p *Provider) Credential(ctx context.Context, organizationID string, clientID, serverID uuid.UUID) (Credential, error) {
	var none Credential
	config, err := p.queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: clientID, OrganizationID: organizationID, McpServerID: serverID})
	if err != nil {
		return none, fmt.Errorf("load GitHub App credential: %w", err)
	}
	target, err := p.queries.GetGitHubAppServerURL(ctx, repo.GetGitHubAppServerURLParams{McpServerID: serverID, OrganizationID: organizationID})
	if err != nil {
		return none, fmt.Errorf("load GitHub App destination: %w", err)
	}
	origin, err := httpsorigin.FromURL(target)
	if err != nil || origin != config.UpstreamOrigin {
		return none, fmt.Errorf("MCP destination does not match the GitHub App credential's allowed origin")
	}
	key := organizationID + ":" + clientID.String() + ":" + config.CredentialVersion.String()
	if token, ok := p.tokens.Get(key); ok && p.now().Before(token.ExpiresAt()) {
		return Credential{ClientCredential: token, Origin: config.UpstreamOrigin}, nil
	}
	result := p.flights.DoChan(key, func() (any, error) {
		if token, ok := p.tokens.Get(key); ok && p.now().Before(token.ExpiresAt()) {
			return token, nil
		}
		pem, err := p.enc.Decrypt(config.PrivateKeyEncrypted)
		if err != nil {
			return none, fmt.Errorf("decrypt GitHub App key: %w", err)
		}
		client, err := github.NewInstanceClient(config.AppID, []byte(pem), config.InstanceUrl, p.http, p.tracerProvider)
		if err != nil {
			return none, fmt.Errorf("mint GitHub App credential: %w", err)
		}
		mintCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		value, expires, err := client.MintInstallationToken(mintCtx, config.InstallationID)
		if err != nil {
			return none, fmt.Errorf("mint GitHub App credential: %w", err)
		}
		minted := p.now()
		token := newClientCredential(value, expires.Add(-time.Minute), func(context.Context) (bool, error) {
			if p.now().Sub(minted) < 30*time.Second {
				return false, nil
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if current, ok := p.tokens.Get(key); ok && current.Value() == value {
				p.tokens.Remove(key)
			}
			return true, nil
		})
		p.mu.Lock()
		p.tokens.Add(key, token)
		p.mu.Unlock()
		return token, nil
	})
	select {
	case <-ctx.Done():
		return none, fmt.Errorf("wait for GitHub App credential: %w", ctx.Err())
	case result := <-result:
		if result.Err != nil {
			return none, result.Err
		}
		token, ok := result.Val.(ClientCredential)
		if !ok {
			return none, fmt.Errorf("unexpected GitHub App credential result")
		}
		return Credential{ClientCredential: token, Origin: config.UpstreamOrigin}, nil
	}
}
