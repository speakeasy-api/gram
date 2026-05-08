package email

// TeamInvite is sent to a recipient who has been invited to join a Gram
// organization. The fields below mirror the merge variables the Loops
// template expects.
type TeamInvite struct {
	// InviteLink is the absolute URL the recipient clicks to accept the
	// invite (e.g. https://app.gram.sh/invite?token=...).
	InviteLink string
	// InviterName is the first name (or display name) of the person who sent
	// the invite. Rendered in the greeting line.
	InviterName string
	// InviterEmail is the email address of the person who sent the invite.
	InviterEmail string
	// OrganizationName is the human-readable name of the organization the
	// recipient is being invited to.
	OrganizationName string
}

func (TeamInvite) TransactionalID() TransactionalID {
	return transactionalIDTeamInvite
}

func (t TeamInvite) Variables() map[string]string {
	return map[string]string{
		"invite_link":       t.InviteLink,
		"inviter_name":      t.InviterName,
		"inviter_email":     t.InviterEmail,
		"organization_name": t.OrganizationName,
	}
}

func (TeamInvite) AddToAudience() bool {
	return true
}
