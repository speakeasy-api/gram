// Package access provides the access control API for Gram's RBAC system.
//
// THIS FILE IS A DESIGN EXAMPLE — it is not meant to compile. It demonstrates
// the intended public API surface, handler integration patterns, and the
// middleware enforcement model. Delete before implementation begins.
//
// Design principles:
//   - Grants are resolved ONCE per request by the middleware and stored in context.
//   - Handlers call access.Can / access.Filter for authorization decisions.
//   - The middleware verifies that at least one check was performed (safety net).
//   - The API is intentionally small: Can, Filter, and Check are the only
//     public functions that handlers need.
//
// Inspiration:
//   - SpiceDB:   client.CheckPermission(ctx, &pb.CheckPermissionRequest{Resource, Permission, Subject})
//   - Clerk:     claims.HasPermission("org:billing:read")
//   - Ory Keto:  client.PermissionApi.CheckPermission(ctx).Namespace(...).Object(...).Relation(...).Execute()
//
// Our model is simpler than all three because we don't have hierarchical
// relationships or external stores. The entire grant set fits in a single
// Postgres query per request.
package access

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// Scopes — typed constants mirroring DB slugs
// ---------------------------------------------------------------------------

type Scope string

const (
	ScopeOrgRead    Scope = "org:read"
	ScopeOrgAdmin   Scope = "org:admin"
	ScopeBuildRead  Scope = "build:read"
	ScopeBuildWrite Scope = "build:write"
	ScopeMCPRead    Scope = "mcp:read"
	ScopeMCPWrite   Scope = "mcp:write"
	ScopeMCPConnect Scope = "mcp:connect"
)

// ---------------------------------------------------------------------------
// Grants — resolved once per request, stored in context
// ---------------------------------------------------------------------------

// Grants holds the complete set of permissions for the current principal.
// It is resolved by the middleware before the handler runs, and is analogous
// to Clerk's SessionClaims — one object per request, read-only during the
// handler's lifetime.
type Grants struct {
	orgID    string
	userID   string
	roleSlug string
	rows     []grantRow // from DB or synthetic (API key / bypass)
	checked  atomic.Bool
}

// grantRow is a single row from principal_grants (or a synthetic equivalent).
type grantRow struct {
	Scope     Scope
	Resources []string // nil = unrestricted; non-nil = allowlist
}

// ---------------------------------------------------------------------------
// Check — a single (scope, resource) tuple
// ---------------------------------------------------------------------------

// Check creates a permission check for a specific scope and resource.
// Use empty string for resourceID on org-scoped checks.
func Check(scope Scope, resourceID string) check {
	return check{scope: scope, resourceID: resourceID}
}

type check struct {
	scope      Scope
	resourceID string
}

// ---------------------------------------------------------------------------
// Can — the primary authorization function
// ---------------------------------------------------------------------------

// Can verifies that the current principal holds ALL of the specified permissions.
// Returns nil if all checks pass, or a 403 error if any check fails.
//
// This is the function handlers call for point-access checks. It marks the
// request as "checked" for the safety-net middleware.
//
// Usage:
//
//	// Single check
//	if err := access.Can(ctx, access.Check(access.ScopeBuildRead, projectID)); err != nil {
//	    return nil, err
//	}
//
//	// Multiple checks (ALL must pass)
//	if err := access.Can(ctx,
//	    access.Check(access.ScopeBuildWrite, projectID),
//	    access.Check(access.ScopeMCPWrite, mcpID),
//	); err != nil {
//	    return nil, err
//	}
func Can(ctx context.Context, checks ...check) error {
	g := GrantsFromContext(ctx)
	if g == nil {
		return ErrNoGrants
	}
	g.checked.Store(true)

	for _, c := range checks {
		if !g.hasAccess(c.scope, c.resourceID) {
			return Denied(c.scope, c.resourceID)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Filter — for list endpoints
// ---------------------------------------------------------------------------

// Filter returns the set of resource IDs the principal is allowed to access
// for the given scope.
//
//   - nil slice:      unrestricted (user can access all resources)
//   - non-nil slice:  allowlist (handler adds WHERE id = ANY($filter))
//   - error:          no access at all (handler returns 403)
//
// This marks the request as "checked" for the safety-net middleware.
//
// Usage:
//
//	filter, err := access.Filter(ctx, access.ScopeBuildRead)
//	if err != nil {
//	    return nil, err // 403
//	}
//	// filter is nil (unrestricted) or []string (allowlist)
//	projects, err := s.repo.ListProjects(ctx, orgID, filter)
func Filter(ctx context.Context, scope Scope) ([]string, error) {
	g := GrantsFromContext(ctx)
	if g == nil {
		return nil, ErrNoGrants
	}
	g.checked.Store(true)

	return g.resourceFilter(scope)
}

// ---------------------------------------------------------------------------
// Internal grant evaluation (not exported)
// ---------------------------------------------------------------------------

// hasAccess implements the core check algorithm:
//  1. Find all grant rows matching scope.
//  2. If ANY row has nil resources (unrestricted) -> true.
//  3. Otherwise, check if resourceID is in the union of all resource arrays.
func (g *Grants) hasAccess(scope Scope, resourceID string) bool {
	for _, row := range g.rows {
		if row.Scope != scope {
			continue
		}

		// Unrestricted grant (resources = NULL in DB) → immediate allow.
		// This is why grants are additive: if ANY row is unrestricted,
		// it doesn't matter what other rows say.
		if row.Resources == nil {
			return true
		}

		// Scoped grant (resources = [...] in DB) → check if the target
		// resource is in this row's allowlist.
		if slices.Contains(row.Resources, resourceID) {
			return true
		}
	}

	// No matching row granted access — either no rows for this scope at all,
	// or rows existed but the resource wasn't in any allowlist.
	return false
}

// resourceFilter implements the filter algorithm for list endpoints:
//  1. Find all grant rows matching scope. No rows -> error.
//  2. If ANY row is unrestricted -> nil (all resources).
//  3. Otherwise -> union of all resource arrays.
func (g *Grants) resourceFilter(scope Scope) ([]string, error) {
	var (
		found     bool
		resources []string
	)
	for _, row := range g.rows {
		if row.Scope != scope {
			continue
		}
		found = true
		if row.Resources == nil {
			return nil, nil // unrestricted
		}
		resources = append(resources, row.Resources...)
	}
	if !found {
		return nil, Denied(scope, "")
	}
	return resources, nil
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

type contextKey struct{}

func GrantsToContext(ctx context.Context, g *Grants) context.Context {
	return context.WithValue(ctx, contextKey{}, g)
}

func GrantsFromContext(ctx context.Context) *Grants {
	g, _ := ctx.Value(contextKey{}).(*Grants)
	return g
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var ErrNoGrants = fmt.Errorf("access: no grants in context")

// Denied returns a 403-style error. In the real implementation this would
// return an oops.ShareableError with CodeForbidden. For now it's a plain error
// so the design example tests can run.
func Denied(scope Scope, resourceID string) error {
	if resourceID == "" {
		return fmt.Errorf("access denied: missing scope %s", scope)
	}
	return fmt.Errorf("access denied: scope %s on resource %s", scope, resourceID)
}
