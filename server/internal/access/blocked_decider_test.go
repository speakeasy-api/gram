package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAccessRequest_BlockedAdminCannotDecide(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	request := requestChatRead(t, ctx, ti).Request

	require.NoError(t, usersrepo.New(ti.conn).SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{
		Blacklisted: true,
		ID:          accessRequestAdminID,
	}))

	_, err := decideAs(ctx, ti, orgID, request.ID, accessRequestAdminID, true)
	require.ErrorIs(t, err, ErrAccessRequestNotAdmin, "a blocked admin gets the same refusal as a non-admin")
}
