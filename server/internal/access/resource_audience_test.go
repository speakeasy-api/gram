package access

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_SetResourceAudience_GrantsPeopleAndRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := uuid.New().String()
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

func TestService_SetResourceAudience_ReplacesTheWholeList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := uuid.New().String()
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

	serverID := uuid.New().String()
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

	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   uuid.New().String(),
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
		ResourceID:   uuid.New().String(),
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
		ResourceID:   uuid.New().String(),
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

	// user:all covers the caller, so it is a lockout by another name.
	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   uuid.New().String(),
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
