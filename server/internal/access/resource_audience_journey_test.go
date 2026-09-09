package access

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The administrator's journey, end to end and in order: open a server's Access
// page, see who can be given access, grant a role, narrow it to two tools,
// hand one person more, block someone, and take a rule away — checking after
// each step both what the page would show and what the engine actually allows.
//
// Every save carries the version from the read before it, exactly as the page
// does, so the handshake is exercised rather than described.
func TestResourceAudience_AdministratorJourney(t *testing.T) {
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
	role := seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "support")
	me := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	read := func() *gen.ResourceAudienceResult {
		t.Helper()
		result, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{
			ResourceKind: "mcp",
			ResourceID:   serverID,
			SessionToken: nil,
			ApikeyToken:  nil,
		})
		require.NoError(t, err)
		return result
	}

	// Saving from the list just read, the way the page does.
	save := func(entries ...*gen.SetResourceAudienceEntry) *gen.ResourceAudienceResult {
		t.Helper()
		result, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
			ResourceKind:    "mcp",
			ResourceID:      serverID,
			Entries:         entries,
			ExpectedVersion: read().Version,
			SessionToken:    nil,
			ApikeyToken:     nil,
		})
		require.NoError(t, err)
		return result
	}

	allows := func(principal urn.Principal, dims authz.MCPToolCallDimensions) bool {
		t.Helper()
		grants, err := authz.LoadGrants(ctx, ti.conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
		require.NoError(t, err)
		allowed, err := authz.GrantsAuthorize(grants, authz.MCPToolCallCheck(serverID, dims))
		require.NoError(t, err)
		return allowed
	}

	rowFor := func(result *gen.ResourceAudienceResult, principal urn.Principal, level string) *gen.ResourceAudienceEntry {
		t.Helper()
		for _, entry := range result.Entries {
			if entry.PrincipalUrn == principal.String() && entry.Level == level {
				return entry
			}
		}
		return nil
	}

	// 1. An untouched server has nothing of its own to show.
	start := read()
	for _, entry := range start.Entries {
		require.Equal(t, audienceAppliesToAllResources, entry.AppliesTo,
			"a server nobody has been given starts with only inherited rules")
	}

	// 2. The picker offers the principals that can be given access.
	options, err := ti.service.ListAudienceOptions(ctx, &gen.ListAudienceOptionsPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	offered := make(map[string]string, len(options.Options))
	for _, option := range options.Options {
		offered[option.PrincipalUrn] = option.Kind
	}
	require.Equal(t, "role", offered[role.String()], "the seeded role is offered")
	require.Equal(t, "user", offered[me.String()], "so is a member")

	// 3. Grant the role, and give one person management of the server.
	afterGrant := save(
		&gen.SetResourceAudienceEntry{PrincipalUrn: role.String(), Level: "use"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: me.String(), Level: "manage"},
	)
	require.NotNil(t, rowFor(afterGrant, role, "use"))
	require.NotNil(t, rowFor(afterGrant, me, "manage"))
	require.NotEqual(t, start.Version, afterGrant.Version, "the version moves with the rules")
	require.True(t, allows(role, authz.MCPToolCallDimensions{Tool: "search"}),
		"the role can now call the server's tools")

	// 4. Narrow the role to two tools. The row says so, and the engine agrees.
	afterNarrowing := save(
		&gen.SetResourceAudienceEntry{PrincipalUrn: role.String(), Level: "use", Tools: []string{"search", "lookup"}},
		&gen.SetResourceAudienceEntry{PrincipalUrn: me.String(), Level: "manage"},
	)
	require.ElementsMatch(t, []string{"search", "lookup"}, rowFor(afterNarrowing, role, "use").Tools)
	require.True(t, allows(role, authz.MCPToolCallDimensions{Tool: "search"}))
	require.False(t, allows(role, authz.MCPToolCallDimensions{Tool: "purge"}),
		"a tool outside the rule is not reachable")

	// 5. Narrow by annotation instead. The two are alternatives, so the tool
	//    names are gone rather than added to.
	afterAnnotation := save(
		&gen.SetResourceAudienceEntry{PrincipalUrn: role.String(), Level: "use", Dispositions: []string{"read_only"}},
		&gen.SetResourceAudienceEntry{PrincipalUrn: me.String(), Level: "manage"},
	)
	roleRow := rowFor(afterAnnotation, role, "use")
	require.Empty(t, roleRow.Tools)
	require.ElementsMatch(t, []string{"read_only"}, roleRow.Dispositions)
	require.True(t, allows(role, authz.MCPToolCallDimensions{Tool: "search", Disposition: "read_only"}))
	require.False(t, allows(role, authz.MCPToolCallDimensions{Tool: "purge", Disposition: "destructive"}))

	// 6. Block the role on this server. A block subtracts the whole family, so
	//    what it had is gone.
	afterBlock := save(
		&gen.SetResourceAudienceEntry{PrincipalUrn: role.String(), Level: "blocked"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: me.String(), Level: "manage"},
	)
	require.NotNil(t, rowFor(afterBlock, role, "blocked"))
	require.False(t, allows(role, authz.MCPToolCallDimensions{Tool: "search", Disposition: "read_only"}),
		"a block takes away what the earlier rule gave")

	// 7. Take the role's rule away entirely, leaving the person's.
	afterRemoval := save(
		&gen.SetResourceAudienceEntry{PrincipalUrn: me.String(), Level: "manage"},
	)
	require.Nil(t, rowFor(afterRemoval, role, "blocked"))
	require.Nil(t, rowFor(afterRemoval, role, "use"))
	require.NotNil(t, rowFor(afterRemoval, me, "manage"), "the other rule is untouched")

	// 8. The page's own list is what a save replaces: organization-wide rules
	//    survive every step above.
	inherited := 0
	for _, entry := range afterRemoval.Entries {
		if entry.AppliesTo == audienceAppliesToAllResources {
			inherited++
		}
	}
	require.Equal(t, len(start.Entries), inherited,
		"the rules this surface does not own are still there")
}

// A block can be narrowed too, which is the only way this surface can take
// something away: grants add, so a narrower allow cannot undo a broader one,
// but a block naming an annotation subtracts exactly that.
func TestResourceAudience_NarrowedBlockSubtractsByAnnotation(t *testing.T) {
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
	role := seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "support")

	allows := func(dims authz.MCPToolCallDimensions) bool {
		t.Helper()
		grants, err := authz.LoadGrants(ctx, ti.conn, authCtx.ActiveOrganizationID, []urn.Principal{role})
		require.NoError(t, err)
		allowed, err := authz.GrantsAuthorize(grants, authz.MCPToolCallCheck(serverID, dims))
		require.NoError(t, err)
		return allowed
	}

	version := func() string {
		t.Helper()
		result, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{
			ResourceKind: "mcp",
			ResourceID:   serverID,
			SessionToken: nil,
			ApikeyToken:  nil,
		})
		require.NoError(t, err)
		return result.Version
	}

	// Everything, then everything except the destructive tools.
	_, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		Entries: []*gen.SetResourceAudienceEntry{
			{PrincipalUrn: role.String(), Level: "use"},
			{PrincipalUrn: role.String(), Level: "blocked", Dispositions: []string{"destructive"}},
		},
		ExpectedVersion: version(),
		SessionToken:    nil,
		ApikeyToken:     nil,
	})
	require.NoError(t, err)

	require.True(t, allows(authz.MCPToolCallDimensions{Tool: "search", Disposition: "read_only"}),
		"the allow still covers the rest of the server")
	require.False(t, allows(authz.MCPToolCallDimensions{Tool: "purge", Disposition: "destructive"}),
		"the narrowed block subtracts the annotation it names")
}

// A gateway and a remote server are addressed by different ids than a
// toolset-backed one, and all three have to resolve to their project or the
// Access tab cannot open at all.
func TestResourceAudience_ReachesEveryKindOfServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	for name, resourceID := range map[string]string{
		"toolset-backed": seedMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		"remote":         seedRemoteMCPServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
		"gateway":        seedMCPGateway(t, ctx, ti.conn, authCtx.ActiveOrganizationID),
	} {
		t.Run(name, func(t *testing.T) {
			me := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

			before, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{
				ResourceKind: "mcp",
				ResourceID:   resourceID,
				SessionToken: nil,
				ApikeyToken:  nil,
			})
			require.NoError(t, err)

			after, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
				ResourceKind:    "mcp",
				ResourceID:      resourceID,
				Entries:         []*gen.SetResourceAudienceEntry{{PrincipalUrn: me.String(), Level: "use"}},
				ExpectedVersion: before.Version,
				SessionToken:    nil,
				ApikeyToken:     nil,
			})
			require.NoError(t, err)

			var granted bool
			for _, entry := range after.Entries {
				if entry.PrincipalUrn == me.String() && entry.AppliesTo == audienceAppliesToResource {
					granted = true
				}
			}
			require.True(t, granted, "the rule names this server")
		})
	}
}

// A server whose project has been deleted is not administrable, and neither is
// one belonging to another organization.
func TestResourceAudience_RefusesServersOutsideTheOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	elsewhere := seedMCPServer(t, ctx, ti.conn, "org_"+uuid.New().String())

	_, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   elsewhere,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err, "another organization's server is not found here")

	_, err = ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{
		ResourceKind: "mcp",
		ResourceID:   uuid.New().String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err, "and neither is an id that names nothing")
}
