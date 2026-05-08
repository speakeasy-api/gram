package marketplace

import (
	"context"
	"errors"
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
	// AccessToken authenticates against GitHub. Used as the password slot of
	// HTTP Basic auth (with username "x-access-token") for git Smart HTTP, and
	// as a Bearer token for the REST API.
	AccessToken string
}

var ErrNotFound = errors.New("upstream not found for token")

// Resolver maps an opaque URL token to the upstream repo it fronts. The
// production impl is DBResolver (queries plugin_github_connections and mints
// an installation token via the GitHub App broker).
type Resolver interface {
	Resolve(ctx context.Context, token string) (Upstream, error)
}
