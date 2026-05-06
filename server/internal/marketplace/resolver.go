package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Upstream describes a private GitHub repo that the proxy fronts.
type Upstream struct {
	// Token is the opaque URL-as-secret identifier the proxy serves under.
	Token string
	// Owner and Repo identify the upstream GitHub repository.
	Owner string
	Repo  string
	// Auth is the basic-auth credential pair used to fetch from GitHub over HTTPS.
	// For the prototype this is a personal access token; v1 will mint installation
	// tokens via the GitHub App broker at server/internal/thirdparty/github.
	Auth BasicAuth
}

type BasicAuth struct {
	Username string
	Password string
}

// CloneURL returns the authenticated HTTPS clone URL for the upstream.
func (u Upstream) CloneURL() string {
	return fmt.Sprintf(
		"https://%s:%s@github.com/%s/%s.git",
		u.Auth.Username, u.Auth.Password, u.Owner, u.Repo,
	)
}

// MirrorKey returns a stable filesystem-safe identifier for the upstream's mirror.
func (u Upstream) MirrorKey() string {
	return fmt.Sprintf("%s__%s", u.Owner, u.Repo)
}

var ErrNotFound = errors.New("upstream not found for token")

// Resolver maps an opaque URL token to the upstream repo it fronts.
//
// The prototype uses EnvResolver (single hardcoded fixture from env vars). v1
// will swap this for an implementation backed by plugin_github_connections and
// the GitHub App installation token broker — neither call site nor handler
// changes.
type Resolver interface {
	Resolve(ctx context.Context, token string) (Upstream, error)
}

// EnvResolver is a single-fixture resolver driven by env vars. Intended for the
// prototype only.
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

	r.up = Upstream{
		Token: token,
		Owner: owner,
		Repo:  name,
		// "x-access-token" works as the username for both GitHub App installation
		// tokens and PATs; the password slot carries the actual token.
		Auth: BasicAuth{Username: "x-access-token", Password: pat},
	}
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
