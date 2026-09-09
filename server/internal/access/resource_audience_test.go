package access

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedMCPServer creates the project and toolset a server id has to resolve to:
// the audience endpoints check the caller against the resource's own project,
// so a bare uuid is not a server anyone can administer.
func seedMCPServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) string {
	t.Helper()

	fixtures := testrepo.New(conn)

	projectID := uuid.New()
	_, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID:             projectID,
		Name:           "Audience",
		Slug:           "audience-" + projectID.String()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	toolsetID := uuid.New()
	_, err = fixtures.CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{
		ID:             toolsetID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           "Audience",
		Slug:           "audience-" + toolsetID.String()[:8],
	})
	require.NoError(t, err)

	return toolsetID.String()
}

func TestService_SetResourceAudience_GrantsPeopleAndRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, workos.Role{
		ID:          uuid.NewString(),
		Name:        "Support",
		Slug:        "support",
		Description: "Support engineers",
		Type:        "OrganizationRole",
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
	})
	rolePrincipal := seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "support")
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	result, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "manage"},
			{PrincipalUrn: rolePrincipal.String(), Level: "use"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	byPrincipal := make(map[string]*gen.ResourceAudienceEntry, len(result.Entries))
	for _, entry := range result.Entries {
		byPrincipal[entry.PrincipalUrn] = entry
	}

	require.Equal(t, "manage", byPrincipal[userPrincipal.String()].Level)
	require.Equal(t, "resource", byPrincipal[userPrincipal.String()].AppliesTo)
	require.Equal(t, "use", byPrincipal[rolePrincipal.String()].Level)
	require.Equal(t, "role", byPrincipal[rolePrincipal.String()].Kind)

	// The write is a real grant: the engine must now authorize the user for
	// this one server, and for no other.
	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userPrincipal)
	require.Len(t, grants, 1)
	require.Equal(t, string(authz.ScopeMCPWrite), grants[0].Scope)
}

// End to end: the rules this surface writes are the rules the engine enforces.
// The service is only worth anything if a person given "connect to these two
// tools" can call those tools on that server and nothing else.
func TestService_SetResourceAudience_EnforcesWhatItWrites(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	otherServerID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	authorizes := func(t *testing.T, check authz.Check) bool {
		t.Helper()
		principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID)
		require.NoError(t, err)
		grants, err := authz.LoadGrants(ctx, ti.conn, authCtx.ActiveOrganizationID, principals)
		require.NoError(t, err)
		allowed, err := authz.GrantsAuthorize(grants, check)
		require.NoError(t, err)
		return allowed
	}

	// Narrowed to two tools on one server.
	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "use", Tools: []string{"search", "lookup"}},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	require.True(t, authorizes(t, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: "search"})),
		"a named tool is allowed")
	require.False(t, authorizes(t, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: "delete_everything"})),
		"a tool the rule does not name is not")
	require.False(t, authorizes(t, authz.MCPToolCallCheck(otherServerID, authz.MCPToolCallDimensions{Tool: "search"})),
		"and neither is the same tool on another server")

	// Widened to the whole server.
	_, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "use"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	require.True(t, authorizes(t, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: "delete_everything"})),
		"an unnarrowed rule covers every tool")

	// Narrowed by annotation instead: read-only tools only.
	_, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "use", Dispositions: []string{"read_only"}},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	require.True(t, authorizes(t, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: "search", Disposition: "read_only"})),
		"a read-only tool is allowed")
	require.False(t, authorizes(t, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: "purge", Disposition: "destructive"})),
		"a destructive one is not")

	// Removed entirely.
	_, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	require.False(t, authorizes(t, authz.MCPToolCallCheck(serverID, authz.MCPToolCallDimensions{Tool: "search"})),
		"removing the rule removes the access")
}

func TestService_SetResourceAudience_ReplacesTheWholeList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "manage"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	// Moving someone to a weaker level must not leave the stronger rule behind.
	result, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "use"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Equal(t, "use", result.Entries[0].Level)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userPrincipal)
	require.Len(t, grants, 1)
	require.Equal(t, string(authz.ScopeMCPConnect), grants[0].Scope)

	// Removing everyone leaves no rule naming the server.
	empty, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Empty(t, empty.Entries)
	require.Empty(t, listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userPrincipal))
}

func TestService_SetResourceAudience_BlockSubtractsOrganizationWideAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, workos.Role{
		ID:          uuid.NewString(),
		Name:        "Contractors",
		Slug:        "contractors",
		Description: "Fixed-term staff",
		Type:        "OrganizationRole",
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
	})
	rolePrincipal := seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "contractors")

	// An organization-wide rule this surface does not own.
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, rolePrincipal, authz.ScopeMCPConnect, authz.WildcardResource)

	result, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: rolePrincipal.String(), Level: "blocked"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	var blocked, inherited *gen.ResourceAudienceEntry
	for _, entry := range result.Entries {
		if entry.Level == "blocked" {
			blocked = entry
		}
		if entry.AppliesTo == "all_resources" {
			inherited = entry
		}
	}
	require.NotNil(t, blocked, "the block is reported as a rule on this resource")
	require.Equal(t, "resource", blocked.AppliesTo)
	require.NotNil(t, inherited, "the organization-wide rule is still reported, and untouched")

	scopes := make([]string, 0, 2)
	for _, grant := range listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, rolePrincipal) {
		scopes = append(scopes, grant.Scope)
	}
	require.ElementsMatch(t, []string{string(authz.ScopeMCPConnect), string(authz.ScopeMCPBlockedConnect)}, scopes)
}

func TestService_SetResourceAudience_RejectsUnknownPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: "user:someone-who-left", Level: "use"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestService_ListResourceAudience_HonoursProjectScopedGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	// A grant naming a different project must not reach this server, even
	// though it names every MCP resource.
	elsewhere := authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource)
	elsewhere.Selector[authz.SelectorKeyProjectID] = uuid.New().String()
	scopedCtx := authz.GrantsToContext(contextvalues.SetAuthContext(ctx, authCtx), []authz.Grant{
		authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
		elsewhere,
	})

	_, err := ti.service.ListResourceAudience(scopedCtx, &gen.ListResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_SetResourceAudience_RefusesStaleVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	before, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	stale := before.Version

	// Someone else saves first, which moves the version on.
	saved, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind:    "mcp",
		ResourceID:      serverID,
		Entries:         []*gen.SetResourceAudienceEntry{{PrincipalUrn: userPrincipal.String(), Level: "use"}},
		ExpectedVersion: &stale,
		SessionToken:    nil,
		ApikeyToken:     nil,
	})
	require.NoError(t, err)
	require.NotEqual(t, stale, saved.Version)

	// The second administrator is still holding the list they read first.
	_, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind:    "mcp",
		ResourceID:      serverID,
		Entries:         nil,
		ExpectedVersion: &stale,
		SessionToken:    nil,
		ApikeyToken:     nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeFailedPrecondition, oopsErr.Code)
}

func TestService_SetResourceAudience_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readOnlyCtx := contextvalues.SetAuthContext(ctx, authCtx)
	readOnlyCtx = authz.GrantsToContext(readOnlyCtx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.SetResourceAudience(readOnlyCtx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		Entries:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_SetResourceAudience_RefusesSelfLockout(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: userPrincipal.String(), Level: "blocked"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestService_SetResourceAudience_RefusesBlockingEveryone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// user:all covers the caller, so it is a lockout by another name.
	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: "*", Level: "blocked"},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestService_SetResourceAudience_NarrowsToTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	result, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{
				PrincipalUrn: userPrincipal.String(),
				Level:        "use",
				Tools:        []string{"search", "lookup"},
			},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.ElementsMatch(t, []string{"search", "lookup"}, result.Entries[0].Tools)

	// One grant row per tool, all naming this server.
	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userPrincipal)
	require.Len(t, grants, 2)

	// Replacing with a disposition clears the per-tool rows: the two are
	// alternatives, and the whole resource is rewritten.
	result, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{
				PrincipalUrn: userPrincipal.String(),
				Level:        "use",
				Dispositions: []string{"read_only"},
			},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Entries[0].Tools)
	require.Equal(t, []string{"read_only"}, result.Entries[0].Dispositions)
	require.Len(t, listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userPrincipal), 1)
}

func TestService_SetResourceAudience_RejectsToolsAndAnnotationsTogether(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		Entries: []*gen.SetResourceAudienceEntry{
			{
				PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String(),
				Level:        "use",
				Tools:        []string{"search"},
				Dispositions: []string{"read_only"},
			},
		},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}
