package email

type CustomDomainUnhealthy struct {
	Email        string
	Domain       string
	IssueMessage string
	DomainLink   string
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
