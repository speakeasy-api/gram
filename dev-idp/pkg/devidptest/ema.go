package devidptest

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// EmaAppOpts configures CreateEmaApp.
type EmaAppOpts struct {
	// ClientID is the id the app authenticates to the token endpoint with.
	// When empty a unique "test-app-<uuid8>" id is generated so parallel
	// tests do not collide on the ema_apps_client_id_key unique index.
	ClientID string

	// ClientSecret, when non-empty, makes this a confidential client that
	// must present the secret to mint. Empty registers a public client.
	ClientSecret string

	// Name overrides the display name. Defaults to the client id.
	Name string

	// JWKS, when non-empty, is a JWKS document holding the app's public key.
	// Setting it makes the app authenticate with private_key_jwt, and its
	// ClientSecret is then ignored.
	JWKS string

	// Disabled registers the app in the disabled state, which every mint
	// request then fails against. Defaults to enabled.
	Disabled bool
}

// CreateEmaApp registers a requesting app: a client allowed
// to ask the IdP for an ID-JAG on a user's behalf.
func CreateEmaApp(t *testing.T, ctx context.Context, q *repo.Queries, opts EmaAppOpts) repo.EmaApp {
	t.Helper()

	clientID := opts.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("test-app-%s", uuid.New().String()[:8])
	}
	name := opts.Name
	if name == "" {
		name = clientID
	}

	app, err := q.CreateEmaApp(ctx, repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     clientID,
		ClientSecret: opts.ClientSecret,
		Jwks:         opts.JWKS,
		Name:         name,
		Enabled:      !opts.Disabled,
	})
	require.NoError(t, err, "create dev-idp ema app")
	return app
}

// EmaResourceOpts configures CreateEmaResource.
type EmaResourceOpts struct {
	// Slug is the path segment the resource's authorization server is served
	// at, and so determines its issuer identifier. When empty a unique
	// "test-resource-<uuid8>" slug is generated.
	Slug string

	// Name overrides the display name. Defaults to the slug.
	Name string

	// ResourceIdentifier is the MCP server URL this resource guards -- the
	// value that lands in an ID-JAG's `resource` claim and in the issued
	// access token's audience. Defaults to "https://mcp.<slug>.example/".
	ResourceIdentifier string
}

// CreateEmaResource registers a resource app, which is one resource
// authorization server mounted at /resource-as/<slug>. Use
// Instance.ResourceASURL(slug) for the issuer identifier an ID-JAG must name
// in `aud`.
func CreateEmaResource(t *testing.T, ctx context.Context, q *repo.Queries, opts EmaResourceOpts) repo.EmaResource {
	t.Helper()

	slug := opts.Slug
	if slug == "" {
		slug = fmt.Sprintf("test-resource-%s", uuid.New().String()[:8])
	}
	name := opts.Name
	if name == "" {
		name = slug
	}
	identifier := opts.ResourceIdentifier
	if identifier == "" {
		identifier = fmt.Sprintf("https://mcp.%s.example/", slug)
	}

	resource, err := q.CreateEmaResource(ctx, repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               slug,
		Name:               name,
		ResourceIdentifier: identifier,
	})
	require.NoError(t, err, "create dev-idp ema resource")
	return resource
}

// AssignEmaApp grants a user the right to drive an app against a resource,
// for the given space-delimited scopes. Without this row the mint leg denies.
func AssignEmaApp(t *testing.T, ctx context.Context, q *repo.Queries, app repo.EmaApp, userID, resourceID uuid.UUID, grantedScopes string) repo.EmaAppAssignment {
	t.Helper()

	assignment, err := q.CreateEmaAppAssignment(ctx, repo.CreateEmaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         app.ID,
		UserID:        userID,
		ResourceID:    resourceID,
		GrantedScopes: grantedScopes,
	})
	require.NoError(t, err, "create dev-idp ema app assignment")
	return assignment
}

// EmaTrustRuleOpts configures TrustEmaIssuer.
type EmaTrustRuleOpts struct {
	// AllowedScopes is a space-delimited ceiling applied on top of whatever
	// the ID-JAG carries. Empty means no ceiling.
	AllowedScopes string

	// AllowedClientIDs is a JSON array of client ids this resource will
	// accept grants for. Empty (or "[]") means any client the issuer
	// vouched for.
	AllowedClientIDs string

	// Disabled creates the rule in the disabled state, so redemption fails
	// with a different reason than a missing rule. Defaults to enabled.
	Disabled bool
}

// TrustEmaIssuer teaches a resource authorization server to accept ID-JAGs
// from `issuer`. Pass Instance.OAuth21URL to trust this dev-idp's own IdP, or
// any other issuer identifier to model a foreign trust domain.
func TrustEmaIssuer(t *testing.T, ctx context.Context, q *repo.Queries, resourceID uuid.UUID, issuer string, opts EmaTrustRuleOpts) repo.EmaTrustRule {
	t.Helper()

	allowedClientIDs := opts.AllowedClientIDs
	if allowedClientIDs == "" {
		allowedClientIDs = "[]"
	}

	rule, err := q.CreateEmaTrustRule(ctx, repo.CreateEmaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       resourceID,
		TrustedIssuer:    issuer,
		AllowedClientIds: allowedClientIDs,
		AllowedScopes:    opts.AllowedScopes,
		Enabled:          !opts.Disabled,
	})
	require.NoError(t, err, "create dev-idp ema trust rule")
	return rule
}
