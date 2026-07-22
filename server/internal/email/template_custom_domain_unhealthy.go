package email

// CustomDomainUnhealthy is sent to an organization's admins when a health
// check first observes the organization's custom domain as unhealthy. It is
// not re-sent while the domain stays unhealthy; the next transition from
// healthy to unhealthy arms it again.
type CustomDomainUnhealthy struct {
	// Email is the recipient's email address, rendered in the template body.
	Email string
	// Domain is the custom domain that failed its health check.
	Domain string
	// IssueMessage is a human-readable description of the detected problem.
	IssueMessage string
	// DomainLink is the dashboard URL of the organization's custom domain
	// settings page, where the check can be reviewed and re-run.
	DomainLink string
}

func (t CustomDomainUnhealthy) TransactionalID() TransactionalID {
	return transactionalIDCustomDomainUnhealthy
}

func (t CustomDomainUnhealthy) AddToAudience() bool { return false }

func (t CustomDomainUnhealthy) Variables() map[string]string {
	return map[string]string{
		"email":         t.Email,
		"domain":        t.Domain,
		"issue_message": t.IssueMessage,
		"domain_link":   t.DomainLink,
	}
}
