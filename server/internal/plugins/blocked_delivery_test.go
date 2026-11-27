package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestResolveDeliveryPrincipals_RefusesBlockedPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	// A blocked person who is no longer a member: only their email is known.
	users := usersrepo.New(ti.conn)
	_, err := users.UpsertUser(ctx, usersrepo.UpsertUserParams{ID: "blocked-former-member", Email: "former@example.com", DisplayName: "Former", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false})
	require.NoError(t, err)

	_, err = plugins.ResolveDeliveryPrincipals(ctx, ti.conn, orgID, "former@example.com", "")
	require.NoError(t, err)

	require.NoError(t, users.SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{Blacklisted: true, ID: "blocked-former-member"}))
	userblock.Forget(ti.conn)

	_, err = plugins.ResolveDeliveryPrincipals(ctx, ti.conn, orgID, "Former@Example.com", "")
	require.ErrorIs(t, err, plugins.ErrDeliveryUserBlocked, "blocked by email even without a linked member")

	_, err = plugins.ResolveDeliveryPrincipals(ctx, ti.conn, orgID, "someone-else@example.com", "blocked-former-member")
	require.ErrorIs(t, err, plugins.ErrDeliveryUserBlocked, "blocked by user ID")
}
