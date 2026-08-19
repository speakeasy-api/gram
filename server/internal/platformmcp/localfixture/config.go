// Package localfixture owns the code-defined reviewed provider used only by the
// explicit local Platform MCP fixture composition.
package localfixture

import (
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

const (
	ProviderKey           = "local-fixture"
	CanonicalRef          = "local-fixture/reviewed-mcp"
	SetupIntent           = "provider_setup"
	OAuthClientName       = "Speakeasy AICP Platform MCP local fixture"
	SetupGuideVersion     = "local-fixture-v1"
	fixtureOAuthPath      = "platform-mcp/local-fixture"
	fixtureIssuerSlug     = "platform-mcp-local-fixture"
	registryIDString      = "6d4b23d1-75b2-4f7e-a0c2-0c4453551f72"
	remoteSessionIssuerID = "28f5abf8-2c45-4ab4-8c5b-9946a54828fd"
)

type Config struct {
	origin                *url.URL
	registry              externalmcp.Registry
	remoteURL             string
	remoteSessionIssuerID uuid.UUID
}

func NewConfig(origin *url.URL) (*Config, error) {
	if err := ValidateOrigin(origin); err != nil {
		return nil, err
	}

	registryID, err := uuid.Parse(registryIDString)
	if err != nil {
		return nil, fmt.Errorf("parse local Platform MCP fixture registry ID: %w", err)
	}
	issuerID, err := uuid.Parse(remoteSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("parse local Platform MCP fixture remote-session issuer ID: %w", err)
	}
	originCopy := *origin
	originCopy.Path = ""
	originCopy.RawPath = ""
	return &Config{
		origin: &originCopy,
		registry: externalmcp.Registry{
			ID:  registryID,
			URL: originCopy.String(),
		},
		remoteURL:             originCopy.JoinPath("platform-mcp", "local-fixture", "mcp").String(),
		remoteSessionIssuerID: issuerID,
	}, nil
}

// ValidateOrigin verifies that origin is a canonical HTTPS origin suitable for
// the local Platform MCP fixture.
func ValidateOrigin(origin *url.URL) error {
	if origin == nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return fmt.Errorf("local Platform MCP fixture requires an HTTPS origin without credentials, path, query, or fragment")
	}
	return nil
}

func (c *Config) Origin() *url.URL {
	if c == nil || c.origin == nil {
		return nil
	}
	originCopy := *c.origin
	return &originCopy
}

func (c *Config) Registry() externalmcp.Registry {
	if c == nil {
		return externalmcp.Registry{
			ID:  uuid.Nil,
			URL: "",
		}
	}
	return c.registry
}

func (c *Config) RemoteSessionIssuerID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.remoteSessionIssuerID
}

func (c *Config) RemoteURL() string {
	if c == nil {
		return ""
	}
	return c.remoteURL
}

func (c *Config) OAuthIssuerURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath(fixtureOAuthPath).String()
}

func (c *Config) OAuthAuthorizationServerMetadataURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath(".well-known", "oauth-authorization-server", fixtureOAuthPath).String()
}

func (c *Config) OAuthAuthorizationURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath(fixtureOAuthPath, "authorize").String()
}

func (c *Config) OAuthTokenURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath(fixtureOAuthPath, "token").String()
}

func (c *Config) OAuthRegistrationURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath(fixtureOAuthPath, "register").String()
}

func (c *Config) OAuthRevocationURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath(fixtureOAuthPath, "revoke").String()
}

func (c *Config) RemoteLoginCallbackURL() string {
	if c == nil || c.origin == nil {
		return ""
	}
	return c.origin.JoinPath("mcp", "remote_login_callback").String()
}

func (c *Config) RegistryDetailsPath() string {
	if c == nil {
		return ""
	}
	registryURL, err := url.Parse(c.registry.URL)
	if err != nil {
		return ""
	}
	return "/" + registryURL.JoinPath("v0.1", "servers", url.PathEscape(CanonicalRef), "versions", "latest").EscapedPath()
}

// SetupResources returns the fixture's own guide. It is kept alongside the
// reviewed mcp-setup-docs corpus rather than replaced by it: the fixture
// provider does not exist upstream, and the local composition must be able to
// exercise the setup handoff without depending on a real provider's guide.
//
// Its observation date tracks the clock so the fixture never drifts into the
// stale or withheld paths that a real guide is subject to — a local fixture
// that expires would fail the local flow for reasons unrelated to the change
// under test.
func (c *Config) SetupResources() []platformmcp.SetupResource {
	observedAt := time.Now().UTC().Truncate(24 * time.Hour)
	body := "## Complete the fixture setup handoff\n\n" +
		"1. Start the authenticated dashboard setup handoff returned by `get_setup_handoff`.\n" +
		"2. Complete the local synthetic authorization page.\n" +
		"3. Recheck authenticated readiness after the completion landing page.\n\n" +
		"Trusted links: use only the same-origin dashboard handoff and completion route returned by the AI Control Plane.\n"
	return []platformmcp.SetupResource{{
		URI:         platformmcp.SetupResourceURI(ProviderKey, SetupIntent),
		Name:        "local-fixture-provider-setup",
		Title:       "Local fixture provider setup",
		Description: "Reviewed local-only setup instructions for the synthetic Platform MCP fixture.",
		Text:        "# Local fixture provider setup\n\n" + body,
		Body:        body,

		Provider:     ProviderKey,
		Intent:       SetupIntent,
		Owner:        "Speakeasy AICP Platform MCP",
		Source:       SetupGuideVersion,
		ObservedAt:   observedAt,
		RevalidateBy: observedAt.AddDate(0, 0, 90),
		Aliases:      []string{CanonicalRef},
		Links:        nil,
		// The fixture provider is synthetic, so it has no published page.
		DocsURL: "",
	}}
}

func (c *Config) CatalogDescriptor() platformmcp.CatalogDescriptor {
	if c == nil {
		return platformmcp.CatalogDescriptor{
			ProviderKey:      "",
			Registry:         externalmcp.Registry{ID: uuid.Nil, URL: ""},
			CanonicalRef:     "",
			AllowedRemoteURL: "",
			SetupIntent:      "",
		}
	}
	return platformmcp.CatalogDescriptor{
		ProviderKey:      ProviderKey,
		Registry:         c.registry,
		CanonicalRef:     CanonicalRef,
		AllowedRemoteURL: c.remoteURL,
		SetupIntent:      SetupIntent,
	}
}
