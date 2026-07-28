package email

// AccessRequestCreated is sent to every org admin when a new access request
// is submitted for the first time (unique fingerprint).
type AccessRequestCreated struct {
	// RequesterEmail is the email of the user requesting access.
	RequesterEmail string
	// DisplayName is the human-readable name of the requested resource.
	DisplayName string
	// ApprovalURL is a deep link to the approval queue in the dashboard.
	ApprovalURL string
}

func (t AccessRequestCreated) TransactionalID() TransactionalID {
	return transactionalIDAccessRequestCreated
}

func (t AccessRequestCreated) AddToAudience() bool { return false }

func (t AccessRequestCreated) Variables() map[string]string {
	return map[string]string{
		"requester_email": t.RequesterEmail,
		"display_name":    t.DisplayName,
		"approval_url":    t.ApprovalURL,
	}
}
