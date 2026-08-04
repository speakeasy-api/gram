package devidptest

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// XaaAppOpts configures CreateXaaApp.
type XaaAppOpts struct {
	// ClientID is the id the app authenticates to the token endpoint with.
	// When empty a unique "test-app-<uuid8>" id is generated so parallel
	// tests do not collide on the xaa_apps_client_id_key unique index.
	ClientID string

	// ClientSecret, when non-empty, makes this a confidential client that
	// must present the secret to mint. Empty registers a public client.
	ClientSecret string

	// Name overrides the display name. Defaults to the client id.
	Name string

	// Disabled registers the app in the disabled state, which every mint
	// request then fails against. Defaults to enabled.
	Disabled bool
}

// CreateXaaApp registers a cross-app access requesting app: a client allowed
// to ask the IdP for an ID-JAG on a user's behalf.
func CreateXaaApp(t *testing.T, ctx context.Context, q *repo.Queries, opts XaaAppOpts) repo.XaaApp {
	t.Helper()

	clientID := opts.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("test-app-%s", uuid.New().String()[:8])
	}
	name := opts.Name
	if name == "" {
		name = clientID
	}

	app, err := q.CreateXaaApp(ctx, repo.CreateXaaAppParams{
		ID:           uuid.New(),
		ClientID:     clientID,
		ClientSecret: opts.ClientSecret,
		Name:         name,
		Enabled:      !opts.Disabled,
	})
	require.NoError(t, err, "create dev-idp xaa app")
	return app
}

// XaaResourceOpts configures CreateXaaResource.
type XaaResourceOpts struct {
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

// CreateXaaResource registers a resource app, which is one resource
// authorization server mounted at /resource-as/<slug>. Use
// Instance.ResourceASURL(slug) for the issuer identifier an ID-JAG must name
// in `aud`.
func CreateXaaResource(t *testing.T, ctx context.Context, q *repo.Queries, opts XaaResourceOpts) repo.XaaResource {
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

	resource, err := q.CreateXaaResource(ctx, repo.CreateXaaResourceParams{
		ID:                 uuid.New(),
		Slug:               slug,
		Name:               name,
		ResourceIdentifier: identifier,
	})
	require.NoError(t, err, "create dev-idp xaa resource")
	return resource
}

// AssignXaaApp grants a user the right to drive an app against a resource,
// for the given space-delimited scopes. Without this row the mint leg denies.
func AssignXaaApp(t *testing.T, ctx context.Context, q *repo.Queries, app repo.XaaApp, userID, resourceID uuid.UUID, grantedScopes string) repo.XaaAppAssignment {
	t.Helper()

	assignment, err := q.CreateXaaAppAssignment(ctx, repo.CreateXaaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         app.ID,
		UserID:        userID,
		ResourceID:    resourceID,
		GrantedScopes: grantedScopes,
	})
	require.NoError(t, err, "create dev-idp xaa app assignment")
	return assignment
}

// XaaTrustRuleOpts configures TrustXaaIssuer.
type XaaTrustRuleOpts struct {
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

// TrustXaaIssuer teaches a resource authorization server to accept ID-JAGs
// from `issuer`. Pass Instance.OAuth21URL to trust this dev-idp's own IdP, or
// any other issuer identifier to model a foreign trust domain.
func TrustXaaIssuer(t *testing.T, ctx context.Context, q *repo.Queries, resourceID uuid.UUID, issuer string, opts XaaTrustRuleOpts) repo.XaaTrustRule {
	t.Helper()

	allowedClientIDs := opts.AllowedClientIDs
	if allowedClientIDs == "" {
		allowedClientIDs = "[]"
	}

	rule, err := q.CreateXaaTrustRule(ctx, repo.CreateXaaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       resourceID,
		TrustedIssuer:    issuer,
		AllowedClientIds: allowedClientIDs,
		AllowedScopes:    opts.AllowedScopes,
		Enabled:          !opts.Disabled,
	})
	require.NoError(t, err, "create dev-idp xaa trust rule")
	return rule
}
