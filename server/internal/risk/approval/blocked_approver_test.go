package approval

import (
	"testing"

	"github.com/stretchr/testify/require"

	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestDecideRefusesBlockedApprover(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	token := f.hold(t, f.call(`{}`))
	id := f.approvalID(t, token)
	f.request(t, token)

	require.NoError(t, usersrepo.New(f.db).SetUserBlacklisted(t.Context(), usersrepo.SetUserBlacklistedParams{
		Blacklisted: true,
		ID:          f.approverID,
	}))

	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: id, Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.ErrorIs(t, err, ErrNotApprover, "a blocked approver gets the same refusal as a non-approver")
}
