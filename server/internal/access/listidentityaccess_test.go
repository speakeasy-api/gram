package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListIdentityAccess_DirectGrantOutranksRoleBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	lockedServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	otherLockedServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	openServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_staff", "Staff", "staff", "Directory-synced staff"))
	rolePrincipal := seededRolePrincipal(t, ctx, ti.conn, orgID, "staff")
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeMCPConnect, authz.WildcardResource)
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeMCPBlockedConnect, lockedServer)
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeMCPBlockedConnect, otherLockedServer)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_staff_admin", "staff-admin@test.com", "Staff Admin", "workos_staff_admin", "membership_staff_admin")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_staff_admin", mockMember("", "membership_staff_admin", "workos_staff_admin", "staff"))
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_staff_admin")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPConnect, lockedServer)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_staff_admin", SessionToken: nil})
	require.NoError(t, err)

	serverIDs := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		serverIDs = append(serverIDs, server.ID)
	}
	require.ElementsMatch(t, []string{lockedServer, openServer}, serverIDs, "the direct grant outranks the role's block on its own server only")
}

func TestService_ListIdentityAccess_OwnBlockOutranksOwnGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	server := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_blocked", "blocked@test.com", "Blocked", "workos_blocked", "membership_blocked")
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_blocked")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPConnect, server)
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPBlockedConnect, server)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_blocked", SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, result.Servers)
}
