package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Upstream describes a private GitHub repo that the proxy fronts and the
// short-lived access token to authenticate against it.
type Upstream struct {
	// Token is the opaque URL-as-secret identifier the proxy serves under.
	// Carried for logging and audit; not part of the GitHub auth.
	Token string
	// Owner and Repo identify the upstream GitHub repository.
	Owner string
	Repo  string
	// AccessToken authenticates against GitHub. For the production resolver
	// this is a GitHub App installation token (~1h TTL); for EnvResolver it's
	// a personal access token. Used as the password slot of HTTP Basic auth
	// (with username "x-access-token") for git Smart HTTP, and as a Bearer
	// token for the REST API.
	AccessToken string
}

var ErrNotFound = errors.New("upstream not found for token")

// Resolver maps an opaque URL token to the upstream repo it fronts.
//
// The production impl is DBResolver (queries plugin_github_connections and
// mints an installation token via the GitHub App broker). EnvResolver is a
// dev/test fallback fixture driven by env vars.
type Resolver interface {
	Resolve(ctx context.Context, token string) (Upstream, error)
}

// EnvResolver is a single-fixture resolver driven by env vars. Useful for
// running the proxy locally against a private repo without needing a DB.
//
// Required env vars:
//
//	GRAM_MARKETPLACE_TOKEN  - the URL token the proxy will accept
//	GRAM_MARKETPLACE_REPO   - "owner/name" of the upstream private repo
//	GRAM_MARKETPLACE_PAT    - GitHub PAT with read access to the repo
type EnvResolver struct {
	once sync.Once
	up   Upstream
	err  error
}

func NewEnvResolver() *EnvResolver { return &EnvResolver{} }

func (r *EnvResolver) load() {
	token := os.Getenv("GRAM_MARKETPLACE_TOKEN")
	repo := os.Getenv("GRAM_MARKETPLACE_REPO")
	pat := os.Getenv("GRAM_MARKETPLACE_PAT")

	if token == "" || repo == "" || pat == "" {
		r.err = errors.New(
			"EnvResolver: GRAM_MARKETPLACE_TOKEN, GRAM_MARKETPLACE_REPO, GRAM_MARKETPLACE_PAT must all be set",
		)
		return
	}

	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		r.err = fmt.Errorf("EnvResolver: GRAM_MARKETPLACE_REPO must be owner/name, got %q", repo)
		return
	}

	r.up = Upstream{Token: token, Owner: owner, Repo: name, AccessToken: pat}
}

func (r *EnvResolver) Resolve(_ context.Context, token string) (Upstream, error) {
	r.once.Do(r.load)
	if r.err != nil {
		return Upstream{}, r.err
	}
	if token != r.up.Token {
		return Upstream{}, ErrNotFound
	}
	return r.up, nil
}
