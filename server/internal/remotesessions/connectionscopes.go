package remotesessions

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// ConnectionScopeInput is what a sign-in through one client decides its scope
// request from, besides the protected resource row.
type ConnectionScopeInput struct {
	// ClientID is the remote session client row.
	ClientID uuid.UUID

	// ClientScope is the client's stored scope; it decides when set.
	ClientScope []string

	// IssuerScopesSupported is the issuer's advertised catalogue.
	IssuerScopesSupported []string

	// IssuerScopeOverride is the operator's scope override on the issuer.
	IssuerScopeOverride []string

	// IssuerOmitScopeFallback sends no scope instead of the issuer catalogue.
	IssuerOmitScopeFallback bool
}

// ClientConnectionScopes is what a sign-in through one client requests: its
// own inputs over the cached resource row when the client's grant is
// qualified to that resource (owns), else over no resource row.
func ClientConnectionScopes(in ConnectionScopeInput, cached ResourceScopes, owns bool) ScopeResolution {
	resource := ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: cached.UseDiscovered}
	if owns {
		resource = cached
	}
	return (Client{ //nolint:exhaustruct_v5 // Only scope inputs are used by RequestedScopes.
		ID:                      in.ClientID,
		ClientScope:             in.ClientScope,
		IssuerScopesSupported:   in.IssuerScopesSupported,
		IssuerScopeOverride:     in.IssuerScopeOverride,
		IssuerOmitScopeFallback: in.IssuerOmitScopeFallback,
	}).RequestedScopes(resource)
}

// GrantScopes drops OIDC-reserved scopes, which Okta rejects in ID-JAG
// requests. An empty result requests no scope.
func GrantScopes(scopes []string) []string {
	var out []string
	for _, scope := range scopes {
		if !oauthwire.IsOIDCReservedScope(scope) {
			out = append(out, scope)
		}
	}
	return out
}

// BindingScopeSet is the scope set Turn on records on an identity chaining
// binding, and where it came from.
type BindingScopeSet struct {
	// Scopes is sorted and deduplicated, OIDC-reserved scopes kept: chaining
	// drops them from its request but bounds an ID-JAG that names no requested
	// scope by the whole set.
	Scopes []string

	// Source is the connection's precedence step, or
	// ScopeSourcePreparationRequest when the request's scopes were used.
	Source remotesessionmetrics.ScopeSource
}

// BindingScopes is the one rule for the scopes an identity chaining binding
// records: the server connection's set when the binding has a connection to
// follow (connection is non-nil) and it names a grantable scope, or when the
// request names none either; else the scopes the preparation request names.
// GrantScopes decides which set applies; the chosen set is recorded whole.
func BindingScopes(connection *ScopeResolution, requested []string) BindingScopeSet {
	if connection != nil && (len(GrantScopes(connection.Scopes)) > 0 || len(connection.Scopes) > 0 && len(GrantScopes(requested)) == 0) {
		return BindingScopeSet{Scopes: sortedScopes(connection.Scopes), Source: connection.Source}
	}
	return BindingScopeSet{Scopes: sortedScopes(requested), Source: remotesessionmetrics.ScopeSourcePreparationRequest}
}

// SameBindingScopes reports whether two recorded scope sets are the same set,
// so a row recorded unsorted or with duplicates compares equal to its
// BindingScopes form.
func SameBindingScopes(a, b []string) bool {
	return slices.Equal(sortedScopes(a), sortedScopes(b))
}

func sortedScopes(scopes []string) []string {
	out := slices.Clone(scopes)
	slices.Sort(out)
	return conv.DefaultSlice(slices.Compact(out), []string{})
}

// BindingConnection names the binding whose server connection is resolved.
type BindingConnection struct {
	// ProjectID is the binding's project.
	ProjectID uuid.UUID

	// OrganizationID is the binding's organization.
	OrganizationID string

	// UserSessionIssuerID is the binding's user session issuer.
	UserSessionIssuerID uuid.UUID

	// ClientID is the remote session client the binding selects.
	ClientID uuid.UUID

	// Resource is the binding's canonical resource.
	Resource string

	// Discover applies the resource row's challenge scopes, pin and
	// advertised scopes, as ChallengeManager.ResourceScopeDiscoveryEnabled
	// does for logins.
	Discover bool

	// MCPServerID optionally scopes evidence to the exact inspected server.
	MCPServerID uuid.UUID
}

// ConnectionScopes resolves cached scopes for the exact remote resource and
// offered client. An issuer/client compatibility row is not an offer. A
// gateway-only or unmatched binding keeps its recorded/requested scopes.
func ConnectionScopes(ctx context.Context, db repo.DBTX, b BindingConnection) (*ScopeResolution, error) {
	q := repo.New(db)
	servers, err := q.ListRemoteServerURLsForOfferedClient(ctx, repo.ListRemoteServerURLsForOfferedClientParams{
		OrganizationID: b.OrganizationID, ProjectID: b.ProjectID,
		UserSessionIssuerID:   conv.ToNullUUID(b.UserSessionIssuerID),
		McpServerID:           uuid.NullUUID{UUID: b.MCPServerID, Valid: b.MCPServerID != uuid.Nil},
		RemoteSessionClientID: conv.ToNullUUID(b.ClientID),
	})
	if err != nil {
		return nil, fmt.Errorf("list remote URLs offering the binding's client: %w", err)
	}
	resourceURL, ok := bindingServerURL(servers, b.Resource)
	if !ok {
		return nil, nil
	}
	clients, err := q.ListReachableRemoteSessionClients(ctx, repo.ListReachableRemoteSessionClientsParams{ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, RemoteSessionClientIds: []uuid.UUID{b.ClientID}})
	if err != nil {
		return nil, fmt.Errorf("read offered registration: %w", err)
	}
	if len(clients) != 1 || clients[0].UpstreamRejectedAt.Valid {
		return nil, nil
	}
	cached, _, err := CachedResourceScopes(ctx, db, b.ProjectID, resourceURL, b.Discover)
	if err != nil {
		return nil, err
	}
	client := clients[0]
	resolution := ClientConnectionScopes(ConnectionScopeInput{ClientID: client.ClientID, ClientScope: client.ClientScope, IssuerScopesSupported: client.ScopesSupported, IssuerScopeOverride: client.ScopeOverride, IssuerOmitScopeFallback: client.OmitScopeFallback.Valid && client.OmitScopeFallback.Bool}, cached, true)
	return &resolution, nil
}

// ConnectionScopeInputFromClientRow reads a client bound to a user session issuer.
func ConnectionScopeInputFromClientRow(r repo.ListRemoteSessionClientsForUserSessionIssuerRow) ConnectionScopeInput {
	return ConnectionScopeInput{
		ClientID:                r.ClientID,
		ClientScope:             r.ClientScope,
		IssuerScopesSupported:   r.ScopesSupported,
		IssuerScopeOverride:     r.ScopeOverride,
		IssuerOmitScopeFallback: r.OmitScopeFallback.Valid && r.OmitScopeFallback.Bool,
	}
}

// ServesBindingResource reports whether a server URL, or the resource its
// cached protected resource document names, is exactly resource; RFC 8707
// resources compare exactly.
func ServesBindingResource(serverURL, documentResource, resource string) bool {
	return resource != "" && (serverURL == resource || documentResource == resource)
}

// bindingServerURL is the one server URL that serves resource.
func bindingServerURL(servers []repo.ListRemoteServerURLsForOfferedClientRow, resource string) (string, bool) {
	var found string
	for _, s := range servers {
		if !ServesBindingResource(s.Url, s.DocumentResource, resource) {
			continue
		}
		if found != "" && found != s.Url {
			return "", false
		}
		found = s.Url
	}
	return found, found != ""
}
