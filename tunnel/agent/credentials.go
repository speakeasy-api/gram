package agent

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/tunnel/jwks"
)

// Credentials mode delivers each Speakeasy user's upstream access token to
// their own stdio server process through a per-session file.
const (
	// AccessTokenFileEnv names the child variable holding its token file path.
	AccessTokenFileEnv = "SPEAKEASY_ACCESS_TOKEN_FILE"

	// defaultCredentialsRoot is where session token files live: a tmpfs on
	// common Linux hosts and in Docker containers.
	defaultCredentialsRoot = "/dev/shm"

	// defaultCredentialMaxAge bounds how long a session keeps a token whose
	// upstream stated no expiry, measured from the last admitted write, so a
	// revoked credential cannot outlive its revocation indefinitely.
	defaultCredentialMaxAge = time.Hour

	// maxCredentialMaxAge caps the configured maximum age, so no setting
	// lets a token outlive its revocation for longer than a day.
	maxCredentialMaxAge = 24 * time.Hour

	// credentialExpiryGrace keeps a session past its token's expiry long
	// enough for a request carrying the refreshed token to arrive.
	credentialExpiryGrace = 30 * time.Second
)

// inheritedCredentialEnv are child variables credentials mode replaces, so an
// inherited value cannot point a child at a shared credential or home.
// OKTA_ACCESS_TOKEN_FILE is the Okta MCP server's own token-file alias.
var inheritedCredentialEnv = []string{
	AccessTokenFileEnv,
	"OKTA_ACCESS_TOKEN_FILE",
	"HOME",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_STATE_HOME",
}

// CredentialsConfig enables per-user upstream credentials for a stdio server.
type CredentialsConfig struct {
	// Issuer is the exact `iss` of trusted caller assertions: an origin
	// without a path or trailing slash.
	Issuer string

	// Audience is the exact `aud` of trusted caller assertions: the tunneled
	// source's saved resource identifier, or tunneled-mcp-server:<ID>.
	Audience string

	// OrganizationID is the exact `organization_id` of trusted assertions.
	OrganizationID string

	// JWKSURL is where verification keys are fetched. Empty uses the
	// issuer's /.well-known/jwks.json.
	JWKSURL string

	// AllowInsecure admits http:// issuer and JWKS URLs on loopback hosts and
	// host.docker.internal, for local development only.
	AllowInsecure bool

	// Root is the memory-backed directory session token files live under.
	// Empty uses /dev/shm.
	Root string

	// MaxAge bounds how long a session keeps a token without a newer one.
	// Zero uses one hour.
	MaxAge time.Duration
}

// normalize validates the configuration and fills in defaults.
func (c CredentialsConfig) normalize() (CredentialsConfig, error) {
	if strings.TrimSpace(c.Audience) == "" {
		return c, errors.New("TUNNEL_IDENTITY_AUDIENCE is required in credentials mode")
	}
	if strings.TrimSpace(c.OrganizationID) == "" {
		return c, errors.New("TUNNEL_IDENTITY_ORGANIZATION_ID is required in credentials mode")
	}
	issuer, err := parseTrustedURL(c.Issuer, c.AllowInsecure)
	if err != nil {
		return c, fmt.Errorf("TUNNEL_IDENTITY_ISSUER: %w", err)
	}
	if issuer.Path != "" || issuer.RawQuery != "" || issuer.ForceQuery {
		return c, errors.New("TUNNEL_IDENTITY_ISSUER must be an origin without a path, query, or trailing slash")
	}
	if c.JWKSURL == "" {
		c.JWKSURL = c.Issuer + jwks.Path
	}
	keysURL, err := parseTrustedURL(c.JWKSURL, c.AllowInsecure)
	if err != nil {
		return c, fmt.Errorf("TUNNEL_IDENTITY_JWKS_URL: %w", err)
	}
	if keysURL.RawQuery != "" || keysURL.ForceQuery {
		return c, errors.New("TUNNEL_IDENTITY_JWKS_URL must not carry a query")
	}
	if c.Root == "" {
		c.Root = defaultCredentialsRoot
	}
	if !filepath.IsAbs(c.Root) || filepath.Clean(c.Root) != c.Root {
		return c, errors.New("TUNNEL_STDIO_CREDENTIALS_DIR must be a clean absolute path")
	}
	if c.MaxAge < 0 || c.MaxAge > maxCredentialMaxAge {
		return c, fmt.Errorf("TUNNEL_STDIO_CREDENTIALS_MAX_AGE must be positive and at most %s", maxCredentialMaxAge)
	}
	if c.MaxAge == 0 {
		c.MaxAge = defaultCredentialMaxAge
	}
	if _, set := os.LookupEnv(AccessTokenFileEnv); set {
		return c, errors.New(AccessTokenFileEnv + " must not be set for the tunnel agent; credentials mode sets it for each server process")
	}
	return c, nil
}

// parseTrustedURL accepts https, or http on a local host when allowed.
func parseTrustedURL(raw string, allowInsecure bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Opaque != "" {
		return nil, errors.New("must be an absolute URL with a host")
	}
	if u.User != nil || u.Fragment != "" || u.RawFragment != "" {
		return nil, errors.New("must not carry user information or a fragment")
	}
	if u.Path != "" && path.Clean(u.Path) != u.Path {
		return nil, errors.New("must have a clean path")
	}
	switch u.Scheme {
	case "https":
		return u, nil
	case "http":
		if allowInsecure && isLocalGatewayHost(u.Hostname()) {
			return u, nil
		}
		return nil, errors.New("must use https:// (http:// only on a local host with TUNNEL_IDENTITY_ALLOW_INSECURE=true)")
	default:
		return nil, errors.New("must use https://")
	}
}

// credentialChildEnv replaces inherited credential variables with the
// session's own token file and home. Package caches stay outside the
// session: unless the agent's environment sets them, they default under the
// agent's own home, so a server installed on first use is downloaded once
// rather than into every session's memory-backed directory.
func credentialChildEnv(base []string, tokenPath, home string) []string {
	cacheEnv := sharedCacheEnv(base)
	env := make([]string, 0, len(base)+len(inheritedCredentialEnv)+len(cacheEnv))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		// An empty XDG_CACHE_HOME means unset; sharedCacheEnv supplies it.
		replaced := slices.Contains(inheritedCredentialEnv, name) || kv == "XDG_CACHE_HOME="
		if !replaced {
			env = append(env, kv)
		}
	}
	env = append(env, cacheEnv...)
	return append(env,
		AccessTokenFileEnv+"="+tokenPath,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
	)
}

// sharedCacheEnv returns cache variables to add for the child: XDG_CACHE_HOME
// and npm's cache under the agent's own home when the agent's environment
// does not set them. Without an absolute agent home it adds nothing, and
// operators set the cache locations explicitly.
func sharedCacheEnv(base []string) []string {
	lookup := func(name string) string {
		for _, kv := range base {
			if k, v, ok := strings.Cut(kv, "="); ok && k == name {
				return v
			}
		}
		return ""
	}
	var add []string
	cache := lookup("XDG_CACHE_HOME")
	if cache == "" {
		agentHome := lookup("HOME")
		if !filepath.IsAbs(agentHome) {
			return nil
		}
		cache = filepath.Join(agentHome, ".cache")
		add = append(add, "XDG_CACHE_HOME="+cache)
	}
	if lookup("npm_config_cache") == "" && lookup("NPM_CONFIG_CACHE") == "" && filepath.IsAbs(cache) {
		add = append(add, "npm_config_cache="+filepath.Join(cache, "npm"))
	}
	return add
}
