package relay

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// credSource records where the effective hooks key came from, which governs
// the 401/403 recovery path: only a cache-sourced key is forgotten and
// re-minted, never an explicit env key.
type credSource int

const (
	credNone credSource = iota
	credEnv
	credCache
	credOrg
)

// creds is the resolved credential used to authenticate an ingest request.
type creds struct {
	ServerURL string
	APIKey    string
	Project   string
	Email     string
	Org       string
	Source    credSource
}

// authFilePath returns the hooks credential cache location: the
// GRAM_HOOKS_AUTH_FILE override, else $XDG_CONFIG_HOME/gram/hooks-auth.env.
func authFilePath() string {
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_AUTH_FILE")); v != "" {
		return v
	}
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "gram", "hooks-auth.env")
}

// readAuthFile parses the cache file into a key/value map. Missing files yield
// an empty map without error so callers can treat "no cache" uniformly.
func readAuthFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if _, seen := out[key]; !seen {
			out[key] = val
		}
	}
	return out, sc.Err()
}

// readCachedAuth loads cached credentials, enforcing that the cache was minted
// for this server and, when both are known, this org. A cache from a different
// org must not authenticate — with shared slugs like "default" its events would
// enforce another org's policies. Within the org the key is shared, but the
// project always comes from this plugin's config: a cache minted in another
// workspace must not route this workspace's events to its project.
func readCachedAuth(cfg Config) (creds, bool) {
	values, err := readAuthFile(authFilePath())
	if err != nil {
		return creds{}, false
	}
	c := creds{
		ServerURL: values["server_url"],
		APIKey:    values["api_key"],
		Project:   values["project"],
		Email:     values["email"],
		Org:       values["org"],
		Source:    credCache,
	}
	if c.ServerURL != cfg.ServerURL || c.APIKey == "" {
		return creds{}, false
	}
	if cfg.OrgID != "" && c.Org != "" && c.Org != cfg.OrgID {
		return creds{}, false
	}
	if cfg.ProjectSlug != "" {
		c.Project = cfg.ProjectSlug
	}
	return c, true
}

// resolveAuth returns the effective credential: an explicit env key wins over
// the cache. Only GRAM_HOOKS_API_KEY is honored — the generic GRAM_API_KEY is
// a different product surface (MCP access) and must not silently authenticate
// hook telemetry. The second return is false when the machine holds no
// credential.
func resolveAuth(cfg Config) (creds, bool) {
	apiKey := strings.TrimSpace(os.Getenv("GRAM_HOOKS_API_KEY"))
	if apiKey != "" {
		project := cfg.ProjectSlug
		if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_PROJECT_SLUG")); v != "" {
			project = v
		}
		return creds{
			ServerURL: cfg.ServerURL,
			APIKey:    apiKey,
			Project:   project,
			Email:     "",
			Org:       cfg.OrgID,
			Source:    credEnv,
		}, true
	}
	if cached, ok := readCachedAuth(cfg); ok {
		return cached, true
	}
	if apiKey := strings.TrimSpace(cfg.HooksAPIKey); apiKey != "" {
		return creds{
			ServerURL: cfg.ServerURL,
			APIKey:    apiKey,
			Project:   cfg.ProjectSlug,
			Email:     "",
			Org:       cfg.OrgID,
			Source:    credOrg,
		}, true
	}
	return creds{}, false
}

// writeAuth atomically persists credentials with 0600 permissions and marks the
// machine as established (the fail-closed ratchet). Values carrying line breaks
// are rejected: the cache is line-oriented and a value from the sign-in
// callback must not be able to inject extra keys. Embedded "=" is fine — the
// parser splits on the first one only.
func writeAuth(c creds) error {
	for _, v := range []string{c.ServerURL, c.APIKey, c.Project, c.Email, c.Org} {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("credential value contains a line break")
		}
	}

	path := authFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}

	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	body := fmt.Sprintf("server_url=%s\napi_key=%s\nproject=%s\nemail=%s\norg=%s\n",
		c.ServerURL, c.APIKey, c.Project, c.Email, c.Org)
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write auth cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit auth cache: %w", err)
	}
	markEstablished()
	clearReauthNeeded()
	return nil
}

// forgetAuth removes the cached credential but leaves the established marker so
// a forgotten or invalidated key cannot silently disable enforcement.
func forgetAuth() {
	_ = os.Remove(authFilePath())
}

// authEstablished reports whether this machine has ever cached hook
// credentials. Before the first success, blocking paths fail open; afterwards
// they fail closed. The marker survives forgetAuth.
func authEstablished() bool {
	if _, err := os.Stat(authFilePath() + ".established"); err == nil {
		return true
	}
	_, err := os.Stat(authFilePath())
	return err == nil
}

func markEstablished() {
	f, err := os.OpenFile(authFilePath()+".established", os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_ = f.Close()
	}
}

// The reauth-needed marker records that a cached key was rejected by the
// server and cleared: prompt submissions keep nudging the user to reconnect
// (fail open) instead of blocking on a credential the machine no longer holds,
// while tool events still fail closed. It is cleared by the next successful
// sign-in.
func markReauthNeeded() {
	f, err := os.OpenFile(authFilePath()+".reauth-needed", os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_ = f.Close()
	}
}

func reauthNeeded() bool {
	_, err := os.Stat(authFilePath() + ".reauth-needed")
	return err == nil
}

func clearReauthNeeded() {
	_ = os.Remove(authFilePath() + ".reauth-needed")
}

// insecureServerURL reports whether sending credentials to serverURL would use
// plaintext HTTP to a non-loopback host. Only exact loopback hosts are exempt
// (local dev servers); everything else must be HTTPS so a hooks key never
// crosses the network in the clear.
func insecureServerURL(serverURL string) bool {
	if strings.HasPrefix(serverURL, "https://") {
		return false
	}
	if !strings.HasPrefix(serverURL, "http://") {
		return true
	}
	// The parsed host decides — a prefix check reads the userinfo trick
	// http://localhost:pw@evil.example as loopback.
	u, err := url.Parse(serverURL)
	if err != nil {
		return true
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return false
	}
	return true
}
