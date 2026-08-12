package mockoidc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type User struct {
	Email         string `yaml:"email"`
	Name          string `yaml:"name"`
	HD            string `yaml:"hd,omitempty"`
	Picture       string `yaml:"picture,omitempty"`
	EmailVerified bool   `yaml:"email_verified"`
}

func (u User) Subject() string {
	sum := sha256.Sum256([]byte(u.Email))
	return hex.EncodeToString(sum[:16])
}

type OAuthClient struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	Name         string   `yaml:"name"`
	RedirectURIs []string `yaml:"redirect_uris"`
}

type ProviderConfig struct {
	Users        []User        `yaml:"users"`
	OAuthClients []OAuthClient `yaml:"oauth_clients"`
}

type Config struct {
	Provider ProviderConfig `yaml:"provider"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Local development gives every worktree its own ports, so a value such as
	// the admin callback URL cannot be hardcoded. os.Expand turns an unset
	// variable into an empty string, which produces a config that parses and
	// then fails much later, so collect the unset names and reject the file.
	var missing []string
	expanded := os.Expand(string(data), func(key string) string {
		val, ok := os.LookupEnv(key)
		if !ok {
			missing = append(missing, key)
			return ""
		}
		return val
	})
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("config references unset environment variables: %s", strings.Join(slices.Compact(missing), ", "))
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if len(cfg.Provider.Users) == 0 {
		return nil, fmt.Errorf("config has no users")
	}
	if len(cfg.Provider.OAuthClients) == 0 {
		return nil, fmt.Errorf("config has no oauth_clients")
	}

	return &cfg, nil
}

func (c *Config) FindUser(email string) (User, bool) {
	for _, u := range c.Provider.Users {
		if u.Email == email {
			return u, true
		}
	}
	return User{}, false
}

func (c *Config) FindClient(clientID string) (OAuthClient, bool) {
	for _, cl := range c.Provider.OAuthClients {
		if cl.ClientID == clientID {
			return cl, true
		}
	}
	return OAuthClient{}, false
}

func (oc OAuthClient) AllowsRedirect(uri string) bool {
	return slices.Contains(oc.RedirectURIs, uri)
}
