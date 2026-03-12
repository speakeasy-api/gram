// Package access — handler integration examples.
//
// THIS FILE IS A DESIGN EXAMPLE — it shows how handlers at various levels of
// complexity would integrate with the access package. It is meant to be read
// during RFC review, not to compile.
//
// Each example maps to a real handler pattern found in the codebase and shows
// the before/after diff.
package access_test

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/access"
)

// ===========================================================================
// EXAMPLE 1: Simple read — GetProject
// ===========================================================================
//
// Before (current code, server/internal/projects/impl.go:76):
//
//	func (s *Service) GetProject(ctx context.Context, payload *gen.GetProjectPayload) (*gen.GetProjectResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//	    // ... query project ...
//	}
//
// After:
//
//	func (s *Service) GetProject(ctx context.Context, payload *gen.GetProjectPayload) (*gen.GetProjectResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    // NEW: RBAC check — build:read on this project
//	    if err := access.Can(ctx, access.Check(access.ScopeBuildRead, projectID)); err != nil {
//	        return nil, err
//	    }
//
//	    // ... query project (unchanged) ...
//	}
//
// Notes:
//   - One line added. The middleware already resolved Grants into context.
//   - projectID comes from resolving payload.Slug -> DB row -> ID.
//     Alternatively, middleware could pass it from authCtx.ProjectID.
//   - If the user doesn't have build:read for this project, they get 403.

func ExampleCan_simpleRead() {
	ctx := context.Background()
	projectID := "proj-abc-123"

	// This is the entire RBAC integration for a simple read endpoint.
	if err := access.Can(ctx, access.Check(access.ScopeBuildRead, projectID)); err != nil {
		// returns 403 — handler stops here
		_ = err
		return
	}
	// ... handler continues with query logic ...
}

// ===========================================================================
// EXAMPLE 2: Simple write — CreateDeployment
// ===========================================================================
//
// Before:
//
//	func (s *Service) CreateDeployment(ctx context.Context, payload *gen.CreateDeploymentPayload) (*gen.CreateDeploymentResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ProjectID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//	    // ... create deployment ...
//	}
//
// After:
//
//	func (s *Service) CreateDeployment(ctx context.Context, payload *gen.CreateDeploymentPayload) (*gen.CreateDeploymentResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ProjectID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    // NEW: RBAC check — build:write on the project
//	    if err := access.Can(ctx, access.Check(access.ScopeBuildWrite, authCtx.ProjectID.String())); err != nil {
//	        return nil, err
//	    }
//
//	    // ... create deployment (unchanged) ...
//	}
//
// Notes:
//   - For "create" endpoints, the resource being created doesn't exist yet.
//     We check against the PARENT resource (the project).
//   - authCtx.ProjectID is already available from the project_slug security
//     scheme, so no extra DB lookup is needed.

func ExampleCan_simpleWrite() {
	ctx := context.Background()
	projectID := "proj-abc-123"

	if err := access.Can(ctx, access.Check(access.ScopeBuildWrite, projectID)); err != nil {
		_ = err
		return
	}
	// ... create the deployment ...
}

// ===========================================================================
// EXAMPLE 3: Org-level admin — CreateKey
// ===========================================================================
//
// Before:
//
//	func (s *Service) CreateKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.CreateKeyResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//	    // ... create API key ...
//	}
//
// After:
//
//	func (s *Service) CreateKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.CreateKeyResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    // NEW: RBAC check — org:admin (no resource ID for org-scoped checks)
//	    if err := access.Can(ctx, access.Check(access.ScopeOrgAdmin, "")); err != nil {
//	        return nil, err
//	    }
//
//	    // ... create API key (unchanged) ...
//	}
//
// Notes:
//   - Org-scoped checks use empty string for resourceID.
//   - org:admin is never resource-scoped (there's only one org per request).

func ExampleCan_orgAdmin() {
	ctx := context.Background()

	// Org-level check — no resource ID needed
	if err := access.Can(ctx, access.Check(access.ScopeOrgAdmin, "")); err != nil {
		_ = err
		return
	}
}

// ===========================================================================
// EXAMPLE 4: List endpoint with resource filtering — ListProjects
// ===========================================================================
//
// Before (server/internal/projects/impl.go:166):
//
//	func (s *Service) ListProjects(ctx context.Context, payload *gen.ListProjectsPayload) (*gen.ListProjectsResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.SessionID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//	    // ... org membership check ...
//	    projects, err := s.repo.ListProjectsByOrganization(ctx, payload.OrganizationID)
//	    // ... map and return ...
//	}
//
// After:
//
//	func (s *Service) ListProjects(ctx context.Context, payload *gen.ListProjectsPayload) (*gen.ListProjectsResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    // NEW: RBAC filter — returns nil (unrestricted) or []string (allowlist)
//	    filter, err := access.Filter(ctx, access.ScopeBuildRead)
//	    if err != nil {
//	        return nil, err // 403 — user has no build:read at all
//	    }
//
//	    // Pass filter to query — nil means no WHERE clause, []string means WHERE id = ANY($filter)
//	    projects, err := s.repo.ListProjectsByOrganization(ctx, payload.OrganizationID, filter)
//	    // ... map and return (unchanged) ...
//	}
//
// The SQLc query changes to:
//
//	-- name: ListProjectsByOrganization :many
//	SELECT * FROM projects
//	WHERE organization_id = $1
//	  AND deleted_at IS NULL
//	  AND ($2::text[] IS NULL OR id::text = ANY($2))
//	ORDER BY created_at DESC;
//
// Notes:
//   - Filter replaces the old org-membership check for list endpoints.
//   - Pagination works correctly because the DB applies the filter.
//   - If the user is unrestricted (role grant with NULL resources), filter
//     returns nil, and the query returns everything — no performance penalty.

func ExampleFilter_listProjects() {
	ctx := context.Background()

	filter, err := access.Filter(ctx, access.ScopeBuildRead)
	if err != nil {
		// 403 — no access at all
		_ = err
		return
	}
	// filter is nil (unrestricted) or []string{"proj-a", "proj-b"} (allowlist)
	_ = filter
	// Pass to: s.repo.ListProjectsByOrganization(ctx, orgID, filter)
}

// ===========================================================================
// EXAMPLE 5: Field-level gating — UpdateToolset (THE KEY EXAMPLE)
// ===========================================================================
//
// This is the critical use case from the images. UpdateToolset has fields that
// require different permission levels:
//
//   GREEN fields (build:write on project):
//     - name, description, default_environment_slug
//     - prompt_template_names, tool_urns, resource_urns
//     - tool_selection_mode
//
//   RED fields (mcp:write on mcp resource):
//     - mcp_enabled, mcp_slug, mcp_is_public, custom_domain_id
//
// A user with build:write but NOT mcp:write can update the green fields
// but NOT the red ones — in a SINGLE endpoint.
//
// Before (server/internal/toolsets/impl.go:201):
//
//	func (s *Service) UpdateToolset(ctx context.Context, payload *gen.UpdateToolsetPayload) (*types.Toolset, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ProjectID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//	    // ... all fields updated without permission checks ...
//	}
//
// After:
//
//	func (s *Service) UpdateToolset(ctx context.Context, payload *gen.UpdateToolsetPayload) (*types.Toolset, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ProjectID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    projectID := authCtx.ProjectID.String()
//
//	    // STEP 1: Every UpdateToolset call requires build:write on the project.
//	    // This gates the "green" fields (name, description, tools, etc).
//	    if err := access.Can(ctx, access.Check(access.ScopeBuildWrite, projectID)); err != nil {
//	        return nil, err
//	    }
//
//	    // STEP 2: If any MCP-related field is being modified, ALSO require
//	    // mcp:write on the MCP resource. This gates the "red" fields.
//	    if hasMCPFields(payload) {
//	        mcpID := existingToolset.McpSlug.String // resolved after fetching existing toolset
//	        if err := access.Can(ctx, access.Check(access.ScopeMCPWrite, mcpID)); err != nil {
//	            return nil, err
//	        }
//	    }
//
//	    // ... rest of handler (unchanged) ...
//	}
//
// The helper:
//
//	func hasMCPFields(p *gen.UpdateToolsetPayload) bool {
//	    return p.McpEnabled != nil || p.McpSlug != nil || p.McpIsPublic != nil || p.CustomDomainID != nil
//	}
//
// KEY INSIGHT: This is why we need handler-level checks, not middleware-only.
// Middleware sees one endpoint (toolsets.updateToolset) and can only check one
// scope. But the handler knows which fields are being modified and can check
// different scopes for different field groups.

func ExampleCan_fieldLevelGating() {
	ctx := context.Background()
	projectID := "proj-abc-123"
	mcpID := "mcp-payments"

	// Always required — gates the "green" fields
	if err := access.Can(ctx, access.Check(access.ScopeBuildWrite, projectID)); err != nil {
		_ = err
		return
	}

	// Conditionally required — only if MCP fields are present in payload
	mcpFieldsPresent := true // payload.McpEnabled != nil || payload.McpSlug != nil || ...
	if mcpFieldsPresent {
		if err := access.Can(ctx, access.Check(access.ScopeMCPWrite, mcpID)); err != nil {
			_ = err
			return
		}
	}
}

// ===========================================================================
// EXAMPLE 6: Multi-scope check in one call — PublishToolset
// ===========================================================================
//
// PublishToolset needs BOTH build:write (to modify the toolset) AND mcp:write
// (to affect the MCP server). Both must pass.
//
//	func (s *Service) PublishToolset(ctx context.Context, payload *gen.PublishToolsetPayload) (*types.Toolset, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ProjectID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    // Both scopes required — if either fails, the whole call is denied.
//	    if err := access.Can(ctx,
//	        access.Check(access.ScopeBuildWrite, authCtx.ProjectID.String()),
//	        access.Check(access.ScopeMCPWrite, mcpID),
//	    ); err != nil {
//	        return nil, err
//	    }
//
//	    // ... publish logic ...
//	}

func ExampleCan_multiScope() {
	ctx := context.Background()
	projectID := "proj-abc-123"
	mcpID := "mcp-payments"

	// Both must pass — single Can call with multiple checks
	if err := access.Can(ctx,
		access.Check(access.ScopeBuildWrite, projectID),
		access.Check(access.ScopeMCPWrite, mcpID),
	); err != nil {
		_ = err
		return
	}
}

// ===========================================================================
// EXAMPLE 7: MCP tool invocation — instances service
// ===========================================================================
//
// The instances service handles MCP RPC calls (tools/call, prompts/get, etc).
// These require mcp:connect on the specific MCP server.
//
//	func (s *Service) HandleRPC(ctx context.Context, payload *gen.HandleRPCPayload) (*gen.HandleRPCResult, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    mcpID := resolveMCPID(ctx, payload) // from request routing
//
//	    if err := access.Can(ctx, access.Check(access.ScopeMCPConnect, mcpID)); err != nil {
//	        return nil, err
//	    }
//
//	    // ... dispatch RPC to MCP server ...
//	}
//
// Note: mcp:connect is independent of build:read. A contractor with
// mcp:connect on "mcp-payments" but no build:read cannot see the project
// that contains the MCP server. Invariant 1: no implicit hierarchy.

func ExampleCan_mcpConnect() {
	ctx := context.Background()
	mcpID := "mcp-payments"

	if err := access.Can(ctx, access.Check(access.ScopeMCPConnect, mcpID)); err != nil {
		_ = err
		return
	}
}

// ===========================================================================
// EXAMPLE 8: Middleware — enforcement + safety net
// ===========================================================================
//
// The middleware is a Goa endpoint middleware (same pattern as MapErrors and
// TraceMethods). It wraps every endpoint with grant resolution and post-check
// verification.
//
//	func Enforce(resolver *Resolver) func(goa.Endpoint) goa.Endpoint {
//	    return func(next goa.Endpoint) goa.Endpoint {
//	        return func(ctx context.Context, req any) (any, error) {
//	            svc, _ := ctx.Value(goa.ServiceKey).(string)
//	            method, _ := ctx.Value(goa.MethodKey).(string)
//
//	            // Check exemptions first
//	            if reason, exempt := Exemptions[svc]; exempt {
//	                _ = reason // logged at startup
//	                return next(ctx, req)
//	            }
//
//	            // Resolve grants from DB/cache (or synthetic for API keys/bypass)
//	            authCtx, _ := contextvalues.GetAuthContext(ctx)
//	            grants, err := resolver.Resolve(ctx, authCtx)
//	            if err != nil {
//	                return nil, err
//	            }
//
//	            // Inject grants into context for handlers to use
//	            ctx = GrantsToContext(ctx, grants)
//
//	            // Call the handler
//	            result, err := next(ctx, req)
//
//	            // SAFETY NET: verify at least one check was performed
//	            if !grants.checked.Load() {
//	                // In dev: panic("handler %s.%s did not perform any access check")
//	                // In prod: log warning
//	                logger.WarnContext(ctx, "handler did not perform access check",
//	                    slog.String("service", svc),
//	                    slog.String("method", method),
//	                )
//	            }
//
//	            return result, err
//	        }
//	    }
//	}
//
// Wiring in Attach (server/internal/toolsets/impl.go):
//
//	func Attach(mux goahttp.Muxer, service *Service) {
//	    endpoints := gen.NewEndpoints(service)
//	    endpoints.Use(middleware.MapErrors())
//	    endpoints.Use(middleware.TraceMethods(service.tracer))
//	    endpoints.Use(access.Enforce(service.resolver))  // <-- NEW
//	    srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
//	}

// ===========================================================================
// EXAMPLE 9: Resolver — how grants are built for different auth types
// ===========================================================================
//
// The Resolver produces Grants differently depending on how the user authenticated:
//
//	type Resolver struct {
//	    repo  *repo.Queries
//	    cache cache.Cache
//	}
//
//	func (r *Resolver) Resolve(ctx context.Context, authCtx *contextvalues.AuthContext) (*Grants, error) {
//	    // 1. Chat session / function token → bypass (always allow)
//	    if authCtx.AccountType == "chat_session" || authCtx.AccountType == "function" {
//	        return bypassGrants(), nil
//	    }
//
//	    // 2. API key → translate legacy scopes, no DB query
//	    if authCtx.APIKeyID != "" {
//	        return syntheticGrantsFromAPIKey(authCtx.APIKeyScopes), nil
//	    }
//
//	    // 3. Session user → query DB (with cache)
//	    cacheKey := fmt.Sprintf("rbac:%s:%s", authCtx.UserID, authCtx.ActiveOrganizationID)
//	    if cached, ok := r.cache.Get(ctx, cacheKey); ok {
//	        return cached.(*Grants), nil
//	    }
//
//	    rows, err := r.repo.GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
//	        OrganizationID: authCtx.ActiveOrganizationID,
//	        RoleSlug:       authCtx.RoleSlug, // from org_user_relationships.workos_role_slug
//	        UserID:         authCtx.UserID,
//	    })
//	    if err != nil {
//	        return nil, fmt.Errorf("resolve grants: %w", err)
//	    }
//
//	    grants := buildGrants(authCtx, rows)
//	    r.cache.Set(ctx, cacheKey, grants, 5*time.Minute)
//	    return grants, nil
//	}
//
// Legacy API key translation (static map, no DB):
//
//	var legacyAPIKeyMap = map[string][]Scope{
//	    "producer": {ScopeOrgRead, ScopeBuildRead, ScopeBuildWrite, ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect},
//	    "consumer": {ScopeOrgRead, ScopeBuildRead, ScopeMCPRead, ScopeMCPConnect},
//	    "chat":     {ScopeMCPRead, ScopeMCPConnect},
//	}
//
//	func syntheticGrantsFromAPIKey(legacyScopes []string) *Grants {
//	    g := &Grants{}
//	    for _, ls := range legacyScopes {
//	        for _, scope := range legacyAPIKeyMap[ls] {
//	            g.rows = append(g.rows, grantRow{Scope: scope, Resources: nil}) // unrestricted
//	        }
//	    }
//	    return g
//	}

// ===========================================================================
// EXAMPLE 10: Complete before/after for UpdateToolset with full context
// ===========================================================================
//
// This is the most complex example — showing the complete handler with all
// the access checks inline, matching the real codebase structure.
//
//	func (s *Service) UpdateToolset(ctx context.Context, payload *gen.UpdateToolsetPayload) (*types.Toolset, error) {
//	    authCtx, ok := contextvalues.GetAuthContext(ctx)
//	    if !ok || authCtx == nil || authCtx.ProjectID == nil {
//	        return nil, oops.C(oops.CodeUnauthorized)
//	    }
//
//	    logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetSlug(string(payload.Slug)))
//
//	    // ---------------------------------------------------------------
//	    // RBAC: build:write is always required (gates green fields)
//	    // ---------------------------------------------------------------
//	    if err := access.Can(ctx, access.Check(access.ScopeBuildWrite, authCtx.ProjectID.String())); err != nil {
//	        return nil, err
//	    }
//
//	    dbtx, err := s.db.Begin(ctx)
//	    if err != nil {
//	        return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
//	    }
//	    defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
//
//	    tr := s.repo.WithTx(dbtx)
//
//	    existingToolset, err := tr.GetToolset(ctx, repo.GetToolsetParams{
//	        Slug:      conv.ToLower(payload.Slug),
//	        ProjectID: *authCtx.ProjectID,
//	    })
//	    if err != nil {
//	        return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, logger)
//	    }
//
//	    // ---------------------------------------------------------------
//	    // RBAC: if any MCP field is being changed, require mcp:write
//	    // ---------------------------------------------------------------
//	    if payload.McpEnabled != nil || payload.McpSlug != nil || payload.McpIsPublic != nil || payload.CustomDomainID != nil {
//	        mcpID := existingToolset.McpSlug.String
//	        if err := access.Can(ctx, access.Check(access.ScopeMCPWrite, mcpID)); err != nil {
//	            return nil, err
//	        }
//	    }
//
//	    // ... build updateParams from existing + payload fields (unchanged) ...
//	    // ... validate, update, commit (unchanged) ...
//
//	    return toolsetDetails, nil
//	}
//
// Why this works:
//   - A member with build:write but NOT mcp:write can update name, description,
//     tools, templates, etc. — all the "green" fields.
//   - If they try to set mcp_enabled=true, the second check kicks in and
//     returns 403.
//   - An admin with both build:write AND mcp:write can update everything.
//   - The safety-net middleware sees that at least one Can() call was made
//     (the first one), so it doesn't panic/warn.
